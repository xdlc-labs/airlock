package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/xdlc-labs/airlock/internal/approval"
	"github.com/xdlc-labs/airlock/internal/braintrust"
	"github.com/xdlc-labs/airlock/internal/cliargs"
	"github.com/xdlc-labs/airlock/internal/collector"
	"github.com/xdlc-labs/airlock/internal/diff"
	"github.com/xdlc-labs/airlock/internal/discovery"
	"github.com/xdlc-labs/airlock/internal/drift"
	"github.com/xdlc-labs/airlock/internal/evalcase"
	"github.com/xdlc-labs/airlock/internal/evaluation"
	"github.com/xdlc-labs/airlock/internal/history"
	"github.com/xdlc-labs/airlock/internal/judge"
	"github.com/xdlc-labs/airlock/internal/langsmith"
	"github.com/xdlc-labs/airlock/internal/manifest"
	"github.com/xdlc-labs/airlock/internal/out"
	"github.com/xdlc-labs/airlock/internal/policy"
	"github.com/xdlc-labs/airlock/internal/promptfoo"
	"github.com/xdlc-labs/airlock/internal/providers"
	"github.com/xdlc-labs/airlock/internal/replay"
	"github.com/xdlc-labs/airlock/internal/rollback"
	"github.com/xdlc-labs/airlock/internal/sentinel"
	"github.com/xdlc-labs/airlock/internal/snapshot"
	"github.com/xdlc-labs/airlock/internal/store"
)

var version = "dev" // overridden by release builds: -ldflags "-X main.version=…"

func main() {
	if len(os.Args) < 2 {
		usage(os.Stderr)
		os.Exit(2)
	}
	cmd := os.Args[1]
	args := os.Args[2:]
	var err error
	switch cmd {
	case "init":
		err = cmdInit(args)
	case "snapshot":
		err = cmdSnapshot(args)
	case "diff":
		err = cmdDiff(args)
	case "test":
		err = cmdTest(args)
	case "ci":
		err = cmdCI(args)
	case "import":
		err = cmdImport(args)
	case "eval":
		err = cmdEval(args)
	case "ingest":
		err = cmdIngest(args)
	case "baseline":
		err = cmdBaseline(args)
	case "drift":
		err = cmdDrift(args)
	case "history":
		err = cmdHistory(args)
	case "judge":
		err = cmdJudge(args)
	case "approve":
		err = cmdApprove(args)
	case "rollback":
		err = cmdRollback(args)
	case "sentinel":
		err = cmdSentinel(args)
	case "version", "-v", "-version", "--version":
		fmt.Printf("airlock %s\n", version)
	case "help", "-h", "--help":
		usage(os.Stdout)
	default:
		fmt.Fprintf(os.Stderr, "unknown command: %s\n\n", cmd)
		usage(os.Stderr)
		os.Exit(2)
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: airlock %s: %v\n", cmd, err)
		os.Exit(1)
	}
}

func usage(w *os.File) {
	fmt.Fprintf(w, `airlock  CI gate for prompts, skills, MCP, and models

Usage:
  airlock <command> [flags]

Commands:
  init        Discover AI artifacts into .airlock/
  snapshot    Freeze a content-addressed snapshot
  diff        Show changes vs a base snapshot
  test        Run the eval suite
  ci          Diff, evals, approval gate, write PR comment
  import      Import promptfoo, langsmith, or braintrust cases
  eval        Promote ingest or results into eval cases
  ingest      Ingest OTel GenAI JSONL (local redaction)
  baseline    Promote ingest into .airlock/evals/prod.jsonl
  drift       Compare live ingest vs baseline
  history     Local release history (optional --serve :8787)
  judge       Calibrate judges or measure attribution
  approve     Record human approval for permission expansion
  rollback    Re-pin a known-good snapshot
  sentinel    Fingerprint upstream models
  version     Print version
  help        Show this help

test flags: --path --suite --affected --mode --json --baseline-results ID --adversarial
ci flags:   --fail-on-change --fail-on-eval --fail-on-inconclusive --fail-on-approval --fail-on-sentinel --comment --skip-eval --adversarial
redact:     --redact pii|hash|off (ingest / baseline)

Local-first. Nothing uploads.
`)
}

func rootFromArgs(args []string) (string, []string, error) {
	root, rest, err := cliargs.List(args).Path()
	return root, []string(rest), err
}

func flagBool(args []string, name string) ([]string, bool) {
	a := cliargs.List(args)
	ok := a.Bool(name)
	return []string(a), ok
}

func flagVal(args []string, name string) ([]string, string) {
	a := cliargs.List(args)
	v := a.Val(name)
	return []string(a), v
}

