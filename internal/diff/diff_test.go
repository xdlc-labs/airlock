package diff_test

import (
	"fmt"
	"slices"
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

func TestComparePopulatesChangePath(t *testing.T) {
	base := &manifest.Snapshot{
		ID:        "base",
		Artifacts: []manifest.ArtifactRef{{Kind: "prompt", ID: "p1", Hash: "aaa"}},
		Manifest: manifest.Manifest{
			Prompts: []manifest.Prompt{{ID: "p1", Path: "prompts/system.md", ContentHash: "aaa"}},
		},
	}
	head := &manifest.Snapshot{
		ID:        "head",
		Artifacts: []manifest.ArtifactRef{{Kind: "prompt", ID: "p1", Hash: "bbb"}},
		Manifest: manifest.Manifest{
			Prompts: []manifest.Prompt{{ID: "p1", Path: "prompts/system.md", ContentHash: "bbb"}},
		},
	}
	r := diff.Compare(base, head)
	if len(r.Changes) != 1 {
		t.Fatalf("expected 1 change, got %+v", r.Changes)
	}
	if r.Changes[0].Path != "prompts/system.md" {
		t.Fatalf("Path = %q, want prompts/system.md", r.Changes[0].Path)
	}
	body := diff.FormatComment(r, "PASS")
	if !strings.Contains(body, "`prompts/system.md`") {
		t.Fatalf("comment should name the file that changed, got:\n%s", body)
	}
	if !strings.Contains(body, "`aaa` → `bbb`") {
		t.Fatalf("comment should show the hash move, got:\n%s", body)
	}
}

func TestComparePathFallsBackToBaseForRemoved(t *testing.T) {
	base := &manifest.Snapshot{
		ID:        "base",
		Artifacts: []manifest.ArtifactRef{{Kind: "skill", ID: "s1", Hash: "aaa"}},
		Manifest: manifest.Manifest{
			Skills: []manifest.Skill{{ID: "s1", Path: ".claude/skills/s1/SKILL.md", ContentHash: "aaa"}},
		},
	}
	head := &manifest.Snapshot{ID: "head"}
	r := diff.Compare(base, head)
	if len(r.Changes) != 1 || r.Changes[0].Status != "removed" {
		t.Fatalf("expected one removal, got %+v", r.Changes)
	}
	if r.Changes[0].Path != ".claude/skills/s1/SKILL.md" {
		t.Fatalf("Path = %q, want the base path", r.Changes[0].Path)
	}
}

func TestFormatCommentPutsApprovalAheadOfChanges(t *testing.T) {
	r := &diff.Result{
		BaseID:          "base1",
		HeadID:          "head1",
		Changes:         []diff.Change{{Kind: "mcp", ID: "local-fs", Status: "changed", OldHash: "aaa", NewHash: "bbb"}},
		NeedsApproval:   true,
		ApprovalReasons: []string{"MCP new permission write on local-fs"},
	}
	cmd := "airlock approve --base base1 --head head1"
	body := diff.FormatCommentWith(r, "NEEDS_APPROVAL", diff.CommentOptions{ApproveCmd: cmd})

	approve := strings.Index(body, cmd)
	changes := strings.Index(body, "### Changes")
	if approve < 0 {
		t.Fatalf("comment missing the approve command, got:\n%s", body)
	}
	if changes < 0 || approve > changes {
		t.Fatalf("the approve command must come before the change table, got:\n%s", body)
	}
	if !strings.Contains(body, "MCP new permission write on local-fs") {
		t.Fatalf("comment missing the approval reason, got:\n%s", body)
	}
}

func TestFormatCommentOmitsApproveCommandWhenAlreadyApproved(t *testing.T) {
	r := &diff.Result{
		BaseID:          "base1",
		HeadID:          "head1",
		Changes:         []diff.Change{{Kind: "mcp", ID: "local-fs", Status: "changed"}},
		NeedsApproval:   true,
		ApprovalReasons: []string{"MCP permissions expanded: local-fs"},
	}
	body := diff.FormatCommentWith(r, "NEEDS_APPROVAL", diff.CommentOptions{})
	if strings.Contains(body, "airlock approve") {
		t.Fatalf("no approve command should appear when the caller gave none, got:\n%s", body)
	}
	if !strings.Contains(body, "MCP permissions expanded: local-fs") {
		t.Fatalf("the reason should still be there, got:\n%s", body)
	}
}

func TestFormatCommentNamesBothSnapshots(t *testing.T) {
	r := &diff.Result{BaseID: "abc123", HeadID: "working-def456"}
	body := diff.FormatComment(r, "PASS")
	if !strings.Contains(body, "base `abc123` → head `working-def456`") {
		t.Fatalf("comment should name the snapshots compared, got:\n%s", body)
	}
	if !strings.Contains(body, "airlock diff --base abc123 --head working-def456") {
		t.Fatalf("comment should show how to reproduce the diff, got:\n%s", body)
	}
}

func TestFormatCommentCapsTheChangeTable(t *testing.T) {
	r := &diff.Result{BaseID: "b", HeadID: "h"}
	for i := 0; i < 45; i++ {
		r.Changes = append(r.Changes, diff.Change{
			Kind: "prompt", ID: fmt.Sprintf("p%02d", i), Status: "changed",
			Path: fmt.Sprintf("prompts/p%02d.md", i), OldHash: "aaa", NewHash: "bbb",
		})
	}
	body := diff.FormatComment(r, "PASS")
	if strings.Contains(body, "p44") {
		t.Fatal("expected the table to stop before the last row")
	}
	if !strings.Contains(body, "15 more not shown") {
		t.Fatalf("expected a count of the rows left out, got:\n%s", body)
	}
	if !strings.Contains(body, "| 45 |") {
		t.Fatalf("the summary must still count every change, got:\n%s", body)
	}
}

func TestClampComment(t *testing.T) {
	var b strings.Builder
	b.WriteString("## Airlock\n")
	for i := 0; i < 500; i++ {
		fmt.Fprintf(&b, "| `~` | `prompt` | `p%03d` | `prompts/p%03d.md` | `aaa` -> `bbb` |\n", i, i)
	}
	body := b.String()

	if got := diff.ClampComment(body, diff.MaxCommentBytes); got != body {
		t.Fatal("a body under the limit must pass through untouched")
	}
	const limit = 2000
	got := diff.ClampComment(body, limit)
	if len(got) > limit {
		t.Fatalf("clamped body is %d bytes, want <= %d", len(got), limit)
	}
	if !strings.Contains(got, "truncated") {
		t.Fatalf("a clamped body must say so, got the tail:\n%s", got[len(got)-200:])
	}
	if !strings.HasPrefix(got, "## Airlock\n") {
		t.Fatal("clamping must keep the head of the report")
	}
	if strings.Contains(got, "| `~` | `prompt` | `p499`") {
		t.Fatal("clamping should have dropped the last rows")
	}
	// The cut lands on a line boundary, so no row is left half-written.
	rows := strings.Split(strings.TrimSpace(got), "\n")
	for _, row := range rows {
		if strings.HasPrefix(row, "| ") && !strings.HasSuffix(row, "|") {
			t.Fatalf("clamped mid-row: %q", row)
		}
	}
}

func TestUnlinkedChangeIsReportedAsUnknownRadius(t *testing.T) {
	// A prompt discovered by file scan that no agent declares: the old report
	// said "agents: none linked", which reads as "affects nothing".
	mf := manifest.Manifest{
		Agents:  []manifest.Agent{{ID: "support-bot", Prompts: []string{"declared"}}},
		Prompts: []manifest.Prompt{{ID: "declared"}, {ID: "orphan", Path: "prompts/orphan.md"}},
	}
	base := &manifest.Snapshot{
		ID:        "base",
		Artifacts: []manifest.ArtifactRef{{Kind: "prompt", ID: "orphan", Hash: "aaa"}},
		Manifest:  mf,
	}
	head := &manifest.Snapshot{
		ID:        "head",
		Artifacts: []manifest.ArtifactRef{{Kind: "prompt", ID: "orphan", Hash: "bbb"}},
		Manifest:  mf,
	}
	r := diff.Compare(base, head)
	if len(r.UnlinkedChanges) != 1 || r.UnlinkedChanges[0] != "prompt:orphan" {
		t.Fatalf("UnlinkedChanges = %v, want [prompt:orphan]", r.UnlinkedChanges)
	}
	body := diff.FormatComment(r, "PASS")
	if !strings.Contains(body, "blast radius is unknown") {
		t.Fatalf("comment should not present an unlinked change as no impact, got:\n%s", body)
	}
	text := diff.FormatText(r)
	if !strings.Contains(text, "unknown") {
		t.Fatalf("terminal report should flag the unknown radius, got:\n%s", text)
	}
}

func TestDeclaredChangeIsNotUnlinked(t *testing.T) {
	mf := manifest.Manifest{
		Agents:  []manifest.Agent{{ID: "support-bot", Prompts: []string{"p1"}}},
		Prompts: []manifest.Prompt{{ID: "p1"}},
	}
	base := &manifest.Snapshot{
		ID:        "base",
		Artifacts: []manifest.ArtifactRef{{Kind: "prompt", ID: "p1", Hash: "aaa"}},
		Manifest:  mf,
	}
	head := &manifest.Snapshot{
		ID:        "head",
		Artifacts: []manifest.ArtifactRef{{Kind: "prompt", ID: "p1", Hash: "bbb"}},
		Manifest:  mf,
	}
	r := diff.Compare(base, head)
	if len(r.UnlinkedChanges) != 0 {
		t.Fatalf("a declared prompt must not be reported as unlinked, got %v", r.UnlinkedChanges)
	}
	if len(r.AffectedAgents) != 1 || r.AffectedAgents[0] != "support-bot" {
		t.Fatalf("AffectedAgents = %v, want [support-bot]", r.AffectedAgents)
	}
}

func TestAgentGainingAnMCPServerNeedsApproval(t *testing.T) {
	// The MCP server is unchanged; only the agent's wiring grew. Before this the
	// agent hash moved with no reason attached.
	server := manifest.MCPServer{ID: "local-fs", SchemaHash: "same", Permissions: []string{"write"}}
	base := &manifest.Snapshot{
		ID: "base",
		Artifacts: []manifest.ArtifactRef{
			{Kind: "agent", ID: "support-bot", Hash: "agent-v1"},
			{Kind: "mcp", ID: "local-fs", Hash: "mcp-v1"},
		},
		Manifest: manifest.Manifest{
			Agents:     []manifest.Agent{{ID: "support-bot"}},
			MCPServers: []manifest.MCPServer{server},
		},
	}
	head := &manifest.Snapshot{
		ID: "head",
		Artifacts: []manifest.ArtifactRef{
			{Kind: "agent", ID: "support-bot", Hash: "agent-v2"},
			{Kind: "mcp", ID: "local-fs", Hash: "mcp-v1"},
		},
		Manifest: manifest.Manifest{
			Agents:     []manifest.Agent{{ID: "support-bot", MCP: []string{"local-fs"}}},
			MCPServers: []manifest.MCPServer{server},
		},
	}
	r := diff.Compare(base, head)
	if !r.NeedsApproval {
		t.Fatalf("wiring an MCP server into an agent must need approval, got %+v", r)
	}
	want := "agent support-bot now uses mcp local-fs (needs review)"
	if !slices.Contains(r.ApprovalReasons, want) {
		t.Fatalf("ApprovalReasons = %v, want one saying %q", r.ApprovalReasons, want)
	}
}

func TestAddedAgentDoesNotEnumerateItsWiring(t *testing.T) {
	base := &manifest.Snapshot{ID: "base"}
	head := &manifest.Snapshot{
		ID: "head",
		Artifacts: []manifest.ArtifactRef{
			{Kind: "agent", ID: "new-bot", Hash: "agent-v1"},
		},
		Manifest: manifest.Manifest{
			Agents: []manifest.Agent{{ID: "new-bot", MCP: []string{"local-fs"}, Tools: []string{"send_email"}}},
		},
	}
	r := diff.Compare(base, head)
	for _, reason := range r.ApprovalReasons {
		if strings.Contains(reason, "now uses") {
			t.Fatalf("a brand-new agent should not report per-link reasons, got %v", r.ApprovalReasons)
		}
	}
}

func TestAgentModelSwapNeedsApproval(t *testing.T) {
	base := &manifest.Snapshot{
		ID:        "base",
		Artifacts: []manifest.ArtifactRef{{Kind: "agent", ID: "bot", Hash: "v1"}},
		Manifest: manifest.Manifest{
			Agents: []manifest.Agent{{ID: "bot", Models: []string{"model-gpt-4o"}}},
		},
	}
	head := &manifest.Snapshot{
		ID:        "head",
		Artifacts: []manifest.ArtifactRef{{Kind: "agent", ID: "bot", Hash: "v2"}},
		Manifest: manifest.Manifest{
			Agents: []manifest.Agent{{ID: "bot", Models: []string{"model-claude-sonnet-4-5"}}},
		},
	}
	r := diff.Compare(base, head)
	want := "agent bot now uses model model-claude-sonnet-4-5 (needs review)"
	if !slices.Contains(r.ApprovalReasons, want) {
		t.Fatalf("ApprovalReasons = %v, want one saying %q", r.ApprovalReasons, want)
	}
}
