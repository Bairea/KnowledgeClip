package bskclient

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"net"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// fakeDaemon is a minimal in-process bsk daemon: it accepts Unix-socket
// connections and answers each ndjson request frame via handler.
type fakeDaemon struct {
	ln      net.Listener
	sock    string
	mu      sync.Mutex
	handler func(method string, params json.RawMessage) (any, *RPCError)
	conns   []net.Conn
	wg      sync.WaitGroup
}

func newFakeDaemon(t *testing.T, handler func(string, json.RawMessage) (any, *RPCError)) *fakeDaemon {
	t.Helper()
	// macOS caps unix socket paths at ~104 chars; t.TempDir paths can exceed
	// that, so use a short root-level temp dir instead.
	dir, err := os.MkdirTemp("", "bskc")
	if err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	sock := filepath.Join(dir, "d.sock")
	ln, err := net.Listen("unix", sock)
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	d := &fakeDaemon{ln: ln, sock: sock, handler: handler}
	d.wg.Add(1)
	go d.acceptLoop()
	t.Cleanup(func() {
		ln.Close()
		d.mu.Lock()
		for _, c := range d.conns {
			c.Close()
		}
		d.mu.Unlock()
		d.wg.Wait()
	})
	return d
}

func (d *fakeDaemon) acceptLoop() {
	defer d.wg.Done()
	for {
		conn, err := d.ln.Accept()
		if err != nil {
			return
		}
		d.mu.Lock()
		d.conns = append(d.conns, conn)
		d.mu.Unlock()
		d.wg.Add(1)
		go func() {
			defer d.wg.Done()
			d.serve(conn)
		}()
	}
}

func (d *fakeDaemon) serve(conn net.Conn) {
	defer conn.Close()
	sc := bufio.NewScanner(conn)
	sc.Buffer(make([]byte, 0, 64*1024), 8*1024*1024)
	for sc.Scan() {
		var frame struct {
			ID     string          `json:"id"`
			Method string          `json:"method"`
			Params json.RawMessage `json:"params"`
		}
		if err := json.Unmarshal(sc.Bytes(), &frame); err != nil {
			continue
		}
		// Call the handler without holding d.mu: a handler may block
		// (e.g. waiting for the test to drop the connection), which must
		// not deadlock fakeDaemon.dropConnections.
		result, rpcErr := d.handler(frame.Method, frame.Params)
		resp := map[string]any{"id": frame.ID}
		if rpcErr != nil {
			resp["error"] = map[string]any{"code": rpcErr.Code, "message": rpcErr.Message}
		} else {
			resp["result"] = result
		}
		out, _ := json.Marshal(resp)
		conn.Write(append(out, '\n'))
	}
}

// dropNext forces the current connection closed (simulating daemon restart).
func (d *fakeDaemon) dropConnections() {
	d.mu.Lock()
	defer d.mu.Unlock()
	for _, c := range d.conns {
		c.Close()
	}
	d.conns = nil
}

func newTestClient(t *testing.T, d *fakeDaemon) *Client {
	t.Helper()
	cmd := "" // never start a real daemon from tests
	c := &Client{sockPath: d.sock, cmdPath: cmd, pending: make(map[string]*pendingCall)}
	t.Cleanup(func() { c.Close() })
	return c
}

func TestPingRoundTrip(t *testing.T) {
	d := newFakeDaemon(t, func(method string, _ json.RawMessage) (any, *RPCError) {
		if method == "system.ping" {
			return map[string]any{"pong": true}, nil
		}
		return nil, &RPCError{Code: CodeUnknownMethod, Message: method}
	})
	c := newTestClient(t, d)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := c.Ping(ctx); err != nil {
		t.Fatalf("ping: %v", err)
	}
}

func TestErrorFrameBecomesRPCError(t *testing.T) {
	d := newFakeDaemon(t, func(_ string, _ json.RawMessage) (any, *RPCError) {
		return nil, &RPCError{Code: CodeNotFound, Message: "session not registered"}
	})
	c := newTestClient(t, d)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	err := c.Ping(ctx)
	if !IsCode(err, CodeNotFound) {
		t.Fatalf("want not_found RPCError, got %v", err)
	}
	var re *RPCError
	if !errors.As(err, &re) || re.Message != "session not registered" {
		t.Fatalf("message lost: %v", err)
	}
}

