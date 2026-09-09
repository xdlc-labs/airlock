package policy_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/xdlc-labs/airlock/internal/policy"
)

func writePolicy(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "policy.yml")
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestFailOnBlockIsRead(t *testing.T) {
	p, err := policy.Load(writePolicy(t, `version: 1
fail_on:
  approval: true
  eval: true
  inconclusive: false
`))
	if err != nil {
		t.Fatal(err)
	}
	if !p.FailOn.Configured() {
		t.Fatal("a policy with a fail_on block must report itself as configured")
	}
	if !p.FailOnApproval() || !p.FailOnEval() {
		t.Fatalf("expected approval and eval gates on, got %+v", p.FailOn)
	}
	if p.FailOnInconclusive() {
		t.Fatal("inconclusive was set false and must stay off")
	}
	if p.FailOnSentinel() {
		t.Fatal("an unset gate must stay off")
	}
}

func TestPolicyWithoutFailOnKeepsOldBehavior(t *testing.T) {
	p, err := policy.Load(writePolicy(t, `version: 1
gates:
  tool_success: { min: 0.9, confidence: 0.95 }
`))
	if err != nil {
		t.Fatal(err)
	}
	if p.FailOn.Configured() {
		t.Fatal("a policy predating fail_on must not look configured")
	}
	for name, got := range map[string]bool{
		"approval":     p.FailOnApproval(),
		"eval":         p.FailOnEval(),
		"inconclusive": p.FailOnInconclusive(),
		"sentinel":     p.FailOnSentinel(),
		"change":       p.FailOnChange(),
	} {
		if got {
			t.Errorf("gate %q must stay off for an existing policy, got on", name)
		}
	}
}

func TestLegacyFailOnAIChangeStillWorks(t *testing.T) {
	p, err := policy.Load(writePolicy(t, "version: 1\nfail_on_ai_change: true\n"))
	if err != nil {
		t.Fatal(err)
	}
	if !p.FailOnChange() {
		t.Fatal("fail_on_ai_change: true must still block AI changes")
	}
	if !p.FailOn.Configured() && !p.FailOnAIChange {
		t.Fatal("the legacy key should still count as an opinion about failing closed")
	}
}

func TestFailOnAIChangeNewKeyWins(t *testing.T) {
	p, err := policy.Load(writePolicy(t, `version: 1
fail_on_ai_change: true
fail_on:
  ai_change: false
`))
	if err != nil {
		t.Fatal(err)
	}
	if p.FailOnChange() {
		t.Fatal("the fail_on block must win over the older key")
	}
}
