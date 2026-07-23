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
	"time"

	"chat-aggregator/internal/models"
)

// BrowserActEngine uses a single browser-act session with multiple tabs.
// Each site gets its own tab within one browser window.
type BrowserActEngine struct {
	cmdPath    string
	scriptsDir string
	mu         sync.Mutex
	initialized bool
	session    string
	browserID  string
	tabs       map[string]*siteTab // siteID -> tab info
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

	// Find or create browser
	browserID, err := e.findOrCreateBrowser("knowledgeclip", "KnowledgeClip multi-site browser")
	if err != nil {
		return fmt.Errorf("find/create browser: %w", err)
	}
	e.browserID = browserID

	e.initialized = true
	log.Printf("[browser-act] initialized: session=%s browser=%s", e.session, e.browserID)
	return nil
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

	// Create new tab
	tabID, err := e.openNewTab(site.URL)
	if err != nil {
		return nil, fmt.Errorf("open new tab: %w", err)
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

// openNewTab opens a URL in a new tab and returns the tab ID.
func (e *BrowserActEngine) openNewTab(url string) (string, error) {
	// Check if there are existing tabs
	tabs, err := e.listTabs()
	if err != nil {
		return "", fmt.Errorf("list tabs: %w", err)
	}

	if len(tabs) == 0 {
		// No tabs exist - open the first tab directly
		_, err := e.runCommand("--session", e.session, "browser", "open", e.browserID, url, "--headed")
		if err != nil {
			return "", fmt.Errorf("open first tab: %w", err)
		}
	} else {
		// Tabs exist - open a new tab
		_, err := e.runCommand("--session", e.session, "navigate", url, "--new-tab")
		if err != nil {
			return "", err
		}
	}

	// Get the new tab ID from tab list
	tabs, err = e.listTabs()
	if err != nil {
		return "", fmt.Errorf("list tabs after open: %w", err)
	}

	// Find the tab with matching URL
	for _, tab := range tabs {
		if tab.URL == url || strings.Contains(tab.URL, url) {
			return tab.ID, nil
		}
	}

	// Fallback: return the last tab (most recently opened)
	if len(tabs) > 0 {
		return tabs[len(tabs)-1].ID, nil
	}

	return "", fmt.Errorf("could not find new tab")
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

	// Switch to the site's tab
	if err := e.switchToTab(tab.tabID); err != nil {
		return "", fmt.Errorf("switch tab: %w", err)
	}

	// Check if input is ready (logged in)
	if err := e.waitForInput(ctx, site); err != nil {
		return "", fmt.Errorf("input not ready: %w", err)
	}

	// Send the prompt
	sendResult, err := e.evalScript(site.ID, "send_prompt.js", map[string]interface{}{
		"prompt": prompt,
	})
	if err != nil {
		return "", fmt.Errorf("send prompt: %w", err)
	}
	log.Printf("[browser-act] send result: %v", sendResult)

	// Wait for answer to stabilize
	if err := e.waitForAnswer(ctx, site); err != nil {
		return "", fmt.Errorf("wait for answer: %w", err)
	}

	// Extract the answer
	answer, err := e.extractAnswer(site)
	if err != nil {
		return "", fmt.Errorf("extract answer: %w", err)
	}

	return answer, nil
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

	if err := e.switchToTab(tab.tabID); err != nil {
		return fmt.Errorf("switch tab: %w", err)
	}

	result, err := e.evalScript(site.ID, "new_chat.js", nil)
	if err != nil {
		return fmt.Errorf("new chat: %w", err)
	}
	log.Printf("[browser-act] new chat result: %v", result)

	// Wait for the page to settle after new chat
	time.Sleep(2 * time.Second)
	return nil
}

// waitForInput polls until the input element is detected or timeout.
func (e *BrowserActEngine) waitForInput(ctx context.Context, site models.Site) error {
	deadline := time.Now().Add(180 * time.Second)
	for time.Now().Before(deadline) {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		result, err := e.evalScript(site.ID, "detect_input.js", nil)
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
func (e *BrowserActEngine) waitForAnswer(ctx context.Context, site models.Site) error {
	deadline := time.Now().Add(180 * time.Second)
	for time.Now().Before(deadline) {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		result, err := e.evalScript(site.ID, "wait_answer.js", map[string]interface{}{
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
func (e *BrowserActEngine) extractAnswer(site models.Site) (string, error) {
	result, err := e.evalScript(site.ID, "extract_answer.js", nil)
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

	// Build the full script with payload injection
	var fullScript string
	if payload != nil {
		payloadJSON, _ := json.Marshal(payload)
		fullScript = fmt.Sprintf(
			"globalThis.__PAYLOAD__ = Object.create(null);\n"+
				"var __payload__ = %s;\n"+
				"Object.keys(__payload__).forEach(function(k) { globalThis.__PAYLOAD__[k] = __payload__[k]; });\n"+
				"%s",
			sanitizeString(string(payloadJSON)), sanitizeString(string(script)),
		)
	} else {
		fullScript = fmt.Sprintf(
			"globalThis.__PAYLOAD__ = Object.create(null);\n%s",
			sanitizeString(string(script)),
		)
	}

	// Use --stdin to pass JS to avoid shell escaping issues
	cmd := exec.Command(e.cmdPath, "--format", "json", "--session", e.session, "eval", "--stdin")
	cmd.Stdin = strings.NewReader(fullScript)

	out, err := cmd.Output()
	if err != nil {
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
		out, err := exec.Command(uvPath, "tool", "dir").Output()
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
func (e *BrowserActEngine) runCommand(args ...string) (map[string]interface{}, error) {
	cmdArgs := append([]string{"--format", "json"}, args...)
	cmd := exec.Command(e.cmdPath, cmdArgs...)

	out, err := cmd.Output()
	if err != nil {
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
