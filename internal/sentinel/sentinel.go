// Package sentinel fingerprints upstream models to catch silent provider drift.
package sentinel

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/xdlc-labs/airlock/internal/manifest"
	"github.com/xdlc-labs/airlock/internal/providers"
)

const PromptVersion = "AIRLOCK_SENTINEL_v1"

// ponytail: one fixed probe prompt; upgrade path = per-provider probes + temperature=0 contract tests.

var probePrompt = providers.Message{
	Role:    "user",
	Content: "Reply with exactly the string AIRLOCK_SENTINEL_v1 and nothing else.",
}

// ErrNoProbe marks a model whose provider Airlock cannot probe. A sweep skips
// those models instead of failing, so one exotic model does not stop the rest.
var ErrNoProbe = errors.New("no probe support for provider")

// Record is a stored fingerprint for one manifest model.
type Record struct {
	ModelID     string    `json:"model_id"`
	Provider    string    `json:"provider,omitempty"`
	Model       string    `json:"model"`
	Fingerprint string    `json:"fingerprint"`
	PromptVer   string    `json:"prompt_version"`
	ProbedAt    time.Time `json:"probed_at"`
}

// Store is the on-disk fingerprint ledger under .airlock/sentinel/.
type Store struct {
	Version int      `json:"version"`
	Records []Record `json:"records"`
	// Skipped holds the model ids the last sweep could not probe. In memory only:
	// it describes a run, not the ledger.
	Skipped []string `json:"-"`
}

// Drift is live vs stored fingerprint mismatch for the same config model string.
type Drift struct {
	ModelID     string `json:"model_id"`
	Model       string `json:"model"`
	Provider    string `json:"provider,omitempty"`
	Stored      string `json:"stored_fingerprint"`
	Live        string `json:"live_fingerprint"`
	ConfigMatch bool   `json:"config_unchanged"`
}

// CheckReport aggregates probe results vs stored fingerprints.
type CheckReport struct {
	Drifts    []Drift  `json:"drifts,omitempty"`
	Missing   []string `json:"missing,omitempty"` // model ids with no stored fingerprint
	Skipped   []string `json:"skipped,omitempty"` // model ids whose provider has no probe support
	Probed    int      `json:"probed"`
	Unchanged int      `json:"unchanged"`
}

const storeVersion = 1

// DefaultPath returns .airlock/sentinel/fingerprints.json for root.
func DefaultPath(root string) string {
	return filepath.Join(root, ".airlock", "sentinel", "fingerprints.json")
}

func Load(path string) (*Store, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var s Store
	if err := json.Unmarshal(data, &s); err != nil {
		return nil, err
	}
	if s.Version == 0 {
		s.Version = storeVersion
	}
	return &s, nil
}

func Save(path string, s *Store) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	if s.Version == 0 {
		s.Version = storeVersion
	}
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return os.WriteFile(path, data, 0o644)
}

func indexStore(s *Store) map[string]Record {
	m := make(map[string]Record, len(s.Records))
	for _, r := range s.Records {
		m[r.ModelID] = r
	}
	return m
}

// ProbeModel sends the sentinel prompt and returns a content fingerprint.
func ProbeModel(ctx context.Context, m manifest.Model, client providers.HTTPDoer) (Record, error) {
	providerName := m.Provider
	if providerName == "" {
		providerName = manifest.GuessProvider(m.Model)
	}
	if providerName == "" {
		providerName = "mock"
	}
	p, err := providers.Resolve(providerName, client)
	if err != nil {
		return Record{}, fmt.Errorf("%w: %s", ErrNoProbe, providerName)
	}
	seed := int64(42)
	resp, err := p.Generate(ctx, providers.Request{
		Provider: providerName,
		Model:    m.Model,
		Messages: []providers.Message{probePrompt},
		Seed:     &seed,
	})
	if err != nil {
		return Record{}, fmt.Errorf("probe %s: %w", m.ID, err)
	}
	fp := FingerprintResponse(resp)
	return Record{
		ModelID:     m.ID,
		Provider:    providerName,
		Model:       m.Model,
		Fingerprint: fp,
		PromptVer:   PromptVersion,
		ProbedAt:    time.Now().UTC(),
	}, nil
}

// FingerprintResponse hashes normalized model output (+ actual model id when present).
func FingerprintResponse(resp providers.Response) string {
	text := strings.TrimSpace(resp.Text)
	actual := resp.Model
	payload := actual + "|" + text
	if len(resp.RawJSON) > 0 {
		payload += "|" + string(resp.RawJSON)
	}
	return manifest.HashString(PromptVersion + "|" + payload)
}

