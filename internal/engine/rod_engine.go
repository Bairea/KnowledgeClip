package engine

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	neturl "net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"chat-aggregator/internal/models"
	"chat-aggregator/internal/storage"

	"github.com/go-rod/rod"
	"github.com/go-rod/rod/lib/input"
	"github.com/go-rod/rod/lib/launcher"
	"github.com/go-rod/rod/lib/proto"
)

type RodEngine struct {
	browser         *rod.Browser
	db              *storage.DB
	pages           map[string]*rod.Page
	mu              sync.Mutex
	browserMu       sync.Mutex
	controlURL      string
	userDataDir     string
	tabGuardStarted bool
}

func (re *RodEngine) ensureBrowser() error {
	re.browserMu.Lock()
	defer re.browserMu.Unlock()
	if re.browser != nil {
		connected := true
		done := make(chan bool)
		go func() {
			_, err := re.browser.Pages()
			done <- (err == nil)
		}()
		select {
		case ok := <-done:
			connected = ok
		case <-time.After(3 * time.Second):
			connected = false
		}
		if connected {
			return nil
		}
		log.Printf("[rod] browser disconnected, attempting reconnect")
		if re.controlURL != "" {
			re.browser = rod.New().ControlURL(re.controlURL)
			reconnectDone := make(chan error)
			go func() {
				reconnectDone <- re.browser.Connect()
			}()
			select {
			case err := <-reconnectDone:
				if err == nil {
					verifyDone := make(chan bool)
					go func() {
						_, e := re.browser.Pages()
						verifyDone <- (e == nil)
					}()
					select {
					case ok := <-verifyDone:
						if ok {
							log.Printf("[rod] browser reconnected to existing controlURL: %s", re.controlURL)
							return nil
						}
					case <-time.After(3 * time.Second):
					}
				}
			case <-time.After(3 * time.Second):
			}
			log.Printf("[rod] reconnect to existing controlURL failed")
		}
		if re.userDataDir == "" {
			re.userDataDir = "./.browser-data"
		}
		portURL := readDevToolsActivePort(re.userDataDir)
		if portURL != "" && portURL != re.controlURL {
			log.Printf("[rod] trying DevToolsActivePort URL: %s", portURL)
			re.browser = rod.New().ControlURL(portURL)
			portConnectDone := make(chan error)
			go func() {
				portConnectDone <- re.browser.Connect()
			}()
			select {
			case err := <-portConnectDone:
				if err == nil {
					verifyDone := make(chan bool)
					go func() {
						_, e := re.browser.Pages()
						verifyDone <- (e == nil)
					}()
					select {
					case ok := <-verifyDone:
						if ok {
							re.controlURL = portURL
							log.Printf("[rod] browser reconnected via DevToolsActivePort: %s", portURL)
							return nil
						}
					case <-time.After(3 * time.Second):
					}
				}
			case <-time.After(3 * time.Second):
			}
			log.Printf("[rod] DevToolsActivePort reconnect failed")
		}
		log.Printf("[rod] killing stale chrome processes and launching new browser")
		killChromeProcesses(re.userDataDir)
		time.Sleep(2 * time.Second)
	} else {
		log.Printf("[rod] browser not initialized, attempting launch")
		if re.userDataDir == "" {
			re.userDataDir = "./.browser-data"
		}
	}

	re.mu.Lock()
	re.pages = make(map[string]*rod.Page)
	re.mu.Unlock()

	cleanupLockFiles(re.userDataDir)

	l := createLauncher(re.userDataDir)

	u, err := l.Launch()
	if err != nil {
		return fmt.Errorf("launch browser: %w", err)
	}
	re.controlURL = u
	re.browser = rod.New().ControlURL(u)
	if err := re.browser.Connect(); err != nil {
		return fmt.Errorf("connect browser: %w", err)
	}
	log.Printf("[rod] browser connected successfully")
	re.startTabGuard()
	return nil
}

type Selectors struct {
	Input           string `json:"input"`
	Submit          string `json:"submit"`
	Answer          string `json:"answer"`
	WaitFor         string `json:"wait_for"`
	NewChat         string `json:"new_chat"`
	CopyButton      string `json:"copy_button"`
	ContentStrategy string `json:"content_strategy"`
}

func findChromeBinary() string {
	candidates := []string{
		`C:\Program Files\Google\Chrome\Application\chrome.exe`,
		`C:\Program Files (x86)\Google\Chrome\Application\chrome.exe`,
		`C:\Users\baizhicong\AppData\Local\Google\Chrome\Application\chrome.exe`,
		`C:\Program Files (x86)\Microsoft\Edge\Application\msedge.exe`,
		`C:\Program Files\Microsoft\Edge\Application\msedge.exe`,
	}
	for _, p := range candidates {
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	return ""
}

func createLauncher(userDataDir string) *launcher.Launcher {
	l := launcher.New()
	chromePath := findChromeBinary()
	if chromePath != "" {
		l = l.Bin(chromePath)
	}
	l = l.UserDataDir(userDataDir)
	l = l.Headless(false).Devtools(true)
	l = l.Delete("enable-automation")
	l = l.Delete("useAutomationExtension")
	l = l.Set("disable-blink-features", "AutomationControlled")
	l = l.Set("disable-features", "IsolateOrigins,site-per-process,AutomationControlled")
	l = l.Set("no-first-run", "true")
	l = l.Set("no-default-browser-check", "true")
	l = l.Set("password-store", "basic")
	l = l.Set("use-mock-keychain", "true")
	l = l.Set("lang", "zh-CN")
	return l
}

func cleanupLockFiles(userDataDir string) {
	lockFiles := []string{
		"lockfile",
		"SingletonLock",
		"SingletonSocket",
		"SingletonCookie",
		"DevToolsActivePort",
	}
	for _, f := range lockFiles {
		path := filepath.Join(userDataDir, f)
		_ = os.Remove(path)
	}
	log.Printf("[rod] cleaned up lock files in %s", userDataDir)
}

// looksLikeThinking reports whether the text is likely thinking/reasoning
// content rather than the final answer. GLM renders a "跳过思考" (skip
// thinking) button and a "起草内容" (draft content) label while the model
// reasons internally; these markers only appear during the thinking phase.
func looksLikeThinking(text string) bool {
	thinkMarkers := []string{
		"跳过思考",
		"起草内容",
		"思考过程",
		"skip thinking",
		"Skip thinking",
	}
	for _, m := range thinkMarkers {
		if strings.Contains(text, m) {
			return true
		}
	}
	return false
}

func readDevToolsActivePort(userDataDir string) string {
	data, err := os.ReadFile(filepath.Join(userDataDir, "DevToolsActivePort"))
	if err != nil {
		return ""
	}
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) < 1 {
		return ""
	}
	port := strings.TrimSpace(lines[0])
	if port == "" {
		return ""
	}
	return "http://127.0.0.1:" + port
}

func killChromeProcesses(userDataDir string) {
	cmd := exec.Command("tasklist", "/FI", "IMAGENAME eq chrome.exe", "/FO", "CSV", "/NH")
	out, err := cmd.Output()
	if err != nil {
		return
	}
	lines := strings.Split(string(out), "\n")
	absDir, _ := filepath.Abs(userDataDir)
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || !strings.Contains(line, "chrome.exe") {
			continue
		}
		fields := strings.Split(line, ",")
		if len(fields) < 2 {
			continue
		}
		pidStr := strings.Trim(fields[1], "\" ")
		if pidStr == "" {
			continue
		}
		checkCmd := exec.Command("wmic", "process", "where", "ProcessId="+pidStr, "get", "CommandLine", "/FORMAT:LIST")
		checkOut, err := checkCmd.Output()
		if err != nil {
			continue
		}
		cmdLine := string(checkOut)
		if strings.Contains(cmdLine, userDataDir) || strings.Contains(cmdLine, absDir) {
			killCmd := exec.Command("taskkill", "/F", "/PID", pidStr)
			_ = killCmd.Run()
			log.Printf("[rod] killed chrome process %s using %s", pidStr, userDataDir)
		}
	}
}

func NewRodEngine(db *storage.DB) *RodEngine {
	chromePath := findChromeBinary()
	if chromePath == "" {
		log.Printf("[rod] no system Chrome found, rod will try to download")
	} else {
		log.Printf("[rod] using browser binary: %s", chromePath)
	}

	userDataDir := "./.browser-data"
	cleanupLockFiles(userDataDir)

	l := createLauncher(userDataDir)

	u, err := l.Launch()
	if err != nil {
		log.Printf("[rod] launch failed: %v", err)
		return &RodEngine{db: db, pages: make(map[string]*rod.Page)}
	}

	browser := rod.New().ControlURL(u)
	if err := browser.Connect(); err != nil {
		log.Printf("[rod] connect failed: %v", err)
		return &RodEngine{db: db, pages: make(map[string]*rod.Page)}
	}

	log.Printf("[rod] browser connected successfully")
	re := &RodEngine{
		browser:     browser,
		db:          db,
		pages:       make(map[string]*rod.Page),
		controlURL:  u,
		userDataDir: userDataDir,
	}
	re.startTabGuard()
	return re
}

func (re *RodEngine) startTabGuard() {
	re.browserMu.Lock()
	if re.tabGuardStarted {
		re.browserMu.Unlock()
		return
	}
	re.tabGuardStarted = true
	re.browserMu.Unlock()

	go func() {
		ticker := time.NewTicker(2 * time.Second)
		defer ticker.Stop()
		for range ticker.C {
			re.cleanupExtraTabs()
		}
	}()
	log.Printf("[rod] tab guard started, will close non-main tabs every 2s")
}

func (re *RodEngine) cleanupExtraTabs() {
	re.browserMu.Lock()
	browser := re.browser
	re.browserMu.Unlock()
	if browser == nil {
		return
	}

	pages, err := browser.Pages()
	if err != nil {
		return
	}

	mainIDs := make(map[string]bool)
	re.mu.Lock()
	for _, p := range re.pages {
		if p == nil {
			continue
		}
		mainIDs[string(p.TargetID)] = true
	}
	re.mu.Unlock()

	closed := 0
	for _, p := range pages {
		if mainIDs[string(p.TargetID)] {
			continue
		}
		info, err := p.Info()
		if err != nil {
			continue
		}
		url := info.URL
		if url == "" || url == "about:blank" {
			continue
		}
		log.Printf("[rod] tab guard: closing extra tab url=%s", url)
		_ = p.Close()
		closed++
	}
	if closed > 0 {
		log.Printf("[rod] tab guard: closed %d extra tabs", closed)
	}
}

func toNetworkCookieParam(c *proto.NetworkCookie) *proto.NetworkCookieParam {
	p := &proto.NetworkCookieParam{
		Name:         c.Name,
		Value:        c.Value,
		Domain:       c.Domain,
		Path:         c.Path,
		Expires:      c.Expires,
		Secure:       c.Secure,
		HTTPOnly:     c.HTTPOnly,
		SameSite:     c.SameSite,
		Priority:     c.Priority,
		SameParty:    c.SameParty,
		SourceScheme: c.SourceScheme,
		PartitionKey: c.PartitionKey,
	}
	if c.SourcePort != 0 {
		p.SourcePort = &c.SourcePort
	}
	return p
}

func (re *RodEngine) getOrCreatePage(site models.Site) (*rod.Page, error) {
	re.mu.Lock()
	defer re.mu.Unlock()

	if page, ok := re.pages[site.ID]; ok && page != nil {
		log.Printf("[rod] reusing existing page for site %s", site.ID)
		return page, nil
	}

	if re.browser == nil {
		return nil, fmt.Errorf("browser not connected for site %s", site.ID)
	}

	log.Printf("[rod] creating new page for site %s -> %s", site.ID, site.URL)
	page, err := re.browser.Page(proto.TargetCreateTarget{URL: ""})
	if err != nil {
		re.browser = nil
		return nil, fmt.Errorf("create page: %w", err)
	}

	if re.db != nil {
		siteCookie, err := storage.GetSiteCookie(re.db, site.ID)
		if err == nil && siteCookie != nil && siteCookie.Cookies != "" {
			var cookies []*proto.NetworkCookieParam
			if json.Unmarshal([]byte(siteCookie.Cookies), &cookies) == nil && len(cookies) > 0 {
				_ = page.SetCookies(cookies)
				log.Printf("[rod] injected %d cookies for site %s", len(cookies), site.ID)
			}
		}
	}

	visibilityJs := `
		Object.defineProperty(navigator, 'webdriver', {get: function() { return undefined; }, configurable: true});
		Object.defineProperty(navigator, 'languages', {get: function() { return ['zh-CN', 'zh', 'en']; }, configurable: true});
		Object.defineProperty(navigator, 'plugins', {get: function() { return [1, 2, 3, 4, 5]; }, configurable: true});
		if (navigator.permissions && navigator.permissions.query) {
			var origQuery = navigator.permissions.query.bind(navigator.permissions);
			navigator.permissions.query = function(params) {
				if (params && params.name === 'notifications') {
					return Promise.resolve({state: 'granted'});
				}
				return origQuery(params);
			};
		}
		window.chrome = window.chrome || {runtime: {}};
		var cdcKeys = Object.keys(window).filter(function(k) { return k.indexOf('cdc_') === 0; });
		cdcKeys.forEach(function(k) { delete window[k]; });
		Object.defineProperty(document, 'hidden', {get: function() { return false; }, configurable: true});
		Object.defineProperty(document, 'webkitHidden', {get: function() { return false; }, configurable: true});
		Object.defineProperty(document, 'visibilityState', {get: function() { return 'visible'; }, configurable: true});
		Object.defineProperty(document, 'webkitVisibilityState', {get: function() { return 'visible'; }, configurable: true});
		document.hasFocus = function() { return true; };

		window.requestAnimationFrame = function(callback) {
			return setTimeout(function() { callback(performance.now()); }, 16);
		};
		window.cancelAnimationFrame = function(id) { clearTimeout(id); };
		window.requestIdleCallback = function(callback) {
			return setTimeout(function() { callback({didTimeout: false, timeRemaining: function() { return 50; }}); }, 16);
		};
		window.cancelIdleCallback = function(id) { clearTimeout(id); };

		window.IntersectionObserver = function(callback, options) {
			return {
				observe: function(target) {
					var rect = target.getBoundingClientRect();
					var entry = {
						boundingClientRect: rect,
						intersectionRatio: 1,
						intersectionRect: rect,
						isIntersecting: true,
						rootBounds: null,
						target: target,
						time: performance.now()
					};
					callback([entry], this);
				},
				unobserve: function() {},
				disconnect: function() {},
				takeRecords: function() { return []; }
			};
		};

		window.ResizeObserver = function(callback) {
			return {
				observe: function(target) {
					var rect = target.getBoundingClientRect();
					callback([{
						target: target,
						contentRect: {x: rect.x, y: rect.y, width: rect.width, height: rect.height, top: rect.top, right: rect.right, bottom: rect.bottom, left: rect.left},
						borderBoxSize: [{inlineSize: rect.width, blockSize: rect.height}],
						contentBoxSize: [{inlineSize: rect.width, blockSize: rect.height}],
						devicePixelContentBoxSize: [{inlineSize: rect.width, blockSize: rect.height}]
					}], this);
				},
				unobserve: function() {},
				disconnect: function() {}
			};
		};

		var blockedPaths = ['/record', '/recording', '/write', '/writing', '/agent', '/canvas', '/draw', '/paint', '/workspace'];
		var isBlockedURL = function(url) {
			if (!url) return false;
			try {
				for (var b = 0; b < blockedPaths.length; b++) {
					if (url.indexOf(blockedPaths[b]) >= 0) return true;
				}
			} catch(e) {}
			return false;
		};

		window.open = function(url) {
			if (url && !isBlockedURL(url)) {
				window.location.href = url;
			}
			return null;
		};

		document.addEventListener('click', function(e) {
			var el = e.target;
			while (el && el.tagName !== 'A') {
				el = el.parentElement;
			}
			if (el && el.tagName === 'A') {
				if (el.target === '_blank' || el.getAttribute('target') === '_blank') {
					e.preventDefault();
					if (el.href && !isBlockedURL(el.href)) {
						window.location.href = el.href;
					}
				}
			}
		}, true);

		var removeBlankTargets = function() {
			var links = document.querySelectorAll('a[target="_blank"]');
			for (var i = 0; i < links.length; i++) {
				links[i].removeAttribute('target');
			}
		};
		if (document.readyState === 'complete' || document.readyState === 'interactive') {
			removeBlankTargets();
		} else {
			document.addEventListener('DOMContentLoaded', removeBlankTargets);
		}
		var observer = new MutationObserver(function() { removeBlankTargets(); });
		try {
			observer.observe(document.documentElement, {childList: true, subtree: true});
		} catch(e) {}

		var origPush = history.pushState ? history.pushState.bind(history) : null;
		var origReplace = history.replaceState ? history.replaceState.bind(history) : null;
		if (origPush) {
			history.pushState = function(state, title, url) {
				if (isBlockedURL(url)) return;
				origPush(state, title, url);
			};
		}
		if (origReplace) {
			history.replaceState = function(state, title, url) {
				if (isBlockedURL(url)) return;
				origReplace(state, title, url);
			};
		}
		try {
			var origAssign = window.location.assign ? window.location.assign.bind(window.location) : null;
			var origRep = window.location.replace ? window.location.replace.bind(window.location) : null;
			if (origAssign) {
				window.location.assign = function(url) {
					if (isBlockedURL(url)) return;
					origAssign(url);
				};
			}
			if (origRep) {
				window.location.replace = function(url) {
					if (isBlockedURL(url)) return;
					origRep(url);
				};
			}
		} catch(e) {}
		// Intercept direct location.href assignment (e.g. Qwen homepage does
		// window.location.href = '/record' which bypasses all the overrides above).
		// Also covers window.location = '...' and document.location = '...' since
		// they all go through Location.prototype.href setter.
		try {
			var hrefDesc = Object.getOwnPropertyDescriptor(Location.prototype, 'href');
			if (hrefDesc && typeof hrefDesc.set === 'function') {
				var origHrefSet = hrefDesc.set;
				Object.defineProperty(Location.prototype, 'href', {
					configurable: true,
					enumerable: true,
					get: hrefDesc.get,
					set: function(url) {
						if (isBlockedURL(url)) {
							console.log('[tab-guard] blocked location.href set to: ' + url);
							return;
						}
						origHrefSet.call(this, url);
					}
				});
			}
		} catch(e) {}
	`
	_, _ = proto.PageAddScriptToEvaluateOnNewDocument{Source: visibilityJs}.Call(page)

	if err := page.Navigate(site.URL); err != nil {
		page.Close()
		return nil, fmt.Errorf("navigate to %s: %w", site.URL, err)
	}

	log.Printf("[rod] waiting for page load: %s", site.URL)
	if err := page.WaitLoad(); err != nil {
		log.Printf("[rod] WaitLoad failed (non-fatal): %v", err)
	}

	_ = page.WaitIdle(3 * time.Second)

	page.Eval(`() => {
		Object.defineProperty(document, 'hidden', {get: function() { return false; }, configurable: true});
		Object.defineProperty(document, 'visibilityState', {get: function() { return 'visible'; }, configurable: true});
		document.hasFocus = function() { return true; };
		document.dispatchEvent(new Event('visibilitychange'));
	}`)

	re.pages[site.ID] = page
	log.Printf("[rod] page ready for site %s", site.ID)
	return page, nil
}

