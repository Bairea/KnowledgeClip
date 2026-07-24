package engine

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"time"

	"chat-aggregator/internal/models"
)

// BrowserActEngine uses a single browser-act session with multiple tabs.
// Each site gets its own tab within one browser window.
type BrowserActEngine struct {
	cmdPath     string
	scriptsDir  string
	mu          sync.Mutex
	initialized bool
	session     string
	browserID   string
	tabs        map[string]*siteTab // siteID -> tab info
	libScript   string              // cached _lib.js content (shared utilities)
	libLoaded   bool
}

// getLibScript returns the cached content of _lib.js (shared utility functions).
// Called under e.mu (via evalOnTab -> evalScript), so no extra sync needed.
func (e *BrowserActEngine) getLibScript() string {
	if e.libLoaded {
		return e.libScript
	}
	e.libLoaded = true
	libPath := filepath.Join(e.scriptsDir, "_lib.js")
	if data, err := os.ReadFile(libPath); err == nil {
		e.libScript = string(data)
		log.Printf("[browser-act] loaded shared lib: %s (%d bytes)", libPath, len(e.libScript))
	}
	return e.libScript
}

type siteTab struct {
	siteID string
	tabID  string
	url    string
}

// NewBrowserActEngine creates a new browser-act engine with single-session multi-tab architecture.
func NewBrowserActEngine(scriptsDir string) (*BrowserActEngine, error) {
	cmdPath, err := findBrowserAct()
	if err != nil {
		return nil, fmt.Errorf("browser-act not found: %w", err)
	}
	log.Printf("[browser-act] using binary: %s", cmdPath)

	return &BrowserActEngine{
		cmdPath:    cmdPath,
		scriptsDir: scriptsDir,
		session:    "knowledgeclip",
		tabs:       make(map[string]*siteTab),
	}, nil
}

// initialize creates the session and browser if not already done.
func (e *BrowserActEngine) initialize() error {
	e.mu.Lock()
	defer e.mu.Unlock()

	if e.initialized {
		return nil
	}

	// Verify browser-act CLI is functional before any daemon operations.
	// A broken Python env, missing DLLs, or corrupt install will crash every
	// command with exit 0xffffffff; detecting this early gives a clear error
	// instead of cryptic daemon failures.
	if err := e.verifyBrowserAct(); err != nil {
		return fmt.Errorf("verify cli: %w", err)
	}

	// Clean stale browser-act daemon state before first use.
	// browser-act stores its daemon endpoint (pid+port) in %APPDATA%/browseract/
	// daemon-state/daemon/daemon.endpoint.json. When the previous server process
	// exits, the endpoint file is left behind but the daemon pid is dead. On the
	// next launch browser-act detects pid_mismatch and tries to fork a new daemon;
	// its logging handler inherits redirected fds that become invalid after fork,
	// raising "OSError: [Errno 9] Bad file descriptor" and crashing the CLI with
	// exit code 0xffffffff. Removing the stale endpoint file beforehand lets
	// browser-act start cleanly.
	cleanStaleDaemonState()

	// Find or create browser
	browserID, err := e.findOrCreateBrowser("knowledgeclip", "KnowledgeClip multi-site browser")
	if err != nil {
		return fmt.Errorf("find/create browser: %w", translateBrowserActErr(err))
	}
	e.browserID = browserID

	e.initialized = true
	log.Printf("[browser-act] initialized: session=%s browser=%s", e.session, e.browserID)
	return nil
}

// verifyBrowserAct runs `browser-act --version` to confirm the CLI is functional
// before attempting daemon operations. A broken Python environment, missing DLLs,
// or corrupt install will crash here with a clear error instead of cryptic
// "exit status 0xffffffff" failures deep in browser list/create.
func (e *BrowserActEngine) verifyBrowserAct() error {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, e.cmdPath, "--version")
	hideWindow(cmd)
	out, err := cmd.Output()
	if err != nil {
		stderr := ""
		if exitErr, ok := err.(*exec.ExitError); ok {
			stderr = strings.TrimSpace(string(exitErr.Stderr))
		}
		if ctx.Err() == context.DeadlineExceeded {
			return fmt.Errorf("browser-act --version 超时(15s)，CLI 可能已挂起；请检查 browser-act 安装")
		}
		if stderr != "" {
			return fmt.Errorf("browser-act CLI 不可用: %w [stderr: %s]", err, truncateForLog(stderr, 300))
		}
		return fmt.Errorf("browser-act CLI 不可用: %w", err)
	}
	log.Printf("[browser-act] CLI verified: %s", truncateForLog(string(out), 100))
	return nil
}

