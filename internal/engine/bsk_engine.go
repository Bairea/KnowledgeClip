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
	"strconv"
	"strings"
	"sync"
	"time"

	"chat-aggregator/internal/models"
)

// BskEngine drives the user's real Chromium through the browser-skill `bsk`
// CLI. It is an optional extension only:
//
//   - never embedded/packaged into the distributed binary (scripts are read
//     from disk via getScriptsDir);
//   - never part of the default engine fallback chain — a site reaches this
//     engine only when its `engine.primary` is explicitly `bsk`;
//   - requires the browser-skill extension to be installed and connected in
//     the user's Chromium (verified at init via `bsk status`).
//
// One bsk session is started for the engine's lifetime (stopped on Close),
// with one agent tab per site. Every evaluation targets its tab through
// `--tab-id`, so there is no active-tab switching and no cross-site
// serialization; a mutex is still held around eval calls to keep concurrent
// polls deterministic.
//
// Extraction logic is shared with the browser-act engine: the same
// scripts/browser-act/<site>/*.js files (detect_input / send_prompt /
// wait_answer / extract_answer / new_chat) run verbatim inside
// `bsk evaluate`, together with the shared lib.js.
type BskEngine struct {
	cmdPath     string
	scriptsDir  string
	mu          sync.Mutex
	initialized bool
	session     string
	tabs        map[string]*siteTab // siteID -> tab
	libScript   string
	libLoaded   bool
}

// NewBskEngine creates a new bsk engine. Returns an error when the bsk CLI
// is not installed; the extension connection itself is verified lazily at
// first use (initialize).
func NewBskEngine(scriptsDir string) (*BskEngine, error) {
	cmdPath, err := exec.LookPath("bsk")
	if err != nil {
		return nil, fmt.Errorf("bsk not found on PATH; install browser-skill CLI (bsk) to use this engine")
	}
	log.Printf("[bsk] using binary: %s", cmdPath)
	return &BskEngine{
		cmdPath:    cmdPath,
		scriptsDir: scriptsDir,
		tabs:       make(map[string]*siteTab),
	}, nil
}

func (e *BskEngine) Name() string { return "bsk" }

// --- lifecycle -------------------------------------------------------------

func (e *BskEngine) initialize() error {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.initializeLocked()
}

// initializeLocked verifies the extension is connected and starts one bsk
// session for the engine lifetime. Caller must hold e.mu.
func (e *BskEngine) initializeLocked() error {
	if e.initialized {
		return nil
	}
	if err := e.verifyExtension(); err != nil {
		return err
	}
	sid, err := e.startSession()
	if err != nil {
		return fmt.Errorf("start bsk session: %w", err)
	}
	e.session = sid
	e.initialized = true
	log.Printf("[bsk] session started: %s", e.session)
	return nil
}

// verifyExtension runs `bsk status --json` and requires at least one browser
// with the browser-skill extension connected.
func (e *BskEngine) verifyExtension() error {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, e.cmdPath, "status", "--json")
	out, err := cmd.Output()
	if err != nil {
		return fmt.Errorf("bsk status failed (is the browser-skill extension connected?): %w", err)
	}
	var st struct {
		Browsers []json.RawMessage `json:"browsers"`
	}
	if err := json.Unmarshal(out, &st); err != nil {
		return fmt.Errorf("parse bsk status: %w", err)
	}
	if len(st.Browsers) == 0 {
		return errors.New("bsk: no browser with the browser-skill extension connected (run bsk doctor)")
	}
	return nil
}

// startSession starts a no-focus bsk session and returns its 4-letter id.
func (e *BskEngine) startSession() (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, e.cmdPath, "session", "start", "--no-focus")
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("session start: %w", err)
	}
	lines := strings.Fields(string(out))
	if len(lines) == 0 || len(lines[len(lines)-1]) != 4 {
		return "", fmt.Errorf("session start returned unexpected output: %q", strings.TrimSpace(string(out)))
	}
	return lines[len(lines)-1], nil
}

