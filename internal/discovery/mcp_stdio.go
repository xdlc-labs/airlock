package discovery

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"
)

// Spawning a server is opt-in (see Options.ProbeStdioMCP), so these bounds exist
// to keep a hostile or wedged server from hanging or flooding a scan, not to make
// running one safe.
const (
	stdioWaitDelay = 2 * time.Second
	stdioMaxLine   = 4 << 20 // one JSON-RPC message
	stdioMaxStderr = 4 << 10 // kept only for the error message
)

// stdioProbeTimeout bounds one server's whole handshake. A var so tests can
// shorten it.
var stdioProbeTimeout = 20 * time.Second

// probeStdioMCPTools starts a stdio MCP server, asks it for its tool list, and
// stops it. The command is taken from the repository's MCP config and run with
// the caller's environment; callers must have opted in.
func probeStdioMCPTools(ctx context.Context, dir string, cfg mcpServerCfg) (json.RawMessage, []string, error) {
	if cfg.Command == "" {
		return nil, nil, errors.New("no command in config")
	}
	ctx, cancel := context.WithTimeout(ctx, stdioProbeTimeout)
	defer cancel()

	// No shell: the command and its arguments are passed through as given.
	cmd := exec.CommandContext(ctx, cfg.Command, cfg.Args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), envPairs(cfg.Env)...)
	cmd.WaitDelay = stdioWaitDelay

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, nil, err
	}
	errBuf := &limitBuffer{max: stdioMaxStderr}
	cmd.Stderr = errBuf

	if err := cmd.Start(); err != nil {
		return nil, nil, err
	}
	defer func() {
		_ = stdin.Close()
		cancel()
		_ = cmd.Wait()
	}()

	conn := &stdioConn{w: stdin, r: newLineReader(stdout)}
	if err := conn.call(1, "initialize", map[string]any{
		"protocolVersion": mcpProtocolVersion,
		"capabilities":    map[string]any{},
		"clientInfo":      map[string]string{"name": "airlock", "version": "1"},
	}, nil); err != nil {
		return nil, nil, withStderr(err, errBuf)
	}
	if err := conn.notify("notifications/initialized", map[string]any{}); err != nil {
		return nil, nil, withStderr(err, errBuf)
	}
	var raw json.RawMessage
	if err := conn.call(2, "tools/list", map[string]any{}, &raw); err != nil {
		return nil, nil, withStderr(err, errBuf)
	}
	return raw, toolNamesFromResult(raw), nil
}

func withStderr(err error, buf *limitBuffer) error {
	msg := strings.TrimSpace(buf.String())
	if msg == "" {
		return err
	}
	return fmt.Errorf("%w (stderr: %s)", err, truncateMCP([]byte(msg)))
}

func envPairs(env map[string]string) []string {
	out := make([]string, 0, len(env))
	for k, v := range env {
		out = append(out, k+"="+v)
	}
	return out
}

// stdioConn speaks newline-delimited JSON-RPC to a spawned MCP server.
type stdioConn struct {
	w io.Writer
	r *bufio.Scanner
}

func (c *stdioConn) notify(method string, params any) error {
	return c.write(map[string]any{"jsonrpc": "2.0", "method": method, "params": params})
}

func (c *stdioConn) call(id int, method string, params any, out any) error {
	if err := c.write(map[string]any{
		"jsonrpc": "2.0",
		"id":      id,
		"method":  method,
		"params":  params,
	}); err != nil {
		return err
	}
	return c.awaitResult(id, method, out)
}

func (c *stdioConn) write(msg map[string]any) error {
	body, err := json.Marshal(msg)
	if err != nil {
		return err
	}
	if _, err := c.w.Write(append(body, '\n')); err != nil {
		return fmt.Errorf("write %v: %w", msg["method"], err)
	}
	return nil
}

// awaitResult reads until the response with the matching id. Servers are free to
// log to stdout and to send notifications before answering, so anything that is
// not the response we asked for is skipped.
func (c *stdioConn) awaitResult(id int, method string, out any) error {
	for c.r.Scan() {
		line := strings.TrimSpace(c.r.Text())
		if line == "" {
			continue
		}
		var env rpcEnvelope
		if err := json.Unmarshal([]byte(line), &env); err != nil {
			continue
		}
		if env.ID == nil || *env.ID != id {
			continue
		}
		if env.Error != nil {
			return fmt.Errorf("rpc %s: %s", method, env.Error.Message)
		}
		if out != nil && len(env.Result) > 0 {
			return json.Unmarshal(env.Result, out)
		}
		return nil
	}
	if err := c.r.Err(); err != nil {
		return fmt.Errorf("read %s: %w", method, err)
	}
	return fmt.Errorf("rpc %s: server closed without answering", method)
}

func newLineReader(r io.Reader) *bufio.Scanner {
	s := bufio.NewScanner(io.LimitReader(r, stdioMaxLine*8))
	s.Buffer(make([]byte, 0, 64<<10), stdioMaxLine)
	return s
}

// limitBuffer keeps the first max bytes written to it and drops the rest.
type limitBuffer struct {
	mu  sync.Mutex
	max int
	buf []byte
}

func (b *limitBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if room := b.max - len(b.buf); room > 0 {
		if len(p) < room {
			room = len(p)
		}
		b.buf = append(b.buf, p[:room]...)
	}
	return len(p), nil
}

func (b *limitBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return string(b.buf)
}
