package engine

import (
	"context"
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"chat-aggregator/internal/engine/bskclient"
	"chat-aggregator/internal/models"
)

// bskPipelineFake is a fake bsk daemon that emulates a chat site's script
// responses (safeStringify-style JSON strings), letting the whole engine
// pipeline run offline: detect → send → wait → extract.
type bskPipelineFake struct {
	ln net.Listener

	mu           sync.Mutex
	waitNotDone  int  // evaluate calls answering done=false before done=true
	evalCalls    int
	failFirstN   int               // first N evaluates fail with failErr
	failErr      *bskclient.RPCError
	loginWall    bool              // detect_input reports a foreign login host
	sessions     []string
	stoppedCount int
}

func newBskPipelineFake(t *testing.T) *bskPipelineFake {
	t.Helper()
	dir, err := os.MkdirTemp("", "bskfake")
	if err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	ln, err := net.Listen("unix", filepath.Join(dir, "d.sock"))
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	f := &bskPipelineFake{ln: ln}
	t.Cleanup(func() { ln.Close() })
	go f.acceptLoop()
	return f
}

func (f *bskPipelineFake) acceptLoop() {
	for {
		conn, err := f.ln.Accept()
		if err != nil {
			return
		}
		go f.serve(conn)
	}
}

func (f *bskPipelineFake) serve(conn net.Conn) {
	defer conn.Close()
	buf := make([]byte, 0, 64*1024)
	tmp := make([]byte, 64*1024)
	for {
		n, err := conn.Read(tmp)
		if err != nil {
			return
		}
		buf = append(buf, tmp[:n]...)
		for {
			idx := indexByte(buf, '\n')
			if idx < 0 {
				break
			}
			line := buf[:idx]
			buf = buf[idx+1:]
			f.dispatch(conn, line)
		}
	}
}

func indexByte(b []byte, c byte) int {
	for i, x := range b {
		if x == c {
			return i
		}
	}
	return -1
}

func (f *bskPipelineFake) reply(conn net.Conn, id string, result map[string]any) {
	out, _ := json.Marshal(map[string]any{"id": id, "result": result})
	conn.Write(append(out, '\n'))
}

func (f *bskPipelineFake) replyErr(conn net.Conn, id, code, msg string) {
	out, _ := json.Marshal(map[string]any{
		"id":    id,
		"error": map[string]any{"code": code, "message": msg},
	})
	conn.Write(append(out, '\n'))
}

func (f *bskPipelineFake) dispatch(conn net.Conn, line []byte) {
	var frame struct {
		ID     string `json:"id"`
		Method string `json:"method"`
		Params struct {
			SessionID  string `json:"session_id"`
			Expression string `json:"expression"`
			URL        string `json:"url"`
		} `json:"params"`
	}
	if err := json.Unmarshal(line, &frame); err != nil {
		return
	}

	f.mu.Lock()
	defer f.mu.Unlock()

	switch frame.Method {
	case "system.status":
		f.reply(conn, frame.ID, map[string]any{
			"protocol_version": "1.1",
			"daemon_version":   "fake",
			"browsers":         []map[string]any{{"instance_id": "fake", "browser_name": "chrome"}},
		})
	case "session.start":
		sid := "s" + string(rune('a'+len(f.sessions))) + "xy"
		f.sessions = append(f.sessions, sid)
		f.reply(conn, frame.ID, map[string]any{"session_id": sid, "agent_window_id": 1})
	case "session.stop":
		f.stoppedCount++
		f.reply(conn, frame.ID, map[string]any{})
	case "tool.tab_list":
		f.reply(conn, frame.ID, map[string]any{"tabs": []map[string]any{}})
	case "tool.tab_create":
		f.reply(conn, frame.ID, map[string]any{"tab_id": 4242, "url": frame.Params.URL})
	case "tool.evaluate":
		f.evalCalls++
		if f.failFirstN > 0 && f.evalCalls <= f.failFirstN {
			f.replyErr(conn, frame.ID, f.failErr.Code, f.failErr.Message)
			return
		}
		expr := frame.Params.Expression
		var payload string
		switch {
		case strings.Contains(expr, "detect_input.js"):
			url := "https://test.local/"
			if f.loginWall {
				url = "https://account.login.example.com/"
			}
			b, _ := json.Marshal(map[string]any{"ready": !f.loginWall, "url": url})
			payload = string(b)
		case strings.Contains(expr, "send_prompt.js"):
			payload = `{"ok": true}`
		case strings.Contains(expr, "wait_answer.js"):
			if f.waitNotDone > 0 {
				f.waitNotDone--
				payload = `{"done": false, "url": "https://test.local/"}`
			} else {
				payload = `{"done": true, "url": "https://test.local/"}`
			}
		case strings.Contains(expr, "extract_answer.js"):
			payload = `{"text": "# 回答\n\n完整 **markdown** 内容。"}`
		default:
			payload = `{"ok": true}`
		}
		// Site scripts return safeStringify output: a JSON string wrapping JSON.
		f.reply(conn, frame.ID, map[string]any{
			"ok": true, "tab_id": 4242, "value": payload,
		})
	default:
		f.replyErr(conn, frame.ID, "unknown_method", frame.Method)
	}
}

