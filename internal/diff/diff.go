package diff

import (
	"fmt"
	"slices"
	"strings"

	"github.com/xdlc-labs/airlock/internal/manifest"
	"github.com/xdlc-labs/airlock/internal/out"
	"github.com/xdlc-labs/airlock/internal/xslices"
)

// Change is one artifact that added, removed, or changed hash.
type Change struct {
	Kind    string `json:"kind"`
	ID      string `json:"id"`
	Status  string `json:"status"` // added|removed|changed
	OldHash string `json:"old_hash,omitempty"`
	NewHash string `json:"new_hash,omitempty"`
}

// Result is a static blast-radius diff between two snapshots.
type Result struct {
	BaseID          string   `json:"base_id"`
	HeadID          string   `json:"head_id"`
	Changes         []Change `json:"changes"`
	AffectedAgents  []string `json:"affected_agents"`
	AffectedEvals   []string `json:"affected_evals"`
	NeedsApproval   bool     `json:"needs_approval"`
	ApprovalReasons []string `json:"approval_reasons,omitempty"`
}

// Compare computes artifact set-diff and walks the head graph for blast radius.
func Compare(base, head *manifest.Snapshot) *Result {
	r := &Result{
		BaseID: base.ID,
		HeadID: head.ID,
	}
	baseMap := index(base.Artifacts)
	headMap := index(head.Artifacts)

	keys := map[string]struct{}{}
	for k := range baseMap {
		keys[k] = struct{}{}
	}
	for k := range headMap {
		keys[k] = struct{}{}
	}
	keyList := make([]string, 0, len(keys))
	for k := range keys {
		keyList = append(keyList, k)
	}
	slices.Sort(keyList)

	changedKeys := map[string]bool{}
	for _, k := range keyList {
		b, bok := baseMap[k]
		h, hok := headMap[k]
		switch {
		case !bok:
			r.Changes = append(r.Changes, Change{Kind: h.Kind, ID: h.ID, Status: "added", NewHash: h.Hash})
			changedKeys[k] = true
		case !hok:
			r.Changes = append(r.Changes, Change{Kind: b.Kind, ID: b.ID, Status: "removed", OldHash: b.Hash})
			changedKeys[k] = true
		case b.Hash != h.Hash:
			r.Changes = append(r.Changes, Change{Kind: h.Kind, ID: h.ID, Status: "changed", OldHash: b.Hash, NewHash: h.Hash})
			changedKeys[k] = true
		}
	}

	r.AffectedAgents, r.AffectedEvals = blastRadius(&head.Manifest, changedKeys)
	// also consider base graph for removed deps
	if len(r.AffectedAgents) == 0 {
		r.AffectedAgents, r.AffectedEvals = blastRadius(&base.Manifest, changedKeys)
	} else {
		a2, e2 := blastRadius(&base.Manifest, changedKeys)
		r.AffectedAgents = xslices.UniqueSorted(append(r.AffectedAgents, a2...))
		r.AffectedEvals = xslices.UniqueSorted(append(r.AffectedEvals, e2...))
	}
	r.NeedsApproval, r.ApprovalReasons = permissionExpansion(base, head, r.Changes)
	if depNeeds, depReasons := dependencyExpansion(r.Changes); depNeeds {
		r.NeedsApproval = true
		r.ApprovalReasons = xslices.UniqueSorted(append(r.ApprovalReasons, depReasons...))
	}
	return r
}

// dependencyExpansion flags a new (non-AI) package dependency landing in the
// same PR as an AI artifact change — agent-driven supply chain: the diff looks
// like "prompt tweak" but quietly widens what the app depends on. A dep-only
// PR with no AI artifact change is left alone; that is SCA's job, not ours.
func dependencyExpansion(changes []Change) (bool, []string) {
	var aiChanged bool
	var added []string
	for _, c := range changes {
		if c.Kind == "dependency" {
			if c.Status == "added" {
				added = append(added, c.ID)
			}
			continue
		}
		if c.Status == "added" || c.Status == "changed" {
			aiChanged = true
		}
	}
	if !aiChanged || len(added) == 0 {
		return false, nil
	}
	reasons := make([]string, 0, len(added))
	for _, id := range added {
		reasons = append(reasons, "new dependency: "+id)
	}
	return true, reasons
}

