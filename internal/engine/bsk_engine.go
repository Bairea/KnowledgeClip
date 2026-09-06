package engine

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"chat-aggregator/internal/engine/bskclient"
	"chat-aggregator/internal/models"
)

// BskEngine drives the user's real Chromium through the browser-skill daemon,
// speaking its IPC protocol directly (internal/engine/bskclient). It is an
// optional extension only:
//
//   - never embedded/packaged into the distributed binary (scripts are read
//     from disk via getScriptsDir);
//   - never part of the default engine fallback chain — a site reaches this
//     engine only when its `engine.primary` is explicitly `bsk`;
//   - requires the browser-skill extension to be connected in the user's
//     Chromium (verified at session start).
//
// Concurrency model: all runtime traffic multiplexes over one daemon
// connection; evaluations target per-site tabs via tab_id and never switch
// the active tab, so sites poll in parallel (no engine-wide mutex). Only
// session/tab bookkeeping is guarded by mu, and it never spans a daemon call.
//
// Resilience model: the daemon stops sessions idle for 5 minutes and the
// extension's WebSocket reconnects can drop them; any not_found error
// transparently rebuilds the session (which also rebuilds every site tab)
// or just the missing tab, then retries the step once.
type BskEngine struct {
	client     *bskclient.Client
	scriptsDir string

	libOnce sync.Once
	lib     string

	mu      sync.Mutex
	session string
	tabs    map[string]*bskTab // siteID -> tab
}

type bskTab struct {
	siteID string
	tabID  int64
	url    string
}

// Tunables (environment overrides keep the defaults out of the code paths).
func bskEvalTimeout() time.Duration {
	if v := os.Getenv("BSK_EVAL_TIMEOUT"); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			return d
		}
	}
	return 30 * time.Second
}

func bskWaitDeadline() time.Duration {
	if v := os.Getenv("BSK_WAIT_DEADLINE"); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			return d
		}
	}
	return 180 * time.Second
}

// bskResendAfter is how long waitForAnswer tolerates "prompt accepted but no
// assistant content" before resending the prompt once.
func bskResendAfter() time.Duration {
	if v := os.Getenv("BSK_RESEND_AFTER"); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			return d
		}
	}
	return 45 * time.Second
}

// bskSendVerifyAfter is how long to wait before probing whether the send
// actually consumed the prompt (editor still holds the text = failed send).
func bskSendVerifyAfter() time.Duration {
	if v := os.Getenv("BSK_SEND_VERIFY_AFTER"); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			return d
		}
	}
	return 8 * time.Second
}

// editorTextLen returns the length of the longest text sitting in any page
// editor (textarea / contenteditable), via a site-agnostic inline probe.
// -1 when the probe cannot run (treated as "unknown").
func (e *BskEngine) editorTextLen(ctx context.Context, site models.Site, tab *bskTab) int {
	sid, err := e.ensureSession(ctx)
	if err != nil {
		return -1
	}
	expr := `(() => { let max = 0; document.querySelectorAll('textarea, [contenteditable="true"]').forEach(el => { const t = el.tagName === 'TEXTAREA' ? (el.value || '') : (el.innerText || el.textContent || ''); const n = t.trim().length; if (n > max) max = n; }); return max; })()`
	raw, err := e.client.Evaluate(ctx, sid, tab.tabID, expr, bskEvalTimeout())
	if err != nil {
		return -1
	}
	var n float64
	if json.Unmarshal(raw, &n) != nil {
		return -1
	}
	return int(n)
}

// NewBskEngine creates a new bsk engine. The bsk CLI is required on PATH for
// daemon lifecycle (`daemon start`); the extension connection is verified
// lazily at session start.
func NewBskEngine(scriptsDir string) (*BskEngine, error) {
	cmdPath, err := exec.LookPath("bsk")
	if err != nil {
		return nil, fmt.Errorf("bsk not found on PATH; install browser-skill CLI (bsk) to use this engine")
	}
	log.Printf("[bsk] using binary: %s (transport: daemon ipc)", cmdPath)
	return &BskEngine{
		client:     bskclient.New(),
		scriptsDir: scriptsDir,
		tabs:       make(map[string]*bskTab),
	}, nil
}

