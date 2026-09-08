package manifest_test

import (
	"testing"

	"github.com/xdlc-labs/airlock/internal/manifest"
)

func mcpHash(t *testing.T, perms []string) string {
	t.Helper()
	m := &manifest.Manifest{
		Version: 1,
		MCPServers: []manifest.MCPServer{{
			ID:          "local-fs",
			Name:        "local-fs",
			SchemaHash:  "pinned-literal-hash",
			Permissions: perms,
		}},
	}
	for _, a := range manifest.Artifacts(m) {
		if a.Kind == "mcp" && a.ID == "local-fs" {
			return a.Hash
		}
	}
	t.Fatal("no mcp artifact produced")
	return ""
}

// An APM lockfile may pin `hash:` as a literal, so SchemaHash does not move
// when `permissions:` is widened. Diff only inspects artifacts whose hash
// changed, so the permission-expansion approval gate never fired.
func TestMCPArtifactHashCoversPermissionWidening(t *testing.T) {
	read := mcpHash(t, []string{"read"})
	readWrite := mcpHash(t, []string{"read", "write"})
	if read == readWrite {
		t.Fatalf("widening permissions did not change the mcp artifact hash: %s", read)
	}
}

func TestMCPArtifactHashIgnoresPermissionOrder(t *testing.T) {
	if a, b := mcpHash(t, []string{"read", "write"}), mcpHash(t, []string{"write", "read"}); a != b {
		t.Fatalf("permission order changed the hash: %s vs %s", a, b)
	}
}

// A server with no declared permissions keeps its schema hash verbatim.
func TestMCPArtifactHashUnchangedWithoutPermissions(t *testing.T) {
	if got := mcpHash(t, nil); got != "pinned-literal-hash" {
		t.Fatalf("hash = %q, want the schema hash verbatim", got)
	}
}
