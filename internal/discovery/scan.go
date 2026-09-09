package discovery

import (
	"cmp"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/xdlc-labs/airlock/internal/manifest"
	"gopkg.in/yaml.v3"
)

// Scan discovers AI artifacts under root and returns a Manifest.
func Scan(root string) (*manifest.Manifest, error) {
	abs, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	m := &manifest.Manifest{
		Version:     manifest.Version,
		GeneratedAt: time.Now().UTC(),
		Root:        abs,
	}

	if err := importAPM(abs, m); err != nil {
		return nil, fmt.Errorf("apm: %w", err)
	}
	if err := scanLockfileDeps(abs, m); err != nil {
		return nil, fmt.Errorf("lockfiles: %w", err)
	}
	if err := scanPrompts(abs, m); err != nil {
		return nil, fmt.Errorf("prompts: %w", err)
	}
	if err := scanSkills(abs, m); err != nil {
		return nil, fmt.Errorf("skills: %w", err)
	}
	if err := scanCursorRules(abs, m); err != nil {
		return nil, fmt.Errorf("cursor-rules: %w", err)
	}
	mcpConfigs, err := scanMCP(abs, m)
	if err != nil {
		return nil, fmt.Errorf("mcp: %w", err)
	}
	if err := scanFrameworkStack(abs, m); err != nil {
		return nil, fmt.Errorf("framework-stack: %w", err)
	}
	if len(mcpConfigs) > 0 {
		enrichMCPSchemas(context.Background(), nil, m, mcpConfigs)
	}
	if err := scanModelHeuristics(abs, m); err != nil {
		return nil, fmt.Errorf("models: %w", err)
	}
	if err := scanEvalHooks(abs, m); err != nil {
		return nil, fmt.Errorf("evals: %w", err)
	}
	if err := scanEnvs(abs, m); err != nil {
		return nil, fmt.Errorf("envs: %w", err)
	}

	ensureDefaultAgent(m)
	manifest.BuildGraph(m)
	if err := manifest.Validate(m); err != nil {
		return nil, err
	}
	return m, nil
}

func ensureDefaultAgent(m *manifest.Manifest) {
	if len(m.Agents) > 0 {
		return
	}
	a := manifest.Agent{ID: "default", Name: "default", Source: "inferred"}
	for _, x := range m.Models {
		a.Models = append(a.Models, x.ID)
	}
	for _, x := range m.Prompts {
		a.Prompts = append(a.Prompts, x.ID)
	}
	for _, x := range m.Tools {
		a.Tools = append(a.Tools, x.ID)
	}
	for _, x := range m.Skills {
		a.Skills = append(a.Skills, x.ID)
	}
	for _, x := range m.MCPServers {
		a.MCP = append(a.MCP, x.ID)
	}
	for _, x := range m.Evals {
		a.Evals = append(a.Evals, x.ID)
	}
	if len(a.Models)+len(a.Prompts)+len(a.Tools)+len(a.Skills)+len(a.MCP) == 0 {
		return
	}
	m.Agents = append(m.Agents, a)
}

// --- APM ---

type apmLock struct {
	Version  int                   `yaml:"version"`
	Packages map[string]apmPackage `yaml:"packages"`
	Agents   map[string]apmAgent   `yaml:"agents"`
	Skills   map[string]apmSkill   `yaml:"skills"`
	Prompts  map[string]apmPrompt  `yaml:"prompts"`
	MCP      map[string]apmMCP     `yaml:"mcp"`
	// looser catch-alls
	Dependencies map[string]apmDep `yaml:"dependencies"`
}

type apmPackage struct {
	Name    string `yaml:"name"`
	Version string `yaml:"version"`
	Hash    string `yaml:"hash"`
	Kind    string `yaml:"kind"`
}

type apmAgent struct {
	Name    string   `yaml:"name"`
	Model   string   `yaml:"model"`
	Prompts []string `yaml:"prompts"`
	Skills  []string `yaml:"skills"`
	MCP     []string `yaml:"mcp"`
	Hash    string   `yaml:"hash"`
}

type apmSkill struct {
	Name string `yaml:"name"`
	Hash string `yaml:"hash"`
	Path string `yaml:"path"`
}

type apmPrompt struct {
	Name string `yaml:"name"`
	Hash string `yaml:"hash"`
	Path string `yaml:"path"`
}