func permissionExpansion(base, head *manifest.Snapshot, changes []Change) (bool, []string) {
	baseTools := map[string]manifest.Tool{}
	for _, t := range base.Manifest.Tools {
		baseTools[t.ID] = t
	}
	baseMCP := map[string]manifest.MCPServer{}
	for _, m := range base.Manifest.MCPServers {
		baseMCP[m.ID] = m
	}
	baseSkills := map[string]manifest.Skill{}
	for _, s := range base.Manifest.Skills {
		baseSkills[s.ID] = s
	}
	var reasons []string
	for _, c := range changes {
		if c.Status != "added" && c.Status != "changed" {
			continue
		}
		switch c.Kind {
		case "tool":
			for _, t := range head.Manifest.Tools {
				if t.ID != c.ID {
					continue
				}
				old, had := baseTools[t.ID]
				se := strings.ToLower(t.SideEffect)
				if se == "write" || looksWriteTool(t.Name) {
					if !had || strings.ToLower(old.SideEffect) != "write" {
						reasons = append(reasons, "new/expanded write tool: "+t.ID)
					}
				}
				if c.Status == "added" && (se == "write" || se == "unknown" || looksWriteTool(t.Name)) {
					if !slices.Contains(reasons, "new/expanded write tool: "+t.ID) {
						reasons = append(reasons, "added tool (needs review): "+t.ID)
					}
				}
			}
		case "skill":
			for _, s := range head.Manifest.Skills {
				if s.ID != c.ID {
					continue
				}
				if _, had := baseSkills[s.ID]; !had {
					reasons = append(reasons, "added skill (needs review): "+s.ID)
					continue
				}
				if c.Status == "changed" {
					reasons = append(reasons, "skill content changed: "+s.ID)
				}
			}
		case "mcp":
			for _, m := range head.Manifest.MCPServers {
				if m.ID != c.ID {
					continue
				}
				old, had := baseMCP[m.ID]
				if !had {
					reasons = append(reasons, "added MCP server: "+m.ID)
					continue
				}
				if len(m.Permissions) > len(old.Permissions) {
					reasons = append(reasons, "MCP permissions expanded: "+m.ID)
				}
				for _, p := range m.Permissions {
					if !slices.Contains(old.Permissions, p) {
						reasons = append(reasons, "MCP new permission "+p+" on "+m.ID)
						break
					}
				}
				// Live tools/list diff: Permissions is only ever hand-maintained
				// via apm.lock.yaml, so a server whose actual tool list grows
				// (discovered live, see mcp_fetch.go) would otherwise pass
				// through as a bare "changed" with no approval signal at all.
				for _, name := range m.ToolNames {
					if slices.Contains(old.ToolNames, name) {
						continue
					}
					if looksWriteTool(name) {
						reasons = append(reasons, "MCP new write-looking tool "+name+" on "+m.ID)
					} else {
						reasons = append(reasons, "MCP new tool (needs review): "+name+" on "+m.ID)
					}
				}
			}
		}
	}
	return len(reasons) > 0, xslices.UniqueSorted(reasons)
}

func looksWriteTool(name string) bool {
	n := strings.ToLower(name)
	// "post" dropped: too generic, false-positives on read-only names like
	// "post_processing_helper" / "status_update_checker" style tools.
	for _, k := range []string{"delete", "send", "write", "create", "payment", "email", "update"} {
		if strings.Contains(n, k) {
			return true
		}
	}
	return false
}

func index(arts []manifest.ArtifactRef) map[string]manifest.ArtifactRef {
	m := make(map[string]manifest.ArtifactRef, len(arts))
	for _, a := range arts {
		m[a.Kind+":"+a.ID] = a
	}
	return m
}

func blastRadius(m *manifest.Manifest, changed map[string]bool) (agents, evals []string) {
	agentSet := map[string]bool{}
	evalSet := map[string]bool{}

	for k := range changed {
		if strings.HasPrefix(k, "agent:") {
			agentSet[strings.TrimPrefix(k, "agent:")] = true
		}
		if strings.HasPrefix(k, "eval:") {
			evalSet[strings.TrimPrefix(k, "eval:")] = true
		}
		if strings.HasPrefix(k, "env:") {
			for _, a := range m.Agents {
				agentSet[a.ID] = true
			}
		}
	}

	for _, e := range m.Graph {
		if !strings.HasPrefix(e.From, "agent:") {
			continue
		}
		aid := strings.TrimPrefix(e.From, "agent:")
		if changed[e.To] || changed[e.From] {
			agentSet[aid] = true
		}
	}

	for _, a := range m.Agents {
		check := func(kind string, ids []string) {
			for _, id := range ids {
				if changed[kind+":"+id] {
					agentSet[a.ID] = true
				}
			}
		}
		check("model", a.Models)
		check("prompt", a.Prompts)
		check("tool", a.Tools)
		check("skill", a.Skills)
		check("mcp", a.MCP)
		check("eval", a.Evals)
		for _, id := range a.Evals {
			if agentSet[a.ID] {
				evalSet[id] = true
			}
		}
	}

	for id := range agentSet {
		agents = append(agents, id)
	}
	for id := range evalSet {
		evals = append(evals, id)
	}
	slices.Sort(agents)
	slices.Sort(evals)
	return agents, evals
}

