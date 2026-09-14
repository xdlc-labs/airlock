package policy_test

import (
	"strings"
	"testing"

	"github.com/xdlc-labs/airlock/internal/policy"
)

// A pure comparative gate (only MaxRegressionPP/MaxNewCritical, no Min, the
// shape of the default task_success / adversarial_critical gates) with no
// baseline pairs to compare against used to just vanish from Report.Metrics
// with zero trace. It must now show up as SKIPPED, and Evaluate itself must
// never fail closed on that row. CI fail-closes SKIPPED when
// fail_on.inconclusive is on (see evalGateErr).
func TestEvaluateNoBaselineTaskSuccessIsSkippedNotSilent(t *testing.T) {
	maxReg := 1.0
	p := policy.Policy{Gates: map[string]policy.GateSpec{
		"task_success": {MaxRegressionPP: &maxReg, Confidence: 0.95},
	}}
	r := policy.Evaluate(p, []policy.MetricRates{{Name: "task_success", Successes: 9, N: 10}})

	if r.Overall != policy.Pass {
		t.Fatalf("a skipped comparative gate must not fail closed, got overall %s", r.Overall)
	}
	if len(r.Metrics) != 1 {
		t.Fatalf("want the skipped gate to appear as a row, got %d metrics: %+v", len(r.Metrics), r.Metrics)
	}
	got := r.Metrics[0]
	if got.Verdict != policy.Skipped {
		t.Fatalf("want SKIPPED verdict, got %s", got.Verdict)
	}
	if !strings.Contains(got.Reason, "no baseline") {
		t.Fatalf("reason should explain why it's skipped, got %q", got.Reason)
	}
}

func TestEvaluateNoBaselineAdversarialCriticalIsSkippedNotSilent(t *testing.T) {
	zero := 0
	p := policy.Policy{Gates: map[string]policy.GateSpec{
		"adversarial_critical": {MaxNewCritical: &zero, Confidence: 0.95},
	}}
	r := policy.Evaluate(p, []policy.MetricRates{{Name: "adversarial_critical", Successes: 3, N: 3}})

	if r.Overall != policy.Pass {
		t.Fatalf("a skipped comparative gate must not fail closed, got overall %s", r.Overall)
	}
	if len(r.Metrics) != 1 || r.Metrics[0].Verdict != policy.Skipped {
		t.Fatalf("want a single SKIPPED row, got %+v", r.Metrics)
	}
}

// When a Min gate is also configured on the same metric, the row still
// renders from the Min evaluation — but the regression half that couldn't
// run without a baseline must be called out in the reason, not silently
// dropped inside an otherwise-normal PASS/FAIL row.
func TestEvaluateNoBaselineNotesSkippedHalfOfMixedGate(t *testing.T) {
	minRate := 0.5
	maxReg := 1.0
	p := policy.Policy{Gates: map[string]policy.GateSpec{
		"task_success": {Min: &minRate, MaxRegressionPP: &maxReg, Confidence: 0.95},
	}}
	r := policy.Evaluate(p, []policy.MetricRates{{Name: "task_success", Successes: 9, N: 10}})

	if len(r.Metrics) != 1 {
		t.Fatalf("want 1 metric row, got %d: %+v", len(r.Metrics), r.Metrics)
	}
	got := r.Metrics[0]
	if got.Verdict == policy.Skipped {
		t.Fatalf("the Min half did evaluate — verdict should be PASS/FAIL/INCONCLUSIVE, not SKIPPED, got %s", got.Verdict)
	}
	if !strings.Contains(got.Reason, "no baseline") {
		t.Fatalf("reason should still note the regression half was skipped, got %q", got.Reason)
	}
}

// With a baseline present, nothing should be marked SKIPPED — this guards
// against a regression that always tags comparative gates as skipped.
func TestEvaluateWithBaselineIsNotSkipped(t *testing.T) {
	maxReg := 1.0
	p := policy.Policy{Gates: map[string]policy.GateSpec{
		"task_success": {MaxRegressionPP: &maxReg, Confidence: 0.95},
	}}
	base := []bool{true, true, true, true, true}
	cand := []bool{true, true, true, true, true}
	r := policy.Evaluate(p, []policy.MetricRates{{
		Name: "task_success", Successes: 5, N: 5, BasePass: base, CandPass: cand,
	}})
	if len(r.Metrics) != 1 || r.Metrics[0].Verdict == policy.Skipped {
		t.Fatalf("baseline present — gate must actually evaluate, not skip, got %+v", r.Metrics)
	}
}
