package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/xdlc-labs/airlock/internal/snapshot"
	"github.com/xdlc-labs/airlock/internal/store"
)

// approvalRepo builds a repo whose working tree needs approval: an edited
// prompt landing together with a new package dependency.
func approvalRepo(t *testing.T) (root, baseID string) {
	t.Helper()
	root = t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "prompts"), 0o755); err != nil {
		t.Fatal(err)
	}
	promptPath := filepath.Join(root, "prompts", "system.md")
	if err := os.WriteFile(promptPath, []byte("v1"), 0o644); err != nil {
		t.Fatal(err)
	}
	base, err := snapshot.Create(root, true)
	if err != nil {
		t.Fatalf("baseline snapshot: %v", err)
	}
	if err := os.WriteFile(promptPath, []byte("v2"), 0o644); err != nil {
		t.Fatal(err)
	}
	lock := `version: 1
packages:
  left-pad:
    name: left-pad
    version: 1.3.0
    kind: npm
`
	if err := os.WriteFile(filepath.Join(root, "apm.lock.yaml"), []byte(lock), 0o644); err != nil {
		t.Fatal(err)
	}
	return root, base.ID
}

// TestCmdCIPolicyFailsClosedWithoutFlags is the point of the fail_on block: a
// workflow that forgets --fail-on-approval still gets a blocked merge.
func TestCmdCIPolicyFailsClosedWithoutFlags(t *testing.T) {
	root, baseID := approvalRepo(t)
	p := store.ForRoot(root)
	if err := p.Ensure(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p.Policy, []byte("version: 1\nfail_on:\n  approval: true\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	err := cmdCI([]string{"--path", root, "--base", baseID, "--skip-eval"})
	if err == nil {
		t.Fatal("expected the policy's fail_on.approval to block the merge with no flag passed")
	}
	if !strings.Contains(err.Error(), "NEEDS_APPROVAL") {
		t.Fatalf("expected a NEEDS_APPROVAL failure, got %v", err)
	}
}

// TestCmdCIPolicyCannotDisableAFlag guards the direction of the merge: policy
// turns gates on, a flag is never overridden by it.
func TestCmdCIPolicyCannotDisableAFlag(t *testing.T) {
	root, baseID := approvalRepo(t)
	p := store.ForRoot(root)
	if err := p.Ensure(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p.Policy, []byte("version: 1\nfail_on:\n  approval: false\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	err := cmdCI([]string{"--path", root, "--base", baseID, "--skip-eval", "--fail-on-approval"})
	if err == nil {
		t.Fatal("--fail-on-approval must still block even when the policy says false")
	}
}

// TestCmdCIWithoutAnyGateStillReports keeps the old default intact: a repo with
// neither flags nor a fail_on block reports and exits 0.
func TestCmdCIWithoutAnyGateStillReports(t *testing.T) {
	root, baseID := approvalRepo(t)
	if err := cmdCI([]string{"--path", root, "--base", baseID, "--skip-eval"}); err != nil {
		t.Fatalf("an ungated run must not fail: %v", err)
	}
	commentPath := filepath.Join(store.ForRoot(root).Airlock, "ci-comment.md")
	data, err := os.ReadFile(commentPath)
	if err != nil {
		t.Fatalf("expected the comment to be written anyway: %v", err)
	}
	if !strings.Contains(string(data), "NEEDS_APPROVAL") {
		t.Fatalf("the report should still say approval is needed, got:\n%s", data)
	}
}

// TestInitWritesFailClosedPolicy is the "default teams keep on" half: a new repo
// starts with the gates already on.
func TestInitWritesFailClosedPolicy(t *testing.T) {
	root := t.TempDir()
	if err := cmdInit([]string{"--path", root}); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(store.ForRoot(root).Policy)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"fail_on:", "approval: true", "eval: true", "inconclusive: true"} {
		if !strings.Contains(string(data), want) {
			t.Fatalf("policy stub missing %q, got:\n%s", want, data)
		}
	}
}

// TestCmdCISentinelGateWithoutFingerprintsDoesNotBlock: the stub policy turns
// fail_on.sentinel on, and a repo that has never probed must still get through
// its first CI run. The gate is unconfigured, not failing.
func TestCmdCISentinelGateWithoutFingerprintsDoesNotBlock(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "prompts"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "prompts", "system.md"), []byte("v1"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := cmdInit([]string{"--path", root}); err != nil {
		t.Fatal(err)
	}
	base, err := snapshot.Create(root, true)
	if err != nil {
		t.Fatal(err)
	}
	if err := cmdCI([]string{"--path", root, "--base", base.ID, "--skip-eval"}); err != nil {
		t.Fatalf("a fresh repo with the stub policy must pass its first ci run, got %v", err)
	}
	if err := cmdCI([]string{"--path", root, "--base", base.ID, "--skip-eval", "--fail-on-sentinel"}); err != nil {
		t.Fatalf("--fail-on-sentinel without fingerprints is unconfigured, not a failure, got %v", err)
	}
}
