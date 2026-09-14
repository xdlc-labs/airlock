package evaluation

import (
	"os"
	"path/filepath"
	"testing"
)

func TestFindBaselinePrefersNamedSnapshot(t *testing.T) {
	dir := t.TempDir()
	want := &RunResult{SnapshotID: "base-aaa"}
	if err := SaveResult(dir, "base-aaa", want); err != nil {
		t.Fatal(err)
	}
	other := &RunResult{SnapshotID: "other"}
	if err := SaveResult(dir, "other", other); err != nil {
		t.Fatal(err)
	}

	got, err := FindBaseline(dir, "base-aaa")
	if err != nil {
		t.Fatal(err)
	}
	if got.SnapshotID != "base-aaa" {
		t.Fatalf("paired %s, want base-aaa", got.SnapshotID)
	}
}

func TestFindBaselineRejectsLatestFromAnotherSnapshot(t *testing.T) {
	dir := t.TempDir()
	other := &RunResult{SnapshotID: "other"}
	if err := SaveResult(dir, "other", other); err != nil {
		t.Fatal(err)
	}

	_, err := FindBaseline(dir, "base-aaa")
	if err == nil {
		t.Fatal("latest.json is a different snapshot and must not pair")
	}
}

func TestFindBaselineLatestWhenItIsTheNamedSnapshot(t *testing.T) {
	dir := t.TempDir()
	want := &RunResult{SnapshotID: "base-aaa"}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := SaveResult(dir, "base-aaa", want); err != nil {
		t.Fatal(err)
	}
	named := filepath.Join(dir, "base-aaa.json")
	if err := os.Remove(named); err != nil {
		t.Fatal(err)
	}

	got, err := FindBaseline(dir, "base-aaa")
	if err != nil {
		t.Fatal(err)
	}
	if got.SnapshotID != "base-aaa" {
		t.Fatalf("paired %s, want base-aaa from latest.json", got.SnapshotID)
	}
}

func TestFindBaselineEmptyIDUsesLatest(t *testing.T) {
	dir := t.TempDir()
	want := &RunResult{SnapshotID: "whatever"}
	if err := SaveResult(dir, "whatever", want); err != nil {
		t.Fatal(err)
	}

	got, err := FindBaseline(dir, "")
	if err != nil {
		t.Fatal(err)
	}
	if got.SnapshotID != "whatever" {
		t.Fatalf("paired %s, want whatever", got.SnapshotID)
	}
}
