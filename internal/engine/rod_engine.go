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
	browser *rod.Browser
	db      *storage.DB
	pages   map[string]*rod.Page
	mu      sync.Mutex
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

func NewRodEngine(db *storage.DB) *RodEngine {
	l := launcher.New()

	chromePath := findChromeBinary()
	if chromePath != "" {
		log.Printf("[rod] using browser binary: %s", chromePath)
		l = l.Bin(chromePath)
	} else {
		log.Printf("[rod] no system Chrome found, rod will try to download")
	}

	userDataDir := "./.browser-data"
	l = l.UserDataDir(userDataDir)
	l = l.Headless(false).Devtools(true)

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
		browser: browser,
		db:      db,
		pages:   make(map[string]*rod.Page),
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
				el.innerText = %q;
				el.dispatchEvent(new InputEvent('input', { bubbles: true, data: %q }));
				return (el.innerText || el.textContent || '').substring(0, 80);
			} else {
				el.textContent = %q;
				el.dispatchEvent(new Event('input', { bubbles: true }));
				return (el.textContent || '').substring(0, 80);
			}
		}
	`, selector, prompt, prompt, prompt, prompt, prompt)

	result, err := page.Eval(js)
	if err != nil {
		return fmt.Errorf("js input failed: %w", err)
	}
	inputResult := result.Value.Str()
	if inputResult == "not found" {
		return errors.New("js input returned not found: element not found")
	}
	log.Printf("[rod] JS input succeeded, value=%q", inputResult[:min(50, len(inputResult))])
	return nil
}

func (re *RodEngine) typePromptLexical(page *rod.Page, selector string, prompt string) error {
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
			var ok = document.execCommand('insertText', false, %q);
			if (!ok) {
				var lexicalEditor = el.__lexicalEditor;
				if (lexicalEditor && lexicalEditor.update) {
					lexicalEditor.update(function() {});
				}
			}
			return ok ? 'ok' : 'execCommand failed';
		}
	`, selector, prompt)

	result, err := page.Eval(js)
	if err != nil {
		return fmt.Errorf("lexical input eval failed: %w", err)
	}

	status := result.Value.String()
	if status != "ok" {
		return fmt.Errorf("lexical input failed: %s", status)
	}

	log.Printf("[rod] Lexical execCommand insertText succeeded")
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

func (re *RodEngine) submitPrompt(page *rod.Page, selector string) error {
	log.Printf("[rod] looking for submit element: %s", selector)

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
	`, selector)

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
	`, selector)

	result2, err2 := page.Timeout(5 * time.Second).Eval(jsClick)
	if err2 == nil && result2.Value.Str() == "ok" {
		log.Printf("[rod] JS click succeeded (fallback)")
		return nil
	}
	if err2 != nil {
		log.Printf("[rod] JS click failed: %v", err2)
	}

	log.Printf("[rod] trying generic submit button search")
	genericJs := `
		() => {
			var input = document.querySelector('textarea, input, [contenteditable=true]');
			if (input) input.focus();

			var selectors = [
				'button[type=submit]',
				'button[class*=send]', 'button[class*=submit]',
				'div[class*=send] button', 'div[class*=submit] button',
				'div[role=button][class*=send]', 'div[role=button][class*=submit]',
				'[aria-label*=send]', '[aria-label*=发送]', '[aria-label*=提交]',
				'svg[class*=send]', 'span[class*=send]'
			];
			for (var i = 0; i < selectors.length; i++) {
				var el = document.querySelector(selectors[i]);
				if (!el) continue;
				var btn = el.tagName === 'BUTTON' ? el : (el.querySelector('button') || el);
				if (btn.disabled) continue;
				var rect = btn.getBoundingClientRect();
				if (rect.width === 0 || rect.height === 0) continue;
				btn.click();
				return {selector: selectors[i], tag: btn.tagName, cls: (btn.getAttribute('class') || '').substring(0, 60), candidates: []};
			}

			if (input) {
				var parent = input.parentElement;
				for (var depth = 0; depth < 4 && parent; depth++) {
					var btns = parent.querySelectorAll('button, [role=button], div[class*=icon], div[class*=btn]');
					var candidates = [];
					for (var j = 0; j < btns.length; j++) {
						if (btns[j] === input) continue;
						if (btns[j].disabled) continue;
						var rect = btns[j].getBoundingClientRect();
						if (rect.width === 0 || rect.height === 0) continue;
						var cls = (btns[j].getAttribute('class') || '').toLowerCase();
						if (cls.indexOf('toggle') >= 0) continue;
						if (cls.indexOf('setting') >= 0) continue;
						if (cls.indexOf('menu') >= 0) continue;
						if (cls.indexOf('upload') >= 0) continue;
						if (cls.indexOf('file') >= 0) continue;
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
	`
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

	focusJs := fmt.Sprintf(`() => { var el = document.querySelector(%q); if (!el) el = document.querySelector('textarea, input, [contenteditable=true]'); if (el) { el.focus(); return true; } return false; }`, selector)
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
	js := fmt.Sprintf(`
		() => {
			var els = document.querySelectorAll(%q);
			if (els.length === 0) return {count: 0, text: ''};
			var startIdx = Math.min(%d, els.length);
			var maxText = '';
			for (var i = startIdx; i < els.length; i++) {
				var t = (els[i].innerText || els[i].textContent || '').trim();
				if (t.length > maxText.length) maxText = t;
			}
			if (!maxText && startIdx > 0) {
				for (var i = 0; i < els.length; i++) {
					var t = (els[i].innerText || els[i].textContent || '').trim();
					if (t.length > maxText.length) maxText = t;
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

	var sels Selectors
	if err := json.Unmarshal([]byte(site.Selectors), &sels); err != nil {
		return "", fmt.Errorf("parse selectors: %w", err)
	}
	log.Printf("[rod] selectors: input=%s submit=%s answer=%s wait_for=%s",
		sels.Input, sels.Submit, sels.Answer, sels.WaitFor)

	if sels.Input == "" || sels.Answer == "" {
		return "", errors.New("missing required selectors: input and answer are required")
	}

	page, err := re.getOrCreatePage(site)
	if err != nil {
		return "", fmt.Errorf("get page: %w", err)
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
		if err := re.submitPrompt(page, sels.Submit); err != nil {
			log.Printf("[rod] submit failed, trying Enter on input: %v", err)
			_ = re.submitPrompt(page, sels.Input)
		}
	} else {
		_ = re.submitPrompt(page, sels.Input)
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

	if postAnswerCount == beforeCount && strings.Contains(postEditorText, prompt[:min(5, len(prompt))]) {
		log.Printf("[rod] editor still has prompt text after click, trying JS click on submit")
		jsSubmitClick := fmt.Sprintf(`
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