func cmdInit(args []string) error {
	root, _, err := rootFromArgs(args)
	if err != nil {
		return err
	}
	p := store.ForRoot(root)
	if err := p.Ensure(); err != nil {
		return err
	}
	m, err := discovery.Scan(root)
	if err != nil {
		return err
	}
	if err := store.WriteManifest(p, m); err != nil {
		return err
	}
	if err := store.WritePolicyStub(p); err != nil {
		return err
	}
	_ = evalcase.WriteBindingsStub(evalcase.DefaultBindingsPath(root))
	wrote := p.Manifest
	if rel, err := filepath.Rel(root, p.Manifest); err == nil {
		wrote = rel
	}
	out.Print(out.Frame("airlock init", []string{
		fmt.Sprintf("agents  %-4d  models  %-4d  prompts  %d", len(m.Agents), len(m.Models), len(m.Prompts)),
		fmt.Sprintf("tools   %-4d  skills  %-4d  mcp      %d", len(m.Tools), len(m.Skills), len(m.MCPServers)),
		fmt.Sprintf("evals   %d", len(m.Evals)),
		"",
		"wrote  " + wrote,
	}))
	return nil
}

func cmdSnapshot(args []string) error {
	root, args, err := rootFromArgs(args)
	if err != nil {
		return err
	}
	_, withSentinel := flagBool(args, "--sentinel")
	snap, err := snapshot.Create(root, true)
	if err != nil {
		return err
	}
	if withSentinel {
		if err := runSentinelProbe(root); err != nil {
			return err
		}
		snap, err = snapshot.Create(root, false)
		if err != nil {
			return err
		}
	}
	out.Print(out.Frame("snapshot", []string{
		snap.ID,
		fmt.Sprintf("artifacts  %d    manifest  %s", len(snap.Artifacts), snap.ManifestHash[:min(12, len(snap.ManifestHash))]),
	}))
	return nil
}

func loadHead(root, headID string) (*manifest.Snapshot, error) {
	if headID == "" || headID == "working" {
		return snapshot.FromWorkingTree(root)
	}
	return snapshot.Load(root, headID)
}

func cmdDiff(args []string) error {
	root, args, err := rootFromArgs(args)
	if err != nil {
		return err
	}
	args, asJSON := flagBool(args, "--json")
	args, baseID := flagVal(args, "--base")
	_, headID := flagVal(args, "--head")
	if headID == "" {
		headID = "working"
	}
	base, err := snapshot.Load(root, baseID)
	if err != nil {
		return fmt.Errorf("base: %w", err)
	}
	head, err := loadHead(root, headID)
	if err != nil {
		return fmt.Errorf("head: %w", err)
	}
	r := diff.Compare(base, head)
	if asJSON {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(r)
	}
	out.Print(diff.FormatText(r))
	return nil
}

func resolveSuiteDir(root string, override string) (string, error) {
	if override != "" {
		cands := []string{override}
		if !filepath.IsAbs(override) {
			cands = append([]string{filepath.Join(root, override)}, cands...)
		}
		for _, c := range cands {
			st, err := os.Stat(c)
			if err != nil {
				continue
			}
			if st.IsDir() {
				return c, nil
			}
			return c, nil
		}
		return "", fmt.Errorf("suite dir not found: %s", override)
	}
	p := store.ForRoot(root)
	for _, c := range []string{p.Evals, filepath.Join(root, ".airlock-evals")} {
		if _, err := os.Stat(filepath.Join(c, "suite.yml")); err == nil {
			return c, nil
		}
		if _, err := os.Stat(filepath.Join(c, "default.jsonl")); err == nil {
			return c, nil
		}
	}
	return p.Evals, fmt.Errorf("no eval suite found; run airlock import or baseline create")
}

func buildHTTPClient(root string, suite evalcase.Suite, modeOverride string) (*http.Client, error) {
	mode := suite.Mode
	if modeOverride != "" {
		mode = modeOverride
	}
	if mode == "" || mode == "live" {
		return &http.Client{Timeout: 120 * time.Second}, nil
	}
	p := store.ForRoot(root)
	if err := p.Ensure(); err != nil {
		return nil, err
	}
	st, err := replay.Open(p.Cassettes)
	if err != nil {
		return nil, err
	}
	miss := replay.MissFail
	if suite.ReplayMiss == "live" {
		miss = replay.MissLive
	}
	rt := &replay.Transport{
		Store:   st,
		Mode:    replay.Mode(mode),
		Miss:    miss,
		Wrapped: http.DefaultTransport,
	}
	return rt.Client(), nil
}

