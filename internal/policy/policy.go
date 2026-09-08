package policy

import (
	"fmt"
	"os"
	"strings"
	"unicode/utf8"

	"github.com/xdlc-labs/airlock/internal/stats"
	"gopkg.in/yaml.v3"
)

type VerdictKind string

const (
	Pass          VerdictKind = "PASS"
	Fail          VerdictKind = "FAIL"
	Inconclusive  VerdictKind = "INCONCLUSIVE"
	NeedsApproval VerdictKind = "NEEDS_APPROVAL"
	// Skipped marks a comparative gate (regression / new-critical delta) that
	// has no baseline result to pair against. It never merges into Overall —
	// the point is only to keep the gate visible in the report instead of it
	// vanishing with zero trace, the way it did before this existed.
	Skipped VerdictKind = "SKIPPED"
)

type Policy struct {
	Version        int                 `yaml:"version"`
	FailOnAIChange bool                `yaml:"fail_on_ai_change"`
	Gates          map[string]GateSpec `yaml:"gates"`
	Budgets        Budgets             `yaml:"budgets"`
	DataBoundary   DataBoundary        `yaml:"data_boundary"`
}

// DataBoundary fails a release when PII/secret patterns appear in model I/O.
type DataBoundary struct {
	FailOnPII bool `yaml:"fail_on_pii"`
}

type GateSpec struct {
	Min             *float64 `yaml:"min"`
	Confidence      float64  `yaml:"confidence"`
	MaxRegressionPP *float64 `yaml:"max_regression_pp"`
	MaxNewCritical  *int     `yaml:"max_new_critical"` // comparative: cand criticals - base criticals
}

type Budgets struct {
	MaxCostPerPR      float64 `yaml:"max_cost_per_pr"`
	MaxSamplesPerCase int     `yaml:"max_samples_per_case"`
}

type MetricEvidence struct {
	Name    string          `json:"name"`
	CI      stats.Interval  `json:"ci"`
	Delta   *stats.Interval `json:"delta,omitempty"`
	Verdict VerdictKind     `json:"verdict"`
	Reason  string          `json:"reason"`
}

type Report struct {
	Overall VerdictKind      `json:"overall"`
	Metrics []MetricEvidence `json:"metrics"`
	Summary string           `json:"summary"`
}

func Default() Policy {
	minTool := 0.99
	minJSON := 0.995
	maxReg := 1.0
	maxNewCrit := 0
	return Policy{
		Version: 1,
		Gates: map[string]GateSpec{
			"tool_success":         {Min: &minTool, Confidence: 0.95},
			"json_valid":           {Min: &minJSON, Confidence: 0.95},
			"task_success":         {MaxRegressionPP: &maxReg, Confidence: 0.95},
			"adversarial_critical": {MaxNewCritical: &maxNewCrit, Confidence: 0.95},
		},
		Budgets: Budgets{MaxCostPerPR: 2.0, MaxSamplesPerCase: 5},
	}
}

func Load(path string) (Policy, error) {
	p := Default()
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return p, nil
		}
		return p, err
	}
	if err := yaml.Unmarshal(data, &p); err != nil {
		return p, err
	}
	if p.Gates == nil {
		p.Gates = Default().Gates
	}
	return p, nil
}

// MetricRates holds observed success counts for candidate (and optional baseline pairing).
type MetricRates struct {
	Name      string
	Successes int
	N         int
	BasePass  []bool // per paired sample; optional
	CandPass  []bool
}