// ProbeAll probes every model in the manifest and writes the store.
func ProbeAll(ctx context.Context, m *manifest.Manifest, client providers.HTTPDoer, path string) (*Store, error) {
	var prev *Store
	if s, err := Load(path); err == nil {
		prev = s
	} else if !os.IsNotExist(err) {
		return nil, err
	}
	idx := map[string]Record{}
	if prev != nil {
		idx = indexStore(prev)
	}
	out := &Store{Version: storeVersion}
	for _, mod := range m.Models {
		rec, err := ProbeModel(ctx, mod, client)
		if errors.Is(err, ErrNoProbe) {
			out.Skipped = append(out.Skipped, mod.ID)
			continue
		}
		if err != nil {
			return nil, err
		}
		idx[mod.ID] = rec
	}
	for _, r := range idx {
		out.Records = append(out.Records, r)
	}
	if err := Save(path, out); err != nil {
		return nil, err
	}
	ApplyToManifest(m, out)
	return out, nil
}

// Check probes live and compares to stored fingerprints.
func Check(ctx context.Context, m *manifest.Manifest, client providers.HTTPDoer, path string) (*CheckReport, error) {
	stored, err := Load(path)
	if err != nil {
		return nil, fmt.Errorf("load fingerprints: %w (run airlock sentinel probe first)", err)
	}
	idx := indexStore(stored)
	rep := &CheckReport{}
	for _, mod := range m.Models {
		live, err := ProbeModel(ctx, mod, client)
		if errors.Is(err, ErrNoProbe) {
			rep.Skipped = append(rep.Skipped, mod.ID)
			continue
		}
		if err != nil {
			return nil, err
		}
		rep.Probed++
		old, ok := idx[mod.ID]
		if !ok {
			rep.Missing = append(rep.Missing, mod.ID)
			continue
		}
		if old.Fingerprint == live.Fingerprint {
			rep.Unchanged++
			continue
		}
		rep.Drifts = append(rep.Drifts, Drift{
			ModelID:     mod.ID,
			Model:       mod.Model,
			Provider:    mod.Provider,
			Stored:      old.Fingerprint,
			Live:        live.Fingerprint,
			ConfigMatch: old.Model == mod.Model,
		})
	}
	return rep, nil
}

// ApplyToManifest folds fingerprints into model ContentHash so diff/CI see provider drift.
func ApplyToManifest(m *manifest.Manifest, s *Store) {
	if m == nil || s == nil {
		return
	}
	idx := indexStore(s)
	for i := range m.Models {
		r, ok := idx[m.Models[i].ID]
		if !ok {
			continue
		}
		base := configHash(m.Models[i])
		m.Models[i].ParamsHash = base
		m.Models[i].ContentHash = manifest.HashString(base + "|fp:" + r.Fingerprint)
		m.Models[i].SnapshotID = r.Fingerprint[:16]
	}
}

func configHash(m manifest.Model) string {
	return manifest.HashString(m.Provider + "|" + m.Model)
}

// HasDrift reports whether any config-stable fingerprint changed.
func (r *CheckReport) HasDrift() bool {
	return r != nil && len(r.Drifts) > 0
}

// FormatText renders a human-readable sentinel report.
func FormatText(r *CheckReport) string {
	if r == nil {
		return "sentinel: no report\n"
	}
	var b strings.Builder
	fmt.Fprintf(&b, "sentinel: probed=%d unchanged=%d drifts=%d missing=%d skipped=%d\n",
		r.Probed, r.Unchanged, len(r.Drifts), len(r.Missing), len(r.Skipped))
	for _, d := range r.Drifts {
		tag := "fingerprint changed"
		if d.ConfigMatch {
			tag = "silent provider drift (config unchanged)"
		}
		fmt.Fprintf(&b, "  DRIFT %s:%s model=%q %s\n    stored=%s… live=%s…\n",
			d.ModelID, d.Provider, d.Model, tag, short(d.Stored), short(d.Live))
	}
	for _, id := range r.Missing {
		fmt.Fprintf(&b, "  MISSING stored fingerprint for %s\n", id)
	}
	for _, id := range r.Skipped {
		fmt.Fprintf(&b, "  SKIPPED %s: no probe support for its provider\n", id)
	}
	return b.String()
}

func short(h string) string {
	if len(h) > 12 {
		return h[:12]
	}
	return h
}