// HasKind reports whether any change has the given artifact kind (e.g. "mcp").
func HasKind(r *Result, kind string) bool {
	if r == nil {
		return false
	}
	for _, c := range r.Changes {
		if c.Kind == kind {
			return true
		}
	}
	return false
}

// CommentMarker identifies the Airlock PR comment so CI can update it in place.
const CommentMarker = "<!-- airlock-gate -->"

// FormatText renders a boxed human-readable report for the terminal.
func FormatText(r *Result) string {
	lines := []string{
		"base  " + r.BaseID,
		"head  " + r.HeadID,
	}
	if len(r.Changes) == 0 {
		lines = append(lines, "", "no AI artifact changes")
		return out.Frame("airlock", lines)
	}
	lines = append(lines, "", "changed")
	for _, c := range r.Changes {
		g := out.Glyph(c.Status)
		switch c.Status {
		case "added", "removed":
			lines = append(lines, fmt.Sprintf("  %s  %-12s %s", g, c.Kind, c.ID))
		default:
			lines = append(lines, fmt.Sprintf("  %s  %-12s %-20s %s -> %s",
				g, c.Kind, c.ID, short(c.OldHash), short(c.NewHash)))
		}
	}
	lines = append(lines, "", "blast radius")
	agents := "(none linked)"
	if len(r.AffectedAgents) > 0 {
		agents = strings.Join(r.AffectedAgents, ", ")
	}
	lines = append(lines, "  agents  "+agents)
	if len(r.AffectedEvals) > 0 {
		lines = append(lines, "  evals   "+strings.Join(r.AffectedEvals, ", "))
	}
	if r.NeedsApproval {
		lines = append(lines, "")
		for _, reason := range r.ApprovalReasons {
			lines = append(lines, "  "+reason)
		}
	}
	return out.Frame("airlock", lines)
}

func commentAlert(overall string, hasChanges bool) (kind, body string) {
	switch overall {
	case "FAIL":
		return "CAUTION", "**FAIL** This PR failed the Airlock gate."
	case "NEEDS_APPROVAL":
		return "WARNING", "**NEEDS_APPROVAL** Merge stays blocked until this change is approved."
	case "INCONCLUSIVE":
		return "WARNING", "**INCONCLUSIVE** Eval evidence is thin. Raise samples or widen the gate."
	default:
		if !hasChanges {
			return "NOTE", "**PASS** No AI artifact changes in this PR."
		}
		return "TIP", "**PASS** AI artifacts changed. Policy is green."
	}
}

func commentGlyph(status string) string {
	switch status {
	case "added":
		return "+"
	case "removed":
		return "-"
	default:
		return "~"
	}
}

// FormatComment is the GitHub PR comment body. overall is PASS, FAIL,
// NEEDS_APPROVAL, or INCONCLUSIVE. Empty overall is derived from the diff.
func FormatComment(r *Result, overall string) string {
	if overall == "" {
		if r.NeedsApproval {
			overall = "NEEDS_APPROVAL"
		} else {
			overall = "PASS"
		}
	}
	alert, lead := commentAlert(overall, len(r.Changes) > 0)
	var b strings.Builder
	b.WriteString(CommentMarker + "\n")
	b.WriteString("## Airlock\n\n")
	fmt.Fprintf(&b, "> [!%s]\n> %s\n", alert, lead)
	if len(r.Changes) == 0 {
		return b.String()
	}
	agents := "none linked"
	if len(r.AffectedAgents) > 0 {
		agents = strings.Join(r.AffectedAgents, ", ")
	}
	evals := "none"
	if len(r.AffectedEvals) > 0 {
		evals = strings.Join(r.AffectedEvals, ", ")
	}
	b.WriteString("\n| gate | changes | agents | evals |\n|---|---:|---|---|\n")
	fmt.Fprintf(&b, "| **%s** | %d | **%s** | **%s** |\n",
		overall, len(r.Changes), agents, evals)

	b.WriteString("\n### Changes\n\n")
	b.WriteString("|  | kind | id |\n|:---:|---|---|\n")
	for _, c := range r.Changes {
		fmt.Fprintf(&b, "| `%s` | `%s` | `%s` |\n", commentGlyph(c.Status), c.Kind, c.ID)
	}
	if r.NeedsApproval {
		b.WriteString("\n### Why this is blocked\n\n")
		for _, reason := range r.ApprovalReasons {
			fmt.Fprintf(&b, "- %s\n", reason)
		}
	}
	return b.String()
}

func short(h string) string {
	if len(h) > 8 {
		return h[:8]
	}
	return h
}

// HasChanges reports whether any AI artifacts differ.
func HasChanges(r *Result) bool {
	return len(r.Changes) > 0
}