func Evaluate(p Policy, metrics []MetricRates) Report {
	var out Report
	overall := Pass
	for _, m := range metrics {
		spec, ok := p.Gates[m.Name]
		if !ok {
			continue
		}
		hasMin := spec.Min != nil
		hasBaselinePairs := len(m.BasePass) > 0 && len(m.CandPass) > 0
		comparativeConfigured := spec.MaxRegressionPP != nil || spec.MaxNewCritical != nil
		hasReg := spec.MaxRegressionPP != nil && hasBaselinePairs
		hasCrit := spec.MaxNewCritical != nil && hasBaselinePairs
		if !hasMin && !hasReg && !hasCrit {
			if comparativeConfigured && !hasBaselinePairs {
				// A gate configured with only MaxRegressionPP/MaxNewCritical (no
				// Min) has nothing else to evaluate without a baseline — it used
				// to just vanish from the report here with zero trace. Surface it
				// as SKIPPED instead so "no baseline yet" is visible, not silent.
				out.Metrics = append(out.Metrics, MetricEvidence{
					Name:    m.Name,
					Verdict: Skipped,
					Reason:  "no baseline result to compare against — run `airlock baseline create` (or pass --base) to enable this gate",
				})
			}
			continue
		}
		level := spec.Confidence
		if level <= 0 {
			level = 0.95
		}
		ci := stats.WilsonCI(m.Successes, m.N, level)
		ev := MetricEvidence{Name: m.Name, CI: ci, Verdict: Inconclusive}

		if hasMin {
			switch {
			case stats.PassesMin(ci, *spec.Min):
				ev.Verdict = Pass
				ev.Reason = fmt.Sprintf("CI low %.4f >= min %.4f", ci.Low, *spec.Min)
			case stats.FailsMin(ci, *spec.Min):
				ev.Verdict = Fail
				ev.Reason = fmt.Sprintf("CI high %.4f < min %.4f", ci.High, *spec.Min)
			default:
				ev.Verdict = Inconclusive
				ev.Reason = fmt.Sprintf("CI [%.4f, %.4f] straddles min %.4f", ci.Low, ci.High, *spec.Min)
				// Without this, a gate whose min is unreachable at the current
				// sample budget just reports INCONCLUSIVE every run with no
				// hint that no amount of retrying will settle it.
				if need := stats.SamplesToClearMin(*spec.Min, level); need > m.N {
					ev.Reason += fmt.Sprintf("; a flawless run needs >= %d samples to clear this min, have %d", need, m.N)
				}
			}
		}

		if hasCrit {
			baseCrit, candCrit := 0, 0
			n := min(len(m.BasePass), len(m.CandPass))
			for i := 0; i < n; i++ {
				if !m.BasePass[i] {
					baseCrit++
				}
				if !m.CandPass[i] {
					candCrit++
				}
			}
			delta := candCrit - baseCrit
			if delta > *spec.MaxNewCritical {
				ev.Verdict = Fail
				ev.Reason = fmt.Sprintf("new critical findings %+d (cand %d base %d, max %d)", delta, candCrit, baseCrit, *spec.MaxNewCritical)
			} else if ev.Verdict != Fail {
				ev.Verdict = Pass
				ev.Reason = fmt.Sprintf("no new criticals (delta %+d <= %d)", delta, *spec.MaxNewCritical)
			}
		}

		if hasReg {
			delta := stats.BootstrapPairedDeltaCI(m.BasePass, m.CandPass, level, 999, 42)
			ev.Delta = &delta
			switch {
			case stats.FailsMaxRegression(delta, *spec.MaxRegressionPP):
				ev.Verdict = Fail
				ev.Reason = fmt.Sprintf("regression CI high %.4f below -%.2fpp", delta.High, *spec.MaxRegressionPP)
			case stats.PassesMaxRegression(delta, *spec.MaxRegressionPP):
				if ev.Verdict != Fail {
					ev.Verdict = Pass
					ev.Reason = fmt.Sprintf("no significant regression (delta CI [%.4f, %.4f])", delta.Low, delta.High)
				}
			default:
				if ev.Verdict == Pass || !hasMin {
					ev.Verdict = Inconclusive
				}
				ev.Reason = fmt.Sprintf("regression inconclusive (delta CI [%.4f, %.4f])", delta.Low, delta.High)
			}
		}

		if comparativeConfigured && !hasBaselinePairs {
			// Min gate already produced a row above, but its regression/critical-
			// delta half was quietly not evaluated — say so instead of leaving
			// that half invisible inside an otherwise-normal PASS/FAIL row.
			ev.Reason += " (no baseline: regression/critical-delta check skipped)"
		}
		out.Metrics = append(out.Metrics, ev)
		overall = merge(overall, ev.Verdict)
	}
	if len(out.Metrics) == 0 {
		overall = Pass
		out.Summary = "no gated metrics"
	} else {
		out.Summary = string(overall)
	}
	out.Overall = overall
	return out
}

func merge(a, b VerdictKind) VerdictKind {
	rank := map[VerdictKind]int{Pass: 0, NeedsApproval: 1, Inconclusive: 2, Fail: 3}
	if rank[b] > rank[a] {
		return b
	}
	return a
}

// reasonWrap is the column budget for a gate's reason text. Reasons carry
// remediation hints and can run long; the surrounding box grows to its widest
// line, so an unwrapped reason pushed the eval table past 160 columns and broke
// it in an ordinary terminal.
const reasonWrap = 62

