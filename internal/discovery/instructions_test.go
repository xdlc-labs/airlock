package discovery

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func write(t *testing.T, root, relPath, body string) {
	t.Helper()
	full := filepath.Join(root, filepath.FromSlash(relPath))
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(full, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func promptPaths(t *testing.T, root string) map[string]string {
	t.Helper()
	m, err := Scan(root)
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]string{}
	for _, p := range m.Prompts {
		out[p.Path] = p.Source
	}
	return out
}

func TestInstructionFilesAreDiscovered(t *testing.T) {
	root := t.TempDir()
	write(t, root, "CLAUDE.md", "always cite the order id")
	write(t, root, "AGENTS.md", "repo conventions")
	write(t, root, "GEMINI.md", "gemini notes")
	write(t, root, ".cursorrules", "legacy cursor rules")
	write(t, root, ".windsurfrules", "windsurf rules")
	write(t, root, ".github/copilot-instructions.md", "copilot notes")
	write(t, root, ".github/instructions/api.instructions.md", "api notes")
	write(t, root, ".claude/agents/reviewer.md", "you are a reviewer")
	write(t, root, "packages/api/CLAUDE.md", "api package notes")

	got := promptPaths(t, root)
	for _, want := range []string{
		"CLAUDE.md", "AGENTS.md", "GEMINI.md", ".cursorrules", ".windsurfrules",
		".github/copilot-instructions.md", ".github/instructions/api.instructions.md",
		".claude/agents/reviewer.md", "packages/api/CLAUDE.md",
	} {
		if _, ok := got[want]; !ok {
			t.Errorf("%s was not discovered; prompts: %v", want, got)
		}
	}
	if got["CLAUDE.md"] != "instructions" {
		t.Errorf("source = %q, want instructions", got["CLAUDE.md"])
	}
}

func TestInstructionFilesChangeTheManifestHash(t *testing.T) {
	root := t.TempDir()
	write(t, root, "CLAUDE.md", "v1")
	before, err := Scan(root)
	if err != nil {
		t.Fatal(err)
	}
	write(t, root, "CLAUDE.md", "v2")
	after, err := Scan(root)
	if err != nil {
		t.Fatal(err)
	}
	var h1, h2 string
	for _, p := range before.Prompts {
		if p.Path == "CLAUDE.md" {
			h1 = p.ContentHash
		}
	}
	for _, p := range after.Prompts {
		if p.Path == "CLAUDE.md" {
			h2 = p.ContentHash
		}
	}
	if h1 == "" || h2 == "" {
		t.Fatal("CLAUDE.md missing from one of the scans")
	}
	if h1 == h2 {
		t.Fatal("editing CLAUDE.md must move its content hash")
	}
}

func TestVendoredInstructionsAreSkipped(t *testing.T) {
	root := t.TempDir()
	write(t, root, "node_modules/some-pkg/CLAUDE.md", "vendor")
	write(t, root, "testdata/fixture-repo/AGENTS.md", "fixture")
	write(t, root, ".venv/lib/CLAUDE.md", "env")
	got := promptPaths(t, root)
	if len(got) != 0 {
		t.Fatalf("expected no prompts from vendored trees, got %v", got)
	}
}

func TestAPMDeclaredPromptIsNotDuplicated(t *testing.T) {
	root := t.TempDir()
	write(t, root, "CLAUDE.md", "instructions")
	write(t, root, "apm.lock.yaml", `version: 1
prompts:
  claude-md:
    name: claude-md
    path: CLAUDE.md
    hash: ""
`)
	m, err := Scan(root)
	if err != nil {
		t.Fatal(err)
	}
	count := 0
	for _, p := range m.Prompts {
		if p.Path == "CLAUDE.md" {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("CLAUDE.md should appear once, got %d: %+v", count, m.Prompts)
	}
}

func TestVSCodeStyleMCPServersAreTrackedIndividually(t *testing.T) {
	root := t.TempDir()
	cfg := map[string]any{"servers": map[string]any{
		"docs":  map[string]any{"url": "https://example.invalid/mcp"},
		"local": map[string]any{"command": "docs-server"},
	}}
	data, err := json.Marshal(cfg)
	if err != nil {
		t.Fatal(err)
	}
	write(t, root, ".vscode/mcp.json", string(data))

	m, err := Scan(root)
	if err != nil {
		t.Fatal(err)
	}
	ids := map[string]bool{}
	for _, s := range m.MCPServers {
		ids[s.ID] = true
	}
	if !ids["mcp-docs"] || !ids["mcp-local"] {
		t.Fatalf("expected one artifact per server, got %+v", m.MCPServers)
	}
	if ids["mcp-config-.vscode-mcp.json"] {
		t.Fatalf("the whole file should not also be hashed as one artifact: %+v", m.MCPServers)
	}
}

func TestGeminiSettingsMCPServersAreFound(t *testing.T) {
	root := t.TempDir()
	write(t, root, ".gemini/settings.json", `{"mcpServers": {"fs": {"command": "fs-server"}}}`)
	m, err := Scan(root)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, s := range m.MCPServers {
		if s.ID == "mcp-fs" {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected mcp-fs from .gemini/settings.json, got %+v", m.MCPServers)
	}
}
