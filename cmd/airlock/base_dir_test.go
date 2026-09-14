package main

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/xdlc-labs/airlock/internal/snapshot"
	"github.com/xdlc-labs/airlock/internal/store"
)

func writeEvalRepo(t *testing.T, root string) {
	t.Helper()
	evals := filepath.Join(root, ".airlock", "evals")
	if err := os.MkdirAll(evals, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "prompts"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "prompts", "system.md"), []byte("v1"), 0o644); err != nil {
		t.Fatal(err)
	}
	suite := `k: 2
min_samples: 1
max_samples_per_case: 2
seed: 42
cases: default.jsonl
mode: live
`
	if err := os.WriteFile(filepath.Join(evals, "suite.yml"), []byte(suite), 0o644); err != nil {
		t.Fatal(err)
	}
	caseLine := `{"id":"json-ok","agent":"support-bot","input":{"provider":"mock","messages":[{"role":"user","content":"ping"}]},"expect":{"json_valid":true},"tags":["json"]}` + "\n"
	if err := os.WriteFile(filepath.Join(evals, "default.jsonl"), []byte(caseLine), 0o644); err != nil {
		t.Fatal(err)
	}
	policy := `version: 1
gates:
  json_valid: { min: 0.01, confidence: 0.95 }
  task_success: { max_regression_pp: 1.0, confidence: 0.95 }
budgets:
  max_samples_per_case: 2
`
	if err := os.WriteFile(store.ForRoot(root).Policy, []byte(policy), 0o644); err != nil {
		t.Fatal(err)
	}
}

func copyTree(src, dst string) error {
	return filepath.WalkDir(src, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		return os.WriteFile(target, data, 0o644)
	})
}

func TestCmdCIBaseDirPairsComparativeGate(t *testing.T) {
	base := t.TempDir()
	writeEvalRepo(t, base)
	snap, err := snapshot.Create(base, true)
	if err != nil {
		t.Fatalf("base snapshot: %v", err)
	}

	head := t.TempDir()
	if err := copyTree(base, head); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(head, "prompts", "system.md"), []byte("v2"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := cmdCI([]string{"--path", head, "--base", snap.ID, "--base-dir", base}); err != nil {
		t.Fatalf("ci with --base-dir: %v", err)
	}
	comment, err := os.ReadFile(filepath.Join(store.ForRoot(head).Airlock, "ci-comment.md"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(comment), "SKIPPED") {
		t.Fatalf("comparative gate should pair against --base-dir, got:\n%s", comment)
	}
	if _, err := os.Stat(filepath.Join(store.ForRoot(head).Results, snap.ID+".json")); err != nil {
		t.Fatalf("expected base result saved under head results: %v", err)
	}
}

func TestCmdCISkippedFailsClosedWithInconclusive(t *testing.T) {
	base := t.TempDir()
	writeEvalRepo(t, base)
	snap, err := snapshot.Create(base, true)
	if err != nil {
		t.Fatalf("base snapshot: %v", err)
	}

	head := t.TempDir()
	if err := copyTree(base, head); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(head, "prompts", "system.md"), []byte("v2"), 0o644); err != nil {
		t.Fatal(err)
	}

	err = cmdCI([]string{"--path", head, "--base", snap.ID, "--fail-on-inconclusive"})
	if err == nil {
		t.Fatal("SKIPPED comparative gate must fail closed when --fail-on-inconclusive is on")
	}
	if !strings.Contains(err.Error(), "SKIPPED") {
		t.Fatalf("want SKIPPED in the error, got %v", err)
	}
}