// daemonStateDir returns browser-act's daemon-state directory.
// browser-act resolves its data dir from %APPDATA% (Windows) / $XDG_CONFIG_HOME
// or ~/.config (Unix). We mirror that resolution so cleanup works regardless of
// the server's working directory.
func daemonStateDir() string {
	appData := os.Getenv("APPDATA")
	if appData != "" {
		return filepath.Join(appData, "browseract", "daemon-state", "daemon")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	if runtime.GOOS == "darwin" {
		return filepath.Join(home, "Library", "Application Support", "browseract", "daemon-state", "daemon")
	}
	return filepath.Join(home, ".config", "browseract", "daemon-state", "daemon")
}

// cleanStaleDaemonState removes the daemon endpoint file when the recorded pid
// is no longer alive. Idempotent and safe to call repeatedly.
func cleanStaleDaemonState() {
	dir := daemonStateDir()
	if dir == "" {
		return
	}
	endpointPath := filepath.Join(dir, "daemon.endpoint.json")
	data, err := os.ReadFile(endpointPath)
	if err != nil {
		return
	}
	var ep struct {
		PID int `json:"pid"`
	}
	if err := json.Unmarshal(data, &ep); err != nil {
		return
	}
	if ep.PID <= 0 {
		return
	}
	alive := processAlive(ep.PID)
	log.Printf("[browser-act] daemon endpoint check: pid=%d alive=%v", ep.PID, alive)
	if alive {
		return
	}
	log.Printf("[browser-act] cleaning stale daemon state: pid=%d is dead, removing %s", ep.PID, endpointPath)
	os.Remove(endpointPath)
	// Remove leftover lock files so the next daemon start is clean
	os.Remove(filepath.Join(dir, "daemon.run.lock"))
	os.Remove(filepath.Join(dir, "daemon.start.lock"))
}

// cleanDaemonStateDir removes all files under browser-act's daemon-state directory.
// This is a last-resort cleanup used when browser-act crashes: stale lock files,
// socket files, log files, or corrupted state can all cause crashes that
// cleanStaleDaemonState (which only checks the endpoint pid) cannot resolve.
func cleanDaemonStateDir() {
	dir := daemonStateDir()
	if dir == "" {
		return
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	cleaned := 0
	for _, entry := range entries {
		path := filepath.Join(dir, entry.Name())
		if rmErr := os.RemoveAll(path); rmErr != nil {
			log.Printf("[browser-act] failed to remove %s: %v", path, rmErr)
		} else {
			cleaned++
		}
	}
	if cleaned > 0 {
		log.Printf("[browser-act] cleaned daemon state directory: removed %d entries from %s", cleaned, dir)
	}
}

// processAlive reports whether a process with the given pid is currently running.
// On Windows it uses OpenProcess (the reliable kernel-level probe); on Unix it
// uses signal 0.
func processAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	if runtime.GOOS == "windows" {
		// OpenProcess returns ERROR_INVALID_PARAMETER (87) for a dead pid.
		// PROCESS_QUERY_INFORMATION (0x400) is sufficient to probe existence.
		handle, err := syscall.OpenProcess(syscall.PROCESS_QUERY_INFORMATION, false, uint32(pid))
		if err != nil {
			return false
		}
		syscall.CloseHandle(handle)
		return true
	}
	p, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	return p.Signal(syscall.Signal(0)) == nil
}

// translateBrowserActErr converts low-level exec errors into actionable,
// human-readable messages so users see guidance instead of hex exit codes.
func translateBrowserActErr(err error) error {
	if err == nil {
		return nil
	}
	msg := err.Error()
	if strings.Contains(msg, "exit status 0xffffffff") || strings.Contains(msg, "exit status -1") {
		return fmt.Errorf("%w (browser-act 进程崩溃；已清理 daemon 状态并重试仍失败；请检查 Chrome 是否可用、browser-act 是否完整安装)", err)
	}
	if strings.Contains(msg, "executable file not found") || strings.Contains(msg, "browser-act not found") {
		return fmt.Errorf("%w (未安装 browser-act；请运行: uv tool install browser-act-cli --python 3.12)", err)
	}
	return err
}

// ensureSiteTab finds or creates a tab for the site.
func (e *BrowserActEngine) ensureSiteTab(site models.Site) (*siteTab, error) {
	e.mu.Lock()
	defer e.mu.Unlock()

	// Check if tab already exists
	if tab, exists := e.tabs[site.ID]; exists {
		// Verify tab is still active
		if e.tabExists(tab.tabID) {
			return tab, nil
		}
		// Tab was closed, remove it
		delete(e.tabs, site.ID)
	}

	// Open or reuse a tab for this site
	tabID, err := e.openOrReuseTab(site.URL)
	if err != nil {
		return nil, fmt.Errorf("open tab: %w", err)
	}

	tab := &siteTab{
		siteID: site.ID,
		tabID:  tabID,
		url:    site.URL,
	}
	e.tabs[site.ID] = tab
	log.Printf("[browser-act] created tab for %s: %s", site.ID, tabID)
	return tab, nil
}

// openOrReuseTab opens a URL in a new tab (or reuses an existing one) and returns the tab ID.
// It also starts the browser session via `browser open` when no active session exists —
// a browser record alone does not create a runnable session; `browser open` launches
// Chrome and binds the session name in one step.
func (e *BrowserActEngine) openOrReuseTab(url string) (string, error) {
	tabs, listErr := e.listTabs()

	// Reuse an existing tab whose URL matches. Handles post-navigation redirects
	// where the tab URL is a longer path under the same origin (e.g. /chat/<id>).
	if listErr == nil {
		for _, tab := range tabs {
			if tab.URL == url || strings.Contains(tab.URL, url) || strings.Contains(url, tab.URL) {
				log.Printf("[browser-act] reusing existing tab %s for %s", tab.ID, url)
				return tab.ID, nil
			}
		}
	}

	// No matching tab. If there is no active session (listTabs failed) or no tabs
	// at all, start the session with `browser open` — this launches Chrome and
	// creates the session. Otherwise open a new tab via navigate --new-tab.
	if listErr != nil || len(tabs) == 0 {
		if _, err := e.runCommand("--session", e.session, "browser", "open", e.browserID, url, "--headed"); err != nil {
			return "", fmt.Errorf("browser open (start session): %w", err)
		}
	} else {
		if _, err := e.runCommand("--session", e.session, "navigate", url, "--new-tab"); err != nil {
			return "", fmt.Errorf("navigate new-tab: %w", err)
		}
	}

	// Find the new tab by URL.
	tabs, err := e.listTabs()
	if err != nil {
		return "", fmt.Errorf("list tabs after open: %w", err)
	}
	for _, tab := range tabs {
		if tab.URL == url || strings.Contains(tab.URL, url) {
			return tab.ID, nil
		}
	}
	// Fallback: return the last tab (most recently opened).
	if len(tabs) > 0 {
		return tabs[len(tabs)-1].ID, nil
	}
	return "", fmt.Errorf("could not find new tab for %s", url)
}

// tabInfo represents a browser tab.
type tabInfo struct {
	ID    string
	URL   string
	Title string
}

// listTabs returns all tabs in the session.
func (e *BrowserActEngine) listTabs() ([]tabInfo, error) {
	data, err := e.runCommand("--session", e.session, "tab", "list")
	if err != nil {
		return nil, err
	}

	raw, _ := json.Marshal(data)
	var result struct {
		Tabs []struct {
			ID    string `json:"tab_id"`
			URL   string `json:"url"`
			Title string `json:"title"`
		} `json:"tabs"`
	}
	if err := json.Unmarshal(raw, &result); err != nil {
		return nil, fmt.Errorf("parse tab list: %w", err)
	}

	tabs := make([]tabInfo, 0, len(result.Tabs))
	for _, t := range result.Tabs {
		tabs = append(tabs, tabInfo{ID: t.ID, URL: t.URL, Title: t.Title})
	}
	return tabs, nil
}

// tabExists checks if a tab with the given ID exists.
func (e *BrowserActEngine) tabExists(tabID string) bool {
	tabs, err := e.listTabs()
	if err != nil {
		return false
	}
	for _, tab := range tabs {
		if tab.ID == tabID {
			return true
		}
	}
	return false
}

// switchToTab switches to the specified tab.
func (e *BrowserActEngine) switchToTab(tabID string) error {
	_, err := e.runCommand("--session", e.session, "tab", "switch", tabID)
	return err
}

// evalOnTab switches to the given tab and runs a script atomically.
// browser-act has a single "active tab": eval always runs on the active tab,
// so switch + eval must be serialized under e.mu to prevent concurrent sites
// from stealing each other's active tab between the switch and the eval.
func (e *BrowserActEngine) evalOnTab(tabID, siteID, scriptName string, payload map[string]interface{}) (interface{}, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if err := e.switchToTab(tabID); err != nil {
		return nil, fmt.Errorf("switch tab: %w", err)
	}
	return e.evalScript(siteID, scriptName, payload)
}

// Name returns the engine name.
func (e *BrowserActEngine) Name() string {
	return "browser-act"
}

// Close cleans up the session.
func (e *BrowserActEngine) Close() error {
	e.mu.Lock()
	defer e.mu.Unlock()

	if !e.initialized {
		return nil
	}

	// Close the session (which closes all tabs)
	if err := e.closeSession(); err != nil {
		log.Printf("[browser-act] error closing session: %v", err)
	}

	e.initialized = false
	e.tabs = make(map[string]*siteTab)
	return nil
}

// SendMessage sends a prompt to the specified site and returns the answer.
func (e *BrowserActEngine) SendMessage(ctx context.Context, site models.Site, prompt string) (string, error) {
	log.Printf("[browser-act] SendMessage: site=%s prompt=%q", site.ID, prompt[:min(50, len(prompt))])

	// Sanitize prompt to remove surrogate characters
	prompt = sanitizeString(prompt)

	// Ensure initialized
	if err := e.initialize(); err != nil {
		return "", fmt.Errorf("initialize: %w", err)
	}

	// Ensure site has a tab
	tab, err := e.ensureSiteTab(site)
	if err != nil {
		return "", fmt.Errorf("ensure tab: %w", err)
	}

	// Check if input is ready (logged in)
	if err := e.waitForInput(ctx, site, tab); err != nil {
		return "", fmt.Errorf("input not ready: %w", err)
	}

	// Send the prompt (switch + eval atomically)
	sendResult, err := e.evalOnTab(tab.tabID, site.ID, "send_prompt.js", map[string]interface{}{
		"prompt": prompt,
	})
	if err != nil {
		return "", fmt.Errorf("send prompt: %w", err)
	}
	log.Printf("[browser-act] send result: %v", sendResult)

	// Wait for answer to stabilize
	if err := e.waitForAnswer(ctx, site, tab); err != nil {
		return "", fmt.Errorf("wait for answer: %w", err)
	}

	// Extract the answer
	answer, err := e.extractAnswer(site, tab)
	if err != nil {
		return "", fmt.Errorf("extract answer: %w", err)
	}

	return answer, nil
}

// SendBatch sends a prompt to multiple sites through a coordinated pipeline:
//  1. Open tabs for all sites (sequential)
//  2. Wait for inputs (round-robin poll, single active tab per eval)
//  3. Send prompts to ready sites (sequential)
//  4. Wait for answers (round-robin poll, extract on completion)
//  5. Call onResult for each site as it completes or errors.
//
// This replaces concurrent per-site SendMessage calls. browser-act has a single
// active tab per session, so all eval operations must serialize through e.mu.
// With N concurrent sites each polling waitForInput independently, the lock was
// oversubscribed (N * ~40% duty cycle > 100%), causing a thundering herd where
// no site could reach send_prompt. The round-robin coordinator makes only one
// polling pass at a time, eliminating contention.
func (e *BrowserActEngine) SendBatch(ctx context.Context, sites []models.Site, prompt string, isNewSession bool, onResult func(site models.Site, content string, err error)) {
	log.Printf("[browser-act] SendBatch: %d sites, isNewSession=%v", len(sites), isNewSession)

	if err := e.initialize(); err != nil {
		for _, site := range sites {
			onResult(site, "", fmt.Errorf("initialize: %w", err))
		}
		return
	}

	type siteState struct {
		site    models.Site
		tab     *siteTab
		ready   bool
		sent    bool
		done    bool
		content string
		err     error
	}
	states := make([]*siteState, 0, len(sites))
	for _, site := range sites {
		states = append(states, &siteState{site: site})
	}

	// Phase 1: Open tabs + new chat (sequential, each evalOnTab holds e.mu briefly)
	for _, st := range states {
		tab, err := e.ensureSiteTab(st.site)
		if err != nil {
			st.err = fmt.Errorf("ensure tab: %w", err)
			st.done = true
			onResult(st.site, "", st.err)
			continue
		}
		st.tab = tab

		if isNewSession {
			if _, err := e.evalOnTab(tab.tabID, st.site.ID, "new_chat.js", nil); err != nil {
				log.Printf("[browser-act] new chat failed for %s: %v (non-fatal)", st.site.ID, err)
			}
			time.Sleep(1 * time.Second)
		}
	}

	// Phase 2: Wait for inputs (round-robin, no concurrent lock contention)
	inputDeadline := time.Now().Add(60 * time.Second)
	for time.Now().Before(inputDeadline) {
		select {
		case <-ctx.Done():
			for _, st := range states {
				if !st.done {
					st.err = ctx.Err()
					st.done = true
					onResult(st.site, "", st.err)
				}
			}
			return
		default:
		}

		allReady := true
		for _, st := range states {
			if st.done || st.ready {
				continue
			}
			allReady = false
			result, err := e.evalOnTab(st.tab.tabID, st.site.ID, "detect_input.js", nil)
			if err == nil {
				if rm, ok := result.(map[string]interface{}); ok && rm["ready"] == true {
					st.ready = true
					log.Printf("[browser-act] input ready: site=%s", st.site.ID)
				}
			}
		}
		if allReady {
			break
		}
		time.Sleep(2 * time.Second)
	}

	for _, st := range states {
		if !st.done && !st.ready {
			st.err = fmt.Errorf("input not ready after 60s (site %s) - user may need to login", st.site.ID)
			st.done = true
			onResult(st.site, "", st.err)
		}
	}

	// Phase 3: Send prompts (sequential, per-site format_prompt applied)
	for _, st := range states {
		if st.done || !st.ready {
			continue
		}
		actualPrompt := prompt
		if st.site.FormatPrompt != "" {
			actualPrompt = prompt + "\n\n" + st.site.FormatPrompt
		}
		_, err := e.evalOnTab(st.tab.tabID, st.site.ID, "send_prompt.js", map[string]interface{}{
			"prompt": sanitizeString(actualPrompt),
		})
		if err != nil {
			st.err = fmt.Errorf("send prompt: %w", err)
			st.done = true
			onResult(st.site, "", st.err)
			continue
		}
		st.sent = true
		log.Printf("[browser-act] prompt sent: site=%s", st.site.ID)
	}

	// Phase 4: Wait for answers (round-robin, extract + onResult on completion)
	// Timeout scales with site count: each round-robin cycle takes ~13s per site,
	// so 6 sites need ~78s per cycle. With stableRounds=1, we need 2 cycles (initial
	// + 1 match) = ~156s. Base 120s + 30s per site gives enough headroom.
	phase4Timeout := time.Duration(120+30*len(states)) * time.Second
	answerDeadline := time.Now().Add(phase4Timeout)
	log.Printf("[browser-act] Phase 4 timeout: %v for %d sites", phase4Timeout, len(states))
	for time.Now().Before(answerDeadline) {
		select {
		case <-ctx.Done():
			for _, st := range states {
				if !st.done && st.sent {
					st.err = ctx.Err()
					st.done = true
					onResult(st.site, "", st.err)
				}
			}
			return
		default:
		}

		allDone := true
		for _, st := range states {
			if st.done || !st.sent {
				continue
			}
			allDone = false
			// stableRounds=1: with N sites in round-robin, each cycle is ~13*N seconds.
			// One matching poll means the text was stable for a full cycle (~78s for 6
			// sites), which provides far more debounce than the rod engine's 2-3s.
			result, err := e.evalOnTab(st.tab.tabID, st.site.ID, "wait_answer.js", map[string]interface{}{
				"stableRounds": 1,
			})
			if err == nil {
				if rm, ok := result.(map[string]interface{}); ok {
					done, _ := rm["done"].(bool)
					answerCount, _ := rm["answerCount"].(float64)
					lastTextLen, _ := rm["lastTextLen"].(float64)
					stableRounds, _ := rm["stableRounds"].(float64)
					log.Printf("[browser-act] wait_answer site=%s done=%v answerCount=%.0f lastTextLen=%.0f stableRounds=%.0f",
						st.site.ID, done, answerCount, lastTextLen, stableRounds)
					if done {
						answer, extErr := e.evalOnTab(st.tab.tabID, st.site.ID, "extract_answer.js", nil)
						if extErr != nil {
							st.err = fmt.Errorf("extract answer: %w", extErr)
						} else if am, ok := answer.(map[string]interface{}); ok {
							text, _ := am["text"].(string)
							st.content = text
							if text == "" {
								st.err = fmt.Errorf("extract_answer returned empty text")
							}
						} else {
							st.err = fmt.Errorf("extract_answer returned non-object: %v", answer)
						}
						st.done = true
						onResult(st.site, st.content, st.err)
					}
				}
			}
		}
		if allDone {
			break
		}
		time.Sleep(2 * time.Second)
	}

	for _, st := range states {
		if !st.done && st.sent {
			st.err = fmt.Errorf("answer did not stabilize after %v (site %s)", phase4Timeout, st.site.ID)
			st.done = true
			onResult(st.site, "", st.err)
		}
	}
}

// StartNewChat starts a new chat for the specified site.
func (e *BrowserActEngine) StartNewChat(site models.Site) error {
	log.Printf("[browser-act] StartNewChat: site=%s", site.ID)

	if err := e.initialize(); err != nil {
		return fmt.Errorf("initialize: %w", err)
	}

	tab, err := e.ensureSiteTab(site)
	if err != nil {
		return fmt.Errorf("ensure tab: %w", err)
	}

	result, err := e.evalOnTab(tab.tabID, site.ID, "new_chat.js", nil)
	if err != nil {
		return fmt.Errorf("new chat: %w", err)
	}
	log.Printf("[browser-act] new chat result: %v", result)

	// Wait for the page to settle after new chat
	time.Sleep(2 * time.Second)
	return nil
}

// waitForInput polls until the input element is detected or timeout.
func (e *BrowserActEngine) waitForInput(ctx context.Context, site models.Site, tab *siteTab) error {
	deadline := time.Now().Add(180 * time.Second)
	for time.Now().Before(deadline) {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		result, err := e.evalOnTab(tab.tabID, site.ID, "detect_input.js", nil)
		if err == nil {
			resultMap, ok := result.(map[string]interface{})
			if ok && resultMap["ready"] == true {
				return nil
			}
		}
		time.Sleep(3 * time.Second)
	}
	return fmt.Errorf("input not ready after 180s (site %s) - user may need to login", site.ID)
}

// waitForAnswer polls until the answer text stabilizes.
func (e *BrowserActEngine) waitForAnswer(ctx context.Context, site models.Site, tab *siteTab) error {
	deadline := time.Now().Add(180 * time.Second)
	for time.Now().Before(deadline) {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		result, err := e.evalOnTab(tab.tabID, site.ID, "wait_answer.js", map[string]interface{}{
			"stableRounds": 3,
		})
		if err == nil {
			resultMap, ok := result.(map[string]interface{})
			if ok && resultMap["done"] == true {
				return nil
			}
		}
		time.Sleep(2 * time.Second)
	}
	return fmt.Errorf("answer did not stabilize after 180s (site %s)", site.ID)
}

// extractAnswer extracts the final answer text from the page.
func (e *BrowserActEngine) extractAnswer(site models.Site, tab *siteTab) (string, error) {
	result, err := e.evalOnTab(tab.tabID, site.ID, "extract_answer.js", nil)
	if err != nil {
		return "", err
	}
	resultMap, ok := result.(map[string]interface{})
	if !ok {
		return "", fmt.Errorf("extract_answer returned non-object: %v", result)
	}
	text, _ := resultMap["text"].(string)
	if text == "" {
		return "", fmt.Errorf("extract_answer returned empty text")
	}
	return text, nil
}

// evalScript reads a JS snippet file and executes it in the current tab.
func (e *BrowserActEngine) evalScript(siteID, scriptName string, payload map[string]interface{}) (interface{}, error) {
	scriptPath := filepath.Join(e.scriptsDir, siteID, scriptName)
	script, err := os.ReadFile(scriptPath)
	if err != nil {
		return nil, fmt.Errorf("read script %s: %w", scriptPath, err)
	}

	// Build the full script: shared lib + payload injection + site script
	libCode := e.getLibScript()
	var fullScript string
	if payload != nil {
		payloadJSON, _ := json.Marshal(payload)
		fullScript = fmt.Sprintf(
			"%s\n"+
				"globalThis.__PAYLOAD__ = Object.create(null);\n"+
				"var __payload__ = %s;\n"+
				"Object.keys(__payload__).forEach(function(k) { globalThis.__PAYLOAD__[k] = __payload__[k]; });\n"+
				"%s",
			libCode, sanitizeString(string(payloadJSON)), sanitizeString(string(script)),
		)
	} else {
		fullScript = fmt.Sprintf(
			"%s\n"+
				"globalThis.__PAYLOAD__ = Object.create(null);\n%s",
			libCode, sanitizeString(string(script)),
		)
	}

	// Use --stdin to pass JS to avoid shell escaping issues.
	// A timeout is mandatory: browser-act eval can hang indefinitely when the
	// browser tab is unresponsive or the script deadlocks. Without a timeout
	// the blocking cmd.Output() holds e.mu (via evalOnTab) and freezes the
	// entire batch coordinator, so no site's deadline check ever runs.
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, e.cmdPath, "--format", "json", "--session", e.session, "eval", "--stdin")
	hideWindow(cmd)
	cmd.Stdin = strings.NewReader(fullScript)

	out, err := cmd.Output()
	if err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return nil, fmt.Errorf("browser-act eval timeout after 30s (script %s)", scriptName)
		}
		if exitErr, ok := err.(*exec.ExitError); ok {
			stderr := string(exitErr.Stderr)
			var result map[string]interface{}
			if jsonErr := json.Unmarshal([]byte(stderr), &result); jsonErr == nil {
				if errMsg, exists := result["error"]; exists {
					return nil, fmt.Errorf("browser-act eval error: %s", errMsg)
				}
			}
			return nil, fmt.Errorf("browser-act eval failed: %s", stderr)
		}
		return nil, fmt.Errorf("browser-act eval: %w", err)
	}

	var result map[string]interface{}
	if err := json.Unmarshal(out, &result); err != nil {
		return nil, fmt.Errorf("parse eval response: %w", err)
	}

	evalResult, ok := result["result"]
	if !ok {
		return nil, fmt.Errorf("eval returned no result field: %v", result)
	}

	// If result is a string that looks like JSON, parse it
	if resultStr, ok := evalResult.(string); ok {
		var parsed interface{}
		if err := json.Unmarshal([]byte(resultStr), &parsed); err == nil {
			return parsed, nil
		}
	}
	return evalResult, nil
}

