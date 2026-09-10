package manifest

import "time"

const Version = 1

// Manifest is the normalized AI dependency graph for a repo.
type Manifest struct {
	Version      int            `json:"version"`
	GeneratedAt  time.Time      `json:"generated_at"`
	Root         string         `json:"root"`
	Agents       []Agent        `json:"agents,omitempty"`
	Models       []Model        `json:"models,omitempty"`
	Prompts      []Prompt       `json:"prompts,omitempty"`
	Tools        []Tool         `json:"tools,omitempty"`
	Skills       []Skill        `json:"skills,omitempty"`
	MCPServers   []MCPServer    `json:"mcp_servers,omitempty"`
	Evals        []EvalHook     `json:"evals,omitempty"`
	Envs         []Env          `json:"envs,omitempty"`
	Sources      []Source       `json:"sources,omitempty"`
	Graph        []Edge         `json:"graph,omitempty"`
	Unpinned     []UnpinnedRisk `json:"unpinned_risks,omitempty"`
	Dependencies []Dependency   `json:"dependencies,omitempty"`
}

type Agent struct {
	ID      string   `json:"id"`
	Name    string   `json:"name,omitempty"`
	Models  []string `json:"models,omitempty"`
	Prompts []string `json:"prompts,omitempty"`
	Tools   []string `json:"tools,omitempty"`
	Skills  []string `json:"skills,omitempty"`
	MCP     []string `json:"mcp,omitempty"`
	Evals   []string `json:"evals,omitempty"`
	Source  string   `json:"source,omitempty"`
}

type Model struct {
	ID          string `json:"id"`
	Provider    string `json:"provider,omitempty"`
	Model       string `json:"model"`
	ParamsHash  string `json:"params_hash,omitempty"`
	SnapshotID  string `json:"snapshot_id,omitempty"`
	ContentHash string `json:"content_hash"`
	Source      string `json:"source,omitempty"`
	Confidence  string `json:"confidence,omitempty"` // high|medium|low
}

type Prompt struct {
	ID          string `json:"id"`
	Path        string `json:"path,omitempty"`
	RemoteRef   string `json:"remote_ref,omitempty"`
	ContentHash string `json:"content_hash"`
	Source      string `json:"source,omitempty"`
}

type Tool struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	SchemaHash string `json:"schema_hash"`
	SideEffect string `json:"side_effect,omitempty"` // read|write|unknown
	Source     string `json:"source,omitempty"`
}

// Skill is an Agent Skills–style unit (SKILL.md tree or APM skill).
type Skill struct {
	ID          string `json:"id"`
	Name        string `json:"name,omitempty"`
	Path        string `json:"path,omitempty"`
	ContentHash string `json:"content_hash"`
	Source      string `json:"source,omitempty"`
}

type MCPServer struct {
	ID          string   `json:"id"`
	Name        string   `json:"name,omitempty"`
	SchemaHash  string   `json:"schema_hash"`
	Permissions []string `json:"permissions,omitempty"`
	// ToolNames is the live tools/list result: read over http(s) on every scan,
	// or from a spawned stdio server when the caller opted into that (see
	// enrichMCPSchemas). Populated independently of Permissions, which today
	// is only ever set by hand-maintained apm.lock.yaml entries: this lets
	// diff.permissionExpansion catch capability growth (a genuinely new tool
	// appearing on the server) even when nobody declared it in Permissions.
	ToolNames []string `json:"tool_names,omitempty"`
	// ConfigHash is the hash of the server's config entry, kept when a stdio
	// probe replaces SchemaHash with the live schema. A later scan that does
	// not probe can then tell whether a committed manifest's tool list still
	// describes this config (see enrichMCPSchemas) instead of dropping it.
	ConfigHash string `json:"config_hash,omitempty"`
	Source     string `json:"source,omitempty"`
}

// Dependency is a non-AI package dependency (npm/pip/go/etc) tracked for the
// agent-driven supply-chain gate: an AI artifact change that co-occurs with a
// new dependency raises NEEDS_APPROVAL (see internal/diff.dependencyExpansion).
type Dependency struct {
	ID        string `json:"id"`
	Ecosystem string `json:"ecosystem,omitempty"` // npm|pip|go|cargo|package
	Version   string `json:"version,omitempty"`
	Hash      string `json:"hash"`
	Source    string `json:"source,omitempty"`
}

type EvalHook struct {
	ID     string `json:"id"`
	Path   string `json:"path,omitempty"`
	Kind   string `json:"kind,omitempty"`
	Source string `json:"source,omitempty"`
}

// Env is a hashed workspace/fixture tree used for agent replay reproducibility.
type Env struct {
	ID          string   `json:"id"`
	Paths       []string `json:"paths,omitempty"`
	ContentHash string   `json:"content_hash"`
	Source      string   `json:"source,omitempty"`
}

type Source struct {
	Kind   string `json:"kind"` // apm.lock|apm.yml|file|mcp.json|heuristic
	Path   string `json:"path,omitempty"`
	Detail string `json:"detail,omitempty"`
}

type Edge struct {
	From string `json:"from"` // agent:<id>
	To   string `json:"to"`   // model:|prompt:|tool:|skill:|mcp:|eval:<id>
	Kind string `json:"kind,omitempty"`
}

type UnpinnedRisk struct {
	Artifact string `json:"artifact"`
	Reason   string `json:"reason"`
}

// ArtifactRef is a hashable unit used in snapshots and diffs.
type ArtifactRef struct {
	Kind string `json:"kind"` // model|prompt|tool|skill|mcp|eval|agent|env
	ID   string `json:"id"`
	Hash string `json:"hash"`
}

// Snapshot is a content-addressed release record.
type Snapshot struct {
	ID           string        `json:"id"`
	CreatedAt    time.Time     `json:"created_at"`
	ManifestHash string        `json:"manifest_hash"`
	Artifacts    []ArtifactRef `json:"artifacts"`
	Manifest     Manifest      `json:"manifest"`
}