// shutdown stops the session; the agent window closes and borrowed tabs are
// auto-returned. Safe to call when no session is running.
func (e *BskEngine) shutdown() {
	if e.session == "" {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, e.cmdPath, "session", "stop", e.session)
	if out, err := cmd.CombinedOutput(); err != nil {
		log.Printf("[bsk] session stop %s: %v (%s)", e.session, err, truncateForLog(string(out), 200))
	} else {
		log.Printf("[bsk] session stopped: %s", e.session)
	}
}

// sessionReset forgets the current session and all tabs so the next call
// re-initializes from scratch (recovery path after session/tab-level errors).
func (e *BskEngine) sessionReset() {
	if e.session != "" {
		// Best effort: close the stale session before re-creating one.
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		exec.CommandContext(ctx, e.cmdPath, "session", "stop", e.session).CombinedOutput()
	}
	e.session = ""
	e.tabs = make(map[string]*siteTab)
	e.initialized = false
}

// Close stops the bsk session.
func (e *BskEngine) Close() error {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.shutdown()
	e.tabs = make(map[string]*siteTab)
	e.initialized = false
	return nil
}

// --- tabs ------------------------------------------------------------------

// ensureSiteTab returns the agent tab for a site, creating or reusing one.
// Session-level failures (e.g. the agent window died mid-run) trigger one
// reset-and-retry cycle.
func (e *BskEngine) ensureSiteTab(site models.Site) (*siteTab, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	var tab *siteTab
	err := e.retryAfterSessionReset(func() error {
		t, err := e.ensureSiteTabLocked(site)
		tab = t
		return err
	})
	return tab, err
}

// ensureSiteTabLocked is the lock-free inner logic; caller must hold e.mu.
func (e *BskEngine) ensureSiteTabLocked(site models.Site) (*siteTab, error) {
	if tab, ok := e.tabs[site.ID]; ok {
		return tab, nil
	}

	// Reuse an existing agent tab whose URL matches (handles post-navigation
	// redirects, e.g. qianwen.com/ → qianwen.com/chat/<id>).
	tabs, err := e.listTabs()
	if err == nil {
		for _, tab := range tabs {
			if urlsMatch(tab.url, site.URL) {
				log.Printf("[bsk] reusing agent tab %s for %s (%s)", tab.tabID, site.ID, tab.url)
				t := &siteTab{siteID: site.ID, tabID: tab.tabID, url: tab.url}
				e.tabs[site.ID] = t
				return t, nil
			}
		}
	}

	data, err := e.runJSON("tab", "create", "--session", e.session, "--url", site.URL)
	if err != nil {
		return nil, fmt.Errorf("tab create: %w", err)
	}
	tabID, ok := data["tab_id"].(float64)
	if !ok || tabID == 0 {
		return nil, fmt.Errorf("tab create returned no tab_id: %v", data)
	}
	t := &siteTab{siteID: site.ID, tabID: strconv.FormatInt(int64(tabID), 10), url: site.URL}
	e.tabs[site.ID] = t
	log.Printf("[bsk] created agent tab for %s: %s", site.ID, t.tabID)
	return t, nil
}

// listTabs returns the agent-window tabs of the current session.
func (e *BskEngine) listTabs() ([]siteTab, error) {
	data, err := e.runJSON("tab", "list", "--session", e.session)
	if err != nil {
		return nil, err
	}
	raw, _ := json.Marshal(data)
	var result struct {
		Tabs []struct {
			ID    string `json:"tab_id"`
			URL   string `json:"url"`
			Scope string `json:"scope"`
		} `json:"tabs"`
	}
	if err := json.Unmarshal(raw, &result); err != nil {
		return nil, fmt.Errorf("parse tab list: %w", err)
	}
	tabs := make([]siteTab, 0, len(result.Tabs))
	for _, t := range result.Tabs {
		if t.Scope != "agent" && t.Scope != "" {
			continue
		}
		tabs = append(tabs, siteTab{tabID: t.ID, url: t.URL})
	}
	return tabs, nil
}

// urlsMatch reports whether two URLs belong to the same page modulo SPA
// redirects: same scheme+host, and one path is a prefix of the other (a
// bare host like qianwen.com/ matches any chat path on that host).
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

