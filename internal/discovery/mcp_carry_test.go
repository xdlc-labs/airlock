package discovery

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/xdlc-labs/airlock/internal/store"
)

func writeMCPConfig(t *testing.T, root string, server map[string]any) {
	t.Helper()
	data, err := json.Marshal(map[string]any{"mcpServers": map[string]any{"local": server}})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "mcp.json"), data, 0o644); err != nil {
		t.Fatal(err)
	}
}

// TestScanCarriesProbedStdioToolList is the promise the README makes: probe on a
// trusted machine, commit the manifest, and CI has the tool list without
// starting the server.
func TestScanCarriesProbedStdioToolList(t *testing.T) {
	root := t.TempDir()
	writeMCPConfig(t, root, helperServerCfg(t, "read_file", "send_email"))

	probed, err := ScanWith(root, Options{ProbeStdioMCP: true})
	if err != nil {
		t.Fatal(err)
	}
	srv := probed.MCPServers[0]
	if srv.ConfigHash == "" || srv.ConfigHash == srv.SchemaHash {
		t.Fatalf("a probed stdio server must keep its config hash beside the schema hash, got %+v", srv)
	}
	if err := store.WriteManifest(store.ForRoot(root), probed); err != nil {
		t.Fatal(err)
	}

	plain, err := Scan(root)
	if err != nil {
		t.Fatal(err)
	}
	got := plain.MCPServers[0]
	if !slices.Equal(got.ToolNames, srv.ToolNames) {
		t.Fatalf("unprobed scan should carry the committed tool list %v, got %v", srv.ToolNames, got.ToolNames)
	}
	if got.SchemaHash != srv.SchemaHash || got.ConfigHash != srv.ConfigHash {
		t.Fatalf("carried server should hash like the probed one:\n probed  %+v\n carried %+v", srv, got)
	}
	if !strings.Contains(got.Source, "mcp-stdio") {
		t.Fatalf("carried server should keep the mcp-stdio tag, got %q", got.Source)
	}

	// A different command is a different server: the old tool list must not
	// vouch for it.
	cfg := helperServerCfg(t, "read_file", "send_email")
	args, _ := cfg["args"].([]string)
	cfg["args"] = append(args, "--changed")
	writeMCPConfig(t, root, cfg)
	changed, err := Scan(root)
	if err != nil {
		t.Fatal(err)
	}
	if changed.MCPServers[0].ToolNames != nil || changed.MCPServers[0].ConfigHash != "" {
		t.Fatalf("a changed config must drop the carried tool list, got %+v", changed.MCPServers[0])
	}
	if changed.MCPServers[0].SchemaHash == srv.SchemaHash {
		t.Fatal("a changed config must fall back to its config hash")
	}
}

// TestScanWithProbeIgnoresCommittedToolList: a probing scan reads the server,
// never the manifest, so a stale committed list cannot mask a new tool.
func TestScanWithProbeIgnoresCommittedToolList(t *testing.T) {
	root := t.TempDir()
	writeMCPConfig(t, root, helperServerCfg(t, "read_file"))
	first, err := ScanWith(root, Options{ProbeStdioMCP: true})
	if err != nil {
		t.Fatal(err)
	}
	// Same config hash, but the server now has one more tool. Simulate by
	// editing the committed manifest to claim fewer tools than the server has.
	first.MCPServers[0].ToolNames = nil
	if err := store.WriteManifest(store.ForRoot(root), first); err != nil {
		t.Fatal(err)
	}
	again, err := ScanWith(root, Options{ProbeStdioMCP: true})
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(again.MCPServers[0].ToolNames, []string{"read_file"}) {
		t.Fatalf("probe must read the live server, got %v", again.MCPServers[0].ToolNames)
	}
}
