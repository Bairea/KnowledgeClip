// Package bskclient speaks the browser-skill (bsk) daemon's IPC protocol
// directly over its Unix socket, replacing per-call `bsk` CLI subprocesses.
//
// Protocol (bsk 0.2.x, protocol_version 1.1, reverse-engineered from the
// binary and verified live):
//
//	transport: newline-delimited JSON over ~/.bsk/run/daemon.sock
//	request:   {"id":"<string>","method":"system.ping","params":{...}}
//	response:  {"id":"<string>","result":<value>}
//	           {"id":"<string>","error":{"code":"not_found","message":"..."}}
//
// In-flight requests are multiplexed by id on one connection; the daemon
// interleaves responses. JS-level evaluation failures are NOT error frames —
// they arrive as result.ok=false with result.error.text.
//
// The bsk binary is used only for daemon lifecycle (`daemon start`); all
// runtime traffic goes through the socket.
package bskclient

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"sync"
	"time"
)

// RPCError is a structured daemon-level error (the "error" frame).
type RPCError struct {
	Code    string
	Message string
}

func (e *RPCError) Error() string {
	if e.Code == "" {
		return e.Message
	}
	return e.Code + ": " + e.Message
}

// Error codes observed from the daemon.
const (
	CodeUnknownMethod        = "unknown_method"
	CodeUnsupported          = "unsupported"
	CodeInvalidParams        = "invalid_params"
	CodeNotFound             = "not_found"
	CodePermissionDenied     = "permission_denied"
	CodeTimeout              = "timeout"
	CodeCdpFailed            = "cdp_failed"
	CodeProtocolError        = "protocol_error"
	CodeCancelled            = "cancelled"
	CodeUserAborted          = "user_aborted"
	CodeVersionTooOld        = "version_too_old"
	CodeMultipleBrowsers     = "multiple_browsers_online"
	CodeNoBrowserConnected   = "no_browser_connected"
)

// IsCode reports whether err is an *RPCError with the given code.
func IsCode(err error, code string) bool {
	var re *RPCError
	if errors.As(err, &re) {
		return re.Code == code
	}
	return false
}

// JSError is a JavaScript exception thrown inside the page during evaluate.
type JSError struct {
	Text string
}

func (e *JSError) Error() string { return "page script error: " + e.Text }

