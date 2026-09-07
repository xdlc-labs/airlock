package diff_test

import (
	"strings"
	"testing"

	"github.com/xdlc-labs/airlock/internal/diff"
	"github.com/xdlc-labs/airlock/internal/manifest"
)

func TestCompareDetectsHashChange(t *testing.T) {
	base := &manifest.Snapshot{
		ID: "base",
		Artifacts: []manifest.ArtifactRef{
			{Kind: "prompt", ID: "p1", Hash: "aaa"},
		},
		Manifest: manifest.Manifest{
			Agents: []manifest.Agent{{ID: "a1", Prompts: []string{"p1"}}},
			Graph:  []manifest.Edge{{From: "agent:a1", To: "prompt:p1"}},
		},
	}
	head := &manifest.Snapshot{
		ID: "head",
		Artifacts: []manifest.ArtifactRef{
			{Kind: "prompt", ID: "p1", Hash: "bbb"},
		},
		Manifest: base.Manifest,
	}
	r := diff.Compare(base, head)
	if len(r.Changes) != 1 || r.Changes[0].Status != "changed" {
		t.Fatalf("got %+v", r.Changes)
	}
	if len(r.AffectedAgents) != 1 || r.AffectedAgents[0] != "a1" {
		t.Fatalf("blast radius %+v", r.AffectedAgents)
	}
	if !diff.HasKind(r, "prompt") {
		t.Fatal("expected prompt kind")
	}
	if diff.HasKind(r, "mcp") {
		t.Fatal("unexpected mcp")
	}
}

func TestFormatCommentIncludesEvalBlastRadius(t *testing.T) {
	base := &manifest.Snapshot{
		ID:        "base",
		Artifacts: []manifest.ArtifactRef{{Kind: "prompt", ID: "p1", Hash: "aaa"}},
		Manifest: manifest.Manifest{
			Agents: []manifest.Agent{{ID: "a1", Prompts: []string{"p1"}, Evals: []string{"e1"}}},
		},
	}
	head := &manifest.Snapshot{
		ID:        "head",
		Artifacts: []manifest.ArtifactRef{{Kind: "prompt", ID: "p1", Hash: "bbb"}},
		Manifest:  base.Manifest,
	}
	r := diff.Compare(base, head)
	if len(r.AffectedEvals) == 0 {
		t.Fatalf("expected affected evals, got %+v", r)
	}
	body := diff.FormatComment(r, "")
	if !strings.Contains(body, diff.CommentMarker) {
		t.Fatalf("PR comment missing marker, got:\n%s", body)
	}
	if !strings.Contains(body, "## Airlock") {
		t.Fatalf("PR comment missing heading, got:\n%s", body)
	}
	if !strings.Contains(body, "**e1**") {
		t.Fatalf("PR comment missing evals blast radius, got:\n%s", body)
	}
	if !strings.Contains(body, "| `~` | `prompt` | `p1` |") {
		t.Fatalf("PR comment missing changes table, got:\n%s", body)
	}
	if !strings.Contains(body, "[!TIP]") {
		t.Fatalf("PR comment missing PASS alert, got:\n%s", body)
	}
}

func TestFormatCommentNoChange(t *testing.T) {
	r := &diff.Result{BaseID: "base", HeadID: "head"}
	body := diff.FormatComment(r, "PASS")
	if !strings.HasPrefix(body, diff.CommentMarker+"\n") {
		t.Fatalf("expected marker prefix, got:\n%s", body)
	}
	if !strings.Contains(body, "## Airlock") {
		t.Fatalf("missing heading, got:\n%s", body)
	}
	if !strings.Contains(body, "[!NOTE]") {
		t.Fatalf("missing PASS note alert, got:\n%s", body)
	}
	if !strings.Contains(body, "No AI artifact changes") {
		t.Fatalf("missing no-change copy, got:\n%s", body)
	}
}

func TestFormatTextListsBlastRadius(t *testing.T) {
	base := &manifest.Snapshot{
		ID:        "baseid",
		Artifacts: []manifest.ArtifactRef{{Kind: "prompt", ID: "p1", Hash: "aaa"}},
		Manifest: manifest.Manifest{
			Agents: []manifest.Agent{{ID: "a1", Prompts: []string{"p1"}, Evals: []string{"e1"}}},
		},
	}
	head := &manifest.Snapshot{
		ID:        "headid",
		Artifacts: []manifest.ArtifactRef{{Kind: "prompt", ID: "p1", Hash: "bbb"}},
		Manifest:  base.Manifest,
	}
	text := diff.FormatText(diff.Compare(base, head))
	if !strings.Contains(text, "base  baseid") {
		t.Fatalf("missing header, got:\n%s", text)
	}
	if !strings.Contains(text, "evals   e1") {
		t.Fatalf("missing evals line, got:\n%s", text)
	}
	if !strings.Contains(text, "╭─ airlock") {
		t.Fatalf("missing box, got:\n%s", text)
	}
}
