package discovery

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/xdlc-labs/airlock/internal/manifest"
)

func TestLiveMCPSchemaFetch(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Method string `json:"method"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		w.Header().Set("Content-Type", "application/json")
		switch req.Method {
		case "initialize":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"jsonrpc": "2.0", "id": 1,
				"result": map[string]any{"protocolVersion": "2024-11-05"},
			})
		case "tools/list":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"jsonrpc": "2.0", "id": 1,
				"result": map[string]any{
					"tools": []map[string]any{{
						"name":        "read_file",
						"description": "read",
						"inputSchema": map[string]any{"type": "object"},
					}},
				},
			})
		default:
			http.Error(w, "unknown", http.StatusBadRequest)
		}
	}))
	defer srv.Close()

	cfgRaw, _ := json.Marshal(map[string]any{"url": srv.URL})
	m := &manifest.Manifest{
		MCPServers: []manifest.MCPServer{{
			ID: "mcp-live", Name: "live", SchemaHash: "config-only", Source: "mcp.json",
		}},
	}
	configs := map[string]json.RawMessage{"live": cfgRaw}
	enrichMCPSchemas(context.Background(), srv.Client(), m, configs, Options{})
	if m.MCPServers[0].SchemaHash == "config-only" {
		t.Fatal("expected live schema hash to replace config-only hash")
	}
	if !strings.Contains(m.MCPServers[0].Source, "mcp-live") {
		t.Fatalf("expected mcp-live source tag, got %q", m.MCPServers[0].Source)
	}
	if len(m.MCPServers[0].ToolNames) != 1 || m.MCPServers[0].ToolNames[0] != "read_file" {
		t.Fatalf("expected ToolNames [read_file], got %v", m.MCPServers[0].ToolNames)
	}
}