type apmMCP struct {
	Name        string   `yaml:"name"`
	Hash        string   `yaml:"hash"`
	Permissions []string `yaml:"permissions"`
}

type apmDep struct {
	Hash    string `yaml:"hash"`
	Version string `yaml:"version"`
	Type    string `yaml:"type"`
	Path    string `yaml:"path"`
	Content string `yaml:"content"`
	Model   string `yaml:"model"`
}

func importAPM(root string, m *manifest.Manifest) error {
	for _, name := range []string{"apm.lock.yaml", "apm.lock.yml", "apm.yml", "apm.yaml"} {
		path := filepath.Join(root, name)
		data, err := os.ReadFile(path)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return err
		}
		m.Sources = append(m.Sources, manifest.Source{Kind: name, Path: rel(root, path)})
		var lock apmLock
		if err := yaml.Unmarshal(data, &lock); err != nil {
			return fmt.Errorf("parse %s: %w", name, err)
		}
		applyAPM(root, m, &lock, rel(root, path))
		return nil // first match wins
	}
	return nil
}

func applyAPM(root string, m *manifest.Manifest, lock *apmLock, src string) {
	for id, p := range lock.Prompts {
		h := p.Hash
		path := p.Path
		if h == "" && path != "" {
			full := path
			if !filepath.IsAbs(full) {
				full = filepath.Join(root, path)
			}
			if fh, err := manifest.HashFile(full); err == nil {
				h = fh
			} else {
				m.Unpinned = append(m.Unpinned, manifest.UnpinnedRisk{
					Artifact: "prompt:" + id, Reason: err.Error(),
				})
				h = manifest.HashString(id + "|" + path)
			}
		}
		if h == "" {
			h = manifest.HashString(id)
		}
		m.Prompts = append(m.Prompts, manifest.Prompt{
			ID: id, Path: path, ContentHash: h, Source: src,
		})
	}
	for id, s := range lock.Skills {
		h := s.Hash
		path := s.Path
		if h == "" && path != "" {
			full := path
			if !filepath.IsAbs(full) {
				full = filepath.Join(root, path)
			}
			if fh, err := manifest.HashFile(full); err == nil {
				h = fh
			} else {
				m.Unpinned = append(m.Unpinned, manifest.UnpinnedRisk{
					Artifact: "skill:" + id, Reason: err.Error(),
				})
				h = manifest.HashString(id + "|" + path)
			}
		}
		if h == "" {
			h = manifest.HashString(id)
		}
		m.Skills = append(m.Skills, manifest.Skill{
			ID: id, Name: coalesce(s.Name, id), Path: path, ContentHash: h, Source: src,
		})
	}
	for id, mc := range lock.MCP {
		h := mc.Hash
		if h == "" {
			h = manifest.HashString(id + strings.Join(mc.Permissions, ","))
			m.Unpinned = append(m.Unpinned, manifest.UnpinnedRisk{
				Artifact: "mcp:" + id, Reason: "no schema hash in APM lock",
			})
		}
		m.MCPServers = append(m.MCPServers, manifest.MCPServer{
			ID: id, Name: coalesce(mc.Name, id), SchemaHash: h, Permissions: mc.Permissions, Source: src,
		})
	}
	for id, dep := range lock.Dependencies {
		kind := strings.ToLower(dep.Type)
		h := dep.Hash
		if h == "" && dep.Content != "" {
			h = manifest.HashString(dep.Content)
		}
		if h == "" && dep.Path != "" {
			if fh, err := manifest.HashFile(filepath.Join(root, dep.Path)); err == nil {
				h = fh
			}
		}
		if h == "" {
			h = manifest.HashString(id + "|" + dep.Version)
		}
		switch kind {
		case "prompt", "instruction":
			m.Prompts = append(m.Prompts, manifest.Prompt{ID: id, Path: dep.Path, ContentHash: h, Source: src})
		case "skill":
			m.Skills = append(m.Skills, manifest.Skill{ID: id, Name: id, Path: dep.Path, ContentHash: h, Source: src})
		case "tool":
			m.Tools = append(m.Tools, manifest.Tool{ID: id, Name: id, SchemaHash: h, SideEffect: "unknown", Source: src})
		case "mcp":
			m.MCPServers = append(m.MCPServers, manifest.MCPServer{ID: id, Name: id, SchemaHash: h, Source: src})
		case "model":
			m.Models = append(m.Models, manifest.Model{
				ID: id, Model: coalesce(dep.Model, id), ContentHash: h, Source: src, Confidence: "high",
			})
		default:
			// not a known AI-artifact kind → track as a plain supply-chain dependency
			if !hasDependency(m, id) {
				h := dep.Hash
				if h == "" {
					h = manifest.HashString(id + "|" + dep.Version)
				}
				m.Dependencies = append(m.Dependencies, manifest.Dependency{
					ID: id, Ecosystem: coalesce(dep.Type, "package"), Version: dep.Version, Hash: h, Source: src,
				})
			}
		}
	}
	for id, pkg := range lock.Packages {
		h := pkg.Hash
		if h == "" {
			h = manifest.HashString(pkg.Name + "|" + pkg.Version)
		}
		switch strings.ToLower(pkg.Kind) {
		case "prompt":
			if !hasPrompt(m, id) {
				m.Prompts = append(m.Prompts, manifest.Prompt{ID: id, ContentHash: h, Source: src})
			}
		case "skill":
			if !hasSkill(m, id) {
				m.Skills = append(m.Skills, manifest.Skill{ID: id, Name: coalesce(pkg.Name, id), ContentHash: h, Source: src})
			}
		case "tool":
			if !hasTool(m, id) {
				m.Tools = append(m.Tools, manifest.Tool{ID: id, Name: coalesce(pkg.Name, id), SchemaHash: h, Source: src})
			}
		case "mcp":
			if !hasMCP(m, id) {
				m.MCPServers = append(m.MCPServers, manifest.MCPServer{ID: id, Name: coalesce(pkg.Name, id), SchemaHash: h, Source: src})
			}
		case "model":
			if !hasModel(m, id) {
				m.Models = append(m.Models, manifest.Model{ID: id, Model: coalesce(pkg.Name, id), ContentHash: h, Source: src, Confidence: "high"})
			}
		default:
			// unrecognized kind (npm/pip/go/cargo/…) → plain supply-chain dependency
			if !hasDependency(m, id) {
				m.Dependencies = append(m.Dependencies, manifest.Dependency{
					ID: id, Ecosystem: coalesce(pkg.Kind, "package"), Version: pkg.Version, Hash: h, Source: src,
				})
			}
		}
	}
	for id, a := range lock.Agents {
		ag := manifest.Agent{
			ID: id, Name: coalesce(a.Name, id), Source: src,
			Prompts: a.Prompts, MCP: a.MCP, Skills: a.Skills,
		}
		if a.Model != "" {
			mid := "model-" + slug(a.Model)
			if !hasModel(m, mid) {
				m.Models = append(m.Models, manifest.Model{
					ID: mid, Model: a.Model, ContentHash: manifest.HashString(a.Model),
					Source: src, Confidence: "high",
				})
			}
			ag.Models = []string{mid}
		}
		m.Agents = append(m.Agents, ag)
	}
}