// newBskFixtureScripts creates a minimal scripts dir satisfying the engine's
// script reads; the fake daemon never actually executes them.
func newBskFixtureScripts(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	site := filepath.Join(dir, "testsite")
	if err := os.MkdirAll(site, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"detect_input.js", "send_prompt.js", "wait_answer.js", "extract_answer.js", "new_chat.js"} {
		// The script name doubles as the fake daemon's routing marker: the
		// engine embeds the file content verbatim into the expression.
		content := "// fixture " + name + "\n1;"
		if err := os.WriteFile(filepath.Join(site, name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "lib.js"), []byte("globalThis.__KC_LIB__ = {};"), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

func newBskTestEngine(t *testing.T, f *bskPipelineFake, scriptsDir string) *BskEngine {
	t.Helper()
	return NewBskEngineWithClient(bskclient.NewWithSocket(f.ln.Addr().String()), scriptsDir)
}

func TestBskPipelineOffline(t *testing.T) {
	f := newBskPipelineFake(t)
	f.mu.Lock()
	f.waitNotDone = 2 // answer stabilizes on the 3rd poll
	f.mu.Unlock()

	eng := newBskTestEngine(t, f, newBskFixtureScripts(t))
	defer eng.Close()

	site := models.Site{ID: "testsite", Name: "Test", URL: "https://test.local/"}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	answer, err := eng.SendMessage(ctx, site, "你好")
	if err != nil {
		t.Fatalf("send message: %v", err)
	}
	if !strings.Contains(answer, "完整 **markdown** 内容") {
		t.Fatalf("unexpected answer: %q", answer)
	}

	// Session must be stopped on Close.
	if err := eng.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	f.mu.Lock()
	stopped := f.stoppedCount
	f.mu.Unlock()
	if stopped == 0 {
		t.Fatal("engine Close did not stop the bsk session")
	}
}

func TestBskPipelineSessionRecovery(t *testing.T) {
	f := newBskPipelineFake(t)
	f.mu.Lock()
	// First evaluate hits a dead session (daemon idle GC / extension
	// reconnect); the engine must rebuild the session and retry transparently.
	f.failFirstN = 1
	f.failErr = &bskclient.RPCError{Code: "not_found", Message: "session not registered or already stopped"}
	f.mu.Unlock()

	eng := newBskTestEngine(t, f, newBskFixtureScripts(t))
	defer eng.Close()

	site := models.Site{ID: "testsite", Name: "Test", URL: "https://test.local/"}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	answer, err := eng.SendMessage(ctx, site, "你好")
	if err != nil {
		t.Fatalf("send message should survive session loss: %v", err)
	}
	if answer == "" {
		t.Fatal("empty answer")
	}
	f.mu.Lock()
	nSessions := len(f.sessions)
	f.mu.Unlock()
	if nSessions < 2 {
		t.Fatalf("expected a rebuilt session, engine used %d", nSessions)
	}
}

func TestBskPipelineLoginWall(t *testing.T) {
	f := newBskPipelineFake(t)
	f.mu.Lock()
	f.loginWall = true
	f.mu.Unlock()

	eng := newBskTestEngine(t, f, newBskFixtureScripts(t))
	defer eng.Close()

	site := models.Site{ID: "testsite", Name: "Test", URL: "https://test.local/"}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	start := time.Now()
	_, err := eng.SendMessage(ctx, site, "你好")
	if err == nil {
		t.Fatal("login wall should fail the send")
	}
	if !strings.Contains(err.Error(), "未登录") && !strings.Contains(err.Error(), "account.login.example.com") {
		t.Fatalf("error should mention the login redirect, got: %v", err)
	}
	if elapsed := time.Since(start); elapsed > 10*time.Second {
		t.Fatalf("login wall should fail fast, took %s", elapsed)
	}
}