// sanitizeString removes surrogate characters that Python's UTF-8 encoder cannot handle.
// Characters like \udcac are UTF-16 surrogates that cause:
// 'utf-8' codec can't encode character '\udcac' in position N: surrogates not allowed
func sanitizeString(s string) string {
	// Replace surrogate characters with replacement character U+FFFD
	return strings.Map(func(r rune) rune {
		if r >= 0xD800 && r <= 0xDFFF {
			return 0xFFFD // Unicode replacement character
		}
		return r
	}, s)
}

// findBrowserAct finds the browser-act CLI binary.
func findBrowserAct() (string, error) {
	if path, err := exec.LookPath("browser-act"); err == nil {
		return path, nil
	}
	uvToolDir := getUVToolDir()
	if uvToolDir != "" {
		candidates := []string{
			filepath.Join(uvToolDir, "browser-act-cli", "bin", "browser-act"),
			filepath.Join(uvToolDir, "browser-act-cli", "Scripts", "browser-act.exe"),
		}
		for _, c := range candidates {
			if _, err := os.Stat(c); err == nil {
				return c, nil
			}
		}
	}
	if runtime.GOOS == "windows" {
		candidates := []string{
			filepath.Join(os.Getenv("USERPROFILE"), "AppData", "Roaming", "npm", "browser-act.cmd"),
			filepath.Join(os.Getenv("USERPROFILE"), "AppData", "Roaming", "npm", "browser-act"),
			`C:\Program Files\nodejs\browser-act.cmd`,
			`C:\Program Files (x86)\nodejs\browser-act.cmd`,
		}
		for _, c := range candidates {
			if _, err := os.Stat(c); err == nil {
				return c, nil
			}
		}
	}
	return "", errors.New("browser-act not found. Install: uv tool install browser-act-cli --python 3.12")
}

