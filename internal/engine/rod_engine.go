package engine

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
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
	browser     *rod.Browser
	db          *storage.DB
	pages       map[string]*rod.Page
	mu          sync.Mutex
	controlURL  string
	userDataDir string
}

func (re *RodEngine) ensureBrowser() error {
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
	} else {
		log.Printf("[rod] browser not initialized, attempting launch")
	}

	re.mu.Lock()
	re.pages = make(map[string]*rod.Page)
	re.mu.Unlock()

	if re.userDataDir == "" {
		re.userDataDir = "./.browser-data"
	}
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
		path := userDataDir + string(os.PathSeparator) + f
		_ = os.Remove(path)
	}
	log.Printf("[rod] cleaned up lock files in %s", userDataDir)
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
	return &RodEngine{
		browser:     browser,
		db:          db,
		pages:       make(map[string]*rod.Page),
		controlURL:  u,
		userDataDir: userDataDir,
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

	log.Printf("[rod] creating new page for site %s -> %s", site.ID, site.URL)
	page, err := re.browser.Page(proto.TargetCreateTarget{URL: ""})
	if err != nil {
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
						if (cls.indexOf('attach') >= 0) continue;
						if (cls.indexOf('sidebar') >= 0) continue;
						if (cls.indexOf('nav') >= 0 && cls.indexOf('send') < 0) continue;
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
			if i >= 5 { break }
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

func (re *RodEngine) getAnswerStatus(page *rod.Page, selector string, beforeCount int) (int, string) {
	if selector == "" {
		return 0, ""
	}

	js := fmt.Sprintf(`
		() => {
			var els = document.querySelectorAll(%q);
			if (els.length === 0) return {count: 0, text: ''};
			function isInThinking(el) {
				var parent = el.parentElement;
				for (var i = 0; i < 3 && parent; i++) {
					var cls = (parent.getAttribute('class') || '').toLowerCase();
					if (cls.indexOf('think-block') >= 0 || cls.indexOf('think-content') >= 0 ||
						cls.indexOf('think_process') >= 0 || cls.indexOf('thinking-block') >= 0 ||
						cls.indexOf('thinking-content') >= 0 || cls.indexOf('reasoning-block') >= 0 ||
						cls.indexOf('reasoning-content') >= 0 || cls.indexOf('thought-block') >= 0) {
						return true;
					}
					parent = parent.parentElement;
				}
				return false;
			}
			var startIdx = Math.min(%d, els.length);
			var maxText = '';
			for (var i = startIdx; i < els.length; i++) {
				if (isInThinking(els[i])) continue;
				var t = (els[i].innerText || els[i].textContent || '').trim();
				if (t.length > maxText.length) maxText = t;
			}
			if (!maxText) {
				for (var i = startIdx; i < els.length; i++) {
					var t = (els[i].innerText || els[i].textContent || '').trim();
					if (t.length > maxText.length) maxText = t;
				}
			}
			if (!maxText && startIdx > 0) {
				if (startIdx >= els.length) {
					var lastEl = els[els.length - 1];
					if (lastEl && !isInThinking(lastEl)) {
						maxText = (lastEl.innerText || lastEl.textContent || '').trim();
					}
					if (!maxText && lastEl) {
						maxText = (lastEl.innerText || lastEl.textContent || '').trim();
					}
					if (!maxText) {
						var prevEl = els.length >= 2 ? els[els.length - 2] : null;
						if (prevEl && !isInThinking(prevEl)) {
							maxText = (prevEl.innerText || prevEl.textContent || '').trim();
						}
					}
				} else {
					for (var i = 0; i < els.length; i++) {
						if (isInThinking(els[i])) continue;
						var t = (els[i].innerText || els[i].textContent || '').trim();
						if (t.length > maxText.length) maxText = t;
					}
					if (!maxText) {
						for (var i = 0; i < els.length; i++) {
							var t = (els[i].innerText || els[i].textContent || '').trim();
							if (t.length > maxText.length) maxText = t;
						}
					}
				}
			}
			return {count: els.length, text: maxText.substring(0, 5000)};
		}
	`, selector, beforeCount)
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
		return "", fmt.Errorf("get page: %w", err)
	}

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
		if fbResult, fbErr := page.Timeout(5*time.Second).Eval(fallbackJs); fbErr == nil {
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

	beforeCount, beforeText := re.getAnswerStatus(page, sels.Answer, 0)
	log.Printf("[rod] answer count before sending: %d textLen=%d", beforeCount, len(beforeText))

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
		log.Printf("[rod] answer count increased after submit (%d -> %d), updating beforeCount to exclude user message", beforeCount, postAnswerCount)
		beforeCount = postAnswerCount
		_, beforeText = re.getAnswerStatus(page, sels.Answer, beforeCount)
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
				if btnDiagRes, btnDiagErr := page.Timeout(5*time.Second).Eval(btnDiagJs); btnDiagErr == nil {
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
	stableRounds := 0
	const requiredStable = 8
	const minPollsBeforeStable = 20
	deadline := time.Now().Add(120 * time.Second)
	pollCount := 0
	renderRetryCount := 0

	for time.Now().Before(deadline) {
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		default:
		}
		pollCount++

		currentCount, currentText := re.getAnswerStatus(page, sels.Answer, beforeCount)

		if currentCount < beforeCount {
			log.Printf("[rod] answer count decreased (%d -> %d), resetting baseline", beforeCount, currentCount)
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
					els[els.length - 1].click();
				}
				window.dispatchEvent(new Event('focus'));
				window.dispatchEvent(new Event('pageshow'));
			}`, sels.Answer))
			time.Sleep(2 * time.Second)
		}

		if currentCount > beforeCount && currentText != "" {
			if currentText == lastText {
				stableRounds++
				if stableRounds >= requiredStable && pollCount >= minPollsBeforeStable {
					log.Printf("[rod] answer stabilized after %d polls (%d chars)", pollCount, len(currentText))
					lastText = currentText
					goto done
				}
			} else {
				stableRounds = 0
				lastText = currentText
				if pollCount <= 5 || pollCount%20 == 0 {
					log.Printf("[rod] polling... count=%d text_len=%d", currentCount, len(currentText))
				}
			}
		} else if currentCount == beforeCount && beforeCount > 0 && currentText != "" && currentText != beforeText {
		if currentText == lastText {
			stableRounds++
			if stableRounds >= requiredStable && pollCount >= minPollsBeforeStable {
				log.Printf("[rod] answer stabilized (same count) after %d polls (%d chars)", pollCount, len(currentText))
				goto done
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
		return "", errors.New("poll answer timeout: no answer received within 120s")
	}

done:
	log.Printf("[rod] answer stabilized, extracting content (strategy=%s)", sels.ContentStrategy)

	extractor := NewContentExtractor(sels.ContentStrategy, sels.CopyButton)
	finalText, extractErr := extractor.Extract(page, sels.Answer, beforeCount, len(lastText))
	if extractErr != nil {
		log.Printf("[rod] content extraction failed: %v, falling back to polling text", extractErr)
		finalText = lastText
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
			if stripRes, stripErr := page.Timeout(5*time.Second).Eval(stripJs); stripErr == nil {
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
						return parts.join('\\n\\n');
					}`, sels.Answer, stripRes.Value.Str())
					if reExtractRes, reErr := page.Timeout(10*time.Second).Eval(extractJs); reErr == nil {
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

func (re *RodEngine) StartNewChat(site models.Site) error {
	if err := re.ensureBrowser(); err != nil {
		return fmt.Errorf("browser health check: %w", err)
	}
	page, err := re.getOrCreatePage(site)
	if err != nil {
		return err
	}
	time.Sleep(2 * time.Second)

	var sels Selectors
	if site.Selectors != "" {
		json.Unmarshal([]byte(site.Selectors), &sels)
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
			beforeNewCount, _ = re.getAnswerStatus(page, sels.Answer, 0)
		}

		searchJs := `
			() => {
				var keywords = ['新建对话', '新建会话', '新对话', '新会话', 'New Chat', 'New chat', 'new chat', '开启新对话', '发起新对话', '新聊天', '创建新对话', '新建聊天'];
				var classKeywords = ['new-chat', 'newchat', 'new_chat', 'new-conversation', 'sidebar-new', 'new-dialog', 'new-talk', 'chat-new', 'start-new', 'create-new', 'add-chat', 'newsession', 'new-session'];
				var candidates = document.querySelectorAll('button, a, div[role=button], [class*=new], [class*=chat], [aria-label], [title]');
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
					if (allText.trim().length === 0 && cls.indexOf('new') < 0 && href === '') continue;
					if (text.length > 30) continue;
					if (allCandidates.length < 10) {
						allCandidates.push({tag: el.tagName, cls: (el.getAttribute('class') || '').substring(0, 50), text: text.substring(0, 20), aria: ariaLabel.substring(0, 20), href: href.substring(0, 30)});
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
					if (!matched && href && (href === '/' || href.endsWith('/chat') || href.indexOf('new') >= 0)) {
						matched = true;
						priority = 5;
					}
					if (!matched) continue;
					el.scrollIntoView({block: 'center'});
					var rect = el.getBoundingClientRect();
					if (rect.width === 0 || rect.height === 0) continue;
					matches.push({
						idx: i,
						tag: el.tagName,
						cls: (el.getAttribute('class') || '').substring(0, 80),
						text: text.substring(0, 30),
						aria: ariaLabel.substring(0, 30),
						href: href.substring(0, 30),
						priority: priority,
						x: rect.x + rect.width / 2,
						y: rect.y + rect.height / 2
					});
				}
				matches.sort(function(a, b) { return b.priority - a.priority; });
				return {matches: matches, candidates: allCandidates};
			}
		`
		result, err := page.Timeout(5 * time.Second).Eval(searchJs)
		if err != nil {
			log.Printf("[rod] new chat search failed for site %s: %v", site.ID, err)
		} else {
			arr := result.Value.Get("matches").Arr()
			if len(arr) > 0 {
				first := arr[0]
				x := first.Get("x").Num()
				y := first.Get("y").Num()
				log.Printf("[rod] new chat found for site %s: tag=%s cls=%s text=%q priority=%d pos=(%.0f,%.0f)",
					site.ID, first.Get("tag").Str(), first.Get("cls").Str(),
					first.Get("text").Str(), first.Get("priority").Int(), x, y)

				if x > 0 && y > 0 {
					page.Mouse.MoveTo(proto.NewPoint(x, y))
					page.Mouse.Click(proto.InputMouseButtonLeft, 1)
					log.Printf("[rod] new chat CDP click at (%.0f, %.0f)", x, y)
					time.Sleep(300 * time.Millisecond)
					clicked = true
				} else {
					log.Printf("[rod] new chat element off-screen (%.0f, %.0f), using JS click fallback", x, y)
					jsClickRes, jsClickErr := page.Eval(`() => {
						var keywords = ['新建对话', '新建会话', '新对话', '新会话', 'New Chat', 'New chat', 'new chat', '开启新对话', '发起新对话', '新聊天', '创建新对话', '新建聊天'];
						var classKeywords = ['new-chat', 'newchat', 'new_chat', 'new-conversation', 'sidebar-new', 'new-dialog', 'new-talk', 'chat-new', 'start-new', 'create-new', 'add-chat', 'newsession', 'new-session'];
						var btns = document.querySelectorAll('button, a, div[role=button]');
						for (var i = 0; i < btns.length; i++) {
							var t = (btns[i].innerText || btns[i].textContent || '').trim().toLowerCase();
							var a = (btns[i].getAttribute('aria-label') || '').trim().toLowerCase();
							var c = (btns[i].getAttribute('class') || '').toLowerCase();
							var all = t + ' ' + a;
							for (var j = 0; j < keywords.length; j++) {
								if (all.indexOf(keywords[j].toLowerCase()) >= 0) {
									btns[i].click();
									return 'clicked: ' + t.substring(0, 30);
								}
							}
							for (var k = 0; k < classKeywords.length; k++) {
								if (c.indexOf(classKeywords[k]) >= 0) {
									btns[i].click();
									return 'clicked: ' + c.substring(0, 30);
								}
							}
						}
						return 'not found';
					}`)
					if jsClickErr == nil && jsClickRes.Value.Str() != "not found" {
						log.Printf("[rod] new chat JS click: %s", jsClickRes.Value.Str())
						clicked = true
					}
				}
			} else {
				log.Printf("[rod] new chat button not found for site %s, candidates seen:", site.ID)
				candidates := result.Value.Get("candidates").Arr()
				for i, c := range candidates {
					if i >= 5 { break }
					log.Printf("[rod]   candidate[%d]: tag=%s cls=%s text=%q aria=%q href=%q",
						i, c.Get("tag").Str(), c.Get("cls").Str(),
						c.Get("text").Str(), c.Get("aria").Str(), c.Get("href").Str())
				}
			}
		}

		if clicked {
			time.Sleep(3 * time.Second)
			if sels.Answer != "" {
				afterNewCount, _ := re.getAnswerStatus(page, sels.Answer, 0)
				if afterNewCount < beforeNewCount {
					log.Printf("[rod] new chat verified for site %s: answer count %d -> %d", site.ID, beforeNewCount, afterNewCount)
				} else if beforeNewCount > 0 {
					log.Printf("[rod] new chat may not have worked for site %s: answer count %d -> %d, trying JS click", site.ID, beforeNewCount, afterNewCount)
					page.Eval(`() => {
						var keywords = ['新建对话', '新建会话', '新对话', '新会话', 'New Chat', 'New chat', 'new chat', '开启新对话', '发起新对话', '新聊天', '创建新对话', '新建聊天'];
						var btns = document.querySelectorAll('button, a, div[role=button]');
						for (var i = 0; i < btns.length; i++) {
							var t = (btns[i].innerText || btns[i].textContent || '').trim().toLowerCase();
							var a = (btns[i].getAttribute('aria-label') || '').trim().toLowerCase();
							for (var j = 0; j < keywords.length; j++) {
								if (t.indexOf(keywords[j].toLowerCase()) >= 0 || a.indexOf(keywords[j].toLowerCase()) >= 0) {
									btns[i].click();
									return 'clicked: ' + t.substring(0, 30);
								}
							}
						}
						return 'not found';
					}`)
					time.Sleep(2 * time.Second)
					afterNewCount2, _ := re.getAnswerStatus(page, sels.Answer, 0)
					if afterNewCount2 >= beforeNewCount && beforeNewCount > 0 {
						log.Printf("[rod] new chat JS click also failed for site %s: answer count %d -> %d, falling back to navigation", site.ID, beforeNewCount, afterNewCount2)
						clicked = false
					}
				}
			}
			if clicked {
			_ = page.WaitIdle(5 * time.Second)
			re.reinjectMocks(page)
			return nil
		}
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