// NewBskEngineWithClient builds the engine on a caller-supplied client
// (used by tests to point at a fake daemon).
func NewBskEngineWithClient(client *bskclient.Client, scriptsDir string) *BskEngine {
	return &BskEngine{
		client:     client,
		scriptsDir: scriptsDir,
		tabs:       make(map[string]*bskTab),
	}
}

func (e *BskEngine) Name() string { return "bsk" }

// Health reports daemon/extension reachability without side effects: it
// never starts the daemon and never creates a session.
func (e *BskEngine) Health(timeout time.Duration) (bool, string) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	if err := e.client.Ping(ctx); err != nil {
		return false, "daemon 未运行（发送时将自动启动，或手动执行 bsk daemon start）"
	}
	stCtx, cancel2 := context.WithTimeout(context.Background(), timeout)
	defer cancel2()
	st, err := e.client.Status(stCtx)
	if err != nil {
		return true, "daemon 可达，状态未知"
	}
	if !st.Connected() {
		return false, "浏览器扩展未连接（打开 Chrome 并启用 browser-skill 扩展）"
	}
	b := st.Browsers[0]
	return true, fmt.Sprintf("%s 已连接（扩展 %s）", b.BrowserName, b.ExtensionVersion)
}

// Close stops the bsk session and tears down the daemon connection.
func (e *BskEngine) Close() error {
	e.mu.Lock()
	sid := e.session
	e.session = ""
	e.tabs = make(map[string]*bskTab)
	e.mu.Unlock()

	if sid != "" {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		if err := e.client.SessionStop(ctx, sid); err != nil {
			log.Printf("[bsk] session stop %s: %v", sid, err)
		}
		cancel()
	}
	return e.client.Close()
}

// --- session & tab bookkeeping ---------------------------------------------

// ensureSession returns the live session id, creating one when needed.
// Verifies extension connectivity on first use with a friendly error.
func (e *BskEngine) ensureSession(ctx context.Context) (string, error) {
	e.mu.Lock()
	if e.session != "" {
		sid := e.session
		e.mu.Unlock()
		return sid, nil
	}
	e.mu.Unlock()

	// Outside the lock: status + session start are daemon calls.
	stCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	st, err := e.client.Status(stCtx)
	if err != nil {
		return "", fmt.Errorf("bsk daemon 不可用: %w", err)
	}
	if !st.Connected() {
		return "", errors.New("browser-skill 扩展未连接：请打开 Chrome 并确认扩展已启用（bsk doctor 可诊断）")
	}

	res, err := e.client.SessionStart(ctx, true)
	if err != nil {
		return "", fmt.Errorf("bsk session start: %w", err)
	}

	e.mu.Lock()
	// A concurrent ensureSession may have won.
	if e.session == "" {
		e.session = res.SessionID
		log.Printf("[bsk] session started: %s", e.session)
	}
	sid := e.session
	e.mu.Unlock()
	return sid, nil
}

// dropSession forgets the session and all tabs; the next call rebuilds
// everything from scratch.
func (e *BskEngine) dropSession() {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.session != "" {
		go func(sid string) {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			_ = e.client.SessionStop(ctx, sid)
		}(e.session)
	}
	e.session = ""
	e.tabs = make(map[string]*bskTab)
}

// ensureSiteTab returns the tab for a site, reusing a matching agent tab or
// creating one. Stale-tab state (post-recovery) is rebuilt transparently.
func (e *BskEngine) ensureSiteTab(ctx context.Context, site models.Site) (*bskTab, error) {
	sid, err := e.ensureSession(ctx)
	if err != nil {
		return nil, err
	}

	e.mu.Lock()
	if tab, ok := e.tabs[site.ID]; ok {
		e.mu.Unlock()
		return tab, nil
	}
	e.mu.Unlock()

	// Reuse an existing agent tab whose URL matches (handles SPA redirects,
	// e.g. qianwen.com/ → qianwen.com/chat/<id>).
	if tabs, err := e.client.TabList(ctx, sid); err == nil {
		for _, t := range tabs {
			if t.Scope != "agent" && t.Scope != "" {
				continue
			}
			if urlsMatch(t.URL, site.URL) {
				log.Printf("[bsk] reusing agent tab %d for %s (%s)", t.TabID, site.ID, t.URL)
				return e.rememberTab(site, t.TabID, t.URL), nil
			}
		}
	}

	tabID, err := e.client.TabCreate(ctx, sid, site.URL)
	if err != nil {
		return nil, fmt.Errorf("tab create: %w", err)
	}
	log.Printf("[bsk] created agent tab for %s: %d", site.ID, tabID)
	return e.rememberTab(site, tabID, site.URL), nil
}