func FormatTable(r Report) string {
	if len(r.Metrics) == 0 {
		return ""
	}
	s := fmt.Sprintf("%-16s %8s %18s  %s\n", "metric", "rate", "95% CI", "gate")
	for _, m := range r.Metrics {
		// A SKIPPED gate never ran, so it has no rate. Printing its zero value
		// rendered as "0.0% [0.0%, 0.0%]", which reads like a total failure
		// rather than a gate awaiting a baseline.
		if m.Verdict == Skipped {
			s += fmt.Sprintf("%-16s %8s %18s  %s\n", m.Name, "-", "-", m.Verdict)
		} else {
			s += fmt.Sprintf("%-16s %6.1f%%  [%5.1f%%, %5.1f%%]  %s\n",
				m.Name, m.CI.Estimate*100, m.CI.Low*100, m.CI.High*100, m.Verdict)
		}
		for _, ln := range wrapWords(m.Reason, reasonWrap) {
			s += "    " + ln + "\n"
		}
	}
	return s
}

// wrapWords greedily wraps on spaces, measuring in runes so that multi-byte
// characters do not throw off the enclosing box's padding. A single word longer
// than the budget is left intact rather than split, so hashes and flags stay
// copy-pasteable.
func wrapWords(text string, width int) []string {
	words := strings.Fields(text)
	if len(words) == 0 {
		return nil
	}
	var (
		lines []string
		cur   string
	)
	for _, w := range words {
		switch {
		case cur == "":
			cur = w
		case utf8.RuneCountInString(cur)+1+utf8.RuneCountInString(w) <= width:
			cur += " " + w
		default:
			lines = append(lines, cur)
			cur = w
		}
	}
	return append(lines, cur)
}

func FormatMarkdown(r Report) string {
	if len(r.Metrics) == 0 {
		return ""
	}
	s := "\n### Eval\n\n"
	s += "| metric | rate | 95% CI | gate | reason |\n|---|---:|---|---|---|\n"
	for _, m := range r.Metrics {
		if m.Verdict == Skipped {
			s += fmt.Sprintf("| `%s` | - | - | **%s** | %s |\n", m.Name, m.Verdict, m.Reason)
			continue
		}
		s += fmt.Sprintf("| `%s` | %.1f%% | [%.1f%%, %.1f%%] | **%s** | %s |\n",
			m.Name, m.CI.Estimate*100, m.CI.Low*100, m.CI.High*100, m.Verdict, m.Reason)
	}
	return s
}

// WithNeedsApproval upgrades overall verdict when static permission expansion requires human review.
func WithNeedsApproval(r Report, needs bool, reasons []string) Report {
	if !needs {
		return r
	}
	prevOverall := r.Overall
	r.Overall = merge(r.Overall, NeedsApproval)
	reason := "needs_approval"
	if len(reasons) > 0 {
		reason = "needs_approval: " + reasons[0]
	}
	if prevOverall == Fail {
		// Fail already dominates the verdict (merge rank keeps it on top) — don't
		// clobber the reason a reviewer actually needs with the approval note.
		if r.Summary != "" {
			r.Summary += "; " + reason
		} else {
			r.Summary = reason
		}
	} else {
		r.Summary = reason
	}
	return r
}

// WithJudgeFloor fails when a calibration report is below its kappa floor (labels were present).
func WithJudgeFloor(r Report, judgeID string, floorOK bool, kappa float64, n int) Report {
	if n == 0 || floorOK {
		return r
	}
	ev := MetricEvidence{
		Name:    "judge:" + judgeID,
		Verdict: Fail,
		Reason:  fmt.Sprintf("kappa %.3f below floor (n=%d)", kappa, n),
	}
	r.Metrics = append(r.Metrics, ev)
	r.Overall = merge(r.Overall, Fail)
	r.Summary = ev.Reason
	return r
}

// WithDataBoundary fails when sensitive patterns were found in model I/O.
func WithDataBoundary(r Report, enabled bool, findings int, example string) Report {
	if !enabled || findings == 0 {
		return r
	}
	reason := fmt.Sprintf("data_boundary: %d sensitive hit(s)", findings)
	if example != "" {
		reason += " e.g. " + example
	}
	ev := MetricEvidence{Name: "data_boundary", Verdict: Fail, Reason: reason}
	r.Metrics = append(r.Metrics, ev)
	r.Overall = merge(r.Overall, Fail)
	r.Summary = reason
	return r
}
