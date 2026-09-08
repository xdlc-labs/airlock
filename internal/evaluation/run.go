package evaluation

import (
	"cmp"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/xdlc-labs/airlock/internal/boundary"
	"github.com/xdlc-labs/airlock/internal/evalcase"
	"github.com/xdlc-labs/airlock/internal/policy"
	"github.com/xdlc-labs/airlock/internal/providers"
	"github.com/xdlc-labs/airlock/internal/stats"
	"golang.org/x/sync/errgroup"
	"golang.org/x/sync/semaphore"
)

type SampleResult struct {
	CaseID    string             `json:"case_id"`
	SampleIdx int                `json:"sample_idx"`
	Response  providers.Response `json:"response"`
	Scores    map[string]bool    `json:"scores"`
	CostUSD   float64            `json:"cost_usd,omitempty"`
	Err       string             `json:"error,omitempty"`
}

type RunResult struct {
	SnapshotID    string               `json:"snapshot_id,omitempty"`
	Samples       []SampleResult       `json:"samples"`
	Rates         []policy.MetricRates `json:"rates"`
	Report        policy.Report        `json:"report"`
	TotalCostUSD  float64              `json:"total_cost_usd"`
	BudgetStopped bool                 `json:"budget_stopped,omitempty"`
}

type Config struct {
	Suite      evalcase.Suite
	Policy     policy.Policy
	Provider   providers.Provider
	Client     providers.HTTPDoer
	MaxWorkers int64
	Baseline   map[string][]bool // metric -> flattened pass vector
	SnapshotID string
	JudgeScore func(ctx context.Context, c evalcase.Case, resp providers.Response) (bool, error)
}

// BaselineFromResult flattens a prior RunResult into metric pass vectors (case order preserved by case_id then sample_idx).
func BaselineFromResult(res *RunResult) map[string][]bool {
	if res == nil {
		return nil
	}
	samples := slices.Clone(res.Samples)
	slices.SortFunc(samples, func(a, b SampleResult) int {
		if c := cmp.Compare(a.CaseID, b.CaseID); c != 0 {
			return c
		}
		return cmp.Compare(a.SampleIdx, b.SampleIdx)
	})
	out := make(map[string][]bool)
	for _, s := range samples {
		for m, pass := range s.Scores {
			out[m] = append(out[m], pass)
		}
	}
	return out
}

func LoadResult(path string) (*RunResult, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var r RunResult
	if err := json.Unmarshal(data, &r); err != nil {
		return nil, err
	}
	return &r, nil
}

func SaveResult(dir, snapshotID string, res *RunResult) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	res.SnapshotID = snapshotID
	data, err := json.MarshalIndent(res, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	if snapshotID != "" {
		if err := os.WriteFile(filepath.Join(dir, snapshotID+".json"), data, 0o644); err != nil {
			return err
		}
	}
	return os.WriteFile(filepath.Join(dir, "latest.json"), data, 0o644)
}

func FindBaseline(resultsDir, baseSnapshotID string) (*RunResult, error) {
	cands := []string{}
	if baseSnapshotID != "" {
		cands = append(cands, filepath.Join(resultsDir, baseSnapshotID+".json"))
	}
	cands = append(cands, filepath.Join(resultsDir, "latest.json"))
	for _, c := range cands {
		r, err := LoadResult(c)
		if err == nil {
			return r, nil
		}
	}
	return nil, os.ErrNotExist
}