func (re *RodEngine) typePrompt(page *rod.Page, selector string, prompt string) error {
	log.Printf("[rod] looking for input element: %s", selector)

	editorTypeJs := fmt.Sprintf(`
		() => {
			var el = document.querySelector(%q);
			if (!el) return 'notfound';
			if (el.getAttribute('data-slate-editor') === 'true') return 'slate';
			if (el.getAttribute('data-lexical-editor') === 'true' || el.__lexicalEditor) return 'lexical';
			if (el.classList && (el.classList.contains('ProseMirror') || el.classList.contains('tiptap'))) return 'prosemirror';
			if (el.isContentEditable) {
				var reactKey = Object.keys(el).find(function(k) {
					return k.indexOf('__reactFiber') === 0;
				});
				if (reactKey) {
					var fiber = el[reactKey];
					for (var i = 0; i < 15 && fiber; i++) {
						if (fiber.memoizedProps && fiber.memoizedProps.editor &&
							typeof fiber.memoizedProps.editor.insertText === 'function') {
							return 'slate';
						}
						if (fiber.memoizedProps && fiber.memoizedProps.editor &&
							fiber.memoizedProps.editor.__lexicalEditor) {
							return 'lexical';
						}
						fiber = fiber.return;
					}
				}
			}
			return 'plain';
		}
	`, selector)

	editorTypeResult, err := page.Timeout(15 * time.Second).Eval(editorTypeJs)
	if err != nil {
		return fmt.Errorf("check editor type for %s: %w", selector, err)
	}
	editorType := editorTypeResult.Value.Str()

	if editorType == "notfound" {
		return fmt.Errorf("input element %s not found", selector)
	}

	if editorType == "slate" {
		log.Printf("[rod] detected Slate.js editor, using Slate API (%d chars)", len(prompt))
		return re.typePromptSlate(page, selector, prompt)
	}

	if editorType == "lexical" {
		log.Printf("[rod] detected Lexical editor, using execCommand (%d chars)", len(prompt))
		return re.typePromptLexical(page, selector, prompt)
	}

	if editorType == "prosemirror" {
		log.Printf("[rod] detected ProseMirror/TipTap editor (%d chars)", len(prompt))
		return re.typePromptProseMirror(page, selector, prompt)
	}

	if editorType == "plain" {
		ceCheckJs := fmt.Sprintf(`() => { var el = document.querySelector(%q); if (!el) return false; return el.isContentEditable; }`, selector)
		if ceResult, err := page.Eval(ceCheckJs); err == nil && ceResult.Value.Bool() {
			log.Printf("[rod] element is contenteditable but detected as plain, waiting for framework init")
			time.Sleep(2 * time.Second)
			retryResult, retryErr := page.Timeout(15 * time.Second).Eval(editorTypeJs)
			if retryErr == nil {
				editorType = retryResult.Value.Str()
				log.Printf("[rod] editor type re-detected after wait: %s", editorType)
				if editorType == "slate" {
					return re.typePromptSlate(page, selector, prompt)
				}
				if editorType == "lexical" {
					return re.typePromptLexical(page, selector, prompt)
				}
				if editorType == "prosemirror" {
					return re.typePromptProseMirror(page, selector, prompt)
				}
			}
		}
	}

	log.Printf("[rod] found plain input element, using JS input (%d chars)", len(prompt))

	js := fmt.Sprintf(`
		() => {
			var el = document.querySelector(%q);
			if (!el) return 'not found';
			if (el.tagName === 'TEXTAREA' || el.tagName === 'INPUT') {
				el.focus();
				el.value = '';
				if (el._valueTracker) {
					el._valueTracker.setValue('');
				}
				var nativeSetter = Object.getOwnPropertyDescriptor(
					window.HTMLTextAreaElement.prototype, 'value'
				) || Object.getOwnPropertyDescriptor(
					window.HTMLInputElement.prototype, 'value'
				);
				if (nativeSetter && nativeSetter.set) {
					nativeSetter.set.call(el, %q);
				} else {
					el.value = %q;
				}
				el.dispatchEvent(new Event('input', { bubbles: true }));
				el.dispatchEvent(new Event('change', { bubbles: true }));
				return el.value.substring(0, 80);
			} else if (el.isContentEditable) {
				el.focus();
				var sel = window.getSelection();
				sel.removeAllRanges();
				var range = document.createRange();
				range.selectNodeContents(el);
				sel.addRange(range);
				var ok = document.execCommand('insertText', false, %q);
				if (!ok) {
					el.innerText = %q;
					el.dispatchEvent(new InputEvent('input', { bubbles: true, data: %q }));
				}
				return (el.innerText || el.textContent || '').substring(0, 80);
			} else {
				el.textContent = %q;
				el.dispatchEvent(new Event('input', { bubbles: true }));
				return (el.textContent || '').substring(0, 80);
			}
		}
	`, selector, prompt, prompt, prompt, prompt, prompt, prompt)

	result, err := page.Eval(js)
	if err != nil {
		return fmt.Errorf("js input failed: %w", err)
	}
	inputResult := result.Value.Str()
	if inputResult == "not found" {
		return errors.New("js input returned not found: element not found")
	}

	verifyJs := fmt.Sprintf(`() => {
		var el = document.querySelector(%q);
		if (!el) return '';
		if (el.tagName === 'TEXTAREA' || el.tagName === 'INPUT') return (el.value || '').substring(0, 80);
		return (el.innerText || el.textContent || '').substring(0, 80);
	}`, selector)
	if verifyResult, vErr := page.Eval(verifyJs); vErr == nil {
		verifyText := verifyResult.Value.Str()
		if verifyText == "" || len(verifyText) < min(10, len(prompt)) {
			log.Printf("[rod] input verification failed (got %q), retrying with execCommand after 2s", verifyText)
			time.Sleep(2 * time.Second)
			retryJs := fmt.Sprintf(`() => {
				var el = document.querySelector(%q);
				if (!el) return 'not found';
				el.focus();
				if (el.isContentEditable) {
					var sel = window.getSelection();
					sel.removeAllRanges();
					var range = document.createRange();
					range.selectNodeContents(el);
					sel.addRange(range);
					document.execCommand('delete');
					var ok = document.execCommand('insertText', false, %q);
					if (!ok) {
						el.innerText = %q;
						el.dispatchEvent(new InputEvent('input', { bubbles: true, data: %q }));
					}
					return (el.innerText || el.textContent || '').substring(0, 80);
				}
				if (el._valueTracker) el._valueTracker.setValue('');
				var nativeSetter = Object.getOwnPropertyDescriptor(
					window.HTMLTextAreaElement.prototype, 'value'
				) || Object.getOwnPropertyDescriptor(
					window.HTMLInputElement.prototype, 'value'
				);
				if (nativeSetter && nativeSetter.set) {
					nativeSetter.set.call(el, %q);
				} else {
					el.value = %q;
				}
				el.dispatchEvent(new Event('input', { bubbles: true }));
				el.dispatchEvent(new Event('change', { bubbles: true }));
				return el.value.substring(0, 80);
			}`, selector, prompt, prompt, prompt, prompt, prompt)
			if retryResult, rErr := page.Eval(retryJs); rErr == nil {
				inputResult = retryResult.Value.Str()
				log.Printf("[rod] retry input result: %q", inputResult[:min(50, len(inputResult))])
			}
		} else {
			log.Printf("[rod] JS input succeeded, value=%q", verifyText[:min(50, len(verifyText))])
		}
	}
	return nil
}

func (re *RodEngine) typePromptProseMirror(page *rod.Page, selector string, prompt string) error {
	js := fmt.Sprintf(`
		() => {
			var el = document.querySelector(%q);
			if (!el) return 'editor element not found';
			el.focus();

			var sel = window.getSelection();
			sel.removeAllRanges();
			var range = document.createRange();
			range.selectNodeContents(el);
			sel.addRange(range);
			document.execCommand('delete');

			var ok = document.execCommand('insertText', false, %q);
			if (ok) {
				var text = (el.innerText || el.textContent || '').trim();
				if (text.length > 0) return 'ok:execCommand:' + text.substring(0, 40);
			}

			var editor = el.editor;
			if (!editor) {
				var desc = el.pmViewDesc;
				if (desc && desc.view) editor = desc.view;
			}
			if (!editor) {
				var reactKey = Object.keys(el).find(function(k) {
					return k.indexOf('__reactFiber') === 0;
				});
				if (reactKey) {
					var fiber = el[reactKey];
					for (var i = 0; i < 15 && fiber; i++) {
						if (fiber.memoizedProps && fiber.memoizedProps.editor &&
							fiber.memoizedProps.editor.view) {
							editor = fiber.memoizedProps.editor.view;
							break;
						}
						fiber = fiber.return;
					}
				}
			}
			if (editor && editor.dispatch && editor.state) {
				editor.dispatch(editor.state.tr.insertText(%q));
				var text2 = (el.innerText || el.textContent || '').trim();
				if (text2.length > 0) return 'ok:prosemirror-api:' + text2.substring(0, 40);
			}

			el.innerHTML = '<p>' + %q.replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;').replace(/\n/g, '<br>') + '</p>';
			el.dispatchEvent(new InputEvent('input', { bubbles: true }));
			var text3 = (el.innerText || el.textContent || '').trim();
			if (text3.length > 0) return 'ok:innerHTML:' + text3.substring(0, 40);
			return 'all methods failed';
		}
	`, selector, prompt, prompt, prompt)

	result, err := page.Timeout(15 * time.Second).Eval(js)
	if err != nil {
		return fmt.Errorf("prosemirror input eval failed: %w", err)
	}

	status := result.Value.String()
	if !strings.HasPrefix(status, "ok") {
		return fmt.Errorf("prosemirror input failed: %s", status)
	}

	log.Printf("[rod] ProseMirror input succeeded: %s", status[:min(60, len(status))])
	return nil
}

func (re *RodEngine) typePromptLexical(page *rod.Page, selector string, prompt string) error {
	js := fmt.Sprintf(`
		async () => {
			var el = document.querySelector(%q);
			if (!el) return 'editor element not found';
			el.focus();

			if (!el.__lexicalEditor) {
				return 'lexical editor not initialized';
			}

			var lexicalEditor = el.__lexicalEditor;
			lexicalEditor.update(function() {
				var root = lexicalEditor._editorState._nodeMap.get('root');
				if (root) root.clear();
			});
			await new Promise(function(r) { setTimeout(r, 150); });

			var ok = document.execCommand('insertText', false, %q);
			await new Promise(function(r) { setTimeout(r, 200); });

			var text = (el.innerText || el.textContent || '').trim();
			if (ok && text.length > 0) {
				return 'ok:execCommand:' + text.substring(0, 40);
			}

			el.innerText = %q;
			el.dispatchEvent(new InputEvent('input', { bubbles: true, data: %q, inputType: 'insertText' }));
			await new Promise(function(r) { setTimeout(r, 200); });

			text = (el.innerText || el.textContent || '').trim();
			if (text.length > 0) {
				return 'ok:innerText:' + text.substring(0, 40);
			}
			return 'all methods failed';
		}
	`, selector, prompt, prompt, prompt)

	result, err := page.Timeout(15 * time.Second).Eval(js)
	if err != nil {
		return fmt.Errorf("lexical input eval failed: %w", err)
	}

	status := result.Value.String()
	if !strings.HasPrefix(status, "ok") {
		log.Printf("[rod] Lexical input failed (%s), retrying with contenteditable fallback after 1s", status)
		time.Sleep(1 * time.Second)

		retryJs := fmt.Sprintf(`
			async () => {
				var el = document.querySelector(%q);
				if (!el) return 'not found';
				el.focus();

				if (el.__lexicalEditor) {
					el.__lexicalEditor.update(function() {
						var root = el.__lexicalEditor._editorState._nodeMap.get('root');
						if (root) root.clear();
					});
					await new Promise(function(r) { setTimeout(r, 150); });
				}

				var sel = window.getSelection();
				sel.removeAllRanges();
				var range = document.createRange();
				range.selectNodeContents(el);
				sel.addRange(range);
				document.execCommand('delete');
				await new Promise(function(r) { setTimeout(r, 100); });

				document.execCommand('insertText', false, %q);
				await new Promise(function(r) { setTimeout(r, 200); });

				var text = (el.innerText || el.textContent || '').trim();
				if (text.length > 0) return 'ok:' + text.substring(0, 40);

				el.innerText = %q;
				el.dispatchEvent(new InputEvent('input', { bubbles: true, data: %q, inputType: 'insertText' }));
				await new Promise(function(r) { setTimeout(r, 200); });

				text = (el.innerText || el.textContent || '').trim();
				if (text.length > 0) return 'ok:innerText:' + text.substring(0, 40);
				return 'failed';
			}
		`, selector, prompt, prompt, prompt)

		retryResult, retryErr := page.Timeout(10 * time.Second).Eval(retryJs)
		if retryErr != nil {
			return fmt.Errorf("lexical input failed: %s, retry error: %w", status, retryErr)
		}
		retryStatus := retryResult.Value.String()
		if !strings.HasPrefix(retryStatus, "ok") {
			return fmt.Errorf("lexical input failed: %s, retry also failed: %s", status, retryStatus)
		}
		log.Printf("[rod] Lexical retry succeeded: %s", retryStatus[:min(60, len(retryStatus))])
		return nil
	}

	log.Printf("[rod] Lexical input succeeded: %s", status[:min(60, len(status))])
	return nil
}

func (re *RodEngine) typePromptSlate(page *rod.Page, selector string, prompt string) error {
	js := fmt.Sprintf(`
		() => {
			var editorEl = document.querySelector(%q);
			if (!editorEl) return 'editor element not found';
			editorEl.focus();

			var reactKey = Object.keys(editorEl).find(function(k) {
				return k.indexOf('__reactFiber') === 0;
			});
			if (!reactKey) return 'no react fiber found';

			var fiber = editorEl[reactKey];
			var current = fiber;
			var slateEditor = null;
			var onChange = null;

			for (var i = 0; i < 30 && current; i++) {
				if (current.memoizedProps && current.memoizedProps.editor &&
					typeof current.memoizedProps.editor.insertText === 'function') {
					slateEditor = current.memoizedProps.editor;
					onChange = current.memoizedProps.onChange;
					break;
				}
				current = current.return;
			}

			if (!slateEditor) return 'slate editor instance not found in fiber tree';

			slateEditor.insertText(%q);
			if (onChange) {
				onChange(slateEditor.children);
			}
			return 'ok';
		}
	`, selector, prompt)

	result, err := page.Eval(js)
	if err != nil {
		return fmt.Errorf("slate input eval failed: %w", err)
	}

	status := result.Value.String()
	if status != "ok" {
		return fmt.Errorf("slate input failed: %s", status)
	}

	log.Printf("[rod] Slate.js insertText succeeded")
	return nil
}