// getUVToolDir returns the uv tools installation directory.
func getUVToolDir() string {
	if uvPath, err := exec.LookPath("uv"); err == nil {
		uvCtx, uvCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer uvCancel()
		uvCmd := exec.CommandContext(uvCtx, uvPath, "tool", "dir")
		hideWindow(uvCmd)
		out, err := uvCmd.Output()
		if err == nil {
			return strings.TrimSpace(string(out))
		}
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	switch runtime.GOOS {
	case "windows", "darwin", "linux":
		return filepath.Join(home, ".local", "share", "uv", "tools")
	}
	return ""
}

// findOrCreateBrowser finds or creates the browser.
func (e *BrowserActEngine) findOrCreateBrowser(name, desc string) (string, error) {
	browsers, err := e.listBrowsers()
	if err != nil {
		return "", err
	}
	for _, b := range browsers {
		if b.Name == name {
			return b.ID, nil
		}
	}

	data, err := e.runCommand("browser", "create", "--type", "chrome", "--name", name, "--desc", desc)
	if err != nil {
		return "", fmt.Errorf("create browser: %w", err)
	}
	id, ok := data["id"].(string)
	if !ok || id == "" {
		return "", fmt.Errorf("browser create returned no id: %v", data)
	}
	log.Printf("[browser-act] created browser: id=%s name=%s", id, name)
	return id, nil
}

// baBrowser represents a browser.
type baBrowser struct {
	ID   string
	Name string
}

// listBrowsers returns all browsers.
func (e *BrowserActEngine) listBrowsers() ([]baBrowser, error) {
	data, err := e.runCommand("browser", "list")
	if err != nil {
		return nil, err
	}
	raw, _ := json.Marshal(data)
	var result struct {
		Browsers []struct {
			ID   string `json:"id"`
			Name string `json:"name"`
		} `json:"browsers"`
	}
	if err := json.Unmarshal(raw, &result); err != nil {
		return nil, fmt.Errorf("parse browser list: %w", err)
	}
	browsers := make([]baBrowser, 0, len(result.Browsers))
	for _, b := range result.Browsers {
		browsers = append(browsers, baBrowser{ID: b.ID, Name: b.Name})
	}
	return browsers, nil
}

// isSessionActive checks if a session is active.
func (e *BrowserActEngine) isSessionActive(sessionName string) bool {
	data, err := e.runCommand("session", "list")
	if err != nil {
		return false
	}
	raw, _ := json.Marshal(data)
	var result struct {
		Sessions []struct {
			Name string `json:"session_name"`
		} `json:"sessions"`
	}
	if err := json.Unmarshal(raw, &result); err != nil {
		return false
	}
	for _, s := range result.Sessions {
		if s.Name == sessionName {
			return true
		}
	}
	return false
}

// closeSession closes the session.
func (e *BrowserActEngine) closeSession() error {
	_, err := e.runCommand("session", "close", e.session)
	return err
}

// runCommand executes a browser-act command and returns the parsed JSON output.
// On process crash (exit 0xffffffff, caused by stale daemon state corrupting
// browser-act's logging handler on fork), it clears the entire daemon state
// directory and retries once so transient post-restart failures self-heal.
func (e *BrowserActEngine) runCommand(args ...string) (map[string]interface{}, error) {
	result, err := e.runCommandOnce(args...)
	if err == nil {
		return result, nil
	}
	// Retry only on process crash, not on timeouts or JSON-level errors.
	if !isBrowserActCrash(err) {
		return result, err
	}
	log.Printf("[browser-act] process crash on %v, clearing daemon state directory and retrying once", args)
	cleanDaemonStateDir()
	result2, err2 := e.runCommandOnce(args...)
	if err2 != nil {
		return result2, translateBrowserActErr(err2)
	}
	return result2, nil
}

// isBrowserActCrash reports whether err corresponds to a browser-act process
// crash (exit code 0xffffffff / -1) rather than a normal JSON error or timeout.
func isBrowserActCrash(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "exit status 0xffffffff") || strings.Contains(msg, "exit status -1")
}