// Run executes cases with k samples; early-stops when gated metrics are decisive.
// ponytail: early stop = aggregate Wilson vs threshold after min_samples; not full mSPRT.
func Run(ctx context.Context, cases []evalcase.Case, cfg Config) (*RunResult, error) {
	if cfg.MaxWorkers <= 0 {
		cfg.MaxWorkers = 8
	}
	k := cfg.Suite.MaxSamplesPerCase
	if k <= 0 {
		k = cfg.Suite.K
	}
	minS := cfg.Suite.MinSamples
	if minS <= 0 {
		minS = 1
	}
	budget := cfg.Policy.Budgets.MaxCostPerPR
	if cfg.Suite.MaxCostUSD > 0 && (budget <= 0 || cfg.Suite.MaxCostUSD < budget) {
		budget = cfg.Suite.MaxCostUSD
	}

	var mu sync.Mutex
	var samples []SampleResult
	var totalCost float64
	var budgetStopped atomic.Bool
	type agg struct {
		ok, n  int
		byCase map[string][]bool
	}
	aggs := map[string]*agg{}

	sem := semaphore.NewWeighted(cfg.MaxWorkers)
	g, ctx := errgroup.WithContext(ctx)

	for _, c := range cases {
		g.Go(func() error {
			for i := 0; i < k; i++ {
				if budgetStopped.Load() {
					return nil
				}
				if err := sem.Acquire(ctx, 1); err != nil {
					return err
				}
				if budgetStopped.Load() {
					sem.Release(1)
					return nil
				}
				sr := runOne(ctx, c, i, cfg)
				sem.Release(1)

				mu.Lock()
				samples = append(samples, sr)
				totalCost += sr.CostUSD
				if budget > 0 && totalCost >= budget {
					budgetStopped.Store(true)
				}
				for metric, pass := range sr.Scores {
					a := aggs[metric]
					if a == nil {
						a = &agg{byCase: map[string][]bool{}}
						aggs[metric] = a
					}
					a.n++
					if pass {
						a.ok++
					}
					a.byCase[c.ID] = append(a.byCase[c.ID], pass)
				}
				decisive := i+1 >= minS && !budgetStopped.Load()
				if decisive {
					for name, spec := range cfg.Policy.Gates {
						a := aggs[name]
						if a == nil || a.n < minS {
							decisive = false
							break
						}
						level := spec.Confidence
						if level <= 0 {
							level = 0.95
						}
						ci := stats.WilsonCI(a.ok, a.n, level)
						if spec.Min != nil {
							if !stats.PassesMin(ci, *spec.Min) && !stats.FailsMin(ci, *spec.Min) {
								decisive = false
								break
							}
						}
					}
				}
				stop := decisive || budgetStopped.Load()
				mu.Unlock()
				if stop {
					break
				}
			}
			return nil
		})
	}
	if err := g.Wait(); err != nil {
		return nil, err
	}

	var rates []policy.MetricRates
	for name, a := range aggs {
		mr := policy.MetricRates{Name: name, Successes: a.ok, N: a.n}
		if base, ok := cfg.Baseline[name]; ok {
			var cand []bool
			for _, c := range cases {
				cand = append(cand, a.byCase[c.ID]...)
			}
			n := min(len(base), len(cand))
			mr.BasePass = base[:n]
			mr.CandPass = cand[:n]
		}
		rates = append(rates, mr)
	}
	// aggs is a map, so gate order was whatever Go's iteration gave us. The
	// eval table and the PR comment are both rendered straight from this
	// slice, and a release gate should not produce a different comment for
	// two identical runs.
	slices.SortFunc(rates, func(a, b policy.MetricRates) int {
		return cmp.Compare(a.Name, b.Name)
	})
	rep := policy.Evaluate(cfg.Policy, rates)
	if budgetStopped.Load() {
		rep = forceBudgetInconclusive(rep)
	}
	if cfg.Policy.DataBoundary.FailOnPII {
		n, ex := countBoundaryHits(cases, samples)
		rep = policy.WithDataBoundary(rep, true, n, ex)
	}
	return &RunResult{
		SnapshotID:    cfg.SnapshotID,
		Samples:       samples,
		Rates:         rates,
		Report:        rep,
		TotalCostUSD:  totalCost,
		BudgetStopped: budgetStopped.Load(),
	}, nil
}

func countBoundaryHits(cases []evalcase.Case, samples []SampleResult) (int, string) {
	byID := map[string]evalcase.Case{}
	for _, c := range cases {
		byID[c.ID] = c
	}
	n := 0
	example := ""
	for _, s := range samples {
		c := byID[s.CaseID]
		var buf strings.Builder
		for _, m := range c.Input.Messages {
			buf.WriteString(m.Content)
			buf.WriteByte('\n')
		}
		buf.WriteString(s.Response.Text)
		hits := boundary.Scan(buf.String())
		if len(hits) == 0 {
			continue
		}
		n += len(hits)
		if example == "" {
			example = hits[0].Kind + ":" + hits[0].Snippet
		}
	}
	return n, example
}

func forceBudgetInconclusive(r policy.Report) policy.Report {
	if r.Overall == policy.Fail {
		return r
	}
	r.Overall = policy.Inconclusive
	r.Summary = "budget_exhausted"
	for i := range r.Metrics {
		if r.Metrics[i].Verdict == policy.Pass || r.Metrics[i].Verdict == policy.Inconclusive {
			r.Metrics[i].Verdict = policy.Inconclusive
			r.Metrics[i].Reason = "budget_exhausted: " + r.Metrics[i].Reason
		}
	}
	return r
}