func (re *RodEngine) submitPrompt(page *rod.Page, submitSelector string, inputSelector string) error {
	if submitSelector != "" && submitSelector != inputSelector {
		log.Printf("[rod] looking for submit element: %s", submitSelector)

		js := fmt.Sprintf(`
		() => {
			var el = document.querySelector(%q);
			if (!el) return null;
			var btn = el.tagName === 'BUTTON' ? el : el.querySelector('button');
			if (!btn) btn = el;
			if (btn.disabled) return {disabled: true};
			var rect = btn.getBoundingClientRect();
			return {
				disabled: false,
				x: rect.x + rect.width / 2,
				y: rect.y + rect.height / 2,
				tag: btn.tagName,
				cls: (btn.getAttribute('class') || '').substring(0, 80)
			};
		}
	`, submitSelector)

		result, err := page.Timeout(10 * time.Second).Eval(js)
		if err != nil {
			log.Printf("[rod] submit JS eval failed: %v, trying fallback", err)
		} else if result.Value.Nil() {
			log.Printf("[rod] submit element not found, waiting 2s and retrying")
			time.Sleep(2 * time.Second)
			result, err = page.Timeout(10 * time.Second).Eval(js)
			if err == nil && !result.Value.Nil() && !result.Value.Get("disabled").Bool() {
				x := result.Value.Get("x").Num()
				y := result.Value.Get("y").Num()
				tag := result.Value.Get("tag").Str()
				cls := result.Value.Get("cls").Str()
				log.Printf("[rod] found submit button on retry: tag=%s class=%s pos=(%.0f,%.0f)", tag, cls, x, y)
				if x > 0 && y > 0 {
					page.Mouse.MoveTo(proto.NewPoint(x, y))
					page.Mouse.Click(proto.InputMouseButtonLeft, 1)
					log.Printf("[rod] CDP mouse click succeeded at (%.0f, %.0f)", x, y)
					return nil
				}
			}
			log.Printf("[rod] submit element not found after retry, trying fallback")
		} else {
			disabled := result.Value.Get("disabled").Bool()
			if disabled {
				log.Printf("[rod] submit button is disabled, trying fallback")
			} else {
				x := result.Value.Get("x").Num()
				y := result.Value.Get("y").Num()
				tag := result.Value.Get("tag").Str()
				cls := result.Value.Get("cls").Str()
				log.Printf("[rod] found submit button: tag=%s class=%s pos=(%.0f,%.0f)", tag, cls, x, y)

				if x > 0 && y > 0 {
					if err := page.Mouse.MoveTo(proto.NewPoint(x, y)); err != nil {
						log.Printf("[rod] mouse move failed: %v, trying JS click", err)
					} else if err := page.Mouse.Click(proto.InputMouseButtonLeft, 1); err != nil {
						log.Printf("[rod] mouse click failed: %v, trying JS click", err)
					} else {
						log.Printf("[rod] CDP mouse click succeeded at (%.0f, %.0f)", x, y)
						return nil
					}
				}
			}
		}

		jsClick := fmt.Sprintf(`
		() => {
			var el = document.querySelector(%q);
			if (!el) return 'not found';
			var btn = el.tagName === 'BUTTON' ? el : el.querySelector('button');
			if (!btn) btn = el;
			if (btn.disabled) return 'disabled';
			btn.click();
			return 'ok';
		}
	`, submitSelector)

		result2, err2 := page.Timeout(5 * time.Second).Eval(jsClick)
		if err2 == nil && result2.Value.Str() == "ok" {
			log.Printf("[rod] JS click succeeded (fallback)")
			return nil
		}
		if err2 != nil {
			log.Printf("[rod] JS click failed: %v", err2)
		}
	}

	log.Printf("[rod] trying generic submit button search")
	time.Sleep(2 * time.Second)
	genericJs := fmt.Sprintf(`
		() => {
			var inputEl = document.querySelector(%q);
			var input = inputEl || document.querySelector('textarea, input, [contenteditable=true], .ProseMirror, [data-testid*=textarea]');
			if (input) input.focus();

			function isInputRelated(el) {
				if (!inputEl) return false;
				if (el === inputEl) return true;
				if (inputEl.contains(el)) return true;
				if (el.contains(inputEl)) return true;
				return false;
			}

			var selectors = [
				'button[type=submit]',
				'button[class*=send]', 'button[class*=submit]',
				'button[class*=primary]',
				'div[class*=send] button', 'div[class*=submit] button',
				'div[class*=send-btn] button', 'div[class*=submit-btn] button',
				'div[role=button][class*=send]', 'div[role=button][class*=submit]',
				'[aria-label*=send]', '[aria-label*=发送]', '[aria-label*=提交]',
				'[data-testid*=send]', '[data-testid*=submit]',
				'svg[class*=send]', 'span[class*=send]',
				'div[class*=send-button]', 'div[class*=send-button-container]',
				'button[class*=icon]:not([class*=upload]):not([class*=file]):not([class*=setting]):not([class*=menu]):not([class*=toggle])',
				'div[class*=enter] svg', 'div[class*=arrow] svg'
			];
			for (var i = 0; i < selectors.length; i++) {
				var el = document.querySelector(selectors[i]);
				if (!el) continue;
				if (isInputRelated(el)) continue;
				var btn = el.tagName === 'BUTTON' ? el : (el.querySelector('button') || el);
				if (btn.disabled) continue;
				var rect = btn.getBoundingClientRect();
				if (rect.width === 0 || rect.height === 0) continue;
				btn.click();
				return {selector: selectors[i], tag: btn.tagName, cls: (btn.getAttribute('class') || '').substring(0, 60), candidates: []};
			}

			if (input) {
				var parent = input.parentElement;
				for (var depth = 0; depth < 8 && parent; depth++) {
					var btns = parent.querySelectorAll('button, [role=button], div[class*=icon], div[class*=btn], div[class*=send], div[class*=submit], span[class*=icon], div[class*=enter], div[class*=arrow]');
					var candidates = [];
					for (var j = 0; j < btns.length; j++) {
						if (btns[j] === input) continue;
						if (isInputRelated(btns[j])) continue;
						if (btns[j].disabled) continue;
					var rect = btns[j].getBoundingClientRect();
					if (rect.width === 0 || rect.height === 0) continue;
					var cls = (btns[j].getAttribute('class') || '').toLowerCase();
					if (cls.indexOf('toggle') >= 0) continue;
				if (cls.indexOf('setting') >= 0) continue;
				if (cls.indexOf('menu') >= 0) continue;
				if (cls.indexOf('upload') >= 0) continue;
				if (cls.indexOf('file') >= 0) continue;
				if (cls.indexOf('voice') >= 0) continue;
				if (cls.indexOf('mic') >= 0) continue;
				if (cls.indexOf('image') >= 0) continue;
				if (cls.indexOf('picture') >= 0) continue;
				if (cls.indexOf('zoom') >= 0) continue;
				if (cls.indexOf('preview') >= 0) continue;
				if (cls.indexOf('lightbox') >= 0) continue;
				if (cls.indexOf('attach') >= 0) continue;
				if (cls.indexOf('sidebar') >= 0) continue;
				if (cls.indexOf('nav') >= 0 && cls.indexOf('send') < 0) continue;
				if (btns[j].closest('table, figure, [class*="image"], [class*="picture"], [class*="zoom"], [class*="preview"], [class*="lightbox"]')) continue;
					var hasSvg = btns[j].querySelector('svg') !== null;
					candidates.push({el: btns[j], cls: cls.substring(0, 50), x: rect.x, hasSvg: hasSvg});
					}
					if (candidates.length > 0) {
						var logged = candidates.map(function(c) {
							return {cls: c.cls, x: Math.round(c.x), svg: c.hasSvg};
						});
						candidates.sort(function(a, b) { return b.x - a.x; });
						candidates[0].el.click();
						return {selector: 'near-input', tag: candidates[0].el.tagName, cls: candidates[0].cls.substring(0, 60), candidates: logged};
					}
					parent = parent.parentElement;
				}
			}
			return null;
	}
	`, inputSelector)
	result3, err3 := page.Timeout(5 * time.Second).Eval(genericJs)
	if err3 == nil && !result3.Value.Nil() {
		selName := result3.Value.Get("selector").Str()
		tagName := result3.Value.Get("tag").Str()
		clsName := result3.Value.Get("cls").Str()
		log.Printf("[rod] generic submit found: selector=%s tag=%s cls=%s",
			selName, tagName, clsName)
		candidates := result3.Value.Get("candidates").Arr()
		for i, c := range candidates {
			if i >= 5 {
				break
			}
			log.Printf("[rod]   submit candidate[%d]: cls=%s x=%d svg=%v",
				i, c.Get("cls").Str(), c.Get("x").Int(), c.Get("svg").Bool())
		}
		return nil
	}

	log.Printf("[rod] generic submit search failed, trying Enter key")

	focusJs := fmt.Sprintf(`() => { var el = document.querySelector(%q); if (!el) el = document.querySelector('textarea, input, [contenteditable=true]'); if (el) { el.focus(); return true; } return false; }`, inputSelector)
	page.Eval(focusJs)
	time.Sleep(300 * time.Millisecond)

	enterJs := `() => {
		var el = document.querySelector('textarea, input, [contenteditable=true]');
		if (!el) return false;
		el.focus();
		var ev = new KeyboardEvent('keydown', {bubbles: true, cancelable: true, key: 'Enter', code: 'Enter', keyCode: 13, which: 13});
		el.dispatchEvent(ev);
		var ev2 = new KeyboardEvent('keypress', {bubbles: true, cancelable: true, key: 'Enter', code: 'Enter', keyCode: 13, which: 13});
		el.dispatchEvent(ev2);
		var ev3 = new KeyboardEvent('keyup', {bubbles: true, cancelable: true, key: 'Enter', code: 'Enter', keyCode: 13, which: 13});
		el.dispatchEvent(ev3);
		return true;
	}`
	page.Eval(enterJs)
	time.Sleep(200 * time.Millisecond)

	log.Printf("[rod] pressing Enter via CDP keyboard")
	if err := page.Keyboard.Press(input.Enter); err != nil {
		return fmt.Errorf("keyboard press Enter failed: %w", err)
	}
	log.Printf("[rod] keyboard Enter pressed")
	return nil
}

func (re *RodEngine) ensureChatMode(page *rod.Page) {
	result, err := page.Timeout(5 * time.Second).Eval(`() => {
		var editor = document.querySelector('[contenteditable=true], textarea');
		var editorText = editor ? (editor.getAttribute('data-placeholder') || editor.textContent || '').trim() : '';
		var isDocMode = editorText.indexOf('搭结构') >= 0 || editorText.indexOf('文档') >= 0 || editorText.indexOf('文章') >= 0;
		if (!isDocMode) return 'not doc mode, editorText="' + editorText.substring(0, 50) + '"';
		var exactTargets = ['对话', '聊天', '主对话', '对话模式', '聊天模式'];
		var containsTargets = ['改为对话', '直接回答', '切换为对话', '切换为聊天'];
		var excludeWords = ['新建', '新对话', '新聊天', '历史', '搜索', '清空', '删除', '设置'];
		var allEls = document.querySelectorAll('button, div[role=button], a, span[role=button], div[role=tab], li[role=tab], span, div');
		var best = null;
		var bestPri = 0;
		for (var i = 0; i < allEls.length; i++) {
			var el = allEls[i];
			if (!el.click) continue;
			var text = (el.innerText || el.textContent || '').trim();
			if (text.length === 0 || text.length > 20) continue;
			var aria = (el.getAttribute('aria-label') || '').trim();
			var href = (el.getAttribute('href') || '').trim();
			var rect = el.getBoundingClientRect();
			if (rect.width === 0 || rect.height === 0) continue;
			var cls = (el.getAttribute('class') || '').toLowerCase();
			if (cls.indexOf('history') >= 0) continue;
			if (cls.indexOf('suggestion') >= 0) continue;
			var skip = false;
			for (var e = 0; e < excludeWords.length; e++) {
				if (text.indexOf(excludeWords[e]) >= 0) { skip = true; break; }
			}
			if (skip) continue;
			var pri = 0;
			for (var t = 0; t < exactTargets.length; t++) {
				if (text === exactTargets[t]) { pri = 100; break; }
			}
			if (pri === 0) {
				for (var t = 0; t < containsTargets.length; t++) {
					if (text.indexOf(containsTargets[t]) >= 0) { pri = 90; break; }
				}
			}
			if (pri === 0 && (aria === '对话' || aria === '聊天' || aria === 'chat')) pri = 80;
			if (pri === 0 && (href.indexOf('/chat') >= 0 || href.indexOf('chat') >= 0) && text.indexOf('对话') >= 0) pri = 75;
			if (pri > 0 && cls.indexOf('active') >= 0) {
				return 'already active: ' + text + ' (cls=' + cls + ')';
			}
			if (pri > bestPri) { bestPri = pri; best = el; }
		}
		if (best && bestPri >= 75) {
			var btext = (best.innerText || best.textContent || '').trim();
			best.click();
			return 'clicked: ' + btext + ' (pri=' + bestPri + ')';
		}
		return 'doc-mode but no mode toggle found';
	}`)
	if err != nil {
		return
	}
	mode := result.Value.Str()
	if strings.HasPrefix(mode, "clicked") || strings.HasPrefix(mode, "already active") {
		log.Printf("[rod] ensureChatMode: %s", mode)
		time.Sleep(1 * time.Second)
		re.closeOverlays(page)
	} else {
		log.Printf("[rod] ensureChatMode: %s", mode)
	}
}

func (re *RodEngine) closeOverlays(page *rod.Page) {
	page.Eval(`() => {
		function closeOverlays() {
			var closeSelectors = [
			'[class*="dialog-close"]', '[class*="modal-close"]', '[class*="lightbox-close"]',
			'[class*="preview-close"]', '[class*="zoom-close"]', '[class*="image-close"]',
			'[class*="enlarge-close"]', '[class*="fullscreen-close"]', '[class*="scale-close"]',
			'svg[class*="close"]', 'button[class*="icon"][class*="close"]',
			'[class*="close-btn"]', '[class*="closeBtn"]', '[class*="close_btn"]',
			'[class*="mw-close"]', '[class*="mw-cancel"]', '[class*="mw-dismiss"]',
			'[class*="meeting-close"]', '[class*="recorder-close"]',
			'button[class*="mw"] [class*="close"]', '[class*="confirm-modal"] [class*="close"]',
			'[class*="confirm-modal"] [class*="cancel"]', '[class*="confirm-modal"] button'
		];
			for (var s = 0; s < closeSelectors.length; s++) {
				var els = document.querySelectorAll(closeSelectors[s]);
				for (var i = 0; i < els.length; i++) {
					var rect = els[i].getBoundingClientRect();
					if (rect.width < 5 || rect.height < 5) continue;
					var style = window.getComputedStyle(els[i]);
					if (style.display === 'none' || style.visibility === 'hidden' || style.opacity === '0') continue;
					try { els[i].click(); } catch(e) { els[i].dispatchEvent(new MouseEvent('click', {bubbles: true})); }
				}
			}

			var overlaySelectors = [
				'[class*="modal"]', '[class*="dialog"]', '[class*="lightbox"]',
				'[class*="preview"]', '[class*="image-viewer"]', '[class*="zoom-overlay"]',
				'[class*="enlarge"]', '[class*="fullscreen"]', '[class*="scale-up"]',
				'[class*="expand-view"]', '[class*="table-preview"]', '[class*="code-preview"]',
				'[class*="image-zoom"]', '[class*="preview-wrap"]', '[class*="viewer"]',
				'[role="dialog"]', '[class*="popup"]', '[class*="popover"]'
			];
			for (var oi = 0; oi < overlaySelectors.length; oi++) {
				var overlays = document.querySelectorAll(overlaySelectors[oi]);
				for (var j = 0; j < overlays.length; j++) {
					var style = window.getComputedStyle(overlays[j]);
					if (style.display === 'none' || style.visibility === 'hidden' || style.opacity === '0') continue;
					var z = style.zIndex;
					if (z && parseInt(z) > 100) {
						var closeBtn = overlays[j].querySelector('[class*="close"], [class*="cancel"], [class*="dismiss"], [class*="back"], [aria-label*="close"], button');
						if (closeBtn) {
							try { closeBtn.click(); } catch(e) { closeBtn.dispatchEvent(new MouseEvent('click', {bubbles: true})); }
						}
					}
				}
			}

			var backdrops = document.querySelectorAll('[class*="backdrop"], [class*="mask"], [class*="overlay"], [class*="dimmer"], [class*="scrim"]');
			for (var k = 0; k < backdrops.length; k++) {
			var style = window.getComputedStyle(backdrops[k]);
			if (style.display === 'none' || style.visibility === 'hidden' || style.opacity === '0') continue;
			var z = style.zIndex;
			if (z && parseInt(z) > 50) {
				try { backdrops[k].click(); } catch(e) {}
			}
		}

		// NOTE: Escape key dispatch removed — pressing Escape during streaming
		// cancels generation on DeepSeek and other chat sites.

		var fixedOverlays = document.querySelectorAll('[style*="position: fixed"], [style*="position:fixed"]');
			for (var fi = 0; fi < fixedOverlays.length; fi++) {
				var style = window.getComputedStyle(fixedOverlays[fi]);
				var z = parseInt(style.zIndex) || 0;
				if (z > 1000 && style.display !== 'none') {
					var closeBtn = fixedOverlays[fi].querySelector('[class*="close"], [class*="cancel"], button, [aria-label*="close"]');
					if (closeBtn) {
						try { closeBtn.click(); } catch(e2) {}
					}
				}
			}

			var confirmModals = document.querySelectorAll('[class*="confirm-modal"]');
			for (var cm = 0; cm < confirmModals.length; cm++) {
				var cmStyle = window.getComputedStyle(confirmModals[cm]);
				if (cmStyle.display === 'none' || cmStyle.visibility === 'hidden') continue;
				var btns = confirmModals[cm].querySelectorAll('button, [role=button], div[class*="btn"], span[class*="btn"]');
				for (var bi = 0; bi < btns.length; bi++) {
					var btxt = (btns[bi].innerText || btns[bi].textContent || '').trim();
					if (btxt === '我知道了' || btxt === '确定' || btxt === 'OK' || btxt === '知道了' || btxt === '关闭') {
						try { btns[bi].click(); } catch(e3) { btns[bi].dispatchEvent(new MouseEvent('click', {bubbles: true})); }
						break;
					}
				}
			}

			var recorderPanels = document.querySelectorAll('[class*="expanded-recorder-container"], [class*="mw-container"], [class*="mw-main"]');
			for (var rp = 0; rp < recorderPanels.length; rp++) {
				var rpStyle = window.getComputedStyle(recorderPanels[rp]);
				if (rpStyle.display === 'none' || rpStyle.visibility === 'hidden') continue;
				var rpBtns = recorderPanels[rp].querySelectorAll('button, [role=button], div[class*="btn"], span[class*="btn"], [class*="close"], [class*="end"], [class*="stop"]');
				for (var rb = 0; rb < rpBtns.length; rb++) {
					var rbtxt = (rpBtns[rb].innerText || rpBtns[rb].textContent || '').trim();
					var rbcls = (rpBtns[rb].getAttribute('class') || '').toLowerCase();
					if (rbtxt === '结束' || rbtxt === '关闭' || rbtxt === '关闭录音' || rbtxt === '停止' || rbtxt === 'End' || rbtxt === 'Close' ||
						rbcls.indexOf('close') >= 0 || rbcls.indexOf('end') >= 0 || rbcls.indexOf('stop') >= 0) {
						try { rpBtns[rb].click(); } catch(e4) { rpBtns[rb].dispatchEvent(new MouseEvent('click', {bubbles: true})); }
						break;
					}
				}
			}

			return 'ok';
		}
		return closeOverlays();
	}`)
}