func (e *BskEngine) rememberTab(site models.Site, tabID int64, url string) *bskTab {
	e.mu.Lock()
	defer e.mu.Unlock()
	if tab, ok := e.tabs[site.ID]; ok {
		return tab // concurrent ensure won
	}
	tab := &bskTab{siteID: site.ID, tabID: tabID, url: url}
	e.tabs[site.ID] = tab
	return tab
}

// forgetTab drops a single site's tab (tab-level not_found recovery).
func (e *BskEngine) forgetTab(siteID string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	delete(e.tabs, siteID)
}

// --- evaluation --------------------------------------------------------------

// evalStep evaluates a site script on a tab: lib.js + payload + script, with
// one transparent recovery attempt for session/tab-level not_found errors.
func (e *BskEngine) evalStep(ctx context.Context, site models.Site, tab *bskTab, scriptName string, payload map[string]any) (json.RawMessage, error) {
	expr, err := e.assembleScript(site.ID, scriptName, payload)
	if err != nil {
		return nil, err
	}

	sid, err := e.ensureSession(ctx)
	if err != nil {
		return nil, err
	}

	value, err := e.client.Evaluate(ctx, sid, tab.tabID, expr, bskEvalTimeout())
	if err == nil {
		return unwrapJSONString(value), nil
	}

	// not_found: the session died (daemon idle GC, extension reconnect) or
	// the tab was closed. Rebuild the missing layer and retry once.
	if bskclient.IsCode(err, bskclient.CodeNotFound) {
		log.Printf("[bsk] not_found on %s/%s: %v — recovering", site.ID, scriptName, err)
		if err := e.recover(ctx, site, tab, err); err != nil {
			return nil, err
		}
		return e.evalStepSimple(ctx, site, tab, expr)
	}
	return nil, err
}

// recover rebuilds what the not_found error pointed at: a missing tab gets a
// new tab in the same session; a dead session gets a full session rebuild
// (which rebuilds all site tabs).
func (e *BskEngine) recover(ctx context.Context, site models.Site, tab *bskTab, origErr error) error {
	if isTabNotFound(origErr) {
		e.forgetTab(site.ID)
		if _, err := e.ensureSiteTab(ctx, site); err != nil {
			return fmt.Errorf("tab recovery failed: %w (original: %v)", err, origErr)
		}
		return nil
	}
	e.dropSession()
	sid, err := e.ensureSession(ctx)
	if err != nil {
		return fmt.Errorf("session recovery failed: %w (original: %v)", err, origErr)
	}
	e.mu.Lock()
	e.tabs = make(map[string]*bskTab) // old session's tabs are gone with it
	e.mu.Unlock()
	if _, err := e.ensureSiteTab(ctx, site); err != nil {
		return fmt.Errorf("session recovery tab: %w (original: %v)", err, origErr)
	}
	_ = sid
	return nil
}

// evalStepSimple re-runs an already-assembled expression on the site's
// (freshly recovered) tab.
func (e *BskEngine) evalStepSimple(ctx context.Context, site models.Site, tab *bskTab, expr string) (json.RawMessage, error) {
	sid, err := e.ensureSession(ctx)
	if err != nil {
		return nil, err
	}
	e.mu.Lock()
	t := e.tabs[site.ID]
	e.mu.Unlock()
	if t != nil {
		tab = t
	}
	return e.client.Evaluate(ctx, sid, tab.tabID, expr, bskEvalTimeout())
}