// runCommandOnce executes a browser-act command exactly once.
func (e *BrowserActEngine) runCommandOnce(args ...string) (map[string]interface{}, error) {
	cmdArgs := append([]string{"--format", "json"}, args...)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, e.cmdPath, cmdArgs...)
	hideWindow(cmd)
	// Force Python 3 to use UTF-8 for stdin/stdout even when spawned from a
	// GUI process on Windows, avoiding 'surrogates not allowed' errors when
	// page DOM contains emoji or non-BMP characters.
	cmd.Env = append(os.Environ(), "PYTHONIOENCODING=utf-8", "PYTHONUTF8=1")

	out, err := cmd.Output()
	if err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return nil, fmt.Errorf("browser-act timeout after 20s: %v", args)
		}
		if exitErr, ok := err.(*exec.ExitError); ok {
			stderr := string(exitErr.Stderr)
			var result map[string]interface{}
			if jsonErr := json.Unmarshal([]byte(stderr), &result); jsonErr == nil {
				if okVal, exists := result["ok"]; exists && okVal == false {
					errMsg, _ := result["error"].(string)
					return result, fmt.Errorf("%s", errMsg)
				}
				return result, nil
			}
			// stderr is not JSON — likely a process crash. Include stderr
			// for diagnostics: browser-act crashes (exit 0xffffffff) often
			// emit a Python traceback here that pinpoints the root cause.
			if trimmed := strings.TrimSpace(stderr); trimmed != "" {
				return nil, fmt.Errorf("browser-act %v: %w [stderr: %s]", args, err, truncateForLog(trimmed, 500))
			}
		}
		return nil, fmt.Errorf("browser-act %v: %w", args, err)
	}

	var result map[string]interface{}
	if err := json.Unmarshal(out, &result); err != nil {
		return map[string]interface{}{"ok": true, "raw": string(out)}, nil
	}

	if okVal, exists := result["ok"]; exists && okVal == false {
		errMsg, _ := result["error"].(string)
		return result, fmt.Errorf("%s", errMsg)
	}
	return result, nil
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// truncateForLog trims and truncates a string for inclusion in error messages
// and log lines, keeping output readable while preserving the diagnostic prefix.
func truncateForLog(s string, max int) string {
	s = strings.TrimSpace(s)
	if len(s) <= max {
		return s
	}
	return s[:max] + "..."
}