func TestConcurrentMultiplexing(t *testing.T) {
	d := newFakeDaemon(t, func(method string, params json.RawMessage) (any, *RPCError) {
		var p struct {
			N int `json:"n"`
		}
		json.Unmarshal(params, &p)
		return map[string]any{"n": p.N * 2}, nil
	})
	c := newTestClient(t, d)

	var wg sync.WaitGroup
	var failures atomic.Int64
	for g := 0; g < 8; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ctx := context.Background()
			for i := 0; i < 25; i++ {
				var res struct {
					N int `json:"n"`
				}
				err := c.Call(ctx, "tool.evaluate", map[string]any{"n": i}, &res)
				if err != nil || res.N != i*2 {
					failures.Add(1)
					return
				}
			}
		}()
	}
	wg.Wait()
	if failures.Load() != 0 {
		t.Fatalf("%d concurrent calls failed", failures.Load())
	}
}

func TestReconnectAfterDrop(t *testing.T) {
	var calls atomic.Int64
	d := newFakeDaemon(t, func(_ string, _ json.RawMessage) (any, *RPCError) {
		calls.Add(1)
		return map[string]any{"pong": true}, nil
	})
	c := newTestClient(t, d)
	ctx := context.Background()

	if err := c.Ping(ctx); err != nil {
		t.Fatalf("first ping: %v", err)
	}

	// Kill the connection while the client holds it.
	d.dropConnections()

	// The next call must transparently re-dial and succeed.
	if err := c.Ping(ctx); err != nil {
		t.Fatalf("ping after drop: %v", err)
	}
	if calls.Load() != 2 {
		t.Fatalf("want 2 daemon calls, got %d", calls.Load())
	}
}

func TestInflightFailsOnDrop(t *testing.T) {
	release := make(chan struct{})
	handlerEntered := make(chan struct{}, 1)
	d := newFakeDaemon(t, func(_ string, _ json.RawMessage) (any, *RPCError) {
		select {
		case handlerEntered <- struct{}{}:
		default:
		}
		<-release
		return map[string]any{"pong": true}, nil
	})
	c := newTestClient(t, d)

	errCh := make(chan error, 1)
	go func() {
		errCh <- c.Ping(context.Background())
	}()

	// Deterministic: drop only after the request actually reached the daemon.
	select {
	case <-handlerEntered:
	case <-time.After(5 * time.Second):
		t.Fatal("request never reached the fake daemon")
	}
	d.dropConnections()
	close(release)

	// The call must not hang: the transport retry re-dials and succeeds now
	// that the daemon is answering again.
	select {
	case err := <-errCh:
		if err != nil {
			t.Fatalf("in-flight call should recover via retry, got: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("in-flight call hung after connection drop")
	}
}

func TestEvaluatePageError(t *testing.T) {
	d := newFakeDaemon(t, func(method string, params json.RawMessage) (any, *RPCError) {
		if method != "tool.evaluate" {
			return nil, &RPCError{Code: CodeUnknownMethod, Message: method}
		}
		var p struct {
			Expression string `json:"expression"`
			TimeoutMs  int64  `json:"timeout_ms"`
		}
		json.Unmarshal(params, &p)
		if p.Expression == "throw 1" {
			return map[string]any{
				"ok":    false,
				"tab_id": 42,
				"error": map[string]any{"text": "Error: boom", "line": 1, "column": 7},
			}, nil
		}
		if p.TimeoutMs != 15000 {
			t.Errorf("timeout_ms not forwarded: %d", p.TimeoutMs)
		}
		return map[string]any{"ok": true, "tab_id": 42, "value": "hello"}, nil
	})
	c := newTestClient(t, d)
	ctx := context.Background()

	v, err := c.EvaluateString(ctx, "sess", 42, "location.href", 15*time.Second)
	if err != nil || v != "hello" {
		t.Fatalf("evaluate: %v %q", err, v)
	}

	_, err = c.Evaluate(ctx, "sess", 42, "throw 1", time.Second)
	var js *JSError
	if !errors.As(err, &js) || js.Text != "Error: boom" {
		t.Fatalf("want JSError, got %v", err)
	}
}

func TestSessionStopTreatsNotFoundAsSuccess(t *testing.T) {
	d := newFakeDaemon(t, func(method string, params json.RawMessage) (any, *RPCError) {
		if method == "session.stop" {
			return nil, &RPCError{Code: CodeNotFound, Message: "session not registered or already stopped"}
		}
		return nil, &RPCError{Code: CodeUnknownMethod, Message: method}
	})
	c := newTestClient(t, d)
	if err := c.SessionStop(context.Background(), "abcd"); err != nil {
		t.Fatalf("session stop should swallow not_found: %v", err)
	}
}