// unwrapJSONString decodes one level of string-encoded JSON. Site scripts
// end with __KC_LIB__.safeStringify(...), whose return value is a JSON
// string; a second JSON document lives inside it.
func unwrapJSONString(raw json.RawMessage) json.RawMessage {
	var s string
	if json.Unmarshal(raw, &s) == nil {
		var inner any
		if json.Unmarshal([]byte(s), &inner) == nil {
			out, err := json.Marshal(inner)
			if err == nil {
				return out
			}
		}
	}
	return raw
}

// isTabNotFound distinguishes tab-level from session-level not_found errors
// (the two message shapes the daemon emits for these cases).
func isTabNotFound(err error) bool {
	var re *bskclient.RPCError
	if !errors.As(err, &re) {
		return false
	}
	return strings.Contains(re.Message, "No tab with id")
}

// assembleScript builds lib.js + payload + site script as one expression.
func (e *BskEngine) assembleScript(siteID, scriptName string, payload map[string]any) (string, error) {
	scriptPath := filepath.Join(e.scriptsDir, siteID, scriptName)
	script, err := os.ReadFile(scriptPath)
	if err != nil {
		return "", fmt.Errorf("read script %s: %w", scriptPath, err)
	}

	var b strings.Builder
	b.WriteString(e.libScript())
	if payload != nil {
		payloadJSON, _ := json.Marshal(payload)
		b.WriteString("\nglobalThis.__PAYLOAD__ = Object.create(null);\n" +
			"var __payload__ = " + sanitizeString(string(payloadJSON)) + ";\n" +
			"Object.keys(__payload__).forEach(function(k) { globalThis.__PAYLOAD__[k] = __payload__[k]; });\n")
	} else {
		b.WriteString("\nglobalThis.__PAYLOAD__ = Object.create(null);\n")
	}
	b.WriteString("\n" + sanitizeString(string(script)) + "\n")
	return b.String(), nil
}

// libScript lazily loads the shared lib.js.
func (e *BskEngine) libScript() string {
	e.libOnce.Do(func() {
		libPath := filepath.Join(e.scriptsDir, "lib.js")
		if data, err := os.ReadFile(libPath); err == nil {
			e.lib = string(data)
			log.Printf("[bsk] loaded shared lib: %s (%d bytes)", libPath, len(e.lib))
		}
	})
	return e.lib
}

// --- outer pipeline ----------------------------------------------------------

// SendMessage sends a prompt to the site and returns the extracted answer.
func (e *BskEngine) SendMessage(ctx context.Context, site models.Site, prompt string) (string, error) {
	log.Printf("[bsk] SendMessage: site=%s prompt=%q", site.ID, prompt[:min(50, len(prompt))])
	prompt = sanitizeString(prompt)

	tab, err := e.ensureSiteTab(ctx, site)
	if err != nil {
		return "", fmt.Errorf("ensure tab: %w", err)
	}
	ReportProgress(ctx, ProgressInput)

	if err := e.waitForInput(ctx, site, tab); err != nil {
		return "", err // already user-readable
	}

	ReportProgress(ctx, ProgressSending)
	if _, err := e.evalStep(ctx, site, tab, "send_prompt.js", map[string]any{"prompt": prompt}); err != nil {
		return "", fmt.Errorf("发送失败: %w", err)
	}

	ReportProgress(ctx, ProgressGenerating)
	if err := e.waitForAnswer(ctx, site, tab, prompt); err != nil {
		return "", err
	}

	ReportProgress(ctx, ProgressExtracting)
	return e.extractAnswer(ctx, site, tab)
}

// StartNewChat starts a new conversation on the site's tab.
func (e *BskEngine) StartNewChat(site models.Site) error {
	log.Printf("[bsk] StartNewChat: site=%s", site.ID)
	ctx, cancel := context.WithTimeout(context.Background(), bskWaitDeadline())
	defer cancel()

	tab, err := e.ensureSiteTab(ctx, site)
	if err != nil {
		return fmt.Errorf("ensure tab: %w", err)
	}
	if _, err := e.evalStep(ctx, site, tab, "new_chat.js", nil); err != nil {
		return fmt.Errorf("new chat: %w", err)
	}
	time.Sleep(2 * time.Second)
	return nil
}

