package store

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/xdlc-labs/airlock/internal/manifest"
)

const (
	DirName      = ".airlock"
	ManifestFile = "manifest.json"
	PolicyFile   = "policy.yml"
	SnapshotsDir = "snapshots"
	EvalsDir     = "evals"
	CassettesDir = "cassettes"
	ResultsDir   = "results"
	IngestDir    = "ingest"
	JudgesDir    = "judges"
	ApprovalsDir = "approvals"
)

type Paths struct {
	Root      string
	Airlock   string
	Manifest  string
	Policy    string
	Snapshots string
	Evals     string
	Cassettes string
	Results   string
	Ingest    string
	Judges    string
	Approvals string
}

func ForRoot(root string) Paths {
	air := filepath.Join(root, DirName)
	return Paths{
		Root:      root,
		Airlock:   air,
		Manifest:  filepath.Join(air, ManifestFile),
		Policy:    filepath.Join(air, PolicyFile),
		Snapshots: filepath.Join(air, SnapshotsDir),
		Evals:     filepath.Join(air, EvalsDir),
		Cassettes: filepath.Join(air, CassettesDir),
		Results:   filepath.Join(air, ResultsDir),
		Ingest:    filepath.Join(air, IngestDir),
		Judges:    filepath.Join(air, JudgesDir),
		Approvals: filepath.Join(air, ApprovalsDir),
	}
}

func (p Paths) Ensure() error {
	for _, d := range []string{p.Snapshots, p.Evals, p.Cassettes, p.Results, p.Ingest, p.Judges, p.Approvals} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			return err
		}
	}
	return nil
}

func WriteManifest(p Paths, m *manifest.Manifest) error {
	if err := p.Ensure(); err != nil {
		return err
	}
	return writeJSON(p.Manifest, m)
}

func ReadManifest(p Paths) (*manifest.Manifest, error) {
	data, err := os.ReadFile(p.Manifest)
	if err != nil {
		return nil, err
	}
	var m manifest.Manifest
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, err
	}
	return &m, nil
}

func WritePolicyStub(p Paths) error {
	if err := p.Ensure(); err != nil {
		return err
	}
	if _, err := os.Stat(p.Policy); err == nil {
		return nil
	}
	stub := `# Airlock policy
version: 1

# Gates that block a merge. New repos start fail-closed: a change that needs
# human sign-off, or evals that fail or stay inconclusive, stops the merge
# without any workflow having to pass --fail-on-* flags. Turn one off here if
# you mean to, so the choice is in review rather than in a workflow file.
fail_on:
  approval: true
  eval: true
  inconclusive: true
  sentinel: true
  # Blocks every AI-artifact change, approved or not. Strictest setting; off.
  ai_change: false

data_boundary:
  fail_on_pii: false
gates:
  tool_success: { min: 0.99, confidence: 0.95 }
  json_valid: { min: 0.995, confidence: 0.95 }
  task_success: { max_regression_pp: 1.0, confidence: 0.95 }
  adversarial_critical: { max_new_critical: 0, confidence: 0.95 }
budgets:
  max_cost_per_pr: 2.00
  max_samples_per_case: 5
`
	return os.WriteFile(p.Policy, []byte(stub), 0o644)
}

// GitignoreFile is written into .airlock/ by init so the state Airlock rebuilds
// on every run does not end up in the repository. Committing snapshots and
// results is what made the approval ledger look like just another generated
// file, and a pull request full of .airlock/ churn hides the one file that
// matters.
const GitignoreFile = ".gitignore"

// WriteGitignoreStub writes .airlock/.gitignore unless one exists.
func WriteGitignoreStub(p Paths) error {
	if err := p.Ensure(); err != nil {
		return err
	}
	path := filepath.Join(p.Airlock, GitignoreFile)
	if _, err := os.Stat(path); err == nil {
		return nil
	}
	stub := `# Written by airlock init. Airlock rebuilds these on every run, so they do
# not belong in the repository. Keep policy.yml, eval-bindings.yml, evals/,
# judges/, cassettes/, and sentinel/ committed: those are yours.
manifest.json
snapshots/
results/latest.json
ci-comment.md
ingest/

# The approval ledger. On GitHub the human sign-off is a pull request review
# (see the Action's github-approvals input), so the ledger stays local. A CI
# system without reviews unblocks through committed ledger entries instead:
# delete the next line to ship them.
approvals/

# A manifest probed with --mcp-stdio carries the tool list of stdio MCP
# servers, and a committed one gives CI that list without CI starting a
# server. Uncomment to commit it.
# !manifest.json
`
	return os.WriteFile(path, []byte(stub), 0o644)
}

func WriteSnapshot(p Paths, snap *manifest.Snapshot) error {
	if err := p.Ensure(); err != nil {
		return err
	}
	path := filepath.Join(p.Snapshots, snap.ID+".json")
	return writeJSON(path, snap)
}

func ReadSnapshot(p Paths, id string) (*manifest.Snapshot, error) {
	path := filepath.Join(p.Snapshots, id+".json")
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var s manifest.Snapshot
	if err := json.Unmarshal(data, &s); err != nil {
		return nil, err
	}
	return &s, nil
}

func LatestSnapshotID(p Paths) (string, error) {
	entries, err := os.ReadDir(p.Snapshots)
	if err != nil {
		if os.IsNotExist(err) {
			return "", fmt.Errorf("no snapshots yet; run airlock snapshot")
		}
		return "", err
	}
	type item struct {
		id  string
		mod time.Time
	}
	var items []item
	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".json" {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		id := strings.TrimSuffix(e.Name(), ".json")
		items = append(items, item{id: id, mod: info.ModTime()})
	}
	if len(items) == 0 {
		return "", fmt.Errorf("no snapshots yet; run airlock snapshot")
	}
	slices.SortFunc(items, func(a, b item) int {
		if c := b.mod.Compare(a.mod); c != 0 {
			return c
		}
		return strings.Compare(b.id, a.id)
	})
	return items[0].id, nil
}

func writeJSON(path string, v any) error {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return os.WriteFile(path, data, 0o644)
}