// sameHost reports whether two URLs share scheme and host (used to detect
// pages that redirected away from the site, e.g. to a login page).
func sameHost(a, b string) bool {
	ua, errA := url.Parse(a)
	ub, errB := url.Parse(b)
	if errA != nil || errB != nil {
		return false
	}
	return ua.Scheme == ub.Scheme && ua.Host == ub.Host
}

// --- evaluation ------------------------------------------------------------

// evalOnTab switches nothing — it evaluates the site script directly on the
// site's tab via --tab-id. Serialized under e.mu. On session/tab-level
// failure the session is reset and one retry is attempted.
func (e *BskEngine) evalOnTab(site models.Site, scriptName string, payload map[string]interface{}) (interface{}, error) {
	e.mu.Lock()
	defer e.mu.Unlock()

	if err := e.initializeLocked(); err != nil {
		return nil, err
	}
	tab, err := e.ensureSiteTabLocked(site)
	if err != nil {
		return nil, err
	}

	result, err := e.evalScript(site.ID, scriptName, payload, tab.tabID)
	if err != nil && isSessionLevelErr(err) {
		log.Printf("[bsk] session-level failure on %s (%s), resetting session and retrying once: %v", site.ID, scriptName, err)
		e.sessionReset()
		if initErr := e.initializeLocked(); initErr != nil {
			return nil, fmt.Errorf("session recovery failed: %w (original: %v)", initErr, err)
		}
		tab2, tabErr := e.ensureSiteTabLocked(site)
		if tabErr != nil {
			return nil, fmt.Errorf("session recovery tab: %w (original: %v)", tabErr, err)
		}
		return e.evalScript(site.ID, scriptName, payload, tab2.tabID)
	}
	return result, err
}

// isSessionLevelErr reports whether an error indicates the bsk session,
// agent window, or tab went away (warranting a session reset), as opposed to
// a script/page-level error.
func isSessionLevelErr(err error) bool {
	msg := err.Error()
	for _, kw := range []string{
		"session", "unreachable", "daemon", "protocol", "window", "cancel",
		"not registered", "does not exist", "resource", "exit status 1",
	} {
		if strings.Contains(msg, kw) {
			return true
		}
	}
	return false
}

// retryAfterSessionReset runs fn; if it fails at the session/daemon level the
// bsk session is reset and re-initialized, then fn is retried once. Caller
// must hold e.mu (initializeLocked requires it).
func (e *BskEngine) retryAfterSessionReset(fn func() error) error {
	err := fn()
	if err == nil || !isSessionLevelErr(err) {
		return err
	}
	log.Printf("[bsk] session-level failure, resetting session and retrying once: %v", err)
	e.sessionReset()
	if initErr := e.initializeLocked(); initErr != nil {
		return fmt.Errorf("session recovery failed: %w (original: %v)", initErr, err)
	}
	return fn()
}

// getLibScript returns the cached content of the shared lib.js.
func (e *BskEngine) getLibScript() string {
	if e.libLoaded {
		return e.libScript
	}
	e.libLoaded = true
	libPath := filepath.Join(e.scriptsDir, "lib.js")
	if data, err := os.ReadFile(libPath); err == nil {
		e.libScript = string(data)
		log.Printf("[bsk] loaded shared lib: %s (%d bytes)", libPath, len(e.libScript))
	}
	return e.libScript
}

// evalScript builds lib.js + payload + site script and runs it on the tab.
func (e *BskEngine) evalScript(siteID, scriptName string, payload map[string]interface{}, tabID string) (interface{}, error) {
	scriptPath := filepath.Join(e.scriptsDir, siteID, scriptName)
	script, err := os.ReadFile(scriptPath)
	if err != nil {
		return nil, fmt.Errorf("read script %s: %w", scriptPath, err)
	}

	fullScript := sanitizeString(e.getLibScript()) + "\n"
	if payload != nil {
		payloadJSON, _ := json.Marshal(payload)
		fullScript += "globalThis.__PAYLOAD__ = Object.create(null);\n" +
			"var __payload__ = " + sanitizeString(string(payloadJSON)) + ";\n" +
			"Object.keys(__payload__).forEach(function(k) { globalThis.__PAYLOAD__[k] = __payload__[k]; });\n"
	} else {
		fullScript += "globalThis.__PAYLOAD__ = Object.create(null);\n"
	}
	fullScript += sanitizeString(string(script))

	return e.runEval(fullScript, tabID)
}