func hasPrompt(m *manifest.Manifest, id string) bool {
	for _, x := range m.Prompts {
		if x.ID == id {
			return true
		}
	}
	return false
}

func hasTool(m *manifest.Manifest, id string) bool {
	for _, x := range m.Tools {
		if x.ID == id {
			return true
		}
	}
	return false
}

func hasSkill(m *manifest.Manifest, id string) bool {
	for _, x := range m.Skills {
		if x.ID == id {
			return true
		}
	}
	return false
}

func hasMCP(m *manifest.Manifest, id string) bool {
	for _, x := range m.MCPServers {
		if x.ID == id {
			return true
		}
	}
	return false
}

func hasModel(m *manifest.Manifest, id string) bool {
	for _, x := range m.Models {
		if x.ID == id {
			return true
		}
	}
	return false
}

func hasDependency(m *manifest.Manifest, id string) bool {
	for _, x := range m.Dependencies {
		if x.ID == id {
			return true
		}
	}
	return false
}

// --- prompts ---

func scanPrompts(root string, m *manifest.Manifest) error {
	seen := map[string]bool{}
	for _, p := range m.Prompts {
		seen[p.ID] = true
		if p.Path != "" {
			seen[filepath.Clean(p.Path)] = true
		}
	}
	patterns := []string{
		"prompts/**/*.md", "prompts/**/*.txt", "prompts/**/*.prompt",
		"**/*.prompt.md", "**/system.prompt",
	}
	var files []string
	for _, pat := range patterns {
		matches, err := doubleStarGlob(root, pat)
		if err != nil {
			return err
		}
		files = append(files, matches...)
	}
	for _, full := range unique(files) {
		relPath := rel(root, full)
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
			ID: id, Path: relPath, ContentHash: h, Source: "file",
		})
		m.Sources = append(m.Sources, manifest.Source{Kind: "file", Path: relPath})
		seen[id] = true
	}
	return nil
}

