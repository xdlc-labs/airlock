package main

import (
	"testing"

	"github.com/xdlc-labs/airlock/internal/policy"
)

// TestEvalGateErr guards the fail-closed mapping from eval verdict to CI exit
// code. --fail-on-eval alone only ever tripped on FAIL: with default
// thresholds (0.99/0.995 min) and default max_samples_per_case: 5, a metric
// can straddle the min forever and sit at INCONCLUSIVE with nothing failing
// CI. --fail-on-inconclusive must close that gap, and must still fail closed
// on FAIL (strictly worse than INCONCLUSIVE) when enabled alone.
func TestEvalGateErr(t *testing.T) {
	cases := []struct {
		name                       string
		report                     *policy.Report
		failEval, failInconclusive bool
		wantErr                    bool
	}{
		{"nil report never gates", nil, true, true, false},
		{"PASS never gates", &policy.Report{Overall: policy.Pass}, true, true, false},
		{"FAIL, no flags: passes silently", &policy.Report{Overall: policy.Fail}, false, false, false},
		{"FAIL, --fail-on-eval: gates", &policy.Report{Overall: policy.Fail}, true, false, true},
		{"INCONCLUSIVE, --fail-on-eval only: still passes silently (the gap)", &policy.Report{Overall: policy.Inconclusive}, true, false, false},
		{"INCONCLUSIVE, --fail-on-inconclusive: gates", &policy.Report{Overall: policy.Inconclusive}, false, true, true},
		{"FAIL, --fail-on-inconclusive only: still gates (worse than inconclusive)", &policy.Report{Overall: policy.Fail}, false, true, true},
		{"INCONCLUSIVE, no flags: passes silently", &policy.Report{Overall: policy.Inconclusive}, false, false, false},
		{"NEEDS_APPROVAL: not this gate's job", &policy.Report{Overall: policy.NeedsApproval}, true, true, false},
		{"SKIPPED metric, --fail-on-inconclusive: gates", &policy.Report{
			Overall: policy.Pass,
			Metrics: []policy.MetricEvidence{{Name: "task_success", Verdict: policy.Skipped}},
		}, false, true, true},
		{"SKIPPED metric, --fail-on-eval only: still passes", &policy.Report{
			Overall: policy.Pass,
			Metrics: []policy.MetricEvidence{{Name: "task_success", Verdict: policy.Skipped}},
		}, true, false, false},
		{"SKIPPED metric, no flags: still passes", &policy.Report{
			Overall: policy.Pass,
			Metrics: []policy.MetricEvidence{{Name: "task_success", Verdict: policy.Skipped}},
		}, false, false, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := evalGateErr(c.report, c.failEval, c.failInconclusive)
			if c.wantErr && err == nil {
				t.Fatal("expected error, got nil")
			}
			if !c.wantErr && err != nil {
				t.Fatalf("expected no error, got %v", err)
			}
		})
	}
}
