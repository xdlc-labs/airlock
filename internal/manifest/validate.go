package manifest

import (
	"fmt"
	"sort"
	"strings"
)

// Validate checks structural invariants of a Manifest.
func Validate(m *Manifest) error {
	if m == nil {
		return fmt.Errorf("manifest is nil")
	}
	if m.Version == 0 {
		return fmt.Errorf("manifest version required")
	}
	ids := map[string]bool{}
	check := func(kind, id string) error {
		if strings.TrimSpace(id) == "" {
			return fmt.Errorf("%s: empty id", kind)
		}
		key := kind + ":" + id
		if ids[key] {
			return fmt.Errorf("duplicate %s", key)
		}
		ids[key] = true
		return nil
	}
	for _, a := range m.Agents {
		if err := check("agent", a.ID); err != nil {
			return err
		}
	}
	for _, x := range m.Models {
		if err := check("model", x.ID); err != nil {
			return err
		}
		if x.Model == "" {
			return fmt.Errorf("model %s: empty model string", x.ID)
		}
		if x.ContentHash == "" {
			return fmt.Errorf("model %s: empty content_hash", x.ID)
		}
	}
	for _, x := range m.Prompts {
		if err := check("prompt", x.ID); err != nil {
			return err
		}
		if x.ContentHash == "" {
			return fmt.Errorf("prompt %s: empty content_hash", x.ID)
		}
	}
	for _, x := range m.Tools {
		if err := check("tool", x.ID); err != nil {
			return err
		}
		if x.SchemaHash == "" {
			return fmt.Errorf("tool %s: empty schema_hash", x.ID)
		}
	}
	for _, x := range m.Skills {
		if err := check("skill", x.ID); err != nil {
			return err
		}
		if x.ContentHash == "" {
			return fmt.Errorf("skill %s: empty content_hash", x.ID)
		}
	}
	for _, x := range m.MCPServers {
		if err := check("mcp", x.ID); err != nil {
			return err
		}
		if x.SchemaHash == "" {
			return fmt.Errorf("mcp %s: empty schema_hash", x.ID)
		}
	}
	for _, x := range m.Evals {
		if err := check("eval", x.ID); err != nil {
			return err
		}
	}
	for _, x := range m.Envs {
		if err := check("env", x.ID); err != nil {
			return err
		}
		if x.ContentHash == "" {
			return fmt.Errorf("env %s: empty content_hash", x.ID)
		}
	}
	for _, e := range m.Graph {
		if e.From == "" || e.To == "" {
			return fmt.Errorf("graph edge missing from/to")
		}
	}
	return nil
}

// mcpArtifactHash binds an MCP server's granted permissions into its artifact
// hash. SchemaHash alone describes the server's tool surface, and in an APM
// lockfile it is often a pinned literal, so widening `permissions:` would not
// move it. Diff only inspects artifacts whose hash changed, so a
// permissions-only edit used to slip through the approval gate entirely.
// Permissions are sorted so reordering is not reported as a change.
func mcpArtifactHash(x MCPServer) string {
	if len(x.Permissions) == 0 {
		return x.SchemaHash
	}
	perms := make([]string, len(x.Permissions))
	copy(perms, x.Permissions)
	sort.Strings(perms)
	return HashString(x.SchemaHash + "|perms:" + strings.Join(perms, ","))
}

// Artifacts flattens hashed units from the manifest for snapshotting.
func Artifacts(m *Manifest) []ArtifactRef {
	out := make([]ArtifactRef, 0, len(m.Models)+len(m.Prompts)+len(m.Tools)+len(m.Skills)+len(m.MCPServers)+len(m.Evals)+len(m.Agents)+len(m.Envs))
	for _, x := range m.Models {
		out = append(out, ArtifactRef{Kind: "model", ID: x.ID, Hash: x.ContentHash})
	}
	for _, x := range m.Prompts {
		out = append(out, ArtifactRef{Kind: "prompt", ID: x.ID, Hash: x.ContentHash})
	}
	for _, x := range m.Tools {
		out = append(out, ArtifactRef{Kind: "tool", ID: x.ID, Hash: x.SchemaHash})
	}
	for _, x := range m.Skills {
		out = append(out, ArtifactRef{Kind: "skill", ID: x.ID, Hash: x.ContentHash})
	}
	for _, x := range m.MCPServers {
		out = append(out, ArtifactRef{Kind: "mcp", ID: x.ID, Hash: mcpArtifactHash(x)})
	}
	for _, x := range m.Evals {
		h := HashString(x.Path + "|" + x.Kind)
		out = append(out, ArtifactRef{Kind: "eval", ID: x.ID, Hash: h})
	}
	for _, x := range m.Envs {
		out = append(out, ArtifactRef{Kind: "env", ID: x.ID, Hash: x.ContentHash})
	}
	for _, x := range m.Dependencies {
		out = append(out, ArtifactRef{Kind: "dependency", ID: x.ID, Hash: x.Hash})
	}
	for _, a := range m.Agents {
		payload := a.ID + "|" + strings.Join(a.Models, ",") + "|" + strings.Join(a.Prompts, ",") +
			"|" + strings.Join(a.Tools, ",") + "|" + strings.Join(a.Skills, ",") + "|" + strings.Join(a.MCP, ",")
		out = append(out, ArtifactRef{Kind: "agent", ID: a.ID, Hash: HashString(payload)})
	}
	return out
}

// BuildGraph synthesizes agent→dep edges from agent link fields.
func BuildGraph(m *Manifest) {
	m.Graph = nil
	for _, a := range m.Agents {
		from := "agent:" + a.ID
		for _, id := range a.Models {
			m.Graph = append(m.Graph, Edge{From: from, To: "model:" + id, Kind: "uses"})
		}
		for _, id := range a.Prompts {
			m.Graph = append(m.Graph, Edge{From: from, To: "prompt:" + id, Kind: "uses"})
		}
		for _, id := range a.Tools {
			m.Graph = append(m.Graph, Edge{From: from, To: "tool:" + id, Kind: "uses"})
		}
		for _, id := range a.Skills {
			m.Graph = append(m.Graph, Edge{From: from, To: "skill:" + id, Kind: "uses"})
		}
		for _, id := range a.MCP {
			m.Graph = append(m.Graph, Edge{From: from, To: "mcp:" + id, Kind: "uses"})
		}
		for _, id := range a.Evals {
			m.Graph = append(m.Graph, Edge{From: from, To: "eval:" + id, Kind: "uses"})
		}
	}
}
