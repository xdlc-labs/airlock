package snapshot

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/xdlc-labs/airlock/internal/discovery"
	"github.com/xdlc-labs/airlock/internal/manifest"
	"github.com/xdlc-labs/airlock/internal/store"
)

// Create scans root (or uses existing manifest), builds a content-addressed snapshot, persists it.
func Create(root string, rescan bool) (*manifest.Snapshot, error) {
	p := store.ForRoot(root)
	if err := p.Ensure(); err != nil {
		return nil, err
	}

	var m *manifest.Manifest
	var err error
	if rescan {
		m, err = discovery.Scan(root)
	} else {
		m, err = store.ReadManifest(p)
		if err != nil {
			m, err = discovery.Scan(root)
		}
	}
	if err != nil {
		return nil, err
	}
	manifest.BuildGraph(m)
	if err := manifest.Validate(m); err != nil {
		return nil, err
	}

	arts := manifest.Artifacts(m)
	id, err := contentID(arts, *m)
	if err != nil {
		return nil, err
	}
	mh := manifest.HashBytes(mustJSON(m))

	snap := &manifest.Snapshot{
		ID:           id,
		CreatedAt:    time.Now().UTC(),
		ManifestHash: mh,
		Artifacts:    arts,
		Manifest:     *m,
	}
	if err := store.WriteManifest(p, m); err != nil {
		return nil, err
	}
	if err := store.WriteSnapshot(p, snap); err != nil {
		return nil, err
	}
	return snap, nil
}

// FromWorkingTree builds an in-memory snapshot without writing (for diff head).
func FromWorkingTree(root string) (*manifest.Snapshot, error) {
	m, err := discovery.Scan(root)
	if err != nil {
		return nil, err
	}
	arts := manifest.Artifacts(m)
	body, err := json.Marshal(arts)
	if err != nil {
		return nil, err
	}
	return &manifest.Snapshot{
		ID:           "working-" + manifest.HashBytes(body)[:12],
		CreatedAt:    time.Now().UTC(),
		ManifestHash: manifest.HashBytes(mustJSON(m)),
		Artifacts:    arts,
		Manifest:     *m,
	}, nil
}

func Load(root, id string) (*manifest.Snapshot, error) {
	p := store.ForRoot(root)
	if id == "" || id == "latest" {
		var err error
		id, err = store.LatestSnapshotID(p)
		if err != nil {
			return nil, err
		}
	}
	return store.ReadSnapshot(p, id)
}

// contentID hashes artifacts plus the manifest, with GeneratedAt and Root
// cleared. Those fields change every CI run (clock, mktemp worktree path)
// and must not rotate the snapshot id that airlock approve keys on.
func contentID(arts []manifest.ArtifactRef, m manifest.Manifest) (string, error) {
	m.GeneratedAt = time.Time{}
	m.Root = ""
	body, err := json.Marshal(struct {
		Artifacts []manifest.ArtifactRef `json:"artifacts"`
		Manifest  manifest.Manifest      `json:"manifest"`
	}{Artifacts: arts, Manifest: m})
	if err != nil {
		return "", err
	}
	return manifest.HashBytes(body)[:16], nil
}

func mustJSON(v any) []byte {
	b, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return b
}

// Summarize returns a one-line human summary.
func Summarize(s *manifest.Snapshot) string {
	return fmt.Sprintf("snapshot  %s  artifacts=%d  manifest=%s",
		s.ID, len(s.Artifacts), short(s.ManifestHash))
}

func short(h string) string {
	if len(h) > 12 {
		return h[:12]
	}
	return h
}
