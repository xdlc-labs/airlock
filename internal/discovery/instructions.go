package discovery

import (
	"path/filepath"
	"strings"

	"github.com/xdlc-labs/airlock/internal/manifest"
)

// instructionGlobs are the agent instruction files that coding agents read on
// every run. They change agent behavior as directly as any file in prompts/,
// and in most repos they are the file people actually edit. Nested ones count:
// a monorepo package's CLAUDE.md steers the agent while it works in there.
var instructionGlobs = []string{
	"**/CLAUDE.md",
	"**/AGENTS.md",
	"**/GEMINI.md",
	"**/.cursorrules",
	"**/.windsurfrules",
	".github/copilot-instructions.md",
	".github/instructions/*.md",
	".claude/agents/*.md",
}

// scanInstructionFiles records agent instruction files as prompts, which is what
// they are: content that reaches the model on every turn.
func scanInstructionFiles(root string, m *manifest.Manifest) error {
	seen := map[string]bool{}
	for _, p := range m.Prompts {
		seen[p.ID] = true
		if p.Path != "" {
			seen[filepath.Clean(p.Path)] = true
		}
	}
	var files []string
	for _, pat := range instructionGlobs {
		matches, err := doubleStarGlob(root, pat)
		if err != nil {
			return err
		}
		files = append(files, matches...)
	}
	for _, full := range unique(files) {
		relPath := rel(root, full)
		if skipInstructionPath(relPath) {
			continue
		}
		id := "prompt-" + slug(relPath)
		if seen[id] || seen[relPath] {
			continue
		}
		h, err := manifest.HashFile(full)
		if err != nil {
			m.Unpinned = append(m.Unpinned, manifest.UnpinnedRisk{Artifact: "prompt:" + id, Reason: err.Error()})
			continue
		}
		m.Prompts = append(m.Prompts, manifest.Prompt{
			ID: id, Path: relPath, ContentHash: h, Source: "instructions",
		})
		m.Sources = append(m.Sources, manifest.Source{Kind: "instructions", Path: relPath})
		seen[id] = true
	}
	return nil
}

// skipInstructionPath drops copies that are not this repo's own instructions:
// vendored agent directories and dependency trees the glob still reaches.
func skipInstructionPath(relPath string) bool {
	for _, seg := range strings.Split(filepath.ToSlash(relPath), "/") {
		switch seg {
		case "testdata", "fixtures", "third_party", "site-packages", ".venv", "venv":
			return true
		}
	}
	return false
}
