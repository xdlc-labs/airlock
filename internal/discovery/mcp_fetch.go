package discovery

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/xdlc-labs/airlock/internal/manifest"
)

// ponytail: HTTP/SSE url MCP only; stdio servers stay config-hash until spawn support lands.

type mcpServerCfg struct {
	URL     string            `json:"url"`
	Headers map[string]string `json:"headers"`
}

// enrichMCPSchemas fetches live tool schemas for MCP servers with http(s) urls.
func enrichMCPSchemas(ctx context.Context, client *http.Client, m *manifest.Manifest, configs map[string]json.RawMessage) {
	if client == nil {
		client = &http.Client{Timeout: 15 * time.Second}
	}
	for i := range m.MCPServers {
		raw, ok := configs[m.MCPServers[i].Name]
		if !ok {
			raw, ok = configs[m.MCPServers[i].ID]
		}
		if !ok {
			continue
		}
		var cfg mcpServerCfg
		if err := json.Unmarshal(raw, &cfg); err != nil || cfg.URL == "" {
			continue
		}
		if !strings.HasPrefix(strings.ToLower(cfg.URL), "http") {
			continue
		}
		schema, toolNames, err := fetchMCPToolsSchema(ctx, client, cfg)
		if err != nil {
			m.Unpinned = append(m.Unpinned, manifest.UnpinnedRisk{
				Artifact: "mcp:" + m.MCPServers[i].ID,
				Reason:   "live schema fetch: " + err.Error(),
			})
			continue
		}
		h := manifest.HashBytes(schema)
		if h != m.MCPServers[i].SchemaHash {
			m.MCPServers[i].SchemaHash = h
			if !strings.Contains(m.MCPServers[i].Source, "mcp-live") {
				m.MCPServers[i].Source += "+mcp-live"
			}
		}
		// Always refresh, independent of SchemaHash equality: this is the field
		// diff.permissionExpansion reads to catch new tools appearing on a
		// server whose Permissions[] was never hand-maintained.
		m.MCPServers[i].ToolNames = toolNames
	}
}

func fetchMCPToolsSchema(ctx context.Context, client *http.Client, cfg mcpServerCfg) ([]byte, []string, error) {
	if err := mcpRPC(ctx, client, cfg, "initialize", map[string]any{
		"protocolVersion": "2024-11-05",
		"capabilities":    map[string]any{},
		"clientInfo":      map[string]string{"name": "airlock", "version": "1"},
	}); err != nil {
		return nil, nil, err
	}
	var raw json.RawMessage
	if err := mcpRPCResult(ctx, client, cfg, "tools/list", map[string]any{}, &raw); err != nil {
		return nil, nil, err
	}
	var parsed struct {
		Tools []struct {
			Name string `json:"name"`
		} `json:"tools"`
	}
	var names []string
	if err := json.Unmarshal(raw, &parsed); err == nil {
		for _, t := range parsed.Tools {
			if t.Name != "" {
				names = append(names, t.Name)
			}
		}
		sort.Strings(names)
	}
	return raw, names, nil
}

func mcpRPC(ctx context.Context, client *http.Client, cfg mcpServerCfg, method string, params any) error {
	var discard any
	return mcpRPCResult(ctx, client, cfg, method, params, &discard)
}

func mcpRPCResult(ctx context.Context, client *http.Client, cfg mcpServerCfg, method string, params any, out any) error {
	body, err := json.Marshal(map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  method,
		"params":  params,
	})
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, cfg.URL, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	for k, v := range cfg.Headers {
		req.Header.Set(k, v)
	}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	if resp.StatusCode >= 300 {
		return fmt.Errorf("HTTP %s: %s", resp.Status, truncateMCP(data))
	}
	var envelope struct {
		Result json.RawMessage `json:"result"`
		Error  *struct {
			Code    int    `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(data, &envelope); err != nil {
		return err
	}
	if envelope.Error != nil {
		return fmt.Errorf("rpc %s: %s", method, envelope.Error.Message)
	}
	if out != nil && len(envelope.Result) > 0 {
		return json.Unmarshal(envelope.Result, out)
	}
	return nil
}

func truncateMCP(b []byte) string {
	if len(b) <= 120 {
		return string(b)
	}
	return string(b[:120]) + "..."
}

// mcpServersKey returns the server map under whichever key this config style
// uses: "mcpServers" for Claude Desktop, Cursor, and Gemini, "servers" for VS
// Code and Zed.
func mcpServersKey(raw map[string]json.RawMessage) (json.RawMessage, bool) {
	for _, key := range []string{"mcpServers", "servers", "context_servers"} {
		if v, ok := raw[key]; ok {
			return v, true
		}
	}
	return nil, false
}

// collectMCPConfigs gathers per-server raw JSON from parsed mcp config files.
func collectMCPConfigs(raw map[string]json.RawMessage) map[string]json.RawMessage {
	out := map[string]json.RawMessage{}
	if serversRaw, ok := mcpServersKey(raw); ok {
		var servers map[string]json.RawMessage
		if err := json.Unmarshal(serversRaw, &servers); err == nil {
			for name, cfg := range servers {
				out[name] = cfg
			}
		}
	}
	return out
}