// runEval executes the assembled JS expression in the tab via
// `bsk evaluate <expr> --session <sid> --tab-id <tab> --json`.
func (e *BskEngine) runEval(expression, tabID string) (interface{}, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, e.cmdPath,
		"evaluate", expression,
		"--session", e.session,
		"--tab-id", tabID,
		"--json",
		"--timeout", "30s",
	)
	out, err := cmd.Output()
	if err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return nil, fmt.Errorf("bsk evaluate timeout after 40s")
		}
		var stderr string
		if exitErr, ok := err.(*exec.ExitError); ok {
			stderr = strings.TrimSpace(string(exitErr.Stderr))
		}
		if stderr != "" {
			// bsk errors are JSON under --json (code/message/hint).
			var es struct {
				Message string `json:"message"`
			}
			if json.Unmarshal([]byte(stderr), &es) == nil && es.Message != "" {
				return nil, fmt.Errorf("bsk evaluate error: %s", es.Message)
			}
			return nil, fmt.Errorf("bsk evaluate failed: %s", truncateForLog(stderr, 300))
		}
		return nil, fmt.Errorf("bsk evaluate: %w", err)
	}

	var result map[string]interface{}
	if err := json.Unmarshal(out, &result); err != nil {
		return nil, fmt.Errorf("parse evaluate response: %w (raw: %s)", err, truncateForLog(string(out), 300))
	}

	if okVal, exists := result["ok"]; exists && okVal == false {
		msg, _ := result["message"].(string)
		if msg == "" {
			msg, _ = result["error"].(string)
		}
		return nil, fmt.Errorf("bsk evaluate ok=false: %s", msg)
	}

	value, ok := result["value"]
	if !ok {
		return nil, fmt.Errorf("bsk evaluate returned no value field: %v", result)
	}

	// The site scripts end with __KC_LIB__.safeStringify(...), which returns a
	// JSON string; decode it into an object when present.
	if s, isStr := value.(string); isStr {
		var parsed interface{}
		if json.Unmarshal([]byte(s), &parsed) == nil {
			return parsed, nil
		}
	}
	return value, nil
}

// --- command runner --------------------------------------------------------

// runJSON executes a bsk command with --json and returns the parsed output.
func (e *BskEngine) runJSON(args ...string) (map[string]interface{}, error) {
	cmdArgs := append([]string{}, args...)
	cmdArgs = append(cmdArgs, "--json")
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, e.cmdPath, cmdArgs...)
	out, err := cmd.Output()
	if err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return nil, fmt.Errorf("bsk %v timeout after 30s", args)
		}
		var stderr string
		if exitErr, ok := err.(*exec.ExitError); ok {
			stderr = strings.TrimSpace(string(exitErr.Stderr))
		}
		var es struct {
			Message string `json:"message"`
		}
		if json.Unmarshal([]byte(stderr), &es) == nil && es.Message != "" {
			return nil, fmt.Errorf("bsk %v: %s", args, es.Message)
		}
		return nil, fmt.Errorf("bsk %v: %w", args, err)
	}
	var result map[string]interface{}
	if err := json.Unmarshal(out, &result); err != nil {
		return nil, fmt.Errorf("parse bsk %v output: %w", args, err)
	}
	return result, nil
}

// --- outer pipeline (mirrors browser-act engine) ---------------------------