// --- skills (Agent Skills SKILL.md trees) ---

func scanSkills(root string, m *manifest.Manifest) error {
	seen := map[string]bool{}
	for _, s := range m.Skills {
		seen[s.ID] = true
		if s.Path != "" {
			seen[filepath.Clean(s.Path)] = true
		}
	}
	bases := []string{
		filepath.Join(root, ".claude", "skills"),
		filepath.Join(root, ".agents", "skills"),
		filepath.Join(root, ".gemini", "skills"),
	}
	for _, base := range bases {
		entries, err := os.ReadDir(base)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return err
		}
		for _, e := range entries {
			if !e.IsDir() {
				continue
			}
			skillDir := filepath.Join(base, e.Name())
			skillMD := filepath.Join(skillDir, "SKILL.md")
			if _, err := os.Stat(skillMD); err != nil {
				continue
			}
			relPath := rel(root, skillMD)
			id := "skill-" + slug(e.Name())
			if seen[id] || seen[relPath] {
				continue
			}
			// Hash the whole skill directory, not just SKILL.md: a skill is
			// commonly SKILL.md plus scripts/resources beside it, and a change
			// to one of those without touching SKILL.md must still register.
			h, err := manifest.HashDirTree(skillDir)
			if err != nil {
				m.Unpinned = append(m.Unpinned, manifest.UnpinnedRisk{Artifact: "skill:" + id, Reason: err.Error()})
				continue
			}
			m.Skills = append(m.Skills, manifest.Skill{
				ID: id, Name: e.Name(), Path: relPath, ContentHash: h, Source: "skill.md",
			})
			m.Sources = append(m.Sources, manifest.Source{Kind: "skill.md", Path: relPath})
			seen[id] = true
		}
	}
	return nil
}

// --- Cursor rules (hashed as prompts) ---

func scanCursorRules(root string, m *manifest.Manifest) error {
	seen := map[string]bool{}
	for _, p := range m.Prompts {
		seen[p.ID] = true
		if p.Path != "" {
			seen[filepath.Clean(p.Path)] = true
		}
	}
	rulesDir := filepath.Join(root, ".cursor", "rules")
	entries, err := os.ReadDir(rulesDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		lower := strings.ToLower(name)
		if !strings.HasSuffix(lower, ".mdc") && !strings.HasSuffix(lower, ".md") {
			continue
		}
		full := filepath.Join(rulesDir, name)
		relPath := rel(root, full)
		id := "prompt-cursor-" + slug(name)
		if seen[id] || seen[relPath] {
			continue
		}
		h, err := manifest.HashFile(full)
		if err != nil {
			m.Unpinned = append(m.Unpinned, manifest.UnpinnedRisk{Artifact: "prompt:" + id, Reason: err.Error()})
			continue
		}
		m.Prompts = append(m.Prompts, manifest.Prompt{
			ID: id, Path: relPath, ContentHash: h, Source: "cursor-rules",
		})
		m.Sources = append(m.Sources, manifest.Source{Kind: "cursor-rules", Path: relPath})
		seen[id] = true
	}
	return nil
}

// --- MCP ---