type rpcResponse struct {
	ID     string          `json:"id"`
	Result json.RawMessage `json:"result"`
	Err    *struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

type pendingCall struct {
	ch   chan rpcResponse
	self chan struct{} // closed to abandon the call (ctx done)
}

type requestFrame struct {
	ID     string          `json:"id"`
	Method string          `json:"method"`
	Params json.RawMessage `json:"params"`
}

// Client is a concurrency-safe bsk daemon IPC client. One connection is
// shared and requests are multiplexed; a broken connection fails all
// in-flight calls once and transparently re-dials on the next call.
type Client struct {
	sockPath string
	cmdPath  string // bsk binary for `daemon start` only

	mu      sync.Mutex
	conn    net.Conn
	nextID  uint64
	pending map[string]*pendingCall
	closed  bool

	dialMu sync.Mutex
}

// New creates a client with default paths: socket at $HOME/.bsk/run/daemon.sock
// (override with BSK_SOCK), bsk binary resolved from PATH.
func New() *Client {
	sock := os.Getenv("BSK_SOCK")
	if sock == "" {
		home, err := os.UserHomeDir()
		if err == nil {
			sock = filepath.Join(home, ".bsk", "run", "daemon.sock")
		}
	}
	cmdPath, _ := exec.LookPath("bsk")
	return &Client{
		sockPath: sock,
		cmdPath:  cmdPath,
		pending:  make(map[string]*pendingCall),
	}
}

// Status is the parsed `system.status` payload.
type Status struct {
	DaemonVersion       string            `json:"daemon_version"`
	ProtocolVersion     string            `json:"protocol_version"`
	PID                 int               `json:"pid"`
	UptimeSecs          int64             `json:"uptime_secs"`
	Browsers            []BrowserStatus   `json:"browsers"`
	VersionSkewBrowsers []json.RawMessage `json:"version_skew_browsers"`
	Sessions            json.RawMessage   `json:"sessions"`
}

// Connected reports whether at least one browser has the extension attached.
func (s *Status) Connected() bool { return len(s.Browsers) > 0 }

// BrowserStatus describes one browser with the extension connected.
type BrowserStatus struct {
	InstanceID               string `json:"instance_id"`
	BrowserName              string `json:"browser_name"`
	BrowserVersion           string `json:"browser_version"`
	ExtensionVersion         string `json:"extension_version"`
	Label                    string `json:"label"`
	SessionCount             int    `json:"session_count"`
	VersionSkew              bool   `json:"version_skew"`
	ExtensionProtocolVersion string `json:"extension_protocol_version"`
}

// SupportedProtocol is the IPC protocol version this package speaks.
const SupportedProtocol = "1.1"

// Status queries the daemon. It fails with a friendly error when the daemon
// is unreachable or no browser extension is connected, and warns on protocol
// version mismatch.
func (c *Client) Status(ctx context.Context) (*Status, error) {
	if err := c.ensureDaemon(ctx); err != nil {
		return nil, err
	}
	var raw Status
	if err := c.call(ctx, "system.status", map[string]any{}, &raw); err != nil {
		return nil, err
	}
	return &raw, nil
}

// Ping does a cheap liveness round-trip.
func (c *Client) Ping(ctx context.Context) error {
	var res struct {
		Pong bool `json:"pong"`
	}
	return c.call(ctx, "system.ping", map[string]any{}, &res)
}

// Call performs one RPC. result may be nil; params is marshalled as-is
// (pass map[string]any{} when the method takes no parameters).
func (c *Client) Call(ctx context.Context, method string, params any, result any) error {
	if err := c.ensureDaemon(ctx); err != nil {
		return err
	}
	return c.call(ctx, method, params, result)
}

// --- session lifecycle ------------------------------------------------------

type SessionStartResult struct {
	SessionID         string `json:"session_id"`
	AgentWindowID     int64  `json:"agent_window_id"`
	BrowserInstanceID string `json:"browser_instance_id"`
}

// SessionStart starts a session; noFocus opens the Agent Window without
// stealing desktop focus.
func (c *Client) SessionStart(ctx context.Context, noFocus bool) (*SessionStartResult, error) {
	params := map[string]any{}
	if noFocus {
		params["no_focus"] = true
	}
	var res SessionStartResult
	if err := c.Call(ctx, "session.start", params, &res); err != nil {
		return nil, err
	}
	if res.SessionID == "" {
		return nil, errors.New("bskclient: session.start returned no session_id")
	}
	return &res, nil
}

// SessionStop stops a session (the Agent Window closes; borrowed tabs return).
// A not_found error is treated as success (already stopped).
func (c *Client) SessionStop(ctx context.Context, sessionID string) error {
	err := c.Call(ctx, "session.stop", map[string]any{"session_id": sessionID}, nil)
	if IsCode(err, CodeNotFound) {
		return nil
	}
	return err
}

// SessionList lists active session ids.
func (c *Client) SessionList(ctx context.Context) ([]string, error) {
	var res struct {
		Sessions []struct {
			SessionID string `json:"session_id"`
		} `json:"sessions"`
	}
	if err := c.Call(ctx, "session.list", map[string]any{}, &res); err != nil {
		return nil, err
	}
	ids := make([]string, 0, len(res.Sessions))
	for _, s := range res.Sessions {
		ids = append(ids, s.SessionID)
	}
	return ids, nil
}

// --- tabs --------------------------------------------------------------------

type TabInfo struct {
	TabID    int64  `json:"tab_id"`
	URL      string `json:"url"`
	Title    string `json:"title"`
	Scope    string `json:"scope"`
	Active   bool   `json:"active"`
	WindowID int64  `json:"window_id"`
}

// TabList lists tabs visible to the session (agent and borrowed user tabs).
func (c *Client) TabList(ctx context.Context, sessionID string) ([]TabInfo, error) {
	var res struct {
		Tabs []TabInfo `json:"tabs"`
	}
	if err := c.Call(ctx, "tool.tab_list", map[string]any{"session_id": sessionID}, &res); err != nil {
		return nil, err
	}
	return res.Tabs, nil
}

// TabCreate opens a new tab in the session's Agent Window.
func (c *Client) TabCreate(ctx context.Context, sessionID, url string) (int64, error) {
	var res struct {
		TabID int64 `json:"tab_id"`
	}
	params := map[string]any{"session_id": sessionID}
	if url != "" {
		params["url"] = url
	}
	if err := c.Call(ctx, "tool.tab_create", params, &res); err != nil {
		return 0, err
	}
	if res.TabID == 0 {
		return 0, errors.New("bskclient: tab_create returned tab_id 0")
	}
	return res.TabID, nil
}

// TabClose closes a tab; a not_found error is treated as success.
func (c *Client) TabClose(ctx context.Context, sessionID string, tabID int64) error {
	err := c.Call(ctx, "tool.tab_close", map[string]any{"session_id": sessionID, "tab_id": tabID}, nil)
	if IsCode(err, CodeNotFound) {
		return nil
	}
	return err
}

// --- evaluate ------------------------------------------------------------------

// EvalErrorDetail carries the location info of a page-side JS exception.
type EvalErrorDetail struct {
	Text   string `json:"text"`
	Line   int    `json:"line"`
	Column int    `json:"column"`
}

type evalResult struct {
	OK    bool             `json:"ok"`
	TabID int64            `json:"tab_id"`
	Value json.RawMessage  `json:"value"`
	Error *EvalErrorDetail `json:"error"`
}

// Evaluate runs a JS expression on a specific tab. It returns the raw JSON
// value; page-side exceptions surface as *JSError, daemon/tab problems as
// *RPCError.
func (c *Client) Evaluate(ctx context.Context, sessionID string, tabID int64, expr string, timeout time.Duration) (json.RawMessage, error) {
	params := map[string]any{
		"session_id":      sessionID,
		"expression":      expr,
		"return_by_value": true,
		"await_promise":   true,
	}
	if tabID != 0 {
		params["tab_id"] = tabID
	}
	if timeout > 0 {
		params["timeout_ms"] = timeout.Milliseconds()
	}
	var res evalResult
	if err := c.call(ctx, "tool.evaluate", params, &res); err != nil {
		return nil, err
	}
	if !res.OK {
		text := "unknown page error"
		if res.Error != nil && res.Error.Text != "" {
			text = res.Error.Text
		}
		return nil, &JSError{Text: text}
	}
	return res.Value, nil
}

// EvaluateString is Evaluate with a string return convenience.
func (c *Client) EvaluateString(ctx context.Context, sessionID string, tabID int64, expr string, timeout time.Duration) (string, error) {
	raw, err := c.Evaluate(ctx, sessionID, tabID, expr, timeout)
	if err != nil {
		return "", err
	}
	var s string
	if err := json.Unmarshal(raw, &s); err != nil {
		return "", fmt.Errorf("evaluate returned non-string: %w", err)
	}
	return s, nil
}

// --- daemon lifecycle -------------------------------------------------------

// ensureDaemon makes sure the daemon is reachable, starting it via the bsk
// CLI when the socket is missing. Cheap no-op once the socket dials.
func (c *Client) ensureDaemon(ctx context.Context) error {
	if _, err := os.Stat(c.sockPath); err == nil {
		return nil
	}
	c.dialMu.Lock()
	defer c.dialMu.Unlock()
	if _, err := os.Stat(c.sockPath); err == nil {
		return nil
	}
	if c.cmdPath == "" {
		return errors.New("bsk daemon not running and bsk CLI not found on PATH (install browser-skill)")
	}
	startCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	cmd := exec.CommandContext(startCtx, c.cmdPath, "daemon", "start", "--quiet")
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("bsk daemon start: %w (%s)", err, truncate(out, 200))
	}
	// Wait for the socket to appear.
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(c.sockPath); err == nil {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(200 * time.Millisecond):
		}
	}
	return errors.New("bsk daemon socket did not appear after daemon start: " + c.sockPath)
}

