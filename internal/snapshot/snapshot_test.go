package snapshot

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestCreateSnapshotIDStableAcrossTimeAndPath(t *testing.T) {
	write := func(root string) {
		t.Helper()
		if err := os.MkdirAll(filepath.Join(root, "prompts"), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, "prompts", "system.md"), []byte("hello\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	a := t.TempDir()
	write(a)
	s1, err := Create(a, true)
	if err != nil {
		t.Fatal(err)
	}

	time.Sleep(5 * time.Millisecond)
	b := t.TempDir()
	write(b)
	s2, err := Create(b, true)
	if err != nil {
		t.Fatal(err)
	}

	if s1.ID != s2.ID {
		t.Fatalf("snapshot id must ignore generated_at and absolute root: %s vs %s", s1.ID, s2.ID)
	}
}