func scanMCP(root string, m *manifest.Manifest) (map[string]json.RawMessage, error) {
	configs := map[string]json.RawMessage{}
	candidates := []string{
		"mcp.json", ".mcp.json", ".cursor/mcp.json",
		"claude_desktop_config.json", ".vscode/mcp.json",
	}
	for _, c := range candidates {
		path := filepath.Join(root, c)
		data, err := os.ReadFile(path)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return nil, err
		}
		m.Sources = append(m.Sources, manifest.Source{Kind: "mcp.json", Path: c})
		var raw map[string]json.RawMessage
		if err := json.Unmarshal(data, &raw); err != nil {
			// still hash whole file as one mcp config
			id := "mcp-config-" + slug(c)
			if !hasMCP(m, id) {
				m.MCPServers = append(m.MCPServers, manifest.MCPServer{
					ID: id, Name: c, SchemaHash: manifest.HashBytes(data), Source: c,
				})
			}
			continue
		}
		for k, v := range collectMCPConfigs(raw) {
			configs[k] = v
		}
		// Claude Desktop / Cursor style: mcpServers map
		if serversRaw, ok := raw["mcpServers"]; ok {
			var servers map[string]any
			if err := json.Unmarshal(serversRaw, &servers); err == nil {
				for name, cfg := range servers {
					id := "mcp-" + slug(name)
					if hasMCP(m, id) {
						continue
					}
					b, _ := json.Marshal(cfg)
					m.MCPServers = append(m.MCPServers, manifest.MCPServer{
						ID: id, Name: name, SchemaHash: manifest.HashBytes(b), Source: c,
					})
				}
				continue
			}
		}
		id := "mcp-config-" + slug(c)
		if !hasMCP(m, id) {
			m.MCPServers = append(m.MCPServers, manifest.MCPServer{
				ID: id, Name: c, SchemaHash: manifest.HashBytes(data), Source: c,
			})
		}
	}
	return configs, nil
}

// --- model heuristics ---

var modelPatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)(?:openai|anthropic|google|model)[_-]?(?:name|id|model)?\s*[=:]\s*["']?([a-z0-9._:-]+)`),
	regexp.MustCompile(`(?i)(?:claude|gpt|gemini|o1|o3)[-a-z0-9.]+`),
}

func scanModelHeuristics(root string, m *manifest.Manifest) error {
	var targets []string
	for _, f := range []string{".env.example", ".env.sample", "config.yaml", "config.yml", "config.json"} {
		p := filepath.Join(root, f)
		if _, err := os.Stat(p); err == nil {
			targets = append(targets, p)
		}
	}
	entries, _ := os.ReadDir(root)
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		n := e.Name()
		if strings.HasSuffix(n, ".yaml") || strings.HasSuffix(n, ".yml") || strings.HasSuffix(n, ".toml") {
			targets = append(targets, filepath.Join(root, n))
		}
	}

	seen := map[string]bool{}
	for _, x := range m.Models {
		seen[x.Model] = true
	}
	for _, path := range unique(targets) {
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		text := string(data)
		for _, re := range modelPatterns {
			for _, match := range re.FindAllStringSubmatch(text, -1) {
				model := match[0]
				if len(match) > 1 && match[1] != "" {
					model = match[1]
				}
				model = strings.Trim(model, `"' `)
				if len(model) < 4 || seen[model] {
					continue
				}
				// filter noise
				if strings.Contains(strings.ToLower(model), "model") && !strings.Contains(model, "-") {
					continue
				}
				if !manifest.LooksLikeModel(model) {
					continue
				}
				seen[model] = true
				id := "model-" + slug(model)
				provider := manifest.GuessProvider(model)
				m.Models = append(m.Models, manifest.Model{
					ID: id, Provider: provider, Model: model,
					ContentHash: manifest.HashString(provider + "|" + model),
					Source:      rel(root, path), Confidence: "low",
				})
				m.Sources = append(m.Sources, manifest.Source{Kind: "heuristic", Path: rel(root, path), Detail: model})
			}
		}
	}
	return nil
}

func scanEvalHooks(root string, m *manifest.Manifest) error {
	for _, pat := range []string{"evals/**/*", "**/*eval*.yaml", "**/*eval*.yml", "promptfoo.yaml", "promptfooconfig.yaml"} {
		matches, err := doubleStarGlob(root, pat)
		if err != nil {
			return err
		}
		for _, path := range matches {
			id := slug(rel(root, path))
			if hasEval(m, id) {
				continue
			}
			m.Evals = append(m.Evals, manifest.EvalHook{ID: id, Path: rel(root, path), Kind: "file", Source: "scan"})
			m.Sources = append(m.Sources, manifest.Source{Kind: "file", Path: rel(root, path), Detail: "eval"})
		}
	}
	return nil
}

