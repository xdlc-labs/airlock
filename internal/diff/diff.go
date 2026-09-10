package diff

import (
	"cmp"
	"fmt"
	"slices"
	"strings"

	"github.com/xdlc-labs/airlock/internal/manifest"
	"github.com/xdlc-labs/airlock/internal/out"
	"github.com/xdlc-labs/airlock/internal/xslices"
)

// Change is one artifact that added, removed, or changed hash.
type Change struct {
	Kind   string `json:"kind"`
	ID     string `json:"id"`
	Status string `json:"status"` // added|removed|changed
	// Path is where the artifact came from: a file for prompts, skills, and
	// evals, the config or lockfile that declared it otherwise. Empty when the
	// manifest records no origin. Reviewers need it to find what moved.
	Path    string `json:"path,omitempty"`
	OldHash string `json:"old_hash,omitempty"`
	NewHash string `json:"new_hash,omitempty"`
}

// Result is a static blast-radius diff between two snapshots.
type Result struct {
	BaseID         string   `json:"base_id"`
	HeadID         string   `json:"head_id"`
	Changes        []Change `json:"changes"`
	AffectedAgents []string `json:"affected_agents"`
	AffectedEvals  []string `json:"affected_evals"`
	// UnlinkedChanges names changed artifacts that reach no agent in either
	// manifest. Their blast radius is unknown, not empty: nothing declared who
	// uses them, so eval selection cannot narrow to them and a reader must not
	// read "agents: none linked" as "affects nothing".
	UnlinkedChanges []string `json:"unlinked_changes,omitempty"`
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

	for i := range r.Changes {
		r.Changes[i].Path = artifactPath(&head.Manifest, &base.Manifest, r.Changes[i].Kind, r.Changes[i].ID)
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
	r.UnlinkedChanges = unlinkedChanges(&head.Manifest, &base.Manifest, r.Changes)

	r.NeedsApproval, r.ApprovalReasons = permissionExpansion(base, head, r.Changes)
	if depNeeds, depReasons := dependencyExpansion(r.Changes); depNeeds {
		r.NeedsApproval = true
		r.ApprovalReasons = xslices.UniqueSorted(append(r.ApprovalReasons, depReasons...))
	}
	if wireNeeds, wireReasons := agentWiringExpansion(base, head, r.Changes); wireNeeds {
		r.NeedsApproval = true
		r.ApprovalReasons = xslices.UniqueSorted(append(r.ApprovalReasons, wireReasons...))
	}
	return r
}

// unlinkedChanges lists changed artifacts that no agent declares. Kinds that are
// not wiring targets (agents themselves, evals, envs, and packages) are left
// out: an agent does not "use" them, so silence about them means nothing.
func unlinkedChanges(head, base *manifest.Manifest, changes []Change) []string {
	linked := map[string]bool{}
	for _, m := range []*manifest.Manifest{head, base} {
		if m == nil {
			continue
		}
		for _, a := range m.Agents {
			for kind, ids := range map[string][]string{
				"model": a.Models, "prompt": a.Prompts,
				"tool": a.Tools, "skill": a.Skills, "mcp": a.MCP,
			} {
				for _, id := range ids {
					linked[kind+":"+id] = true
				}
			}
		}
	}
	var out []string
	for _, c := range changes {
		switch c.Kind {
		case "agent", "eval", "env", "dependency":
			continue
		}
		if !linked[c.Kind+":"+c.ID] {
			out = append(out, c.Kind+":"+c.ID)
		}
	}
	return xslices.UniqueSorted(out)
}

// agentWiringExpansion flags an agent that gained a capability by wiring, not by
// the capability itself changing. Attaching an existing MCP server or write tool
// to an agent leaves that artifact's hash untouched, so before this the only
// signal was the agent's own hash moving, with no reason attached to it. Agents
// added in this diff are skipped: their parts show up as added artifacts, which
// permissionExpansion already reasons about.
func agentWiringExpansion(base, head *manifest.Snapshot, changes []Change) (bool, []string) {
	baseAgents := map[string]manifest.Agent{}
	for _, a := range base.Manifest.Agents {
		baseAgents[a.ID] = a
	}
	var reasons []string
	for _, c := range changes {
		if c.Kind != "agent" || c.Status != "changed" {
			continue
		}
		old, had := baseAgents[c.ID]
		if !had {
			continue
		}
		for _, a := range head.Manifest.Agents {
			if a.ID != c.ID {
				continue
			}
			for _, w := range []struct {
				kind string
				was  []string
				now  []string
			}{
				{"mcp", old.MCP, a.MCP},
				{"tool", old.Tools, a.Tools},
				{"skill", old.Skills, a.Skills},
				{"model", old.Models, a.Models},
			} {
				for _, id := range w.now {
					if slices.Contains(w.was, id) {
						continue
					}
					reasons = append(reasons,
						fmt.Sprintf("agent %s now uses %s %s (needs review)", a.ID, w.kind, id))
				}
			}
		}
	}
	return len(reasons) > 0, xslices.UniqueSorted(reasons)
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
	if len(r.UnlinkedChanges) > 0 {
		lines = append(lines, "  unknown "+strings.Join(r.UnlinkedChanges, ", "),
			"          (no agent declares these; blast radius is unknown, not empty)")
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

// CommentOptions carries what the comment cannot derive from the diff alone.
type CommentOptions struct {
	// ApproveCmd is the command that records the human decision. Given here so
	// it sits with the reason the merge is blocked instead of below the eval
	// tables the caller appends afterwards. Empty when approval is already on
	// the ledger, or not needed.
	ApproveCmd string
	// ReviewUnblock says the sign-off is a pull request review rather than a
	// ledger entry, so the comment asks for a review and not for a command.
	ReviewUnblock bool
	// ApprovedBy names who has already signed off, and how. Empty when nobody
	// has, or when approval is not needed.
	ApprovedBy string
}

// MaxCommentBytes is GitHub's limit for one issue comment. A body over it is
// rejected outright, so a gate that produced too much text would report nothing
// at all.
const MaxCommentBytes = 65536

// commentMaxRows bounds the change table. A PR that rewrites a prompt library
// should still leave the verdict and the reasons readable.
const commentMaxRows = 30

// FormatComment is the GitHub PR comment body. overall is PASS, FAIL,
// NEEDS_APPROVAL, or INCONCLUSIVE. Empty overall is derived from the diff.
func FormatComment(r *Result, overall string) string {
	return FormatCommentWith(r, overall, CommentOptions{})
}

// FormatCommentWith is FormatComment with the caller's extras. The order is
// deliberate: verdict, then what to do about it, then the detail. A reviewer who
// reads only the first screen should still know whether the merge is blocked and
// what unblocks it.
func FormatCommentWith(r *Result, overall string, opt CommentOptions) string {
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
		b.WriteString(commentSnapshots(r))
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

	if r.NeedsApproval {
		b.WriteString("\n### Why this is blocked\n\n")
		for _, reason := range r.ApprovalReasons {
			fmt.Fprintf(&b, "- %s\n", reason)
		}
		switch {
		case opt.ApprovedBy != "":
			fmt.Fprintf(&b, "\nSigned off by %s. The gate treats this change as approved.\n", opt.ApprovedBy)
		case opt.ReviewUnblock:
			b.WriteString("\nA human has to sign off. Approve this pull request as a reviewer with write access" +
				" and the gate re-runs. The review has to be on the current commit: a new push asks again.\n")
		case opt.ApproveCmd != "":
			b.WriteString("\nA human has to sign off. Record it, then re-run the gate:\n\n")
			fmt.Fprintf(&b, "```bash\n%s\n```\n", opt.ApproveCmd)
		}
	}

	b.WriteString("\n### Changes\n\n")
	b.WriteString("|  | kind | id | where | hash |\n|:---:|---|---|---|---|\n")
	shown := r.Changes
	if len(shown) > commentMaxRows {
		shown = shown[:commentMaxRows]
	}
	for _, c := range shown {
		fmt.Fprintf(&b, "| `%s` | `%s` | `%s` | %s | %s |\n",
			commentGlyph(c.Status), c.Kind, c.ID, commentWhere(c), commentHash(c))
	}
	if rest := len(r.Changes) - len(shown); rest > 0 {
		fmt.Fprintf(&b, "\n_%d more not shown. `airlock diff --base %s --head %s` lists them all._\n",
			rest, r.BaseID, r.HeadID)
	}
	if len(r.UnlinkedChanges) > 0 {
		fmt.Fprintf(&b, "\n> [!NOTE]\n> No agent declares `%s`, so the blast radius is unknown rather than"+
			" empty and eval selection could not narrow to it. Declaring it in `apm.lock.yaml` gates it properly.\n",
			strings.Join(r.UnlinkedChanges, "`, `"))
	}
	b.WriteString(commentSnapshots(r))
	return b.String()
}

func commentWhere(c Change) string {
	if c.Path == "" {
		return "—"
	}
	return "`" + c.Path + "`"
}

func commentHash(c Change) string {
	switch c.Status {
	case "added":
		return "`" + short(c.NewHash) + "`"
	case "removed":
		return "~~`" + short(c.OldHash) + "`~~"
	default:
		return fmt.Sprintf("`%s` → `%s`", short(c.OldHash), short(c.NewHash))
	}
}

// commentSnapshots names the two snapshots compared and how to run the same
// comparison locally, folded away because it is reference, not news.
func commentSnapshots(r *Result) string {
	return fmt.Sprintf("\n<details><summary>Snapshots</summary>\n\n"+
		"base `%s` → head `%s`\n\n```bash\nairlock diff --base %s --head %s\n```\n</details>\n",
		r.BaseID, r.HeadID, r.BaseID, r.HeadID)
}

// ClampComment trims a comment body to limit bytes, keeping the head of it, so an
// oversized gate report still posts. It cuts on a line boundary and says that it
// cut.
func ClampComment(body string, limit int) string {
	if limit <= 0 || len(body) <= limit {
		return body
	}
	const note = "\n\n_Report truncated to fit one comment. Run `airlock ci` locally for the full report._\n"
	keep := limit - len(note)
	if keep < 0 {
		return body[:limit]
	}
	cut := body[:keep]
	if i := strings.LastIndexByte(cut, '\n'); i > 0 {
		cut = cut[:i]
	}
	return cut + note
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

// artifactPath finds where an artifact lives, preferring the head manifest so a
// moved file reports its new home, and falling back to base for removals.
func artifactPath(head, base *manifest.Manifest, kind, id string) string {
	if p := pathIn(head, kind, id); p != "" {
		return p
	}
	return pathIn(base, kind, id)
}

func pathIn(m *manifest.Manifest, kind, id string) string {
	if m == nil {
		return ""
	}
	switch kind {
	case "prompt":
		for _, x := range m.Prompts {
			if x.ID == id {
				return cmp.Or(x.Path, x.RemoteRef, x.Source)
			}
		}
	case "skill":
		for _, x := range m.Skills {
			if x.ID == id {
				return cmp.Or(x.Path, x.Source)
			}
		}
	case "eval":
		for _, x := range m.Evals {
			if x.ID == id {
				return cmp.Or(x.Path, x.Source)
			}
		}
	case "model":
		for _, x := range m.Models {
			if x.ID == id {
				return x.Source
			}
		}
	case "tool":
		for _, x := range m.Tools {
			if x.ID == id {
				return x.Source
			}
		}
	case "mcp":
		for _, x := range m.MCPServers {
			if x.ID == id {
				return x.Source
			}
		}
	case "env":
		for _, x := range m.Envs {
			if x.ID == id {
				return x.Source
			}
		}
	case "dependency":
		for _, x := range m.Dependencies {
			if x.ID == id {
				return x.Source
			}
		}
	case "agent":
		for _, x := range m.Agents {
			if x.ID == id {
				return x.Source
			}
		}
	}
	return ""
}