func (re *RodEngine) getAnswerStatus(page *rod.Page, selector string, beforeCount int, prompt string) (int, string) {
	if selector == "" {
		return 0, ""
	}

	snippetLen := 100
	promptFirst := ""
	promptLast := ""
	if len(prompt) > 0 {
		if len(prompt) > snippetLen {
			promptFirst = prompt[:snippetLen]
			promptLast = prompt[len(prompt)-snippetLen:]
		} else {
			promptFirst = prompt
		}
	}

	js := fmt.Sprintf(`
		() => {
			var els = document.querySelectorAll(%q);
			if (els.length === 0) return {count: 0, text: ''};
			function isInThinking(el) {
				var parent = el.parentElement;
				for (var i = 0; i < 6 && parent; i++) {
					var cls = (parent.getAttribute('class') || '').toLowerCase();
					// Only match explicit thinking/reasoning container classes.
					// Avoid broad words like 'mind'/'analysis'/'reflect'/'contemplate'
					// that match legitimate answer containers (e.g. 'mind-map',
					// 'analysis-result') and cause the answer to be filtered out.
					if (cls.indexOf('think-block') >= 0 || cls.indexOf('think-content') >= 0 ||
						cls.indexOf('think_process') >= 0 || cls.indexOf('thinking-block') >= 0 ||
						cls.indexOf('thinking-content') >= 0 || cls.indexOf('thinking-process') >= 0 ||
						cls.indexOf('reasoning-block') >= 0 || cls.indexOf('reasoning-content') >= 0 ||
						cls.indexOf('reasoning-text') >= 0 || cls.indexOf('thought-block') >= 0 ||
						cls.indexOf('thought-content') >= 0 || cls.indexOf('thought-process') >= 0 ||
						cls.indexOf('chain-of-thought') >= 0 || cls.indexOf('cot-block') >= 0 ||
						cls.indexOf('inner-mono') >= 0 || cls.indexOf('deep-think') >= 0 ||
						cls.indexOf('pre-think') >= 0) {
						return true;
					}
					var tag = parent.tagName;
					if (tag === 'DETAILS') {
						var sum = parent.querySelector('summary');
						if (sum) {
							var st = (sum.textContent || '').toLowerCase();
							if (st.indexOf('思考') >= 0 || st.indexOf('think') >= 0) {
								return true;
							}
						}
					}
					parent = parent.parentElement;
				}
				return false;
			}
			var promptFirst = %q;
			var promptLast = %q;
			function isPromptText(text) {
				if (!text) return false;
				if (promptFirst && text.indexOf(promptFirst) >= 0 && text.length < promptFirst.length + 300) return true;
				if (promptLast && promptLast !== promptFirst && text.indexOf(promptLast) >= 0 && text.length < promptLast.length + 300) return true;
				return false;
			}
			var startIdx = Math.min(%d, els.length);
			var maxText = '';
			for (var i = startIdx; i < els.length; i++) {
				if (isInThinking(els[i])) continue;
				var t = (els[i].innerText || els[i].textContent || '').trim();
				if (isPromptText(t)) continue;
				if (t.length > maxText.length) maxText = t;
			}
			if (!maxText) {
				for (var i = startIdx; i < els.length; i++) {
					var t = (els[i].innerText || els[i].textContent || '').trim();
					if (isPromptText(t)) continue;
					if (t.length > maxText.length) maxText = t;
				}
			}
			if (!maxText && startIdx > 0) {
				if (startIdx >= els.length) {
					var lastEl = els[els.length - 1];
					if (lastEl && !isInThinking(lastEl) && !isPromptText((lastEl.innerText || lastEl.textContent || '').trim())) {
						maxText = (lastEl.innerText || lastEl.textContent || '').trim();
					}
					if (!maxText && lastEl) {
						maxText = (lastEl.innerText || lastEl.textContent || '').trim();
					}
					if (!maxText) {
						var prevEl = els.length >= 2 ? els[els.length - 2] : null;
						if (prevEl && !isInThinking(prevEl) && !isPromptText((prevEl.innerText || prevEl.textContent || '').trim())) {
							maxText = (prevEl.innerText || prevEl.textContent || '').trim();
						}
					}
				} else {
					var lastEl = els[els.length - 1];
					if (lastEl && !isInThinking(lastEl) && !isPromptText((lastEl.innerText || lastEl.textContent || '').trim())) {
						maxText = (lastEl.innerText || lastEl.textContent || '').trim();
					}
					if (!maxText && lastEl) {
						maxText = (lastEl.innerText || lastEl.textContent || '').trim();
					}
				}
			}
			return {count: els.length, text: maxText};
		}
	`, selector, promptFirst, promptLast, beforeCount)
	result, err := page.Timeout(5 * time.Second).Eval(js)
	if err != nil {
		return 0, ""
	}
	count := result.Value.Get("count").Int()
	text := result.Value.Get("text").Str()
	return count, text
}