type envFile struct {
	ID    string   `json:"id"`
	Paths []string `json:"paths"`
}

func scanEnvs(root string, m *manifest.Manifest) error {
	matches, _ := doubleStarGlob(root, "**/env.json")
	cands := make([]string, 0, 2+len(matches))
	cands = append(cands,
		filepath.Join(root, ".airlock", "env.json"),
		filepath.Join(root, "env.json"),
	)
	cands = append(cands, matches...)
	seen := map[string]bool{}
	for _, path := range cands {
		if seen[path] {
			continue
		}
		seen[path] = true
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		var ef envFile
		if err := json.Unmarshal(data, &ef); err != nil {
			continue
		}
		if ef.ID == "" {
			ef.ID = slug(rel(root, filepath.Dir(path)))
			if ef.ID == "" || ef.ID == "." {
				ef.ID = "default-env"
			}
		}
		if hasEnv(m, ef.ID) {
			continue
		}
		h, err := manifest.HashTree(root, ef.Paths)
		if err != nil {
			m.Unpinned = append(m.Unpinned, manifest.UnpinnedRisk{Artifact: "env:" + ef.ID, Reason: err.Error()})
			continue
		}
		m.Envs = append(m.Envs, manifest.Env{
			ID: ef.ID, Paths: ef.Paths, ContentHash: h, Source: rel(root, path),
		})
		m.Sources = append(m.Sources, manifest.Source{Kind: "file", Path: rel(root, path), Detail: "env"})
	}
	return nil
}

func hasEnv(m *manifest.Manifest, id string) bool {
	for _, e := range m.Envs {
		if e.ID == id {
			return true
		}
	}
	return false
}

func hasEval(m *manifest.Manifest, id string) bool {
	for _, e := range m.Evals {
		if e.ID == id {
			return true
		}
	}
	return false
}

// --- helpers ---

func doubleStarGlob(root, pattern string) ([]string, error) {
	// limited ** support: walk and match suffix/prefix
	var out []string
	parts := strings.Split(pattern, "**")
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			n := d.Name()
			if n == ".git" || n == "node_modules" || n == ".airlock" || n == "vendor" {
				return filepath.SkipDir
			}
			return nil
		}
		relPath := rel(root, path)
		if matchDoubleStar(relPath, pattern, parts) {
			out = append(out, path)
		}
		return nil
	})
	return out, err
}

func matchDoubleStar(relPath, pattern string, parts []string) bool {
	relPath = filepath.ToSlash(relPath)
	pattern = filepath.ToSlash(pattern)
	if !strings.Contains(pattern, "**") {
		ok, _ := filepath.Match(pattern, relPath)
		return ok
	}
	if len(parts) == 2 {
		prefix := strings.TrimPrefix(parts[0], "/")
		suffix := strings.TrimPrefix(parts[1], "/")
		if prefix != "" && !strings.HasPrefix(relPath, strings.TrimSuffix(prefix, "/")) {
			// also allow prefix as directory component
			if !strings.Contains(relPath, strings.Trim(prefix, "/")) {
				return false
			}
		}
		if suffix == "" {
			return true
		}
		ok, _ := filepath.Match(suffix, filepath.Base(relPath))
		if ok {
			return true
		}
		// suffix may be like /*.md
		ok, _ = filepath.Match(suffix, relPath)
		return ok
	}
	return false
}

func rel(root, path string) string {
	r, err := filepath.Rel(root, path)
	if err != nil {
		return path
	}
	return filepath.ToSlash(r)
}

func slug(s string) string {
	s = filepath.ToSlash(s)
	s = strings.ReplaceAll(s, "/", "-")
	s = strings.ReplaceAll(s, " ", "-")
	s = strings.ToLower(s)
	var b strings.Builder
	for _, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' || r == '_' || r == '.' {
			b.WriteRune(r)
		} else {
			b.WriteByte('-')
		}
	}
	return b.String()
}

func coalesce(a, b string) string {
	return cmp.Or(a, b)
}

func unique(in []string) []string {
	seen := make(map[string]struct{}, len(in))
	out := make([]string, 0, len(in))
	for _, s := range in {
		if _, ok := seen[s]; ok {
			continue
		}
		seen[s] = struct{}{}
		out = append(out, s)
	}
	return out
}