// waitForInput polls detect_input.js until the input is ready. Fails fast
// when the page has redirected off the site's host (login wall).
func (e *BskEngine) waitForInput(ctx context.Context, site models.Site, tab *bskTab) error {
	deadline := time.Now().Add(bskWaitDeadline())
	for time.Now().Before(deadline) {
		if err := ctx.Err(); err != nil {
			return err
		}
		raw, err := e.evalStep(ctx, site, tab, "detect_input.js", nil)
		if err == nil {
			var rm map[string]any
			if json.Unmarshal(raw, &rm) == nil {
				if runaway, msg := offHost(rm, site.URL); runaway {
					return fmt.Errorf("页面跳转到 %s（站点未登录？请先在浏览器中登录该站点）", msg)
				}
				if rm["ready"] == true {
					return nil
				}
			}
		} else if isFatalPageErr(err) {
			return fmt.Errorf("输入框检测失败: %w", err)
		}
		time.Sleep(3 * time.Second)
	}
	return fmt.Errorf("等待 %s 输入框就绪超时（该站点可能需要登录）", site.ID)
}

// waitForAnswer polls wait_answer.js until the answer text stabilizes.
// Two rescues keep transient site misbehavior from failing the send:
//
//   - stuck-send: the site accepted the prompt but no assistant message
//     ever appeared → resend the prompt once (fresh window);
//   - timeout rescue: on deadline, try extraction anyway — the answer may
//     have completed between polls or stability tracking may have missed.
func (e *BskEngine) waitForAnswer(ctx context.Context, site models.Site, tab *bskTab, prompt string) error {
	deadline := time.Now().Add(bskWaitDeadline())
	start := time.Now()
	resent := false
	reloaded := false
	promptLen := len([]rune(prompt))

	for time.Now().Before(deadline) {
		if err := ctx.Err(); err != nil {
			return err
		}
		raw, err := e.evalStep(ctx, site, tab, "wait_answer.js", map[string]any{"stableRounds": 3})
		if err == nil {
			var rm map[string]any
			if json.Unmarshal(raw, &rm) == nil {
				if runaway, msg := offHost(rm, site.URL); runaway {
					return fmt.Errorf("页面跳转到 %s（站点未登录？请先在浏览器中登录该站点）", msg)
				}
				if rm["done"] == true {
					return nil
				}
				// Send-failure rescues (each fires at most once):
				//   a) editor still holds the prompt after sendVerifyAfter —
				//      the submit never went through → resend immediately;
				//   b) prompt consumed but no assistant content after
				//      resendAfter — the SPA may have swallowed the render
				//      (conversation exists server-side) → reload the tab;
				//   c) still nothing after the reload → resend the prompt.
				if count := assistantCount(rm); count == 0 {
					elapsed := time.Since(start)
					if !resent && !reloaded && elapsed > bskSendVerifyAfter() {
						if n := e.editorTextLen(ctx, site, tab); n >= promptLen*2/3 && n > 0 {
							resent = true
							start = time.Now()
							log.Printf("[bsk] %s: editor still holds the prompt (send not consumed) — resending", site.ID)
							if _, err := e.evalStep(ctx, site, tab, "send_prompt.js", map[string]any{"prompt": prompt}); err != nil {
								log.Printf("[bsk] %s resend failed: %v", site.ID, err)
							}
						}
					}
					if !reloaded && time.Since(start) > bskResendAfter() {
						reloaded = true
						start = time.Now()
						log.Printf("[bsk] %s: prompt consumed but no assistant content — reloading tab", site.ID)
						if sid, serr := e.ensureSession(ctx); serr == nil {
							if _, rerr := e.client.Evaluate(ctx, sid, tab.tabID, "location.reload(); true", bskEvalTimeout()); rerr != nil {
								log.Printf("[bsk] %s reload failed: %v", site.ID, rerr)
							}
						}
						time.Sleep(5 * time.Second) // let the page come back before polling resumes
						continue
					}
					if reloaded && !resent && time.Since(start) > bskResendAfter() {
						resent = true
						start = time.Now()
						log.Printf("[bsk] %s: still no assistant content after reload — resending prompt", site.ID)
						if _, err := e.evalStep(ctx, site, tab, "send_prompt.js", map[string]any{"prompt": prompt}); err != nil {
							log.Printf("[bsk] %s resend failed: %v", site.ID, err)
						}
					}
				}
			}
		}
		// wait_answer errors (transient DOM churn) are not fatal; keep polling.
		time.Sleep(2 * time.Second)
	}

	// Timeout rescue: extract whatever is on the page. A completed answer
	// beats a timeout error for the user.
	if text, err := e.extractAnswer(ctx, site, tab); err == nil && text != "" {
		log.Printf("[bsk] %s: wait timed out but extraction found %d chars — using it", site.ID, len(text))
		return nil
	}
	return fmt.Errorf("%s 回答在 %s 内未稳定（站点生成过慢或页面异常）", site.ID, bskWaitDeadline())
}