func (re *RodEngine) SendMessage(ctx context.Context, site models.Site, prompt string) (string, error) {
	log.Printf("[rod] SendMessage start: site=%s prompt=%q", site.ID, prompt[:min(50, len(prompt))])

	if err := re.ensureBrowser(); err != nil {
		return "", fmt.Errorf("browser health check: %w", err)
	}

	var sels Selectors
	if err := json.Unmarshal([]byte(site.Selectors), &sels); err != nil {
		return "", fmt.Errorf("parse selectors: %w", err)
	}
	log.Printf("[rod] selectors: input=%s submit=%s answer=%s wait_for=%s",
		sels.Input, sels.Submit, sels.Answer, sels.WaitFor)

	if sels.Input == "" {
		return "", errors.New("missing required selector: input is required")
	}

	page, err := re.getOrCreatePage(site)
	if err != nil {
		log.Printf("[rod] getOrCreatePage failed, retrying after browser check: %v", err)
		if retryErr := re.ensureBrowser(); retryErr != nil {
			return "", fmt.Errorf("browser health check on retry: %w", retryErr)
		}
		page, err = re.getOrCreatePage(site)
		if err != nil {
			return "", fmt.Errorf("get page: %w", err)
		}
	}
	ReportProgress(ctx, ProgressInput)

	if sels.Answer == "" {
		fallbackJs := `() => {
			var selectors = [
				'[class*=markdown]', '[class*=message-content]', '[class*=answer]',
				'[class*=response]', '[class*=assistant]', '[class*=reply]',
				'[class*=bubble]', '[class*=content-card]', '[class*=flow-markdown]',
				'[class*=chat-message]', '[class*=ai-message]', '[class*=bot-message]',
				'.ds-assistant-message-main-content', '[class*=answer-common-card]',
				'article', '[class*=result-content]',
				'[class*=prose]', '[class*=rich-text]', '[class*=text-content]',
				'[class*=output-content]', '[class*=chat-content]',
				'[class*=conversation-content]', '[class*=message-text]'
			];
			for (var s = 0; s < selectors.length; s++) {
				var els = document.querySelectorAll(selectors[s]);
				if (els.length > 0) {
					for (var k = 0; k < els.length; k++) {
						if (els[k].getBoundingClientRect().width > 50) return selectors[s];
					}
				}
			}
			return '';
		}`
		if fbResult, fbErr := page.Timeout(5 * time.Second).Eval(fallbackJs); fbErr == nil {
			resolved := fbResult.Value.Str()
			if resolved != "" {
				sels.Answer = resolved
				log.Printf("[rod] answer selector resolved from fallback: %s", resolved)
			} else {
				log.Printf("[rod] answer selector fallback found no matching elements")
			}
		}
	}

	if sels.WaitFor != "" {
		log.Printf("[rod] waiting for element: %s", sels.WaitFor)
		if _, err := page.Timeout(30 * time.Second).Element(sels.WaitFor); err != nil {
			log.Printf("[rod] wait_for timeout (non-fatal): %v", err)
		}
	}
	time.Sleep(1 * time.Second)

	re.closeOverlays(page)

	re.ensureChatMode(page)

	preDiagJs := fmt.Sprintf(`() => {
		var el = document.querySelector(%q);
		var url = window.location.href;
		var allCE = document.querySelectorAll('[contenteditable=true]').length;
		var allTextarea = document.querySelectorAll('textarea').length;
		return JSON.stringify({found: !!el, url: url, ceCount: allCE, textareaCount: allTextarea});
	}`, sels.Input)
	if preDiagResult, preDiagErr := page.Timeout(5 * time.Second).Eval(preDiagJs); preDiagErr == nil {
		diagStr := preDiagResult.Value.Str()
		log.Printf("[rod] pre-typePrompt diag: %s", diagStr)
		// Use navigatedAwayFromChat for ALL sites (not just /chat URLs) so that
		// sites like Qwen (www.qianwen.com/) are also guarded against /record,
		// /write, etc. navigation.
		if strings.Contains(diagStr, `"url":"`) {
			urlStart := strings.Index(diagStr, `"url":"`) + 7
			urlEnd := strings.Index(diagStr[urlStart:], `"`) + urlStart
			currentURL := diagStr[urlStart:urlEnd]
			if re.navigatedAwayFromChat(currentURL, site.URL) {
				log.Printf("[rod] page navigated away from chat (now %s), navigating back to %s", currentURL, site.URL)
				if navErr := page.Timeout(10 * time.Second).Navigate(site.URL); navErr == nil {
					_ = page.WaitLoad()
					_ = page.WaitIdle(3 * time.Second)
					re.refreshPageAfterNavigation(page)
					re.closeOverlays(page)
					time.Sleep(1 * time.Second)
				}
			}
		}
	} else {
		log.Printf("[rod] pre-typePrompt diag failed: %v", preDiagErr)
	}

	beforeCount, beforeText := re.getAnswerStatus(page, sels.Answer, 0, prompt)
	log.Printf("[rod] answer count before sending: %d textLen=%d", beforeCount, len(beforeText))

	promptFirst := ""
	promptLast := ""
	if len(prompt) > 100 {
		promptFirst = prompt[:100]
		promptLast = prompt[len(prompt)-100:]
	} else {
		promptFirst = prompt
	}

	ReportProgress(ctx, ProgressSending)
	if err := re.typePrompt(page, sels.Input, prompt); err != nil {
		return "", fmt.Errorf("type prompt: %w", err)
	}
	time.Sleep(500 * time.Millisecond)

	preCheckJs := fmt.Sprintf(`() => { var el = document.querySelector(%q); if (!el) return 'not found'; if (el.tagName === 'TEXTAREA' || el.tagName === 'INPUT') return (el.value || '').substring(0, 80); return (el.textContent || el.innerText || '').substring(0, 80); }`, sels.Input)
	preText := ""
	if preDiag, err := page.Eval(preCheckJs); err == nil {
		preText = preDiag.Value.Str()
		log.Printf("[rod] pre-submit editor text: %q", preText)
	}

	if preText == "" || preText == "not found" {
		log.Printf("[rod] editor text is empty after typePrompt, retrying with nativeSetter")
		retryJs := fmt.Sprintf(`
			() => {
				var el = document.querySelector(%q);
				if (!el) return 'not found';
				if (el.tagName === 'TEXTAREA' || el.tagName === 'INPUT') {
					if (el._valueTracker) el._valueTracker.setValue('');
					var nativeSetter = Object.getOwnPropertyDescriptor(
						window.HTMLTextAreaElement.prototype, 'value'
					) || Object.getOwnPropertyDescriptor(
						window.HTMLInputElement.prototype, 'value'
					);
					if (nativeSetter && nativeSetter.set) {
						nativeSetter.set.call(el, %q);
					} else {
						el.value = %q;
					}
					el.dispatchEvent(new Event('input', { bubbles: true }));
					el.dispatchEvent(new Event('change', { bubbles: true }));
					return el.value.substring(0, 80);
				}
				return 'not textarea';
			}
		`, sels.Input, prompt, prompt)
		if retryResult, err := page.Eval(retryJs); err == nil {
			log.Printf("[rod] nativeSetter retry result: %q", retryResult.Value.Str())
		}
	}

	if sels.Submit != "" {
		if err := re.submitPrompt(page, sels.Submit, sels.Input); err != nil {
			log.Printf("[rod] submit failed, trying Enter on input: %v", err)
			_ = re.submitPrompt(page, "", sels.Input)
		}
	} else {
		_ = re.submitPrompt(page, "", sels.Input)
	}
	ReportProgress(ctx, ProgressGenerating)
	time.Sleep(1 * time.Second)

	diagJs := fmt.Sprintf(`
		() => {
			var el = document.querySelector(%q);
			var text = 'not found';
			if (el) {
				if (el.tagName === 'TEXTAREA' || el.tagName === 'INPUT') {
					text = (el.value || '').substring(0, 80);
				} else {
					text = (el.textContent || el.innerText || '').substring(0, 80);
				}
			}
			return {url: location.href, editorText: text, answerCount: document.querySelectorAll(%q).length};
		}
	`, sels.Input, sels.Answer)
	var postEditorText string
	var postAnswerCount int
	if diagResult, err := page.Eval(diagJs); err == nil {
		postEditorText = diagResult.Value.Get("editorText").Str()
		postAnswerCount = diagResult.Value.Get("answerCount").Int()
		log.Printf("[rod] post-submit diag: url=%s editorText=%q answerCount=%d",
			diagResult.Value.Get("url").Str(), postEditorText, postAnswerCount)
	}

	if postAnswerCount <= beforeCount {
		log.Printf("[rod] post-submit answerCount=%d <= beforeCount=%d, waiting 3s for elements to load and re-checking", postAnswerCount, beforeCount)
		time.Sleep(3 * time.Second)
		if diagResult2, err := page.Eval(diagJs); err == nil {
			postAnswerCount = diagResult2.Value.Get("answerCount").Int()
			postEditorText = diagResult2.Value.Get("editorText").Str()
			log.Printf("[rod] post-submit re-check: editorText=%q answerCount=%d", postEditorText, postAnswerCount)
		}
	}

	if postAnswerCount > beforeCount {
		// New elements appeared right after submit. These are usually user-message
		// echoes that match the answer selector. Only skip them when they actually
		// contain the prompt text; otherwise they may be the AI answer already
		// rendering (e.g. fast sites), and skipping would lose the response.
		checkPromptJs := fmt.Sprintf(`
			() => {
				var els = document.querySelectorAll(%q);
				var promptFirst = %q;
				var promptLast = %q;
				var start = Math.min(%d, els.length);
				var end = Math.min(%d, els.length);
				var userMsgCount = 0;
				for (var i = start; i < end; i++) {
					var t = (els[i].innerText || els[i].textContent || '').trim();
					if ((promptFirst && t.indexOf(promptFirst) >= 0 && t.length < promptFirst.length + 300) ||
						(promptLast && promptLast !== promptFirst && t.indexOf(promptLast) >= 0 && t.length < promptLast.length + 300)) {
						userMsgCount++;
					}
				}
				return {newCount: end - start, userMsgCount: userMsgCount};
			}
		`, sels.Answer, promptFirst, promptLast, beforeCount, postAnswerCount)
		newIsUser := false
		if checkRes, checkErr := page.Timeout(3 * time.Second).Eval(checkPromptJs); checkErr == nil {
			newCount := checkRes.Value.Get("newCount").Int()
			userMsgCount := checkRes.Value.Get("userMsgCount").Int()
			if newCount > 0 && userMsgCount == newCount {
				newIsUser = true
			}
			log.Printf("[rod] post-submit new elements: %d, user-msg-like: %d", newCount, userMsgCount)
		}
		if newIsUser {
			log.Printf("[rod] answer count increased after submit (%d -> %d) and new elements are user messages, updating beforeCount", beforeCount, postAnswerCount)
			beforeCount = postAnswerCount
			_, beforeText = re.getAnswerStatus(page, sels.Answer, beforeCount, prompt)
		} else {
			log.Printf("[rod] answer count increased (%d -> %d) but new elements do not look like user messages, keeping beforeCount to avoid skipping the AI answer", beforeCount, postAnswerCount)
		}
	}

	if postAnswerCount == beforeCount && strings.Contains(postEditorText, prompt[:min(5, len(prompt))]) {
		log.Printf("[rod] editor still has prompt text after click, trying JS click on submit")

		var jsSubmitClick string
		if sels.Submit != "" {
			jsSubmitClick = fmt.Sprintf(`
				() => {
					var btn = document.querySelector(%q);
					if (!btn) return 'not found';
					if (btn.tagName !== 'BUTTON') {
						var inner = btn.querySelector('button');
						if (inner) btn = inner;
					}
					btn.click();
					return 'clicked: ' + btn.tagName + ' ' + (btn.getAttribute('aria-label') || btn.getAttribute('class') || '').substring(0, 50);
				}
			`, sels.Submit)
		} else {
			jsSubmitClick = fmt.Sprintf(`
				() => {
					var inputEl = document.querySelector(%q);
					if (!inputEl) return 'input not found';
					inputEl.focus();
					function isInputRelated(el) {
						if (el === inputEl) return true;
						if (inputEl.contains(el)) return true;
						if (el.contains(inputEl)) return true;
						return false;
					}
					var input = inputEl;
					var parent = input.parentElement;
					for (var depth = 0; depth < 8 && parent; depth++) {
						var btns = parent.querySelectorAll('button, [role=button], div[class*=icon], div[class*=btn], div[class*=send], div[class*=submit], div[class*=enter], div[class*=arrow]');
						var candidates = [];
						for (var j = 0; j < btns.length; j++) {
							if (btns[j] === input) continue;
							if (isInputRelated(btns[j])) continue;
							if (btns[j].disabled) continue;
							var rect = btns[j].getBoundingClientRect();
							if (rect.width === 0 || rect.height === 0) continue;
							var cls = (btns[j].getAttribute('class') || '').toLowerCase();
							if (cls.indexOf('toggle') >= 0 || cls.indexOf('setting') >= 0 ||
								cls.indexOf('menu') >= 0 || cls.indexOf('upload') >= 0 ||
								cls.indexOf('file') >= 0 || cls.indexOf('voice') >= 0 ||
								cls.indexOf('mic') >= 0 || cls.indexOf('image') >= 0 ||
								cls.indexOf('attach') >= 0 || cls.indexOf('sidebar') >= 0) continue;
							candidates.push({el: btns[j], cls: cls.substring(0, 50), x: rect.x, y: rect.y});
						}
						if (candidates.length > 0) {
							var logged = candidates.map(function(c) { return c.cls + '@(' + Math.round(c.x) + ',' + Math.round(c.y) + ')'; });
							candidates.sort(function(a, b) { return b.x - a.x; });
							candidates[0].el.scrollIntoView({block: 'center'});
							candidates[0].el.click();
							return 'clicked: ' + candidates[0].el.tagName + ' ' + candidates[0].cls + ' | all: ' + logged.join(', ');
						}
						parent = parent.parentElement;
					}
					return 'no button found near input';
				}
			`, sels.Input)
		}
		if jsRes, jsErr := page.Eval(jsSubmitClick); jsErr == nil {
			log.Printf("[rod] JS submit click: %s", jsRes.Value.Str())
		}
		time.Sleep(2 * time.Second)

		var afterJsClickText string
		var afterJsClickCount int
		if diag2, err := page.Eval(diagJs); err == nil {
			afterJsClickText = diag2.Value.Get("editorText").Str()
			afterJsClickCount = diag2.Value.Get("answerCount").Int()
		}
		if afterJsClickCount == beforeCount && strings.Contains(afterJsClickText, prompt[:min(5, len(prompt))]) {
			log.Printf("[rod] JS click also failed, trying Enter key")
			focusJs := fmt.Sprintf(`() => { var el = document.querySelector(%q); if (el) el.focus(); return el != null; }`, sels.Input)
			page.Eval(focusJs)
			time.Sleep(200 * time.Millisecond)
			if err := page.Keyboard.Press(input.Enter); err != nil {
				log.Printf("[rod] Enter key failed: %v", err)
			} else {
				log.Printf("[rod] Enter key pressed as fallback")
				time.Sleep(2 * time.Second)
			}

			var afterEnterText string
			var afterEnterCount int
			if diag3, err := page.Eval(diagJs); err == nil {
				afterEnterText = diag3.Value.Get("editorText").Str()
				afterEnterCount = diag3.Value.Get("answerCount").Int()
			}
			if afterEnterCount == beforeCount && strings.Contains(afterEnterText, prompt[:min(5, len(prompt))]) {
				log.Printf("[rod] Enter key did not submit, trying Ctrl+Enter")
				page.Eval(focusJs)
				time.Sleep(200 * time.Millisecond)
				if err := page.Keyboard.Press(input.ControlLeft); err == nil {
					page.Keyboard.Press(input.Enter)
					page.Keyboard.Press(input.ControlLeft)
					log.Printf("[rod] Ctrl+Enter pressed as fallback")
					time.Sleep(2 * time.Second)
				}

				var afterCtrlText string
				var afterCtrlCount int
				if diag4, err := page.Eval(diagJs); err == nil {
					afterCtrlText = diag4.Value.Get("editorText").Str()
					afterCtrlCount = diag4.Value.Get("answerCount").Int()
				}
				if afterCtrlCount == beforeCount && strings.Contains(afterCtrlText, prompt[:min(5, len(prompt))]) {
					log.Printf("[rod] Ctrl+Enter also failed, dumping all buttons for diagnostic")
					btnDiagJs := fmt.Sprintf(`
					() => {
						var inputEl = document.querySelector(%q);
						var inputRect = inputEl ? inputEl.getBoundingClientRect() : null;
						var btns = document.querySelectorAll('button, [role=button], div[class*=icon], div[class*=btn], div[class*=send], div[class*=submit], div[class*=enter], div[class*=arrow], svg[class*=send], svg[class*=submit]');
						var result = [];
						for (var i = 0; i < btns.length && result.length < 15; i++) {
							var rect = btns[i].getBoundingClientRect();
							if (rect.width === 0 || rect.height === 0) continue;
							result.push({
								tag: btns[i].tagName,
								cls: (btns[i].getAttribute('class') || '').substring(0, 60),
								aria: (btns[i].getAttribute('aria-label') || '').substring(0, 30),
								type: btns[i].getAttribute('type') || '',
								disabled: btns[i].disabled || false,
								x: Math.round(rect.x),
								y: Math.round(rect.y),
								w: Math.round(rect.width),
								h: Math.round(rect.height),
								distFromInput: inputRect ? Math.round(Math.abs(rect.y - inputRect.y)) : -1
							});
						}
						result.sort(function(a, b) { return a.distFromInput - b.distFromInput; });
						return result;
					}
				`, sels.Input)
					if btnDiagRes, btnDiagErr := page.Timeout(5 * time.Second).Eval(btnDiagJs); btnDiagErr == nil {
						arr := btnDiagRes.Value.Arr()
						log.Printf("[rod] button diagnostic: %d buttons found (sorted by distance from input):", len(arr))
						for i, v := range arr {
							if i >= 10 {
								break
							}
							log.Printf("[rod]   btn[%d]: tag=%s cls=%s aria=%s disabled=%v pos=(%d,%d) size=%dx%d distFromInput=%d",
								i, v.Get("tag").Str(), v.Get("cls").Str(), v.Get("aria").Str(),
								v.Get("disabled").Bool(), v.Get("x").Int(), v.Get("y").Int(),
								v.Get("w").Int(), v.Get("h").Int(), v.Get("distFromInput").Int())
						}
					}
				}
			}
		}
	}

	var lastText string
	var finalText string
	stableRounds := 0
	const requiredStable = 8
	// Adaptive minimum polls before declaring stability. Long answers (tables,
	// code blocks) need more time to finish streaming; short answers can be
	// declared stable sooner. This reduces wait time for fast sites like
	// DeepSeek while keeping safety for slow streaming sites like Kimi.
	minPollsBeforeStable := 20
	deadline := time.Now().Add(120 * time.Second)
	pollCount := 0
	renderRetryCount := 0
	// Track the maximum element count seen during polling. Some sites (e.g.
	// DeepSeek) remove the streaming element after completion and re-render
	// a different structure, causing count to drop to 0 even though a full
	// answer was captured in lastText. We use maxCount to detect this case.
	maxCount := 0
	// lastHTML saves the outerHTML of the last answer element captured during
	// streaming. Sites like DeepSeek use virtual lists that recycle the answer
	// element after streaming completes; when that happens we convert this
	// saved HTML snapshot to Markdown instead of relying on the live DOM.
	lastHTML := ""
	lastHTMLTextLen := 0

	// Inject JS to disable virtual list recycling (e.g. DeepSeek's
	// ds-virtual-list-visible-items). Virtual lists remove off-screen elements
	// to save memory, but this causes the answer element to disappear mid-stream
	// before extraction. Setting overflow:visible and maxHeight:none on virtual
	// list containers forces them to render all items without recycling.
	// Also capture the answer element's outerHTML via a high-frequency interval
	// (200ms) as a fallback in case recycling still happens.
	stopCaptureJs := fmt.Sprintf(`() => {
		// Disable virtual list recycling by making containers render all items.
		document.querySelectorAll('[class*="virtual-list"], [class*="VirtualList"]').forEach(function(el) {
			el.style.overflow = 'visible';
			el.style.maxHeight = 'none';
			el.style.height = 'auto';
		});

		if (window.__answerCaptureInterval) clearInterval(window.__answerCaptureInterval);
		window.__capturedAnswerHTML = '';
		window.__capturedAnswerTextLen = 0;
		window.__answerCaptureInterval = setInterval(function() {
			var els = document.querySelectorAll(%q);
			if (els.length === 0) return;
			var el = els[els.length - 1];
			var text = (el.innerText || el.textContent || '').trim();
			if (text.length > window.__capturedAnswerTextLen) {
				window.__capturedAnswerHTML = el.outerHTML;
				window.__capturedAnswerTextLen = text.length;
			}
		}, 200);
		return 'ok';
	}`, sels.Answer)
	page.Timeout(3 * time.Second).Eval(stopCaptureJs)

	for time.Now().Before(deadline) {
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		default:
		}
		pollCount++

		// NOTE: Do NOT call closeOverlays periodically during polling.
		// closeOverlays dispatches Escape keys and clicks elements with "close"/"back"
		// in their class, which cancels ongoing generation on DeepSeek and resets the
		// SPA to the welcome page mid-stream. closeOverlays is only safe to call after
		// streaming completes (at the done: label before content extraction).

		// Re-inject virtual list override on every poll to catch dynamically
		// created containers (DeepSeek creates new virtual list wrappers as the
		// conversation grows).
		if pollCount%5 == 0 {
			page.Timeout(2 * time.Second).Eval(`() => { document.querySelectorAll('[class*="virtual-list"], [class*="VirtualList"]').forEach(function(el) { el.style.overflow = 'visible'; el.style.maxHeight = 'none'; el.style.height = 'auto'; }); }`)
		}

		currentCount, currentText := re.getAnswerStatus(page, sels.Answer, beforeCount, prompt)

		// Capture the answer element's outerHTML while it still exists in the
		// DOM. Sites like DeepSeek use virtual lists that recycle the element
		// after streaming completes; we'll convert this snapshot to Markdown
		// when the live element is gone at extraction time.
		if currentCount > 0 && len(currentText) > lastHTMLTextLen {
			snapshotJs := fmt.Sprintf(`() => { var els = document.querySelectorAll(%q); if (els.length === 0) return ''; var el = els[els.length - 1]; return el.outerHTML; }`, sels.Answer)
			if snapRes, snapErr := page.Timeout(3 * time.Second).Eval(snapshotJs); snapErr == nil {
				htmlStr := snapRes.Value.Str()
				if len(htmlStr) > len(currentText) {
					lastHTML = htmlStr
					lastHTMLTextLen = len(currentText)
				}
			}
		}

		// Preventive: scroll the answer element into view every 3 polls (1.5s)
		// to prevent virtual scrolling (e.g. DeepSeek's ds-virtual-list-visible-items)
		// from recycling off-screen answer elements mid-stream.
		if currentCount > 0 && pollCount%3 == 0 {
			page.Eval(fmt.Sprintf(`() => { var els = document.querySelectorAll(%q); if (els.length > 0) { els[els.length-1].scrollIntoView({block: 'center'}); } }`, sels.Answer))
		}

		if currentCount > maxCount {
			maxCount = currentCount
		}

		// If we already collected substantial answer text and the element count
		// dropped to 0 (e.g. DeepSeek's virtual list recycles off-screen elements),
		// the live element is gone for good. Use the best available HTML snapshot
		// (from either the high-frequency JS interval or our Go-poll capture) to
		// extract Markdown. If no snapshot is available, wait briefly to see if
		// the element reappears before falling back to polling text.
		if currentCount == 0 && maxCount > 0 && len(lastText) > 100 && pollCount >= 6 {
			// Diagnostic: check what's actually in the DOM when the element disappears.
			diagJs := `() => {
			var out = {};
			out.url = window.location.href;
			out.scrollTop = window.scrollY;
			out.innerHeight = window.innerHeight;
			out.bodyScrollHeight = document.body ? document.body.scrollHeight : -1;
			out.bodyChildrenCount = document.body ? document.body.children.length : -1;
			out.bodyInnerHtmlLen = document.body ? document.body.innerHTML.length : -1;
			out.docInnerHtmlLen = document.documentElement.innerHTML.length;
			out.deepseekMarkdown = document.querySelectorAll('.ds-markdown').length;
			out.assistantMessage = document.querySelectorAll('.ds-assistant-message-main-content').length;
			out.virtualListItems = document.querySelectorAll('.ds-virtual-list-visible-items').length;
			out.virtualListChildren = 0;
			var vl = document.querySelector('.ds-virtual-list-visible-items');
			if (vl) out.virtualListChildren = vl.children.length;
			out.stopButtons = document.querySelectorAll('[class*="stop"], [aria-label*="停止"], [aria-label*="stop"]').length;
			var stopBtns = document.querySelectorAll('[class*="stop"], [aria-label*="停止"], [aria-label*="stop"]');
			out.stopBtnDetails = [];
			for (var i = 0; i < stopBtns.length && i < 3; i++) {
				var r = stopBtns[i].getBoundingClientRect();
				out.stopBtnDetails.push({ tag: stopBtns[i].tagName, cls: (stopBtns[i].className || '').substring(0, 60), w: r.width, h: r.height, text: (stopBtns[i].innerText || '').substring(0, 20) });
			}
			// Check for any visible text content
			out.bodyTextLen = document.body ? (document.body.innerText || '').trim().length : -1;
			out.bodyTextPreview = document.body ? (document.body.innerText || '').trim().substring(0, 200) : '';
			return out;
		}`
			if diagRes, diagErr := page.Timeout(3 * time.Second).Eval(diagJs); diagErr == nil {
				jsonBytes, _ := diagRes.Value.MarshalJSON()
				log.Printf("[rod] recycle diag: %s", string(jsonBytes))
			}

			// Prefer the high-frequency JS-captured HTML (updates every 200ms),
			// which is more recent than our Go-poll capture (every 500ms).
			jsCaptureHtml := ""
			if jsCapRes, jsCapErr := page.Timeout(2 * time.Second).Eval(`() => ({ html: window.__capturedAnswerHTML || '', textLen: window.__capturedAnswerTextLen || 0 })`); jsCapErr == nil {
				jsCaptureHtml = jsCapRes.Value.Get("html").Str()
				jsCaptureTextLen := jsCapRes.Value.Get("textLen").Int()
				if len(jsCaptureHtml) > len(lastHTML) {
					log.Printf("[rod] using JS-captured HTML snapshot (%d chars, textLen=%d) over Go-poll snapshot (%d chars)", len(jsCaptureHtml), jsCaptureTextLen, len(lastHTML))
					lastHTML = jsCaptureHtml
				}
			}
			// (Keep the JS capture interval running during the 3s wait below —
			// it may capture newer content if the page re-renders.)

			log.Printf("[rod] answer count dropped to 0 after capturing %d chars (maxCount=%d, htmlSnapshot=%d chars)", len(lastText), maxCount, len(lastHTML))
			if !isStillGenerating(page) && !isThinkingText(lastText) {
				// The page might be re-rendering (SPA transition). Wait up to 8s
				// for the page to re-render and the answer element to reappear.
				// DeepSeek's SPA sometimes blanks the entire page body during
				// re-render; the element comes back after a few seconds.
				for waitRound := 0; waitRound < 4; waitRound++ {
					time.Sleep(2 * time.Second)
					recheckCount, recheckText := re.getAnswerStatus(page, sels.Answer, beforeCount, prompt)
					if recheckCount > 0 && len(recheckText) >= len(lastText) {
						log.Printf("[rod] answer element reappeared after %ds wait (count=%d textLen=%d >= lastText=%d), continuing poll", (waitRound+1)*2, recheckCount, len(recheckText), len(lastText))
						// Restart JS capture interval for the new element.
						page.Timeout(2 * time.Second).Eval(fmt.Sprintf(`() => { if (window.__answerCaptureInterval) clearInterval(window.__answerCaptureInterval); window.__capturedAnswerHTML = ''; window.__capturedAnswerTextLen = 0; window.__answerCaptureInterval = setInterval(function() { var els = document.querySelectorAll(%q); if (els.length === 0) return; var el = els[els.length - 1]; var text = (el.innerText || el.textContent || '').trim(); if (text.length > window.__capturedAnswerTextLen) { window.__capturedAnswerHTML = el.outerHTML; window.__capturedAnswerTextLen = text.length; } }, 200); }`, sels.Answer))
						lastText = recheckText
						stableRounds = 0
						goto continuePolling
					}
					log.Printf("[rod] answer element still not reappeared after %ds wait (recheckCount=%d recheckTextLen=%d, lastText=%d)", (waitRound+1)*2, recheckCount, len(recheckText), len(lastText))
				}
				// Also check JS-captured HTML one more time (the 200ms interval
				// might have captured newer content before the element disappeared).
				if jsCapRes2, jsCapErr2 := page.Timeout(2 * time.Second).Eval(`() => ({ html: window.__capturedAnswerHTML || '', textLen: window.__capturedAnswerTextLen || 0 })`); jsCapErr2 == nil {
					jsHtml2 := jsCapRes2.Value.Get("html").Str()
					if len(jsHtml2) > len(lastHTML) {
						log.Printf("[rod] updating HTML snapshot from JS capture after 3s wait (%d -> %d chars)", len(lastHTML), len(jsHtml2))
						lastHTML = jsHtml2
					}
				}
				// Stop the JS capture interval now that we're about to extract.
				page.Timeout(2 * time.Second).Eval(`() => { if (window.__answerCaptureInterval) { clearInterval(window.__answerCaptureInterval); window.__answerCaptureInterval = null; } }`)
				log.Printf("[rod] answer element did not reappear after 3s, extracting from saved HTML snapshot (%d chars, lastText=%d chars)", len(lastHTML), len(lastText))
				if len(lastHTML) > 0 {
					h2m := &HtmlToMarkdownExtractor{}
					if snapText, snapErr := h2m.ExtractFromHTML(page, lastHTML, prompt); snapErr == nil && len(snapText) > 50 {
						log.Printf("[rod] HTML snapshot extraction succeeded: %d chars", len(snapText))
						finalText = snapText
						lastText = currentText
						goto done
					} else if snapErr != nil {
						log.Printf("[rod] HTML snapshot extraction failed: %v, falling back to polling text", snapErr)
					}
				}
				log.Printf("[rod] falling back to polling text (%d chars)", len(lastText))
				goto done
			}
		}

		if currentCount < beforeCount {
			log.Printf("[rod] answer count decreased (%d -> %d), resetting baseline", beforeCount, currentCount)
			re.closeOverlays(page)
			if urlCheckRes, urlCheckErr := page.Timeout(3 * time.Second).Eval(`() => window.location.href`); urlCheckErr == nil {
				curURL := urlCheckRes.Value.Str()
				if re.navigatedAwayFromChat(curURL, site.URL) {
					log.Printf("[rod] page navigated away from chat during polling (now %s), navigating back to %s", curURL, site.URL)
					if navErr := page.Timeout(10 * time.Second).Navigate(site.URL); navErr == nil {
						_ = page.WaitLoad()
						_ = page.WaitIdle(3 * time.Second)
						re.refreshPageAfterNavigation(page)
						re.closeOverlays(page)
						time.Sleep(1 * time.Second)
					}
				}
			}
			beforeCount = currentCount
			beforeText = currentText
			lastText = ""
			stableRounds = 0
		}

		if currentCount > beforeCount && currentText == "" && pollCount >= 5 && renderRetryCount < 3 {
			renderRetryCount++
			log.Printf("[rod] answer element exists but empty at poll=%d, re-injecting mocks (retry %d)", pollCount, renderRetryCount)
			re.reinjectMocks(page)
			page.Eval(fmt.Sprintf(`() => {
				var els = document.querySelectorAll(%q);
				if (els.length > 0) {
					els[els.length - 1].scrollIntoView({block: 'center'});
				}
				document.dispatchEvent(new Event('visibilitychange'));
				window.dispatchEvent(new Event('focus'));
				window.dispatchEvent(new Event('pageshow'));
			}`, sels.Answer))
			time.Sleep(2 * time.Second)
		}

		if currentCount > beforeCount && currentText != "" {
			// Adaptive minimum polls: short answers (<2000 chars) can stabilize
			// after 6 polls (3s), medium answers (<10000) after 12 polls (6s),
			// long answers keep the default 20 polls (10s). This significantly
			// reduces wait time for fast sites like DeepSeek with short answers.
			adaptiveMin := minPollsBeforeStable
			if len(currentText) < 2000 {
				adaptiveMin = 6
			} else if len(currentText) < 10000 {
				adaptiveMin = 12
			}
			if currentText == lastText {
				stableRounds++
				if stableRounds >= requiredStable && pollCount >= adaptiveMin {
					if isStillGenerating(page) || isThinkingText(currentText) {
						stableRounds = 0
						log.Printf("[rod] text stable but still generating or thinking, continuing poll (poll=%d len=%d)", pollCount, len(currentText))
					} else {
						log.Printf("[rod] answer stabilized after %d polls (%d chars, adaptiveMin=%d)", pollCount, len(currentText), adaptiveMin)
						lastText = currentText
						goto done
					}
				}
				// Early extraction: when the text is briefly stable (2 rounds = 1s)
				// and the element still exists, try clipboard extraction. Some sites
				// (e.g. DeepSeek) remove the streaming element right after completion,
				// so waiting for the full requiredStable would miss the window. If
				// clipboard succeeds with substantial markdown text, use it directly.
				// Skip when the text looks like thinking/reasoning content (GLM shows
				// a "跳过思考" button while reasoning); the stable pause is the model
				// between thought steps, not the final answer.
				if stableRounds >= 2 && pollCount >= 4 && len(currentText) > 200 && !looksLikeThinking(currentText) {
					log.Printf("[rod] early clipboard extraction attempt (stableRounds=%d poll=%d len=%d)", stableRounds, pollCount, len(currentText))
					earlyExtractor := NewContentExtractor(sels.ContentStrategy, sels.CopyButton)
					if earlyText, earlyErr := earlyExtractor.Extract(page, sels.Answer, beforeCount, len(currentText), prompt); earlyErr == nil && len(earlyText) > len(currentText)/2 {
						if looksLikeThinking(earlyText) {
							log.Printf("[rod] early extraction result looks like thinking content (%d chars), continuing to poll", len(earlyText))
							stableRounds = 0
						} else {
							log.Printf("[rod] early extraction succeeded: %d chars (vs polling %d), using it", len(earlyText), len(currentText))
							finalText = earlyText
							lastText = currentText
							goto done
						}
					} else if earlyErr != nil {
						log.Printf("[rod] early extraction failed: %v", earlyErr)
					} else {
						log.Printf("[rod] early extraction too short: %d chars (vs polling %d), continuing", len(earlyText), len(currentText))
					}
				}
			} else {
				stableRounds = 0
				lastText = currentText
				if pollCount <= 5 || pollCount%20 == 0 {
					log.Printf("[rod] polling... count=%d text_len=%d", currentCount, len(currentText))
				}
			}
		} else if currentCount == beforeCount && beforeCount > 0 && currentText != "" && currentText != beforeText {
			// Same adaptive minimum for same-count branch (element text updated in place)
			adaptiveMinSame := minPollsBeforeStable
			if len(currentText) < 2000 {
				adaptiveMinSame = 6
			} else if len(currentText) < 10000 {
				adaptiveMinSame = 12
			}
			if currentText == lastText {
				stableRounds++
				if stableRounds >= requiredStable && pollCount >= adaptiveMinSame {
					if isStillGenerating(page) || isThinkingText(currentText) {
						stableRounds = 0
						log.Printf("[rod] text stable (same count) but still generating or thinking, continuing poll (poll=%d len=%d)", pollCount, len(currentText))
					} else {
						log.Printf("[rod] answer stabilized (same count) after %d polls (%d chars, adaptiveMin=%d)", pollCount, len(currentText), adaptiveMinSame)
						goto done
					}
				}
			} else {
				stableRounds = 0
				lastText = currentText
				if pollCount <= 5 || pollCount%20 == 0 {
					log.Printf("[rod] polling (same count)... text_len=%d", len(currentText))
				}
			}
		}

		if pollCount == 10 && currentCount == 0 {
			log.Printf("[rod] configured answer selector %q found 0 elements after 10 polls, trying fallback selectors", sels.Answer)
			fallbackSelectors := []string{
				`[class*=flow-markdown]`, `[class*=message-content]`, `[class*=answer]`,
				`[class*=response]`, `[class*=assistant]`, `[class*=reply]`,
				`[class*=bubble]`, `[class*=content-card]`,
				`[class*=chat-message]`, `[class*=ai-message]`, `[class*=bot-message]`,
				`[class*=prose]`, `[class*=rich-text]`, `[class*=text-content]`,
				`[class*=output-content]`, `[class*=chat-content]`,
				`[class*=conversation-content]`, `[class*=message-text]`,
				`[class*=receive]`, `[class*=msg-content]`, `[class*=answer-content]`,
				`[class*=result-content]`, `[class*=detail-content]`,
				`[class*=md-body]`, `[class*=md-content]`, `[class*=markdown-body]`,
				`[class*=text-block]`, `[class*=msg-text]`,
			}
			for _, fbSel := range fallbackSelectors {
				fbJs := fmt.Sprintf(`() => { var els = document.querySelectorAll(%q); var count = 0; var maxText = ''; for (var i = 0; i < els.length; i++) { if (els[i].getBoundingClientRect().width > 50) { count++; var t = (els[i].innerText || els[i].textContent || '').trim(); if (t.length > maxText.length) maxText = t; } } return {count: count, text: maxText.substring(0, 5000)}; }`, fbSel)
				if fbRes, fbErr := page.Timeout(3 * time.Second).Eval(fbJs); fbErr == nil {
					fbCount := fbRes.Value.Get("count").Int()
					fbText := fbRes.Value.Get("text").Str()
					if fbCount > 0 {
						log.Printf("[rod] fallback selector %q found %d visible elements (textLen=%d), switching from %q", fbSel, fbCount, len(fbText), sels.Answer)
						sels.Answer = fbSel
						beforeCount = fbCount
						beforeText = fbText
						lastText = ""
						stableRounds = 0
						goto continuePolling
					}
				}
			}

			iframeDiagJs := `() => {
				var iframes = document.querySelectorAll('iframe');
				var result = [];
				for (var i = 0; i < iframes.length; i++) {
					var rect = iframes[i].getBoundingClientRect();
					if (rect.width < 100 || rect.height < 100) continue;
					result.push({
						src: (iframes[i].src || '').substring(0, 120),
						w: Math.round(rect.width),
						h: Math.round(rect.height),
						x: Math.round(rect.x),
						y: Math.round(rect.y)
					});
				}
				return result;
			}`
			if iframeRes, iframeErr := page.Timeout(3 * time.Second).Eval(iframeDiagJs); iframeErr == nil {
				arr := iframeRes.Value.Arr()
				if len(arr) > 0 {
					log.Printf("[rod] %d visible iframes found:", len(arr))
					for i, v := range arr {
						if i >= 5 {
							break
						}
						log.Printf("[rod]   iframe[%d]: src=%s size=%dx%d pos=(%d,%d)",
							i, v.Get("src").Str(), v.Get("w").Int(), v.Get("h").Int(),
							v.Get("x").Int(), v.Get("y").Int())
					}
				} else {
					log.Printf("[rod] no visible iframes found")
				}
			}

			diagAnswerJs := `() => {
				var candidates = [];
				var allEls = document.querySelectorAll('div, article, section, pre');
				for (var i = 0; i < allEls.length; i++) {
					var el = allEls[i];
					var cls = el.getAttribute('class') || '';
					if (cls.indexOf('sidebar') >= 0 || cls.indexOf('w-sidebar') >= 0 ||
						cls.indexOf('nav') >= 0 || cls.indexOf('menu') >= 0) continue;
					var parent = el.parentElement;
					var skipParent = false;
					for (var j = 0; j < 5 && parent; j++) {
						var pcls = parent.getAttribute('class') || '';
						if (pcls.indexOf('sidebar') >= 0 || pcls.indexOf('w-sidebar') >= 0) {
							skipParent = true;
							break;
						}
						parent = parent.parentElement;
					}
					if (skipParent) continue;
					var text = (el.innerText || el.textContent || '').trim();
					if (text.length > 30) {
						var rect = el.getBoundingClientRect();
						if (rect.width > 200 && rect.height > 30) {
							candidates.push({
								tag: el.tagName,
								cls: cls.substring(0, 80),
								textLen: text.length,
								textPreview: text.substring(0, 80)
							});
						}
					}
				}
				candidates.sort(function(a, b) { return b.textLen - a.textLen; });
				return candidates.slice(0, 10);
			}`
			if diagRes, diagErr := page.Timeout(5 * time.Second).Eval(diagAnswerJs); diagErr == nil {
				arr := diagRes.Value.Arr()
				log.Printf("[rod] diagnostic (non-sidebar): %d potential answer containers:", len(arr))
				for i, v := range arr {
					if i >= 8 {
						break
					}
					log.Printf("[rod]   candidate[%d]: tag=%s cls=%s textLen=%d preview=%q",
						i, v.Get("tag").Str(), v.Get("cls").Str(), v.Get("textLen").Int(), v.Get("textPreview").Str())
				}
			}
		}

		if pollCount == 30 && (currentCount < beforeCount || (currentCount == 0 && beforeCount == 0)) {
			re.closeOverlays(page)
			if urlCheckRes2, urlCheckErr2 := page.Timeout(3 * time.Second).Eval(`() => window.location.href`); urlCheckErr2 == nil {
				curURL2 := urlCheckRes2.Value.Str()
				if re.navigatedAwayFromChat(curURL2, site.URL) {
					log.Printf("[rod] page navigated away during polling (now %s), navigating back to %s", curURL2, site.URL)
					if navErr2 := page.Timeout(10 * time.Second).Navigate(site.URL); navErr2 == nil {
						_ = page.WaitLoad()
						_ = page.WaitIdle(3 * time.Second)
						re.refreshPageAfterNavigation(page)
						re.closeOverlays(page)
						time.Sleep(1 * time.Second)
					}
				}
			}
			log.Printf("[rod] no viable answer elements after 30 polls (count=%d before=%d), trying fallback selectors", currentCount, beforeCount)
			promptSnippet := prompt[:min(20, len(prompt))]
			fbSelectors30 := []string{
				`[class*=markdown]`, `[class*=message-content]`, `[class*=prose]`,
				`[class*=answer]`, `[class*=response]`, `[class*=assistant]`,
				`[class*=chat-message]`, `[class*=bubble]`, `[class*=content-card]`,
				`[class*=receive]`, `[class*=msg-content]`, `[class*=answer-content]`,
				`[class*=result-content]`, `[class*=md-body]`, `[class*=md-content]`,
				`[class*=markdown-body]`, `[class*=text-block]`, `[class*=msg-text]`,
				`[class*=flow-markdown]`, `[class*=rich-text]`, `[class*=text-content]`,
				`[class*=output-content]`, `[class*=chat-content]`,
				`[class*=conversation-content]`, `[class*=detail-content]`,
			}
			fbFound := false
			for _, fbSel := range fbSelectors30 {
				fbJs := fmt.Sprintf(`() => {
				var els = document.querySelectorAll(%q);
				var totalCount = 0;
				var count = 0;
				var maxText = '';
				var snip = %q;
				for (var i = 0; i < els.length; i++) {
					if (els[i].getBoundingClientRect().width < 50) continue;
					totalCount++;
					var t = (els[i].innerText || els[i].textContent || '').trim();
					if (t.length < 30) continue;
					if (snip && t.indexOf(snip) >= 0) continue;
					count++;
					if (t.length > maxText.length) maxText = t;
				}
				return {count: count, totalCount: totalCount, text: maxText.substring(0, 5000)};
			}`, fbSel, promptSnippet)
				if fbRes, fbErr := page.Timeout(3 * time.Second).Eval(fbJs); fbErr == nil {
					fbCount := fbRes.Value.Get("count").Int()
					fbTotal := fbRes.Value.Get("totalCount").Int()
					fbText := fbRes.Value.Get("text").Str()
					if fbCount > 0 && len(fbText) > 200 {
						log.Printf("[rod] fallback selector %q found %d non-prompt elements (total=%d, textLen=%d), switching from %q", fbSel, fbCount, fbTotal, len(fbText), sels.Answer)
						sels.Answer = fbSel
						beforeCount = fbTotal
						beforeText = fbText
						lastText = ""
						stableRounds = 0
						fbFound = true
						goto continuePolling
					}
				}
			}
			if !fbFound {
				log.Printf("[rod] no fallback selector found elements excluding prompt, dumping diagnostics")
			}
			fullDiagJs := `() => {
				var candidates = [];
				var allEls = document.querySelectorAll('div, p, span, article, section, pre, td, li, blockquote');
				for (var i = 0; i < allEls.length; i++) {
					var el = allEls[i];
					var cls = el.getAttribute('class') || '';
					if (cls.indexOf('sidebar') >= 0 || cls.indexOf('w-sidebar') >= 0) continue;
					if (cls.indexOf('nav') >= 0 || cls.indexOf('menu') >= 0) continue;
					var parent = el.parentElement;
					var skipParent = false;
					for (var j = 0; j < 5 && parent; j++) {
						var pcls = parent.getAttribute('class') || '';
						if (pcls.indexOf('sidebar') >= 0 || pcls.indexOf('w-sidebar') >= 0 ||
							pcls.indexOf('nav') >= 0) {
							skipParent = true;
							break;
						}
						parent = parent.parentElement;
					}
					if (skipParent) continue;
					var text = (el.innerText || el.textContent || '').trim();
					if (text.length < 10 || text.length > 5000) continue;
					var rect = el.getBoundingClientRect();
					if (rect.width < 100 || rect.height < 10) continue;
					var hasBlockChild = false;
					for (var c = 0; c < el.children.length; c++) {
						var ctag = el.children[c].tagName;
						if (ctag === 'DIV' || ctag === 'P' || ctag === 'ARTICLE' || ctag === 'SECTION' || ctag === 'UL' || ctag === 'OL' || ctag === 'PRE' || ctag === 'BLOCKQUOTE') {
							hasBlockChild = true;
							break;
						}
					}
					if (hasBlockChild) continue;
					candidates.push({
						tag: el.tagName,
						cls: cls.substring(0, 80),
						textLen: text.length,
						textPreview: text.substring(0, 100),
						x: Math.round(rect.x),
						y: Math.round(rect.y),
						w: Math.round(rect.width),
						h: Math.round(rect.height)
					});
				}
				candidates.sort(function(a, b) { return a.y - b.y; });
				return candidates.slice(0, 20);
			}`
			if fullDiagRes, fullDiagErr := page.Timeout(5 * time.Second).Eval(fullDiagJs); fullDiagErr == nil {
				arr := fullDiagRes.Value.Arr()
				log.Printf("[rod] full diagnostic: %d leaf elements with text found (sorted by Y):", len(arr))
				for i, v := range arr {
					if i >= 15 {
						break
					}
					log.Printf("[rod]   elem[%d]: tag=%s cls=%s textLen=%d pos=(%d,%d) size=%dx%d text=%q",
						i, v.Get("tag").Str(), v.Get("cls").Str(), v.Get("textLen").Int(),
						v.Get("x").Int(), v.Get("y").Int(), v.Get("w").Int(), v.Get("h").Int(),
						v.Get("textPreview").Str())
				}
			}

			parentDiagJs := `() => {
				var result = [];
				var seen = {};
				var allEls = document.querySelectorAll('div, article, section');
				for (var i = 0; i < allEls.length; i++) {
					var el = allEls[i];
					var cls = el.getAttribute('class') || '';
					if (cls.indexOf('sidebar') >= 0 || cls.indexOf('w-sidebar') >= 0) continue;
					if (cls.indexOf('nav') >= 0 || cls.indexOf('menu') >= 0) continue;
					var parent = el.parentElement;
					var skipParent = false;
					for (var j = 0; j < 5 && parent; j++) {
						var pcls = parent.getAttribute('class') || '';
						if (pcls.indexOf('sidebar') >= 0 || pcls.indexOf('w-sidebar') >= 0 ||
							pcls.indexOf('nav') >= 0) {
							skipParent = true;
							break;
						}
						parent = parent.parentElement;
					}
					if (skipParent) continue;
					var text = (el.innerText || el.textContent || '').trim();
					if (text.length < 100 || text.length > 10000) continue;
					var rect = el.getBoundingClientRect();
					if (rect.width < 200) continue;
					var hasListOrPre = el.querySelector('ul, ol, pre, table, blockquote, h1, h2, h3, h4, h5, h6') !== null;
					if (!hasListOrPre) continue;
					var promptText = text.substring(0, 50);
					if (seen[promptText]) continue;
					seen[promptText] = true;
					result.push({
						tag: el.tagName,
						cls: cls.substring(0, 100),
						textLen: text.length,
						textPreview: text.substring(0, 120),
						childTags: Array.from(el.children).map(function(c) { return c.tagName; }).join(','),
						x: Math.round(rect.x),
						y: Math.round(rect.y),
						w: Math.round(rect.width),
						h: Math.round(rect.height)
					});
				}
				result.sort(function(a, b) { return b.textLen - a.textLen; });
				return result.slice(0, 10);
			}`
			if parentRes, parentErr := page.Timeout(5 * time.Second).Eval(parentDiagJs); parentErr == nil {
				arr := parentRes.Value.Arr()
				log.Printf("[rod] parent container diagnostic: %d containers with structured content found:", len(arr))
				for i, v := range arr {
					if i >= 8 {
						break
					}
					log.Printf("[rod]   parent[%d]: tag=%s cls=%s textLen=%d children=[%s] pos=(%d,%d) size=%dx%d text=%q",
						i, v.Get("tag").Str(), v.Get("cls").Str(), v.Get("textLen").Int(),
						v.Get("childTags").Str(),
						v.Get("x").Int(), v.Get("y").Int(), v.Get("w").Int(), v.Get("h").Int(),
						v.Get("textPreview").Str())
				}
			}
		}

	continuePolling:

		if pollCount%20 == 0 {
			log.Printf("[rod] still polling... count=%d before=%d poll=%d", currentCount, beforeCount, pollCount)
		}

		time.Sleep(500 * time.Millisecond)
	}

	if lastText == "" {
		log.Printf("[rod] poll timeout: no answer received within 120s (polls=%d)", pollCount)
		timeoutDiagJs := `() => {
			var results = [];
			var allEls = document.querySelectorAll('div, section, article, pre, code, [class*=markdown], [class*=message], [class*=answer], [class*=response], [class*=content], [class*=bubble], [class*=reply]');
			for (var i = 0; i < allEls.length && results.length < 30; i++) {
				var el = allEls[i];
				var text = (el.innerText || el.textContent || '').trim();
				if (text.length < 10) continue;
				var rect = el.getBoundingClientRect();
				if (rect.width < 50 || rect.height < 20) continue;
				var cls = (el.getAttribute('class') || '').substring(0, 80);
				var tag = el.tagName;
				var children = el.children.length;
				results.push({tag: tag, cls: cls, textLen: text.length, text: text.substring(0, 80), children: children, x: Math.round(rect.x), y: Math.round(rect.y), w: Math.round(rect.width), h: Math.round(rect.height)});
			}
			results.sort(function(a, b) { return b.textLen - a.textLen; });
			return JSON.stringify(results.slice(0, 20));
		}`
		if timeoutDiagResult, timeoutDiagErr := page.Timeout(5 * time.Second).Eval(timeoutDiagJs); timeoutDiagErr == nil {
			log.Printf("[rod] poll timeout DOM diag: %s", timeoutDiagResult.Value.Str())
		}
		return "", errors.New("poll answer timeout: no answer received within 120s")
	}