// SendMessage sends a prompt to the site and returns the extracted answer.
func (e *BskEngine) SendMessage(ctx context.Context, site models.Site, prompt string) (string, error) {
	log.Printf("[bsk] SendMessage: site=%s prompt=%q", site.ID, prompt[:min(50, len(prompt))])
	prompt = sanitizeString(prompt)

	if err := e.initialize(); err != nil {
		return "", fmt.Errorf("initialize: %w", err)
	}
	tab, err := e.ensureSiteTab(site)
	if err != nil {
		return "", fmt.Errorf("ensure tab: %w", err)
	}

	if err := e.waitForInput(ctx, site, tab); err != nil {
		return "", fmt.Errorf("input not ready: %w", err)
	}

	sendResult, err := e.evalOnTab(site, "send_prompt.js", map[string]interface{}{"prompt": prompt})
	if err != nil {
		return "", fmt.Errorf("send prompt: %w", err)
	}
	log.Printf("[bsk] send result: %v", sendResult)

	if err := e.waitForAnswer(ctx, site, tab); err != nil {
		return "", fmt.Errorf("wait for answer: %w", err)
	}

	answer, err := e.extractAnswer(site, tab)
	if err != nil {
		return "", fmt.Errorf("extract answer: %w", err)
	}
	return answer, nil
}

// StartNewChat starts a new conversation on the site's tab.
func (e *BskEngine) StartNewChat(site models.Site) error {
	log.Printf("[bsk] StartNewChat: site=%s", site.ID)
	if err := e.initialize(); err != nil {
		return fmt.Errorf("initialize: %w", err)
	}
	tab, err := e.ensureSiteTab(site)
	if err != nil {
		return fmt.Errorf("ensure tab: %w", err)
	}
	if _, err := e.evalOnTab(site, "new_chat.js", nil); err != nil {
		return fmt.Errorf("new chat: %w", err)
	}
	_ = tab
	time.Sleep(2 * time.Second)
	return nil
}

// waitForInput polls detect_input.js until the input is ready. Fails fast
// when the page has redirected off the site's host (e.g. a login wall).
func (e *BskEngine) waitForInput(ctx context.Context, site models.Site, tab *siteTab) error {
	deadline := time.Now().Add(180 * time.Second)
	for time.Now().Before(deadline) {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		result, err := e.evalOnTab(site, "detect_input.js", nil)
		if err == nil {
			if rm, ok := result.(map[string]interface{}); ok {
				if runaway, msg := offHost(rm, site.URL); runaway {
					return fmt.Errorf("page navigated to %s (login required?)", msg)
				}
				if rm["ready"] == true {
					return nil
				}
			}
		}
		time.Sleep(3 * time.Second)
	}
	return fmt.Errorf("input not ready after 180s (site %s) - user may need to login", site.ID)
}

// waitForAnswer polls wait_answer.js until the answer text stabilizes.
func (e *BskEngine) waitForAnswer(ctx context.Context, site models.Site, tab *siteTab) error {
	deadline := time.Now().Add(180 * time.Second)
	for time.Now().Before(deadline) {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		result, err := e.evalOnTab(site, "wait_answer.js", map[string]interface{}{"stableRounds": 3})
		if err == nil {
			if rm, ok := result.(map[string]interface{}); ok {
				if runaway, msg := offHost(rm, site.URL); runaway {
					return fmt.Errorf("page navigated to %s (login required?)", msg)
				}
				if rm["done"] == true {
					return nil
				}
			}
		}
		time.Sleep(2 * time.Second)
	}
	return fmt.Errorf("answer did not stabilize after 180s (site %s)", site.ID)
}

// offHost reports whether a script result carries a page URL that no longer
// belongs to the site's host (login redirect), returning the page host.
// Non-http(s) URLs (about:blank, chrome://newtab) are ignored — a freshly
// created tab has not committed its navigation yet.
func offHost(rm map[string]interface{}, siteURL string) (bool, string) {
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

// extractAnswer extracts the final answer text from the page.
func (e *BskEngine) extractAnswer(site models.Site, tab *siteTab) (string, error) {
	result, err := e.evalOnTab(site, "extract_answer.js", nil)
	if err != nil {
		return "", err
	}
	rm, ok := result.(map[string]interface{})
	if !ok {
		return "", fmt.Errorf("extract_answer returned non-object: %v", result)
	}
	text, _ := rm["text"].(string)
	if text == "" {
		return "", fmt.Errorf("extract_answer returned empty text")
	}
	return text, nil
}