// --- transport ---------------------------------------------------------------

func (c *Client) call(ctx context.Context, method string, params any, result any) error {
	paramsRaw, err := json.Marshal(params)
	if err != nil {
		return fmt.Errorf("marshal %s params: %w", method, err)
	}

	conn, err := c.getConn()
	if err != nil {
		return fmt.Errorf("bsk daemon connect: %w", err)
	}

	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return errors.New("bskclient: client closed")
	}
	c.nextID++
	id := strconv.FormatUint(c.nextID, 10)
	full, err := json.Marshal(requestFrame{
		ID:     id,
		Method: method,
		Params: paramsRaw,
	})
	if err != nil {
		c.mu.Unlock()
		return err
	}

	pending := &pendingCall{ch: make(chan rpcResponse, 1), self: make(chan struct{})}
	c.pending[id] = pending
	if _, werr := conn.Write(append(full, '\n')); werr != nil {
		delete(c.pending, id)
		c.dropConnLocked(conn)
		c.mu.Unlock()
		return fmt.Errorf("bsk daemon write (%s): %w", method, werr)
	}
	c.mu.Unlock()

	var resp rpcResponse
	select {
	case resp = <-pending.ch:
	case <-pending.self:
		return fmt.Errorf("bsk %s: call abandoned", method)
	case <-ctx.Done():
		// Deregister so a late response doesn't leak.
		c.mu.Lock()
		delete(c.pending, id)
		c.mu.Unlock()
		return fmt.Errorf("bsk %s: %w", method, ctx.Err())
	}

	if resp.Err != nil {
		return &RPCError{Code: resp.Err.Code, Message: resp.Err.Message}
	}
	if result != nil && len(resp.Result) > 0 {
		if err := json.Unmarshal(resp.Result, result); err != nil {
			return fmt.Errorf("bsk %s: decode result: %w", method, err)
		}
	}
	return nil
}