done:
	// Stop the high-frequency JS capture interval (if still running).
	page.Timeout(2 * time.Second).Eval(`() => { if (window.__answerCaptureInterval) { clearInterval(window.__answerCaptureInterval); window.__answerCaptureInterval = null; } }`)
	ReportProgress(ctx, ProgressExtracting)
	log.Printf("[rod] answer stabilized, extracting content (strategy=%s)", sels.ContentStrategy)

	// If early extraction already set finalText (e.g. clipboard succeeded during
	// polling while the element still existed), skip the normal extraction path.
	if finalText == "" {
		// Close any zoom/preview/modal overlays that may have opened during polling
		// (e.g. Doubao enlarges a table/image when it gets clicked). If an overlay is
		// covering the answer, the clipboard copy button search would click into the
		// wrong element and capture the zoomed fragment instead of the full reply.
		re.closeOverlays(page)

		extractor := NewContentExtractor(sels.ContentStrategy, sels.CopyButton)
		extractText, extractErr := extractor.Extract(page, sels.Answer, beforeCount, len(lastText), prompt)
		if extractErr != nil {
			log.Printf("[rod] content extraction failed: %v, falling back to polling text", extractErr)
			finalText = lastText
		} else if looksLikeThinking(extractText) {
			// The clipboard extractor may have captured the thinking region
			// (GLM wraps the whole message in .answer-content). Re-try with
			// the html2md extractor which prunes thinking subtrees during the
			// tree walk.
			log.Printf("[rod] extracted text looks like thinking content (%d chars), re-trying with html2md", len(extractText))
			h2m := &HtmlToMarkdownExtractor{}
			if h2mText, h2mErr := h2m.Extract(page, sels.Answer, beforeCount, len(lastText), prompt); h2mErr == nil && len(h2mText) > 50 && !looksLikeThinking(h2mText) {
				log.Printf("[rod] html2md re-extraction succeeded (%d chars), using it", len(h2mText))
				finalText = h2mText
			} else {
				log.Printf("[rod] html2md re-extraction did not help, falling back to polling text (%d chars)", len(lastText))
				finalText = lastText
			}
		} else {
			finalText = extractText
		}
	} else {
		log.Printf("[rod] using early extraction result (%d chars), skipping normal extraction", len(finalText))
	}

	// Validate the extracted text actually corresponds to the answer the polling
	// loop tracked. If the extractor grabbed a different (e.g. previous-turn)
	// message, lastText and finalText will share no common substring. In that
	// case prefer the html2md extractor (which is scoped by beforeCount) or fall
	// back to the polling text. This prevents multi-turn mismatches where the
	// clipboard copied an older message.
	if len(lastText) > 50 && len(finalText) > 50 && finalText != lastText {
		if !shareCommonSubstring(lastText, finalText, 20) {
			log.Printf("[rod] extracted text does not match polling text (lastText=%d finalText=%d), re-trying with html2md extractor", len(lastText), len(finalText))
			h2m := &HtmlToMarkdownExtractor{}
			if h2mText, h2mErr := h2m.Extract(page, sels.Answer, beforeCount, len(lastText), prompt); h2mErr == nil && len(h2mText) > 50 {
				if shareCommonSubstring(lastText, h2mText, 20) {
					log.Printf("[rod] html2md re-extraction matches polling text (%d chars), using it", len(h2mText))
					finalText = h2mText
				} else if len(h2mText) > len(finalText) {
					log.Printf("[rod] html2md re-extraction longer (%d chars) but no overlap; keeping original", len(h2mText))
				}
			}
			if !shareCommonSubstring(finalText, lastText, 20) && len(lastText) > len(finalText) {
				log.Printf("[rod] no extraction matched polling text; falling back to polling text (%d chars)", len(lastText))
				finalText = lastText
			}
		}
	}

	if beforeCount == 0 && len(prompt) > 10 {
		promptCheck := prompt[:min(30, len(prompt))]
		if strings.Contains(finalText[:min(len(finalText), len(promptCheck)+50)], promptCheck) {
			stripJs := fmt.Sprintf(`() => {
				var els = document.querySelectorAll(%q);
				var parts = [];
				var promptSnip = %q;
				for (var i = 0; i < els.length; i++) {
					var t = (els[i].innerText || els[i].textContent || '').trim();
					if (t.indexOf(promptSnip) >= 0 && t.length < promptSnip.length + 200) continue;
					parts.push(i);
				}
				return JSON.stringify(parts);
			}`, sels.Answer, promptCheck)
			if stripRes, stripErr := page.Timeout(5 * time.Second).Eval(stripJs); stripErr == nil {
				var indices []int
				if json.Unmarshal([]byte(stripRes.Value.Str()), &indices) == nil && len(indices) > 0 {
					extractJs := fmt.Sprintf(`() => {
						function htmlToMd(el) {
							function esc(s) { return (s || '').replace(/\|/g, '\\|'); }
							function convert(node, depth) {
								if (depth > 15) return '';
								var result = '';
								for (var i = 0; i < node.childNodes.length; i++) {
									var child = node.childNodes[i];
									if (child.nodeType === 3) {
										result += child.textContent.replace(/\s+/g, ' ');
									} else if (child.nodeType === 1) {
										var tag = child.tagName.toLowerCase();
										var cls = (child.getAttribute('class') || '').toLowerCase();
										if (tag === 'button' || tag === 'svg' || tag === 'path' ||
											cls.indexOf('copy') >= 0 || cls.indexOf('download') >= 0 ||
											cls.indexOf('clipboard') >= 0 || cls.indexOf('toolbar') >= 0 ||
											cls.indexOf('action') >= 0 || cls.indexOf('code-header') >= 0) continue;
										switch (tag) {
											case 'h1': result += '\n# ' + convert(child, depth+1).trim() + '\n\n'; break;
											case 'h2': result += '\n## ' + convert(child, depth+1).trim() + '\n\n'; break;
											case 'h3': result += '\n### ' + convert(child, depth+1).trim() + '\n\n'; break;
											case 'h4': result += '\n#### ' + convert(child, depth+1).trim() + '\n\n'; break;
											case 'h5': result += '\n##### ' + convert(child, depth+1).trim() + '\n\n'; break;
											case 'h6': result += '\n###### ' + convert(child, depth+1).trim() + '\n\n'; break;
											case 'p': result += '\n' + convert(child, depth+1).trim() + '\n\n'; break;
											case 'br': result += '\n'; break;
											case 'hr': result += '\n---\n\n'; break;
											case 'strong': case 'b': result += '**' + convert(child, depth+1).trim() + '**'; break;
											case 'em': case 'i': result += '*' + convert(child, depth+1).trim() + '*'; break;
											case 'del': case 's': result += '~~' + convert(child, depth+1).trim() + '~~'; break;
											case 'code':
												var codeCls = child.getAttribute('class') || '';
												var langMatch = codeCls.match(/language-(\w+)/);
												if (langMatch && child.parentElement && child.parentElement.tagName.toLowerCase() === 'pre') {
													result += convert(child, depth+1);
												} else {
													result += '\x60' + (child.textContent || '') + '\x60';
												}
												break;
											case 'pre':
												var codeEl = child.querySelector('code');
												var codeSource = codeEl || child;
												var codeClone = codeSource.cloneNode(true);
												var lineNumEls = codeClone.querySelectorAll('[class*="line-number"], [class*="lineno"], [data-line-number]');
												for (var ln = 0; ln < lineNumEls.length; ln++) lineNumEls[ln].remove();
												var codeText = (codeClone.textContent || '').trim();
												var preCls = child.getAttribute('class') || '';
												var codeLang = '';
												var preM = preCls.match(/language-(\w+)/);
												if (preM) codeLang = preM[1];
												if (!codeLang && codeEl) {
												var cc = codeEl.getAttribute('class') || '';
												var cm = cc.match(/language-(\w+)/);
												if (cm) codeLang = cm[1];
											}
											if (!codeLang && codeText.length > 0) {
												if (/^\s*(def |import |from |print\(|class |if __name__)/.test(codeText)) codeLang = 'python';
												else if (/^\s*(function |const |let |var |import |export )/.test(codeText)) codeLang = 'javascript';
												else if (/^\s*(func |package )/.test(codeText)) codeLang = 'go';
												else if (/^\s*(pub fn |fn |use |mod )/.test(codeText)) codeLang = 'rust';
												else if (/^\s*(#include|#define|#ifndef)/.test(codeText)) codeLang = 'cpp';
												else if (/\b(graph TD|graph LR|flowchart|sequenceDiagram|classDiagram|stateDiagram|erDiagram|gantt|journey|pie)\b/.test(codeText)) codeLang = 'mermaid';
											}
											if (codeText.length > 0) {
												result += '\n\x60\x60\x60' + codeLang + '\n' + codeText + '\n\x60\x60\x60\n\n';
											}
												break;
											case 'ul': case 'ol':
												var items = child.children;
												for (var li = 0; li < items.length; li++) {
													if (items[li].tagName === 'LI') {
														var prefix = tag === 'ol' ? (li+1) + '. ' : '- ';
														result += prefix + convert(items[li], depth+1).trim() + '\n';
													}
												}
												result += '\n';
												break;
											case 'blockquote':
												var bqText = convert(child, depth+1).trim();
												var bqLines = bqText.split('\n');
												for (var bi = 0; bi < bqLines.length; bi++) {
													result += '> ' + bqLines[bi] + '\n';
												}
												result += '\n';
												break;
											case 'table':
												var rows = child.querySelectorAll('tr');
												for (var ri = 0; ri < rows.length; ri++) {
													var cells = rows[ri].querySelectorAll('th,td');
													var rowData = [];
													for (var ci = 0; ci < cells.length; ci++) {
														rowData.push(esc(convert(cells[ci], depth+1).trim()));
													}
													result += '| ' + rowData.join(' | ') + ' |\n';
													if (ri === 0) {
														result += '|' + Array(rows[0].querySelectorAll('th,td').length+1).join(' --- |') + '\n';
													}
												}
												result += '\n';
												break;
											case 'a':
												var href = child.getAttribute('href') || '';
												var linkText = convert(child, depth+1).trim();
												if (href) result += '[' + linkText + '](' + href + ')';
												else result += linkText;
												break;
											case 'img':
												var src = child.getAttribute('src') || '';
												var alt = child.getAttribute('alt') || '';
												if (src) result += '![' + alt + '](' + src + ')';
												break;
											default: result += convert(child, depth+1); break;
										}
									}
								}
								return result;
							}
							return convert(el, 0).replace(/\n{3,}/g, '\n\n').replace(/[ \t]+\n/g, '\n').trim();
						}
						var els = document.querySelectorAll(%q);
						var indices = %s;
						var parts = [];
						for (var ii = 0; ii < indices.length; ii++) {
							var idx = indices[ii];
							if (idx >= els.length) continue;
							var md = htmlToMd(els[idx]).trim();
							var raw = (els[idx].innerText || els[idx].textContent || '').trim();
							if (md.length < raw.length * 0.7 && raw.length > md.length + 50) {
								md = raw;
							}
							if (md) parts.push(md);
						}
						return parts.join('\n\n');
					}`, sels.Answer, stripRes.Value.Str())
					if reExtractRes, reErr := page.Timeout(10 * time.Second).Eval(extractJs); reErr == nil {
						reText := reExtractRes.Value.Str()
						if len(reText) > 50 && len(reText) > len(finalText)/2 {
							log.Printf("[rod] re-extracted without prompt element: %d chars (was %d)", len(reText), len(finalText))
							finalText = reText
						}
					}
				}
			}
		}
	}

	log.Printf("[rod] answer received: %d chars, text=%q", len(finalText), finalText[:min(100, len(finalText))])

	// Strip thinking/reasoning process text that some sites (e.g. GLM, DeepThink)
	// include inside the answer container. These appear as sections delimited by
	// markers like "思考结束" / "思考开始" or embedded in elements with thinking
	// classes that the html2md filter missed (e.g. when content_strategy=html2md
	// captures the whole .answer-content including the thinking panel).
	finalText = stripThinkingProcess(finalText)
	if len(finalText) == 0 {
		finalText = lastText
	}

	re.closeOverlays(page)

	if re.db != nil {
		cookies, err := page.Cookies(nil)
		if err == nil && len(cookies) > 0 {
			params := make([]*proto.NetworkCookieParam, len(cookies))
			for i, c := range cookies {
				params[i] = toNetworkCookieParam(c)
			}
			cookieData, marshalErr := json.Marshal(params)
			if marshalErr == nil {
				_ = storage.SaveSiteCookie(re.db, models.SiteCookie{
					SiteID:  site.ID,
					Cookies: string(cookieData),
				})
			}
		}
	}

	return finalText, nil
}