func cmdTest(args []string) error {
	root, args, err := rootFromArgs(args)
	if err != nil {
		return err
	}
	args, asJSON := flagBool(args, "--json")
	args, affected := flagBool(args, "--affected")
	args, adversarial := flagBool(args, "--adversarial")
	args, suiteOverride := flagVal(args, "--suite")
	args, mode := flagVal(args, "--mode")
	_, baseResults := flagVal(args, "--baseline-results")

	suiteDir, err := resolveSuiteDir(root, suiteOverride)
	if err != nil {
		return err
	}
	var suite evalcase.Suite
	var cases []evalcase.Case
	var lerr error
	if st, serr := os.Stat(suiteDir); serr == nil && !st.IsDir() {
		suite, cases, lerr = evalcase.LoadSuiteFile(suiteDir)
	} else {
		suite, cases, lerr = evalcase.LoadSuite(suiteDir)
	}
	if lerr != nil {
		return lerr
	}
	if adversarial || suite.Kind == "adversarial" {
		if tagged := evalcase.FilterByTag(cases, "adversarial"); len(tagged) > 0 {
			cases = tagged
		}
	}
	if affected {
		agents, err := affectedAgents(root)
		if err != nil {
			return err
		}
		cases = evalcase.FilterByAgents(cases, agents)
		out.Printf("affected agents: %s (%d cases)\n", strings.Join(agents, ", "), len(cases))
	}
	if len(cases) == 0 {
		return fmt.Errorf("no cases to run")
	}
	pol, err := policy.Load(store.ForRoot(root).Policy)
	if err != nil {
		return err
	}
	client, err := buildHTTPClient(root, suite, mode)
	if err != nil {
		return err
	}
	if mode != "" {
		suite.Mode = mode
	}
	candID := ""
	if snap, err := snapshot.Load(root, ""); err == nil {
		candID = snap.ID
	}
	cfg := evaluation.Config{Suite: suite, Policy: pol, Client: client, SnapshotID: candID}
	var baseRes *evaluation.RunResult
	if base, err := evaluation.FindBaseline(store.ForRoot(root).Results, baseResults); err == nil {
		cfg.Baseline = evaluation.BaselineFromResult(base)
		baseRes = base
		if base.SnapshotID != "" {
			out.Printf("paired baseline: %s\n", base.SnapshotID)
		}
	}
	reg := loadJudges(root, client)
	if len(reg.ByID) > 0 {
		cfg.JudgeScore = judgeHook(reg)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	res, err := evaluation.Run(ctx, cases, cfg)
	if err != nil {
		return err
	}
	res.Report = applyJudgeFloors(res.Report, store.ForRoot(root).Judges, reg)
	_ = evaluation.SaveResult(store.ForRoot(root).Results, candID, res)

	if asJSON {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(res)
	}
	if table := policy.FormatTable(res.Report); table != "" {
		out.Print(out.FrameText("eval", table))
	}
	if baseRes != nil {
		if rows := evaluation.CompareResults(baseRes, res, pol); len(rows) > 0 {
			out.Print(evaluation.FormatCompareText(rows))
		}
	}
	if res.BudgetStopped {
		out.Printf("budget_stopped total_cost=$%.4f\n", res.TotalCostUSD)
	}
	wrote := filepath.Join(store.ForRoot(root).Results, "latest.json")
	if rel, err := filepath.Rel(root, wrote); err == nil {
		wrote = rel
	}
	out.Verdict(string(res.Report.Overall), fmt.Sprintf("samples=%d  cost=$%.4f  wrote  %s", len(res.Samples), res.TotalCostUSD, wrote))
	if res.Report.Overall == policy.Fail {
		return fmt.Errorf("eval verdict FAIL")
	}
	return nil
}

func affectedAgents(root string) ([]string, error) {
	base, err := snapshot.Load(root, "")
	if err != nil {
		return nil, err
	}
	head, err := snapshot.FromWorkingTree(root)
	if err != nil {
		return nil, err
	}
	return diff.Compare(base, head).AffectedAgents, nil
}

// evalGateErr maps an eval Report's overall verdict to a CI exit decision.
// --fail-on-eval alone only trips on FAIL: default thresholds (0.99/0.995 min)
// vs default sample size (max_samples_per_case: 5) can leave a metric
// straddling the min forever, stuck at INCONCLUSIVE with nothing failing CI.
// --fail-on-inconclusive closes that gap (and, since FAIL is strictly worse
// than INCONCLUSIVE, also covers FAIL so enabling it alone still fails closed).
func evalGateErr(report *policy.Report, failEval, failInconclusive bool) error {
	if report == nil {
		return nil
	}
	switch report.Overall {
	case policy.Fail:
		if failEval || failInconclusive {
			return fmt.Errorf("eval verdict FAIL")
		}
	case policy.Inconclusive:
		if failInconclusive {
			return fmt.Errorf("eval verdict INCONCLUSIVE (raise max_samples_per_case or widen the gate to resolve)")
		}
	}
	return nil
}

func cmdCI(args []string) error {
	root, args, err := rootFromArgs(args)
	if err != nil {
		return err
	}
	args, failFlag := flagBool(args, "--fail-on-change")
	args, failEval := flagBool(args, "--fail-on-eval")
	args, failInconclusive := flagBool(args, "--fail-on-inconclusive")
	args, failApproval := flagBool(args, "--fail-on-approval")
	args, failSentinel := flagBool(args, "--fail-on-sentinel")
	args, _ = flagBool(args, "--comment") // kept: comment file is always written
	args, skipEval := flagBool(args, "--skip-eval")
	args, adversarial := flagBool(args, "--adversarial")
	args, baseID := flagVal(args, "--base")
	_, headID := flagVal(args, "--head")
	if headID == "" {
		headID = "working"
	}

	base, err := snapshot.Load(root, baseID)
	if err != nil {
		return fmt.Errorf("base: %w", err)
	}
	head, err := loadHead(root, headID)
	if err != nil {
		return err
	}
	dr := diff.Compare(base, head)
	mcpTouched := diff.HasKind(dr, "mcp")
	skillTouched := diff.HasKind(dr, "skill")
	secSurface := mcpTouched || skillTouched

	var evalMD string
	var evalReport *policy.Report
	var baseRes *evaluation.RunResult
	if !skipEval {
		suite, cases, lerr := resolveCasesForDiff(root, dr, adversarial, secSurface, mcpTouched, skillTouched)
		if lerr == nil && len(cases) > 0 {
			pol, _ := policy.Load(store.ForRoot(root).Policy)
			client, _ := buildHTTPClient(root, suite, suite.Mode)
			cfg := evaluation.Config{Suite: suite, Policy: pol, Client: client, SnapshotID: head.ID}
			if bres, err := evaluation.FindBaseline(store.ForRoot(root).Results, base.ID); err == nil {
				cfg.Baseline = evaluation.BaselineFromResult(bres)
				baseRes = bres
			}
			reg := loadJudges(root, client)
			if len(reg.ByID) > 0 {
				cfg.JudgeScore = judgeHook(reg)
			}
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
			res, rerr := evaluation.Run(ctx, cases, cfg)
			cancel()
			if rerr == nil {
				res.Report = policy.WithNeedsApproval(res.Report, dr.NeedsApproval, dr.ApprovalReasons)
				res.Report = applyJudgeFloors(res.Report, store.ForRoot(root).Judges, reg)
				evalReport = &res.Report
				evalMD = policy.FormatMarkdown(res.Report)
				if baseRes != nil {
					evalMD += evaluation.FormatCompareMarkdown(evaluation.CompareResults(baseRes, res, pol))
				}
				_ = evaluation.SaveResult(store.ForRoot(root).Results, head.ID, res)
			} else {
				evalMD = fmt.Sprintf("\n### Eval\n\n_eval error: %v_\n", rerr)
			}
		}
	}
	if evalReport == nil && dr.NeedsApproval {
		rep := policy.WithNeedsApproval(policy.Report{Overall: policy.Pass}, true, dr.ApprovalReasons)
		evalReport = &rep
		evalMD += policy.FormatMarkdown(rep)
	}

	overall := policy.Pass
	if evalReport != nil {
		overall = evalReport.Overall
	}
	// A pending human gate is a determinate, actionable outcome; an
	// INCONCLUSIVE eval is the absence of one, and it outranks NeedsApproval
	// in merge(). Taking the eval verdict as the headline therefore hid the
	// approval requirement even though that is what blocks the merge, so the
	// comment ended up contradicting its own Unblock section. FAIL still
	// wins, being strictly worse. This is display only: --fail-on-eval and
	// --fail-on-inconclusive read evalReport.Overall, which is untouched.
	if dr.NeedsApproval && overall != policy.Fail {
		overall = policy.NeedsApproval
	}
	body := diff.FormatComment(dr, string(overall)) + evalMD
	unblock := ""
	if dr.NeedsApproval && !approval.Has(store.ForRoot(root).Approvals, base.ID, head.ID) {
		unblock = fmt.Sprintf("airlock approve --base %s --head %s", base.ID, head.ID)
		body += fmt.Sprintf("\n### Unblock\n\n```\n%s\n```\n", unblock)
	}

	p := store.ForRoot(root)
	if err := p.Ensure(); err != nil {
		return err
	}
	commentPath := filepath.Join(p.Airlock, "ci-comment.md")
	if err := os.WriteFile(commentPath, []byte(body), 0o644); err != nil {
		return err
	}
	wrote := commentPath
	if rel, err := filepath.Rel(root, commentPath); err == nil {
		wrote = rel
	}

	out.Print(diff.FormatText(dr))
	if evalReport != nil {
		if table := policy.FormatTable(*evalReport); table != "" {
			out.Print(out.FrameText("eval", table))
		}
	}
	extra := []string{"wrote  " + wrote}
	if unblock != "" {
		extra = append([]string{unblock}, extra...)
	}
	out.Verdict(string(overall), extra...)

	// Fail-closed checks below MUST run regardless of --comment: writing a PR
	// comment is not an escape hatch from the gate.
	fail := failFlag || store.ReadPolicyFailOnChange(store.ForRoot(root))
	if fail && diff.HasChanges(dr) {
		return fmt.Errorf("AI artifacts changed (fail_on_ai_change)")
	}
	if err := evalGateErr(evalReport, failEval, failInconclusive); err != nil {
		return err
	}
	if failApproval {
		if err := approval.Require(store.ForRoot(root).Approvals, base.ID, head.ID, dr.NeedsApproval); err != nil {
			return err
		}
	}
	if failSentinel || sentinelStoreExists(root) {
		if rep, err := runSentinelCheck(root); err != nil {
			if failSentinel {
				return err
			}
		} else if rep.HasDrift() {
			out.Print(sentinel.FormatText(rep))
			if failSentinel {
				return fmt.Errorf("model sentinel drift detected")
			}
		}
	}
	return nil
}

func cmdImport(args []string) error {
	if len(args) < 1 {
		return fmt.Errorf("usage: airlock import promptfoo|langsmith|braintrust <file> [--path DIR]")
	}
	switch args[0] {
	case "promptfoo":
		return cmdImportPromptfoo(args[1:])
	case "langsmith":
		return cmdImportCases(args[1:], "langsmith")
	case "braintrust":
		return cmdImportCases(args[1:], "braintrust")
	default:
		return fmt.Errorf("unknown import source %q", args[0])
	}
}

func cmdImportPromptfoo(args []string) error {
	root, rest, err := rootFromArgs(args)
	if err != nil {
		return err
	}
	if len(rest) < 1 {
		return fmt.Errorf("usage: airlock import promptfoo <file> [--path DIR]")
	}
	file := rest[0]
	if !filepath.IsAbs(file) {
		if _, err := os.Stat(file); err != nil {
			cand := filepath.Join(root, file)
			if _, err2 := os.Stat(cand); err2 == nil {
				file = cand
			}
		}
	}
	res, err := promptfoo.ImportFile(file)
	if err != nil {
		return err
	}
	for _, w := range res.Warnings {
		out.Warn(w)
	}
	return writeImportedCases(root, res.Cases, "default.jsonl")
}

func cmdImportCases(args []string, source string) error {
	root, rest, err := rootFromArgs(args)
	if err != nil {
		return err
	}
	if len(rest) < 1 {
		return fmt.Errorf("usage: airlock import %s <file> [--path DIR]", source)
	}
	file := rest[0]
	if !filepath.IsAbs(file) {
		cand := filepath.Join(root, file)
		if _, err2 := os.Stat(cand); err2 == nil {
			file = cand
		}
	}
	var cases []evalcase.Case
	switch source {
	case "langsmith":
		cases, err = langsmith.ImportFile(file)
	case "braintrust":
		cases, err = braintrust.ImportFile(file)
	default:
		return fmt.Errorf("unknown source %q", source)
	}
	if err != nil {
		return err
	}
	outName := source + ".jsonl"
	return writeImportedCases(root, cases, outName)
}

func writeImportedCases(root string, cases []evalcase.Case, outName string) error {
	p := store.ForRoot(root)
	if err := p.Ensure(); err != nil {
		return err
	}
	out := filepath.Join(p.Evals, outName)
	if err := evalcase.WriteJSONL(out, cases); err != nil {
		return err
	}
	suitePath := filepath.Join(p.Evals, "suite.yml")
	if _, err := os.Stat(suitePath); err != nil {
		s := evalcase.DefaultSuite()
		data := fmt.Sprintf("k: %d\nmin_samples: %d\nmax_samples_per_case: %d\nseed: %d\ncases: %s\nmode: replay\nreplay_miss: fail\n",
			s.K, s.MinSamples, s.MaxSamplesPerCase, s.Seed, outName)
		_ = os.WriteFile(suitePath, []byte(data), 0o644)
	}
	fmt.Printf("Imported %d cases → %s\n", len(cases), out)
	return nil
}

func cmdEval(args []string) error {
	if len(args) < 1 || args[0] != "promote" {
		return fmt.Errorf("usage: airlock eval promote --from ingest|results [--failures-only] [--limit N] [--tag TAG] [--path DIR]")
	}
	root, rest, err := rootFromArgs(args[1:])
	if err != nil {
		return err
	}
	rest, from := flagVal(rest, "--from")
	rest, tag := flagVal(rest, "--tag")
	rest, limitS := flagVal(rest, "--limit")
	_, failuresOnly := flagBool(rest, "--failures-only")
	if from == "" {
		from = "ingest"
	}
	opt := evalcase.PromoteOptions{FailuresOnly: failuresOnly, Tag: tag}
	if limitS != "" {
		opt.Limit, _ = strconv.Atoi(limitS)
	}
	p := store.ForRoot(root)
	out := filepath.Join(p.Evals, "promoted.jsonl")
	var n int
	switch from {
	case "ingest":
		n, err = evaluation.PromoteFromIngest(filepath.Join(p.Ingest, "latest.jsonl"), out, opt)
	case "results":
		n, err = evaluation.PromoteFromResults(filepath.Join(p.Results, "latest.json"), out, opt)
	default:
		return fmt.Errorf("--from must be ingest or results")
	}
	if err != nil {
		return err
	}
	fmt.Printf("Promoted %d cases → %s\n", n, out)
	return nil
}

func resolveCasesForDiff(root string, dr *diff.Result, adversarial, secSurface, mcpTouched, skillTouched bool) (evalcase.Suite, []evalcase.Case, error) {
	suiteDir, err := resolveSuiteDir(root, "")
	if err != nil {
		return evalcase.Suite{}, nil, err
	}
	p := store.ForRoot(root)
	bindings, _ := evalcase.LoadBindings(evalcase.DefaultBindingsPath(root))
	var changes []evalcase.ArtifactChange
	for _, c := range dr.Changes {
		changes = append(changes, evalcase.ArtifactChange{Kind: c.Kind, ID: c.ID, Status: c.Status})
	}
	if names := evalcase.SelectSuites(changes, bindings); len(names) > 0 && diff.HasChanges(dr) {
		out.Printf("artifact -> suite bindings: %s\n", strings.Join(names, ", "))
		suite, cases, err := evalcase.LoadBoundCases(p.Evals, names)
		if err == nil && len(cases) > 0 {
			if len(dr.AffectedAgents) > 0 {
				if filtered := evalcase.FilterByAgents(cases, dr.AffectedAgents); len(filtered) > 0 {
					cases = filtered
				}
			}
			return suite, cases, nil
		}
	}
	suite, cases, lerr := evalcase.LoadSuite(suiteDir)
	if lerr != nil || len(cases) == 0 {
		return suite, cases, lerr
	}
	wantAdv := adversarial || suite.Kind == "adversarial" || secSurface
	if wantAdv {
		if tagged := evalcase.FilterByTag(cases, "adversarial"); len(tagged) > 0 {
			cases = tagged
			if mcpTouched {
				out.Print("MCP change detected: running adversarial cases")
			} else if skillTouched {
				out.Print("Skill change detected: running adversarial cases")
			}
		} else if secSurface {
			advPath := filepath.Join(suiteDir, "suite.adversarial.yml")
			if as, ac, aerr := evalcase.LoadSuiteFile(advPath); aerr == nil && len(ac) > 0 {
				suite, cases = as, ac
				why := "MCP"
				if skillTouched && !mcpTouched {
					why = "Skill"
				}
				out.Printf("%s change detected: loaded %s\n", why, advPath)
			}
		}
	}
	if len(dr.AffectedAgents) > 0 {
		if filtered := evalcase.FilterByAgents(cases, dr.AffectedAgents); len(filtered) > 0 {
			cases = filtered
		}
	}
	return suite, cases, nil
}

func cmdIngest(args []string) error {
	if len(args) < 1 || args[0] != "otel" {
		return fmt.Errorf("usage: airlock ingest otel --file spans.jsonl [--redact pii|hash|off] [--path DIR]")
	}
	root, rest, err := rootFromArgs(args[1:])
	if err != nil {
		return err
	}
	rest, file := flagVal(rest, "--file")
	rest, redact := flagVal(rest, "--redact")
	rest, _ = flagBool(rest, "--hash-strings")
	hash := false
	for _, a := range args {
		if a == "--hash-strings" {
			hash = true
		}
	}
	if file == "" && len(rest) > 0 {
		file = rest[0]
	}
	if file == "" {
		return fmt.Errorf("--file required")
	}
	spans, err := collector.LoadJSONL(file)
	if err != nil {
		return err
	}
	p := store.ForRoot(root)
	if err := p.Ensure(); err != nil {
		return err
	}
	out := filepath.Join(p.Ingest, "latest.jsonl")
	opt := collector.ParseRedact(redact)
	if hash {
		opt.HashStrings = true
		if opt.Mode == "" || opt.Mode == "pii" {
			opt.Mode = "hash"
		}
	}
	if err := collector.WriteIngest(out, spans, opt); err != nil {
		return err
	}
	ok, n := collector.SuccessRate(spans)
	fmt.Printf("Ingested %d spans → %s (success labeled %d/%d, redact=%s)\n", len(spans), out, ok, n, opt.Mode)
	return nil
}

func cmdBaseline(args []string) error {
	if len(args) < 1 || args[0] != "create" {
		return fmt.Errorf("usage: airlock baseline create --from ingest [--samples N] [--redact pii|hash|off] [--path DIR]")
	}
	root, rest, err := rootFromArgs(args[1:])
	if err != nil {
		return err
	}
	rest, from := flagVal(rest, "--from")
	rest, samplesS := flagVal(rest, "--samples")
	_, redact := flagVal(rest, "--redact")
	if from == "" {
		from = "ingest"
	}
	n := 50
	if samplesS != "" {
		n, _ = strconv.Atoi(samplesS)
	}
	p := store.ForRoot(root)
	ingestPath := filepath.Join(p.Ingest, "latest.jsonl")
	if from != "ingest" {
		ingestPath = from
	}
	spans, err := collector.LoadJSONL(ingestPath)
	if err != nil {
		return err
	}
	opt := collector.ParseRedact(redact)
	cases := collector.ToCases(spans, opt, n)
	if err := p.Ensure(); err != nil {
		return err
	}
	out := filepath.Join(p.Evals, "prod.jsonl")
	if err := evalcase.WriteJSONL(out, cases); err != nil {
		return err
	}
	suitePath := filepath.Join(p.Evals, "suite.yml")
	const suiteYML = "k: 3\nmin_samples: 2\nmax_samples_per_case: 3\nseed: 42\ncases: prod.jsonl\nmode: live\nreplay_miss: fail\n"
	_ = os.WriteFile(suitePath, []byte(suiteYML), 0o644)
	_ = collector.WriteIngest(filepath.Join(p.Ingest, "baseline.jsonl"), spans, opt)
	fmt.Printf("Baseline: %d cases → %s (redact=%s)\n", len(cases), out, opt.Mode)
	return nil
}

func cmdDrift(args []string) error {
	root, args, err := rootFromArgs(args)
	if err != nil {
		return err
	}
	_, baseFile := flagVal(args, "--baseline")
	p := store.ForRoot(root)
	if baseFile == "" {
		baseFile = filepath.Join(p.Ingest, "baseline.jsonl")
	}
	liveFile := filepath.Join(p.Ingest, "latest.jsonl")
	base, err := collector.LoadJSONL(baseFile)
	if err != nil {
		return fmt.Errorf("baseline: %w", err)
	}
	live, err := collector.LoadJSONL(liveFile)
	if err != nil {
		return fmt.Errorf("live ingest: %w", err)
	}
	pol, _ := policy.Load(p.Policy)
	maxReg := 1.0
	if g, ok := pol.Gates["task_success"]; ok && g.MaxRegressionPP != nil {
		maxReg = *g.MaxRegressionPP
	}
	rep := drift.FromSpans(base, live, maxReg)
	fmt.Print(drift.FormatText(rep))
	if rep.Verdict == policy.Fail {
		return fmt.Errorf("drift FAIL")
	}
	return nil
}

func judgeHook(reg *judge.Registry) func(context.Context, evalcase.Case, providers.Response) (bool, error) {
	return func(ctx context.Context, c evalcase.Case, resp providers.Response) (bool, error) {
		id := c.Expect.Judge
		if id == "" {
			return true, nil
		}
		return reg.Score(ctx, id, c, resp)
	}
}

func loadJudges(root string, client providers.HTTPDoer) *judge.Registry {
	p := store.ForRoot(root)
	dirs := []string{
		filepath.Join(root, "judges"),
		p.Judges,
	}
	reg, err := judge.LoadDirs(dirs...)
	if err != nil || reg == nil {
		reg = &judge.Registry{ByID: map[string]judge.Spec{}}
	}
	reg.Client = client
	return reg
}

func applyJudgeFloors(rep policy.Report, judgesDir string, reg *judge.Registry) policy.Report {
	if reg == nil {
		return rep
	}
	for id := range reg.ByID {
		cal, err := judge.LoadCalibration(judgesDir, id)
		if err != nil {
			continue
		}
		rep = policy.WithJudgeFloor(rep, id, cal.FloorOK, cal.Kappa, cal.N)
	}
	return rep
}

func cmdJudge(args []string) error {
	if len(args) < 1 {
		return fmt.Errorf("need calibrate or attribution subcommand")
	}
	switch args[0] {
	case "calibrate":
		return cmdJudgeCalibrate(args[1:])
	case "attribution":
		return cmdJudgeAttribution(args[1:])
	default:
		return fmt.Errorf("unknown judge subcommand %q", args[0])
	}
}

func cmdJudgeCalibrate(args []string) error {
	root, rest, err := rootFromArgs(args)
	if err != nil {
		return err
	}
	rest, id := flagVal(rest, "--id")
	_, labels := flagVal(rest, "--labels")
	if id == "" || labels == "" {
		return fmt.Errorf("usage: airlock judge calibrate --id ID --labels FILE.jsonl [--path DIR]")
	}
	p := store.ForRoot(root)
	if err := p.Ensure(); err != nil {
		return err
	}
	reg := loadJudges(root, nil)
	items, err := judge.LoadLabels(labels)
	if err != nil {
		return err
	}
	rep, err := reg.Calibrate(context.Background(), id, items)
	if err != nil {
		return err
	}
	if err := judge.WriteCalibration(p.Judges, id, rep); err != nil {
		return err
	}
	fmt.Printf("Judge %s: kappa=%.3f agree=%d/%d floor_ok=%v → %s\n",
		id, rep.Kappa, rep.Agree, rep.N, rep.FloorOK, filepath.Join(p.Judges, id+".calibration.json"))
	if !rep.FloorOK {
		return fmt.Errorf("kappa below floor")
	}
	return nil
}

func cmdJudgeAttribution(args []string) error {
	root, rest, err := rootFromArgs(args)
	if err != nil {
		return err
	}
	rest, oldID := flagVal(rest, "--old")
	rest, newID := flagVal(rest, "--new")
	_, setPath := flagVal(rest, "--set")
	if oldID == "" || newID == "" || setPath == "" {
		return fmt.Errorf("usage: airlock judge attribution --old ID --new ID --set FILE.jsonl [--path DIR]")
	}
	reg := loadJudges(root, nil)
	items, err := judge.LoadLabels(setPath)
	if err != nil {
		return err
	}
	delta, err := judge.AttributionDelta(context.Background(), reg, reg, oldID, newID, items)
	if err != nil {
		return err
	}
	fmt.Printf("Attribution delta (new-old pass rate): %+.3f on %d refs\n", delta, len(items))
	return nil
}

func cmdApprove(args []string) error {
	root, rest, err := rootFromArgs(args)
	if err != nil {
		return err
	}
	rest, baseID := flagVal(rest, "--base")
	rest, headID := flagVal(rest, "--head")
	rest, note := flagVal(rest, "--note")
	_, by := flagVal(rest, "--by")
	if baseID == "" {
		return fmt.Errorf("usage: airlock approve --base ID --head ID [--note TEXT] [--by WHO] [--path DIR]")
	}
	if headID == "" {
		headID = "working"
	}
	if by == "" {
		by = os.Getenv("USER")
	}
	p := store.ForRoot(root)
	if err := p.Ensure(); err != nil {
		return err
	}
	base, err := snapshot.Load(root, baseID)
	if err != nil {
		return err
	}
	head, err := loadHead(root, headID)
	if err != nil {
		return err
	}
	dr := diff.Compare(base, head)
	rec := approval.Record{
		Base: base.ID, Head: head.ID,
		Reasons: dr.ApprovalReasons, DecidedBy: by, Note: note,
	}
	if len(dr.ApprovalReasons) > 0 {
		out.Print("Reasons requiring approval:")
		for _, reason := range dr.ApprovalReasons {
			out.Printf("  - %s\n", reason)
		}
	} else if !dr.NeedsApproval {
		out.Print("Note: this base/head pair currently has no NEEDS_APPROVAL reasons. Recording approval anyway.")
	}
	if err := approval.Write(p.Approvals, rec); err != nil {
		return err
	}
	wrote := approval.Path(p.Approvals, base.ID, head.ID)
	if rel, err := filepath.Rel(root, wrote); err == nil {
		wrote = rel
	}
	out.Printf("Approved %s -> %s (%s)\n", base.ID, head.ID, wrote)
	return nil
}

func cmdRollback(args []string) error {
	root, rest, err := rootFromArgs(args)
	if err != nil {
		return err
	}
	_, to := flagVal(rest, "--to")
	if to == "" {
		return fmt.Errorf("usage: airlock rollback --to SNAPSHOT_ID [--path DIR]")
	}
	p := store.ForRoot(root)
	snap, err := rollback.RestoreManifest(root, to)
	if err != nil {
		return err
	}
	d := rollback.Decision{
		PreferSnapshotID: to,
		PreferModel:      rollback.PreferModelFromSnapshot(snap),
		Reason:           "known-good manifest re-approval",
	}
	path, err := rollback.WriteRoutingHint(p.Airlock, d)
	if err != nil {
		return err
	}
	fmt.Printf("Rollback: restored manifest from %s\nRouting hint: %s (prefer_model=%s)\n", to, path, d.PreferModel)
	return nil
}

func cmdHistory(args []string) error {
	root, args, err := rootFromArgs(args)
	if err != nil {
		return err
	}
	_, serve := flagVal(args, "--serve")
	p := store.ForRoot(root)
	entries, err := history.List(p.Airlock)
	if err != nil {
		return err
	}
	if serve != "" {
		if !strings.HasPrefix(serve, ":") {
			serve = ":" + serve
		}
		return history.Serve(serve, root)
	}
	for _, e := range entries {
		fmt.Printf("%s  %-8s  %-16s  %s\n", e.ModTime.Format(time.RFC3339), e.Kind, e.ID, e.Verdict)
	}
	return nil
}

func cmdSentinel(args []string) error {
	if len(args) < 1 {
		return fmt.Errorf("usage: airlock sentinel probe|check [--path DIR] [--apply] [--json] [--fail]")
	}
	switch args[0] {
	case "probe":
		return cmdSentinelProbe(args[1:])
	case "check":
		return cmdSentinelCheck(args[1:])
	default:
		return fmt.Errorf("unknown sentinel subcommand %q", args[0])
	}
}

func cmdSentinelProbe(args []string) error {
	root, args, err := rootFromArgs(args)
	if err != nil {
		return err
	}
	_, apply := flagBool(args, "--apply")
	if err := runSentinelProbe(root); err != nil {
		return err
	}
	if apply {
		p := store.ForRoot(root)
		m, err := store.ReadManifest(p)
		if err != nil {
			return err
		}
		st, err := sentinel.Load(sentinel.DefaultPath(root))
		if err != nil {
			return err
		}
		sentinel.ApplyToManifest(m, st)
		if err := store.WriteManifest(p, m); err != nil {
			return err
		}
		fmt.Printf("Applied fingerprints to %s\n", p.Manifest)
	}
	fmt.Printf("Wrote %s\n", sentinel.DefaultPath(root))
	return nil
}

func cmdSentinelCheck(args []string) error {
	root, args, err := rootFromArgs(args)
	if err != nil {
		return err
	}
	args, asJSON := flagBool(args, "--json")
	_, fail := flagBool(args, "--fail")
	rep, err := runSentinelCheck(root)
	if err != nil {
		return err
	}
	if asJSON {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(rep); err != nil {
			return err
		}
	} else {
		fmt.Print(sentinel.FormatText(rep))
	}
	if fail && rep.HasDrift() {
		return fmt.Errorf("model sentinel drift detected")
	}
	return nil
}

func runSentinelProbe(root string) error {
	m, err := discovery.Scan(root)
	if err != nil {
		return err
	}
	if len(m.Models) == 0 {
		return fmt.Errorf("no models in manifest to probe")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	_, err = sentinel.ProbeAll(ctx, m, providers.DefaultHTTPClient(), sentinel.DefaultPath(root))
	return err
}

func runSentinelCheck(root string) (*sentinel.CheckReport, error) {
	m, err := discovery.Scan(root)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	return sentinel.Check(ctx, m, providers.DefaultHTTPClient(), sentinel.DefaultPath(root))
}

func sentinelStoreExists(root string) bool {
	_, err := os.Stat(sentinel.DefaultPath(root))
	return err == nil
}
