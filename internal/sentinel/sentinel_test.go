package sentinel_test

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/xdlc-labs/airlock/internal/manifest"
	"github.com/xdlc-labs/airlock/internal/providers"
	"github.com/xdlc-labs/airlock/internal/sentinel"
)

func TestProbeAndDetectDrift(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	path := sentinel.DefaultPath(dir)

	m := &manifest.Manifest{
		Models: []manifest.Model{{
			ID: "model-gpt-test", Provider: "mock", Model: "gpt-test",
			ContentHash: manifest.HashString("mock|gpt-test"),
		}},
	}
	if _, err := sentinel.ProbeAll(ctx, m, nil, path); err != nil {
		t.Fatal(err)
	}
	st, err := sentinel.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(st.Records) != 1 || st.Records[0].Fingerprint == "" {
		t.Fatalf("expected stored fingerprint, got %+v", st.Records)
	}

	rep, err := sentinel.Check(ctx, m, nil, path)
	if err != nil {
		t.Fatal(err)
	}
	if rep.HasDrift() {
		t.Fatalf("unexpected drift on stable mock: %+v", rep.Drifts)
	}

	// Simulate silent provider drift: same model string, different output.
	old := providers.MockSentinelReply
	providers.MockSentinelReply = "AIRLOCK_SENTINEL_v1_DRIFT"
	t.Cleanup(func() { providers.MockSentinelReply = old })

	rep, err = sentinel.Check(ctx, m, nil, path)
	if err != nil {
		t.Fatal(err)
	}
	if !rep.HasDrift() {
		t.Fatal("expected drift after provider output change")
	}
	if len(rep.Drifts) != 1 || !rep.Drifts[0].ConfigMatch {
		t.Fatalf("want config-stable drift, got %+v", rep.Drifts)
	}
}

func TestApplyToManifestChangesContentHash(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "fp.json")
	m := &manifest.Manifest{
		Models: []manifest.Model{{
			ID: "model-gpt-test", Provider: "mock", Model: "gpt-test",
			ContentHash: manifest.HashString("mock|gpt-test"),
		}},
	}
	before := m.Models[0].ContentHash
	st := &sentinel.Store{
		Version: 1,
		Records: []sentinel.Record{{
			ModelID: "model-gpt-test", Provider: "mock", Model: "gpt-test",
			Fingerprint: "abc123fingerprint",
			PromptVer:   sentinel.PromptVersion,
		}},
	}
	sentinel.ApplyToManifest(m, st)
	if m.Models[0].ContentHash == before {
		t.Fatal("ApplyToManifest should fold fingerprint into content_hash")
	}
	if m.Models[0].ParamsHash == "" {
		t.Fatal("expected params_hash for config identity")
	}
	_ = path
}

func TestProbeAllSkipsUnprobeableProvider(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "sentinel.json")
	m := &manifest.Manifest{Models: []manifest.Model{
		{ID: "model-gpt-4o", Provider: "mock", Model: "gpt-4o"},
		{ID: "model-mistral-large-latest", Provider: "mistral", Model: "mistral-large-latest"},
	}}
	st, err := sentinel.ProbeAll(context.Background(), m, nil, path)
	if err != nil {
		t.Fatalf("one unprobeable model must not fail the sweep: %v", err)
	}
	if len(st.Records) != 1 {
		t.Fatalf("expected 1 record, got %+v", st.Records)
	}
	if len(st.Skipped) != 1 || st.Skipped[0] != "model-mistral-large-latest" {
		t.Fatalf("expected the mistral model skipped, got %+v", st.Skipped)
	}
}

func TestCheckSkipsUnprobeableProvider(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "sentinel.json")
	m := &manifest.Manifest{Models: []manifest.Model{
		{ID: "model-gpt-4o", Provider: "mock", Model: "gpt-4o"},
		{ID: "model-qwen-max", Provider: "alibaba", Model: "qwen-max"},
	}}
	if _, err := sentinel.ProbeAll(context.Background(), m, nil, path); err != nil {
		t.Fatal(err)
	}
	rep, err := sentinel.Check(context.Background(), m, nil, path)
	if err != nil {
		t.Fatal(err)
	}
	if rep.Probed != 1 {
		t.Fatalf("expected probed=1, got %d", rep.Probed)
	}
	if len(rep.Skipped) != 1 || rep.Skipped[0] != "model-qwen-max" {
		t.Fatalf("expected the qwen model skipped, got %+v", rep.Skipped)
	}
	if len(rep.Drifts) != 0 {
		t.Fatalf("expected no drift, got %+v", rep.Drifts)
	}
}