// shareCommonSubstring reports whether a and b share a common substring of at
// least minLen consecutive characters. Both inputs are normalised by stripping
// markdown punctuation and whitespace so that a plain-text polling snapshot and
// a markdown extraction of the same answer are recognised as matching.
func shareCommonSubstring(a, b string, minLen int) bool {
	if len(a) < minLen || len(b) < minLen {
		return false
	}
	strip := func(s string) string {
		var sb strings.Builder
		sb.Grow(len(s))
		for _, r := range s {
			switch r {
			case ' ', '\t', '\n', '\r', '#', '*', '`', '_', '~', '|', '>', '-', '=', '[', ']', '(', ')', '!', '.', ',', ':', ';', '"', '\'', '\\', '/':
				continue
			}
			sb.WriteRune(r)
		}
		return sb.String()
	}
	na := strip(a)
	nb := strip(b)
	if len(na) < minLen || len(nb) < minLen {
		return false
	}
	for i := 0; i+minLen <= len(na); i += 4 {
		sub := na[i : i+minLen]
		if strings.Contains(nb, sub) {
			return true
		}
	}
	if len(na) >= minLen {
		if strings.Contains(nb, na[:minLen]) {
			return true
		}
		if strings.Contains(nb, na[len(na)-minLen:]) {
			return true
		}
	}
	return false
}

// navigatedAwayFromChat reports whether currentURL has navigated away from
// the site's chat page. It compares hostnames and detects known non-chat path
// segments (write/record/agent/canvas/etc.) that indicate the click landed on
// a non-chat page.
func (re *RodEngine) navigatedAwayFromChat(currentURL, siteURL string) bool {
	if currentURL == "" {
		return false
	}
	siteHost := ""
	if u, err := neturl.Parse(siteURL); err == nil {
		siteHost = u.Hostname()
	}
	curHost := ""
	curPath := ""
	if u, err := neturl.Parse(currentURL); err == nil {
		curHost = u.Hostname()
		curPath = u.Path
	}
	// Different hostname = definitely navigated away
	if siteHost != "" && curHost != "" && siteHost != curHost {
		return true
	}
	// Known non-chat path segments
	nonChatSegments := []string{
		"/write", "/writing", "/article", "/doc", "/record", "/recording",
		"/agent", "/canvas", "/draw", "/paint", "/image", "/video", "/audio",
		"/trans", "/translate", "/summar", "/present", "/slide", "/sheet",
		"/setting", "/profile", "/account", "/billing", "/help", "/about",
		"/moyin", "/chat-file", "/file", "/workspace",
	}
	for _, seg := range nonChatSegments {
		if strings.Contains(curPath, seg) {
			return true
		}
	}
	// If site URL has /chat but current doesn't, navigated away
	if strings.Contains(siteURL, "/chat") && !strings.Contains(currentURL, "/chat") {
		return true
	}
	return false
}

// isStillGenerating reports whether the page currently shows a visible
// "stop generating" / "thinking" / "loading" indicator. When such an indicator
// is visible the answer text may be temporarily stable (e.g. between
// paragraphs) even though generation has not finished. We keep polling in that
// case to avoid truncating the answer.
func isStillGenerating(page *rod.Page) bool {
	if page == nil {
		return false
	}
	checkJs := `() => {
		// Only detect explicit "stop generating" buttons. Avoid broad selectors
		// like [class*="loading"] / [class*="typing"] / [class*="thinking"] which
		// match sidebar loaders, input cursors, and other always-present
		// elements, causing false positives that prevent answers from ever being
		// marked stable (trapped in endless polling).
		var candidates = document.querySelectorAll(
			'button[class*="stop"], [role="button"][class*="stop"], ' +
			'button[aria-label*="停止"], button[aria-label*="Stop"], ' +
			'button[aria-label*="stop"], [class*="stop-generating"], [class*="stop-generation"]'
		);
		for (var i = 0; i < candidates.length; i++) {
			var el = candidates[i];
			var rect = el.getBoundingClientRect();
			if (rect.width === 0 && rect.height === 0) continue;
			var style = window.getComputedStyle(el);
			if (style.display === 'none' || style.visibility === 'hidden' || style.opacity === '0') continue;
			var text = (el.innerText || el.textContent || '').toLowerCase().trim();
			var ariaLabel = (el.getAttribute('aria-label') || '').toLowerCase().trim();
			var cls = (el.getAttribute('class') || '').toLowerCase();
			var allText = text + ' ' + ariaLabel;
			if (allText.indexOf('停止生成') >= 0 || allText.indexOf('stop generating') >= 0 ||
				allText.indexOf('停止') >= 0 || allText.indexOf('stop') >= 0 ||
				cls.indexOf('stop-generating') >= 0 || cls.indexOf('stop-generation') >= 0 ||
				cls.indexOf('stop-icon') >= 0 || cls.indexOf('stop-btn') >= 0) {
				return true;
			}
		}
		return false;
	}`
	result, err := page.Timeout(2 * time.Second).Eval(checkJs)
	if err != nil {
		return false
	}
	return result.Value.Bool()
}