// assistantCount extracts the assistant message count from a wait_answer
// result; -1 when the script doesn't report it (never triggers resend).
func assistantCount(rm map[string]any) int {
	if v, ok := rm["answerCount"].(float64); ok {
		return int(v)
	}
	return -1
}

// extractAnswer extracts the final answer markdown from the page.
func (e *BskEngine) extractAnswer(ctx context.Context, site models.Site, tab *bskTab) (string, error) {
	raw, err := e.evalStep(ctx, site, tab, "extract_answer.js", nil)
	if err != nil {
		return "", fmt.Errorf("提取回答失败: %w", err)
	}
	var rm struct {
		Text string `json:"text"`
	}
	if err := json.Unmarshal(raw, &rm); err != nil {
		return "", fmt.Errorf("extract_answer 返回非对象: %v", err)
	}
	if rm.Text == "" {
		return "", errors.New("提取到空回答")
	}
	return rm.Text, nil
}

// isFatalPageErr reports whether a page-side error during input detection is
// permanent (script cannot find what it needs), as opposed to transient DOM
// churn while the page loads — those are retried by the poll loop.
func isFatalPageErr(err error) bool {
	var js *bskclient.JSError
	if errors.As(err, &js) {
		return true
	}
	return bskclient.IsCode(err, bskclient.CodeInvalidParams)
}

// offHost reports whether a script result carries a page URL that no longer
// belongs to the site's host (login redirect). Non-http(s) URLs
// (about:blank, chrome://newtab) are ignored — a freshly created tab has not
// committed its navigation yet.
func offHost(rm map[string]any, siteURL string) (bool, string) {
	u, _ := rm["url"].(string)
	if u == "" {
		return false, ""
	}
	parsed, err := url.Parse(u)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return false, ""
	}
	if parsed.Host == "" || sameHost(u, siteURL) {
		return false, ""
	}
	return true, parsed.Host
}

// urlsMatch reports whether two URLs belong to the same page modulo SPA
// redirects: same scheme+host, and one path is a prefix of the other.
func urlsMatch(a, b string) bool {
	ua, errA := url.Parse(a)
	ub, errB := url.Parse(b)
	if errA != nil || errB != nil || ua.Scheme == "" || ub.Scheme == "" {
		return false
	}
	if ua.Scheme != ub.Scheme || ua.Host != ub.Host {
		return false
	}
	pa, pb := strings.TrimSuffix(ua.Path, "/"), strings.TrimSuffix(ub.Path, "/")
	return pa == "" || pb == "" || pa == pb || strings.HasPrefix(pb, pa) || strings.HasPrefix(pa, pb)
}

// sameHost reports whether two URLs share scheme and host.
func sameHost(a, b string) bool {
	ua, errA := url.Parse(a)
	ub, errB := url.Parse(b)
	if errA != nil || errB != nil {
		return false
	}
	return ua.Scheme == ub.Scheme && ua.Host == ub.Host
}