// getConn returns the live connection, dialing a new one (with a reader
// goroutine) when needed.
func (c *Client) getConn() (net.Conn, error) {
	c.mu.Lock()
	conn := c.conn
	c.mu.Unlock()
	if conn != nil {
		return conn, nil
	}

	c.dialMu.Lock()
	defer c.dialMu.Unlock()
	c.mu.Lock()
	conn = c.conn
	c.mu.Unlock()
	if conn != nil {
		return conn, nil
	}

	d := net.Dialer{Timeout: 5 * time.Second}
	nc, err := d.DialContext(context.Background(), "unix", c.sockPath)
	if err != nil {
		return nil, err
	}
	c.mu.Lock()
	// Another dial may have won the race.
	if c.conn != nil {
		loser := nc
		c.mu.Unlock()
		loser.Close()
		return c.conn, nil
	}
	c.conn = nc
	c.mu.Unlock()
	go c.readLoop(nc)
	return nc, nil
}

// readLoop routes responses to pending calls; on exit it fails everything
// still registered so callers can retry on a fresh connection.
func (c *Client) readLoop(conn net.Conn) {
	sc := bufio.NewScanner(conn)
	sc.Buffer(make([]byte, 0, 64*1024), 32*1024*1024)
	for sc.Scan() {
		line := sc.Bytes()
		if len(line) == 0 {
			continue
		}
		var resp rpcResponse
		if err := json.Unmarshal(line, &resp); err != nil {
			continue // not a response frame (future: events)
		}
		if resp.ID == "" {
			continue
		}
		c.mu.Lock()
		p := c.pending[resp.ID]
		delete(c.pending, resp.ID)
		c.mu.Unlock()
		if p != nil {
			select {
			case p.ch <- resp:
			case <-p.self:
			}
		}
	}
	_ = sc.Err()

	// Connection died: fail all in-flight calls on it.
	c.mu.Lock()
	if c.conn == conn {
		c.conn = nil
	}
	for id, p := range c.pending {
		close(p.self)
		delete(c.pending, id)
	}
	c.mu.Unlock()
	conn.Close()
}

// dropConnLocked tears down the connection after a synchronous write error.
// In-flight calls fail via readLoop's cleanup when the reader notices EOF.
// Caller holds c.mu.
func (c *Client) dropConnLocked(conn net.Conn) {
	if c.conn == conn {
		c.conn = nil
	}
	conn.Close()
}

// Close tears the client down, failing all in-flight calls.
func (c *Client) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return nil
	}
	c.closed = true
	if c.conn != nil {
		c.conn.Close()
		c.conn = nil
	}
	for id, p := range c.pending {
		close(p.self)
		delete(c.pending, id)
	}
	return nil
}

func truncate(b []byte, max int) string {
	if len(b) <= max {
		return string(b)
	}
	return string(b[:max]) + "..."
}
