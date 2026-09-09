package discovery

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/xdlc-labs/airlock/internal/manifest"
)

// TestHelperMCPServer is not a real test: it is the stdio MCP server the tests
// below spawn, re-executing this test binary. It answers initialize and
// tools/list, and writes a line of noise first to prove the client skips
// anything that is not the response it asked for.
func TestHelperMCPServer(t *testing.T) {
	if os.Getenv("AIRLOCK_TEST_MCP_SERVER") != "1" {
		t.Skip("helper process")
	}
	out := bufio.NewWriter(os.Stdout)
	defer out.Flush()
	fmt.Fprintln(out, "server starting, this is not JSON")
	out.Flush()

	if os.Getenv("AIRLOCK_TEST_MCP_MODE") == "hang" {
		time.Sleep(time.Minute) // killed by the probe's timeout
		return
	}

	tools := strings.Split(os.Getenv("AIRLOCK_TEST_MCP_TOOLS"), ",")
	in := bufio.NewScanner(os.Stdin)
	for in.Scan() {
		var req struct {
			ID     *int   `json:"id"`
			Method string `json:"method"`
		}
		if err := json.Unmarshal(in.Bytes(), &req); err != nil || req.ID == nil {
			continue // notification or noise
		}
		var result any
		switch req.Method {
		case "initialize":
			result = map[string]any{"protocolVersion": mcpProtocolVersion}
		case "tools/list":
			list := make([]map[string]any, 0, len(tools))
			for _, name := range tools {
				if name == "" {
					continue
				}
				list = append(list, map[string]any{"name": name, "inputSchema": map[string]any{"type": "object"}})
			}
			result = map[string]any{"tools": list}
		default:
			continue
		}
		_ = json.NewEncoder(out).Encode(map[string]any{"jsonrpc": "2.0", "id": *req.ID, "result": result})
		out.Flush()
	}
}

// helperServerCfg builds an mcp.json server entry that runs TestHelperMCPServer.
func helperServerCfg(t *testing.T, tools ...string) map[string]any {
	t.Helper()
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	return map[string]any{
		"command": self,
		"args":    []string{"-test.run=TestHelperMCPServer"},
		"env": map[string]string{
			"AIRLOCK_TEST_MCP_SERVER": "1",
			"AIRLOCK_TEST_MCP_TOOLS":  strings.Join(tools, ","),
		},
	}
}

func mcpManifest(root string) (*manifest.Manifest, []manifest.MCPServer) {
	m := &manifest.Manifest{
		Root: root,
		MCPServers: []manifest.MCPServer{{
			ID: "mcp-local", Name: "local", SchemaHash: "config-only", Source: "mcp.json",
		}},
	}
	return m, m.MCPServers
}

func TestStdioMCPProbeReadsToolList(t *testing.T) {
	raw, err := json.Marshal(helperServerCfg(t, "read_file", "delete_file"))
	if err != nil {
		t.Fatal(err)
	}
	m, _ := mcpManifest(t.TempDir())
	enrichMCPSchemas(context.Background(), nil, m, map[string]json.RawMessage{"local": raw}, Options{ProbeStdioMCP: true})

	got := m.MCPServers[0]
	if got.SchemaHash == "config-only" {
		t.Fatal("expected the live schema hash to replace the config-only hash")
	}
	if !strings.Contains(got.Source, "mcp-stdio") {
		t.Fatalf("expected mcp-stdio source tag, got %q", got.Source)
	}
	want := []string{"delete_file", "read_file"} // sorted
	if len(got.ToolNames) != 2 || got.ToolNames[0] != want[0] || got.ToolNames[1] != want[1] {
		t.Fatalf("ToolNames = %v, want %v", got.ToolNames, want)
	}
	if len(m.Unpinned) != 0 {
		t.Fatalf("expected no unpinned risk, got %+v", m.Unpinned)
	}
}

func TestStdioMCPNotProbedByDefault(t *testing.T) {
	// A command that would fail loudly if it ever ran.
	raw, err := json.Marshal(map[string]any{
		"command": filepath.Join(t.TempDir(), "does-not-exist"),
		"args":    []string{"--boom"},
	})
	if err != nil {
		t.Fatal(err)
	}
	m, before := mcpManifest(t.TempDir())
	enrichMCPSchemas(context.Background(), nil, m, map[string]json.RawMessage{"local": raw}, Options{})

	if m.MCPServers[0].SchemaHash != before[0].SchemaHash {
		t.Fatal("default scan must leave a stdio server at its config hash")
	}
	if m.MCPServers[0].ToolNames != nil {
		t.Fatalf("default scan must not read a tool list, got %v", m.MCPServers[0].ToolNames)
	}
	if len(m.Unpinned) != 0 {
		t.Fatalf("skipping a stdio server is not a risk finding, got %+v", m.Unpinned)
	}
}

func TestStdioMCPProbeFailureIsRecorded(t *testing.T) {
	raw, err := json.Marshal(map[string]any{
		"command": filepath.Join(t.TempDir(), "does-not-exist"),
	})
	if err != nil {
		t.Fatal(err)
	}
	m, _ := mcpManifest(t.TempDir())
	enrichMCPSchemas(context.Background(), nil, m, map[string]json.RawMessage{"local": raw}, Options{ProbeStdioMCP: true})

	if len(m.Unpinned) != 1 {
		t.Fatalf("expected one unpinned risk, got %+v", m.Unpinned)
	}
	if m.MCPServers[0].SchemaHash != "config-only" {
		t.Fatal("a failed probe must leave the config hash alone")
	}
}

func TestStdioMCPProbeTimesOutOnHungServer(t *testing.T) {
	defer func(d time.Duration) { stdioProbeTimeout = d }(stdioProbeTimeout)
	stdioProbeTimeout = 200 * time.Millisecond

	cfg := mcpServerCfg{}
	raw, err := json.Marshal(helperServerCfg(t))
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(raw, &cfg); err != nil {
		t.Fatal(err)
	}
	cfg.Env["AIRLOCK_TEST_MCP_MODE"] = "hang"

	start := time.Now()
	if _, _, err := probeStdioMCPTools(context.Background(), t.TempDir(), cfg); err == nil {
		t.Fatal("expected an error from a server that never answers")
	}
	if elapsed := time.Since(start); elapsed > 10*time.Second {
		t.Fatalf("probe took %s to give up on a hung server", elapsed)
	}
}

func TestScanWithStdioProbe(t *testing.T) {
	root := t.TempDir()
	cfg := map[string]any{"mcpServers": map[string]any{"local": helperServerCfg(t, "send_email")}}
	data, err := json.Marshal(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "mcp.json"), data, 0o644); err != nil {
		t.Fatal(err)
	}

	plain, err := Scan(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(plain.MCPServers) != 1 || plain.MCPServers[0].ToolNames != nil {
		t.Fatalf("Scan must not probe stdio servers, got %+v", plain.MCPServers)
	}

	probed, err := ScanWith(root, Options{ProbeStdioMCP: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(probed.MCPServers) != 1 {
		t.Fatalf("expected one MCP server, got %+v", probed.MCPServers)
	}
	if names := probed.MCPServers[0].ToolNames; len(names) != 1 || names[0] != "send_email" {
		t.Fatalf("ToolNames = %v, want [send_email]", names)
	}
}