// isThinkingText reports whether the given text looks like a "thinking..." /
// "generating..." placeholder rather than the actual answer. GLM and some other
// models show short transitional phrases while still reasoning; treating those
// as a stable answer would truncate the real response.
func isThinkingText(text string) bool {
	t := strings.TrimSpace(text)
	if t == "" {
		return false
	}
	// Only short transitional text counts. Real answers are typically > 100 chars.
	if len([]rune(t)) > 100 {
		return false
	}
	lower := strings.ToLower(t)
	patterns := []string{
		"正在思考", "思考中", "让我想想", "让我思考",
		"thinking", "let me think", "let me consider",
		"reasoning", "正在推理", "推理中",
		"generating", "正在生成", "生成中",
		"typing", "正在输入",
		"loading", "加载中",
		"please wait", "请稍等", "请稍候",
	}
	for _, p := range patterns {
		if strings.Contains(lower, p) {
			return true
		}
	}
	return false
}

// stripThinkingProcess removes embedded thinking/reasoning sections from the
// final answer text. Some sites (notably GLM/chatglm.cn) render the thinking
// process inside the same .answer-content container as the final answer,
// prefixed by markers like "思考结束" or wrapped in "思考开始...思考结束".
// This function strips those sections so only the real answer remains.
func stripThinkingProcess(text string) string {
	if text == "" {
		return text
	}
	// Common thinking-section markers used by Chinese LLM sites.
	// We strip everything from the start of a thinking marker to (and
	// including) the corresponding end marker. If only an end marker is
	// present, we strip everything before and including it.
	endMarkers := []string{"思考结束", "推理结束", "思考完成", "推理完成", "Think End", "Think end", "End of Thought", "End of thought"}
	startMarkers := []string{"思考开始", "推理开始", "思考过程", "推理过程", "Think Start", "Think start", "Start of Thought", "Begin Thought"}

	// First try: strip from start marker to end marker (inclusive).
	for _, start := range startMarkers {
		for _, end := range endMarkers {
			sIdx := strings.Index(text, start)
			if sIdx < 0 {
				continue
			}
			eIdx := strings.Index(text[sIdx:], end)
			if eIdx < 0 {
				continue
			}
			eIdx += sIdx + len(end)
			// Keep text before the start marker and after the end marker.
			before := text[:sIdx]
			after := text[eIdx:]
			text = strings.TrimSpace(before + after)
		}
	}

	// Second pass: if only an end marker exists, strip everything up to and
	// including it (the thinking section has no explicit start marker but the
	// answer begins right after the end marker).
	for _, end := range endMarkers {
		idx := strings.Index(text, end)
		if idx < 0 {
			continue
		}
		after := text[idx+len(end):]
		after = strings.TrimLeft(after, "\r\n\t :、。，-")
		if len(after) > 0 {
			text = after
			break
		}
	}

	// Final cleanup: remove any stray thinking-related single-line markers
	// that survived the section stripping (e.g. "思考结束" on its own line
	// when there was no start marker to pair with).
	lines := strings.Split(text, "\n")
	var kept []string
	skipPatterns := []string{"思考结束", "思考开始", "推理结束", "推理开始", "思考过程", "推理过程"}
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		skip := false
		for _, p := range skipPatterns {
			if trimmed == p || trimmed == strings.ToLower(p) {
				skip = true
				break
			}
		}
		if !skip {
			kept = append(kept, line)
		}
	}
	return strings.TrimSpace(strings.Join(kept, "\n"))
}

func (re *RodEngine) StartNewChat(site models.Site) error {
	if err := re.ensureBrowser(); err != nil {
		return fmt.Errorf("browser health check: %w", err)
	}
	page, err := re.getOrCreatePage(site)
	if err != nil {
		log.Printf("[rod] getOrCreatePage failed in StartNewChat, retrying: %v", err)
		if retryErr := re.ensureBrowser(); retryErr != nil {
			return fmt.Errorf("browser health check on retry: %w", retryErr)
		}
		page, err = re.getOrCreatePage(site)
		if err != nil {
			return err
		}
	}
	time.Sleep(2 * time.Second)

	var sels Selectors
	if site.Selectors != "" {
		json.Unmarshal([]byte(site.Selectors), &sels)
	}

	// FIRST: if the page has navigated away from the chat page (e.g. to an
	// AI-writing or record page), navigate back to the site URL before checking
	// for the input element. The /record page has a [contenteditable=true]
	// element for note-taking that would falsely match sels.Input and cause
	// StartNewChat to return early without navigating back.
	if urlResult, urlErr := page.Timeout(3 * time.Second).Eval(`() => window.location.href`); urlErr == nil {
		currentURL := urlResult.Value.Str()
		if re.navigatedAwayFromChat(currentURL, site.URL) {
			log.Printf("[rod] StartNewChat: site %s is on non-chat page (%s), navigating to %s", site.ID, currentURL, site.URL)
			if navErr := page.Timeout(10 * time.Second).Navigate(site.URL); navErr == nil {
				_ = page.WaitLoad()
				_ = page.WaitIdle(3 * time.Second)
				re.refreshPageAfterNavigation(page)
				re.closeOverlays(page)
				time.Sleep(1 * time.Second)
			}
		}
	}

	if sels.Input != "" {
		checkJs := fmt.Sprintf(`() => { return !!document.querySelector(%q); }`, sels.Input)
		hasInput := false
		for attempt := 0; attempt < 10; attempt++ {
			if checkResult, checkErr := page.Timeout(2 * time.Second).Eval(checkJs); checkErr == nil && checkResult.Value.Bool() {
				hasInput = true
				break
			}
			time.Sleep(500 * time.Millisecond)
		}
		if hasInput {
			log.Printf("[rod] StartNewChat: site %s already has input element %s, skipping new chat button", site.ID, sels.Input)
			return nil
		}
	}

	if strings.Contains(site.URL, "/chat") {
		log.Printf("[rod] StartNewChat: site %s has /chat URL, navigating directly to %s instead of clicking buttons", site.ID, site.URL)
		if navErr := page.Timeout(10 * time.Second).Navigate(site.URL); navErr == nil {
			_ = page.WaitLoad()
			_ = page.WaitIdle(3 * time.Second)
			re.refreshPageAfterNavigation(page)
			re.closeOverlays(page)
			time.Sleep(1 * time.Second)
		}
		return nil
	}

	clicked := false

	if sels.NewChat != "" {
		js := fmt.Sprintf(`
			() => {
				var el = document.querySelector(%q);
				if (el) {
					el.click();
					return 'clicked: ' + (el.className || '').substring(0, 60);
				}
				return 'not found';
			}
		`, sels.NewChat)
		result, err := page.Timeout(5 * time.Second).Eval(js)
		if err == nil && result.Value.Str() != "not found" {
			log.Printf("[rod] new chat via selector: %s", result.Value.Str())
			clicked = true
		}
	}

	if !clicked {
		beforeNewCount := 0
		if sels.Answer != "" {
			beforeNewCount, _ = re.getAnswerStatus(page, sels.Answer, 0, "")
		}

		// Non-chat path/keyword exclusions - prevent clicking AI-writing/record/agent
		// links that would navigate away from the chat page.
		excludeTextKeywords := []string{"AI写作", "AI写作助手", "写作", "文章", "文档", "绘画", "画图", "记录", "录制", "智能体", "agent", "翻译", "总结", "分析", "报告", "PPT", "表格"}
		excludePathKeywords := []string{"write", "writing", "article", "doc", "record", "recording", "agent", "canvas", "draw", "paint", "image", "video", "audio", "trans", "translate", "summar", "present", "slide", "sheet", "setting", "profile", "account", "billing", "help", "about"}
		excludeTextJs := ""
		for i, kw := range excludeTextKeywords {
			if i > 0 {
				excludeTextJs += ","
			}
			excludeTextJs += "'" + kw + "'"
		}
		excludePathJs := ""
		for i, kw := range excludePathKeywords {
			if i > 0 {
				excludePathJs += ","
			}
			excludePathJs += "'" + kw + "'"
		}

		// Search and click in one step: find the best "new chat" candidate and
		// click it directly via JS (el.click()). This works for both on-screen
		// and off-screen elements, eliminating blind re-search fallbacks that
		// could find wrong buttons (e.g. "AI写作", "record").
		searchAndClickJs := fmt.Sprintf(`
			() => {
				var keywords = ['新建对话', '新建会话', '新对话', '新会话', '新聊天', '新建聊天', '开启新对话', '发起新对话', '创建新对话', 'New Chat', 'New chat', 'new chat'];
				var classKeywords = ['new-chat', 'newchat', 'new_chat', 'new-conversation', 'sidebar-new', 'new-dialog', 'new-talk', 'chat-new', 'new-session'];
				var excludeTextKeywords = [%s];
				var excludePathKeywords = [%s];
				var candidates = document.querySelectorAll('button, a, div[role=button]');
				var matches = [];
				var allCandidates = [];
				for (var i = 0; i < candidates.length; i++) {
					var el = candidates[i];
					var text = (el.innerText || el.textContent || '').trim();
					var ariaLabel = (el.getAttribute('aria-label') || '').trim();
					var title = (el.getAttribute('title') || '').trim();
					var cls = (el.getAttribute('class') || '').toLowerCase();
					var href = (el.getAttribute('href') || '').toLowerCase();
					var allText = (text + ' ' + ariaLabel + ' ' + title).toLowerCase();
					if (allText.trim().length === 0) continue;
					if (text.length > 30) continue;
					var excluded = false;
					for (var ei = 0; ei < excludeTextKeywords.length; ei++) {
						if (text.indexOf(excludeTextKeywords[ei]) >= 0) { excluded = true; break; }
					}
					if (!excluded) {
						for (var pi = 0; pi < excludePathKeywords.length; pi++) {
							var pk = excludePathKeywords[pi];
							if (href.indexOf(pk) >= 0 || cls.indexOf(pk) >= 0) { excluded = true; break; }
						}
					}
					if (el.getAttribute('target') === '_blank') excluded = true;
					if (excluded) continue;
					if (allCandidates.length < 10) {
						allCandidates.push({tag: el.tagName, cls: el.getAttribute('class') || '', text: text.substring(0, 20), aria: ariaLabel.substring(0, 20), href: href.substring(0, 30)});
					}
					var matched = false;
					var priority = 0;
					for (var j = 0; j < keywords.length; j++) {
						if (allText.indexOf(keywords[j].toLowerCase()) >= 0) {
							matched = true;
							priority = el.tagName === 'BUTTON' ? 100 : (el.tagName === 'A' ? 60 : 40);
							break;
						}
					}
					if (!matched) {
						for (var k = 0; k < classKeywords.length; k++) {
							if (cls.indexOf(classKeywords[k]) >= 0) {
								matched = true;
								priority = el.tagName === 'BUTTON' ? 30 : 10;
								break;
							}
						}
					}
					if (!matched && href && href !== '' && (href === '/' || href.endsWith('/chat') || href.indexOf('/new-chat') >= 0 || href.indexOf('/new_chat') >= 0 || href.indexOf('/newchat') >= 0)) {
						matched = true;
						priority = 5;
					}
					if (!matched) continue;
					var rect = el.getBoundingClientRect();
					if (rect.width === 0 || rect.height === 0) continue;
					matches.push({el: el, priority: priority, tag: el.tagName, cls: el.getAttribute('class') || '', text: text.substring(0, 30), aria: ariaLabel.substring(0, 30), href: href.substring(0, 30)});
				}
				matches.sort(function(a, b) { return b.priority - a.priority; });
				if (matches.length === 0) return {clicked: false, info: 'no matches', candidates: allCandidates};
				var best = matches[0];
				try {
					best.el.scrollIntoView({block: 'center'});
					best.el.click();
					return {clicked: true, tag: best.tag, cls: best.cls.substring(0, 80), text: best.text, aria: best.aria, href: best.href, priority: best.priority, candidates: allCandidates};
				} catch(e) {
					return {clicked: false, info: 'click error: ' + e.message, candidates: allCandidates};
				}
			}
		`, excludeTextJs, excludePathJs)
		result, err := page.Timeout(5 * time.Second).Eval(searchAndClickJs)
		if err != nil {
			log.Printf("[rod] new chat search-and-click failed for site %s: %v", site.ID, err)
		} else if result.Value.Get("clicked").Bool() {
			log.Printf("[rod] new chat clicked for site %s: tag=%s cls=%s text=%q priority=%d",
				site.ID, result.Value.Get("tag").Str(), result.Value.Get("cls").Str(),
				result.Value.Get("text").Str(), result.Value.Get("priority").Int())
			clicked = true
		} else {
			log.Printf("[rod] new chat button not found for site %s: %s", site.ID, result.Value.Get("info").Str())
			candidates := result.Value.Get("candidates").Arr()
			for i, c := range candidates {
				if i >= 5 {
					break
				}
				log.Printf("[rod]   candidate[%d]: tag=%s cls=%s text=%q aria=%q href=%q",
					i, c.Get("tag").Str(), c.Get("cls").Str(),
					c.Get("text").Str(), c.Get("aria").Str(), c.Get("href").Str())
			}
		}

		if clicked {
			time.Sleep(3 * time.Second)
			verified := false
			if sels.Answer != "" && beforeNewCount > 0 {
				afterNewCount, _ := re.getAnswerStatus(page, sels.Answer, 0, "")
				if afterNewCount < beforeNewCount {
					log.Printf("[rod] new chat verified for site %s: answer count %d -> %d", site.ID, beforeNewCount, afterNewCount)
					verified = true
				}
			} else {
				verified = true
			}

			if !verified {
				// Verification failed: navigate to site URL directly instead of
				// doing another blind button search that could click wrong buttons.
				log.Printf("[rod] new chat verification failed for site %s, navigating to %s", site.ID, site.URL)
				if navErr := page.Timeout(10 * time.Second).Navigate(site.URL); navErr == nil {
					_ = page.WaitLoad()
					_ = page.WaitIdle(3 * time.Second)
					re.refreshPageAfterNavigation(page)
					re.closeOverlays(page)
					time.Sleep(1 * time.Second)
				}
			} else {
				_ = page.WaitIdle(5 * time.Second)
				re.reinjectMocks(page)
			}

			// Post-click URL guard: detect navigation away from the site's chat page.
			urlCheckJs := `() => window.location.href`
			if urlResult, urlErr := page.Timeout(3 * time.Second).Eval(urlCheckJs); urlErr == nil {
				currentURL := urlResult.Value.Str()
				if re.navigatedAwayFromChat(currentURL, site.URL) {
					log.Printf("[rod] new chat: page navigated away from chat URL (now %s), navigating back to %s", currentURL, site.URL)
					if navErr := page.Timeout(10 * time.Second).Navigate(site.URL); navErr == nil {
						_ = page.WaitLoad()
						_ = page.WaitIdle(3 * time.Second)
						re.refreshPageAfterNavigation(page)
					}
				}
			}
			return nil
		}
	}

	log.Printf("[rod] new chat: navigating to %s for site %s", site.URL, site.ID)
	if err := page.Navigate(site.URL); err != nil {
		log.Printf("[rod] new chat navigate failed: %v", err)
		return err
	}
	re.refreshPageAfterNavigation(page)
	return nil
}

func (re *RodEngine) reinjectMocks(page *rod.Page) {
	page.Eval(`() => {
		Object.defineProperty(document, 'hidden', {get: function() { return false; }, configurable: true});
		Object.defineProperty(document, 'visibilityState', {get: function() { return 'visible'; }, configurable: true});
		document.hasFocus = function() { return true; };

		window.requestAnimationFrame = function(callback) {
			return setTimeout(function() { callback(performance.now()); }, 16);
		};
		window.cancelAnimationFrame = function(id) { clearTimeout(id); };
		window.requestIdleCallback = function(callback) {
			return setTimeout(function() { callback({didTimeout: false, timeRemaining: function() { return 50; }}); }, 16);
		};
		window.cancelIdleCallback = function(id) { clearTimeout(id); };

		window.IntersectionObserver = function(callback, options) {
			return {
				observe: function(target) {
					var rect = target.getBoundingClientRect();
					callback([{
						boundingClientRect: rect,
						intersectionRatio: 1,
						intersectionRect: rect,
						isIntersecting: true,
						rootBounds: null,
						target: target,
						time: performance.now()
					}], this);
				},
				unobserve: function() {},
				disconnect: function() {},
				takeRecords: function() { return []; }
			};
		};

		window.ResizeObserver = function(callback) {
			return {
				observe: function(target) {
					var rect = target.getBoundingClientRect();
					callback([{
						target: target,
						contentRect: {x: rect.x, y: rect.y, width: rect.width, height: rect.height, top: rect.top, right: rect.right, bottom: rect.bottom, left: rect.left},
						borderBoxSize: [{inlineSize: rect.width, blockSize: rect.height}],
						contentBoxSize: [{inlineSize: rect.width, blockSize: rect.height}],
						devicePixelContentBoxSize: [{inlineSize: rect.width, blockSize: rect.height}]
					}], this);
				},
				unobserve: function() {},
				disconnect: function() {}
			};
		};

		document.dispatchEvent(new Event('visibilitychange'));
	}`)
}

func (re *RodEngine) refreshPageAfterNavigation(page *rod.Page) {
	if err := page.WaitLoad(); err != nil {
		log.Printf("[rod] WaitLoad after new chat (non-fatal): %v", err)
	}
	_ = page.WaitIdle(5 * time.Second)
	time.Sleep(2 * time.Second)
	re.reinjectMocks(page)
	log.Printf("[rod] page refreshed after new chat")
}

func (re *RodEngine) ResetPages() {
	re.mu.Lock()
	defer re.mu.Unlock()

	for id, page := range re.pages {
		if page != nil {
			_ = page.Close()
		}
		delete(re.pages, id)
	}
	log.Printf("[rod] all pages reset")
}

func (re *RodEngine) Close() error {
	re.ResetPages()
	if re.browser != nil {
		return re.browser.Close()
	}
	return nil
}

func (re *RodEngine) Name() string {
	return "rod"
}