func runOne(ctx context.Context, c evalcase.Case, idx int, cfg Config) SampleResult {
	sr := SampleResult{CaseID: c.ID, SampleIdx: idx, Scores: map[string]bool{}}
	pname := c.Input.Provider
	if pname == "" {
		pname = "mock"
	}
	p, err := providers.Resolve(pname, cfg.Client)
	if err != nil {
		sr.Err = err.Error()
		return sr
	}
	req := providers.Request{
		Provider: pname,
		Model:    c.Input.Model,
		Messages: toMsgs(c.Input.Messages),
		Tools:    toTools(c.Input.Tools),
	}
	if cfg.Suite.Seed != 0 {
		seed := cfg.Suite.Seed + int64(idx)
		req.Seed = &seed
	}
	resp, err := p.Generate(ctx, req)
	if err != nil {
		sr.Err = err.Error()
		return sr
	}
	sr.Response = resp
	sr.CostUSD = resp.CostUSD
	sr.Scores = Score(c, resp)
	if c.Expect.Judge != "" && cfg.JudgeScore != nil {
		ok, jerr := cfg.JudgeScore(ctx, c, resp)
		if jerr != nil {
			sr.Err = jerr.Error()
			sr.Scores["judge"] = false
		} else {
			sr.Scores["judge"] = ok
			if !ok {
				sr.Scores["task_success"] = false
			}
		}
	}
	return sr
}

func toMsgs(in []evalcase.Message) []providers.Message {
	out := make([]providers.Message, 0, len(in))
	for _, m := range in {
		out = append(out, providers.Message{Role: m.Role, Content: m.Content})
	}
	return out
}

func toTools(in []evalcase.ToolDef) []providers.Tool {
	out := make([]providers.Tool, 0, len(in))
	for _, t := range in {
		out = append(out, providers.Tool{Name: t.Name, Description: t.Description, Parameters: t.Parameters})
	}
	return out
}

func Score(c evalcase.Case, resp providers.Response) map[string]bool {
	out := map[string]bool{}
	e := c.Expect
	taskOK := true
	matched := false

	if e.ExactMatch != "" {
		matched = true
		ok := resp.Text == e.ExactMatch
		out["exact_match"] = ok
		taskOK = taskOK && ok
	}
	if e.Contains != "" {
		matched = true
		ok := strings.Contains(resp.Text, e.Contains)
		out["contains"] = ok
		taskOK = taskOK && ok
	}
	if e.NotContains != "" {
		matched = true
		ok := !strings.Contains(resp.Text, e.NotContains)
		out["adversarial_critical"] = ok
		taskOK = taskOK && ok
	}
	if e.Regex != "" {
		matched = true
		re, err := regexp.Compile(e.Regex)
		ok := err == nil && re.MatchString(resp.Text)
		out["regex"] = ok
		taskOK = taskOK && ok
	}
	if e.JSONValid {
		matched = true
		ok := json.Valid([]byte(resp.Text))
		out["json_valid"] = ok
		taskOK = taskOK && ok
	}
	if e.ToolName != "" || e.ToolArgsSchema != "" {
		matched = true
		toolOK := false
		for _, tc := range resp.ToolCalls {
			if e.ToolName != "" && tc.Name != e.ToolName {
				continue
			}
			schemaName := e.ToolArgsSchema
			if schemaName == "" {
				schemaName = e.ToolName
			}
			argsOK := true
			if e.ToolArgsSchema != "" {
				argsOK = validateToolArgs(c, schemaName, tc)
			}
			nameOK := e.ToolName == "" || tc.Name == e.ToolName
			if nameOK && argsOK {
				toolOK = true
				break
			}
		}
		out["tool_success"] = toolOK
		taskOK = taskOK && toolOK
	}
	if e.TaskSuccess != nil {
		out["task_success"] = *e.TaskSuccess
	} else if matched {
		out["task_success"] = taskOK
	}
	return out
}

func validateToolArgs(c evalcase.Case, toolName string, tc providers.ToolCall) bool {
	if !json.Valid(tc.Args) {
		return false
	}
	var args map[string]any
	if err := json.Unmarshal(tc.Args, &args); err != nil {
		return false
	}
	var schema json.RawMessage
	for _, t := range c.Input.Tools {
		if t.Name == toolName {
			schema = t.Parameters
			break
		}
	}
	if len(schema) == 0 {
		return true
	}
	var sch map[string]any
	if err := json.Unmarshal(schema, &sch); err != nil {
		return false
	}
	req, _ := sch["required"].([]any)
	for _, r := range req {
		key, _ := r.(string)
		if key == "" {
			continue
		}
		if _, ok := args[key]; !ok {
			return false
		}
	}
	return true
}
