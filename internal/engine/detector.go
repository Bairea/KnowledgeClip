package engine

import (
	"fmt"
	"log"
	"time"

	"github.com/go-rod/rod"
	"github.com/go-rod/rod/lib/launcher"
)

func DetectSelectors(url string) (map[string]string, error) {
	l := launcher.New().Headless(true).NoSandbox(true)
	l = l.Delete("enable-automation")
	l = l.Set("disable-blink-features", "AutomationControlled")

	chromePath := findChromeBinary()
	if chromePath != "" {
		log.Printf("[detect] using browser binary: %s", chromePath)
		l = l.Bin(chromePath)
	}

	u, err := l.Launch()
	if err != nil {
		return nil, fmt.Errorf("launch browser: %w", err)
	}

	browser := rod.New().ControlURL(u).MustConnect()
	defer browser.Close()

	page := browser.MustPage("")
	defer page.Close()

	if err := page.Navigate(url); err != nil {
		return nil, fmt.Errorf("navigate: %w", err)
	}

	if err := page.WaitLoad(); err != nil {
		return nil, fmt.Errorf("wait load: %w", err)
	}

	_ = page.WaitIdle(5 * time.Second)

	result, err := page.Timeout(15*time.Second).Eval(detectJS)
	if err != nil {
		return nil, fmt.Errorf("detect eval: %w", err)
	}

	if result.Value.Nil() {
		return make(map[string]string), nil
	}

	detected := make(map[string]string)
	for _, key := range []string{"input", "submit", "answer", "wait_for", "copy_button", "content_strategy"} {
		s := result.Value.Get(key).Str()
		if s != "" && s != "<nil>" {
			detected[key] = s
		}
	}

	if detected["input"] == "" {
		detected["input"] = "textarea"
	}

	if detected["answer"] != "" && detected["wait_for"] == "" {
		detected["wait_for"] = detected["answer"] + ":last-child"
	}

	if detected["content_strategy"] == "" {
		detected["content_strategy"] = "clipboard"
	}

	log.Printf("[detect] result for %s: %+v", url, detected)
	return detected, nil
}

const detectJS = `() => {
	var result = {};

	var inputCandidates = [
		'textarea',
		'[contenteditable=true]',
		'[data-slate-editor=true]',
		'[data-lexical-editor]',
		'#chat-input',
		'#prompt-textarea',
		'input[type=text]',
		'div[role=textbox]',
		'div[contenteditable]',
		'.chat-input',
		'.chat-input-editor',
		'[class*=input-editor]',
		'[class*=chat-input]',
		'[class*=prompt-input]'
	];
	for (var i = 0; i < inputCandidates.length; i++) {
		var el = document.querySelector(inputCandidates[i]);
		if (el) {
			var rect = el.getBoundingClientRect();
			if (rect.width > 0 && rect.height > 0) {
				result.input = inputCandidates[i];
				break;
			}
		}
	}

	var submitCandidates = [
		'button[type=submit]',
		'#send-btn',
		'.send-button',
		'button[class*=send]',
		'button[class*=submit]',
		'div[class*=send]',
		'div[class*=send] button',
		'div[class*=submit] button',
		'div[role=button][class*=send]',
		'div[role=button][class*=submit]',
		'[aria-label*=send]',
		'[aria-label*=发送]',
		'[aria-label*=提交]',
		'button[aria-label*=发送]',
		'div.send-button-container',
		'div[class*=send-button]',
		'svg[class*=send]',
		'span[class*=send]'
	];
	for (var i = 0; i < submitCandidates.length; i++) {
		var el = document.querySelector(submitCandidates[i]);
		if (el) {
			var btn = el.tagName === 'BUTTON' ? el : (el.querySelector('button') || el);
			var rect = btn.getBoundingClientRect();
			if (rect.width > 0 && rect.height > 0 && !btn.disabled) {
				result.submit = submitCandidates[i];
				break;
			}
		}
	}

	if (!result.submit && result.input) {
		var inputEl = document.querySelector(result.input);
		if (inputEl) {
			var parent = inputEl.parentElement;
			for (var depth = 0; depth < 4 && parent; depth++) {
				var btns = parent.querySelectorAll('button, [role=button], div[class*=icon], div[class*=btn]');
				var candidates = [];
				for (var j = 0; j < btns.length; j++) {
					if (btns[j] === inputEl) continue;
					if (btns[j].disabled) continue;
					var rect = btns[j].getBoundingClientRect();
					if (rect.width === 0 || rect.height === 0) continue;
					var cls = (btns[j].getAttribute('class') || '').toLowerCase();
					if (cls.indexOf('toggle') >= 0) continue;
					if (cls.indexOf('setting') >= 0) continue;
					if (cls.indexOf('menu') >= 0) continue;
					if (cls.indexOf('upload') >= 0) continue;
					if (cls.indexOf('file') >= 0) continue;
					candidates.push({el: btns[j], x: rect.x, cls: cls});
				}
				if (candidates.length > 0) {
					candidates.sort(function(a, b) { return b.x - a.x; });
					var best = candidates[0];
					if (best.el.tagName === 'BUTTON') {
						result.submit = best.el.getAttribute('class') ? 'button.' + best.cls.split(' ')[0] : 'button';
					} else {
						result.submit = best.el.getAttribute('class') ? best.el.tagName.toLowerCase() + '.' + best.cls.split(' ')[0] : best.el.tagName.toLowerCase();
					}
					break;
				}
				parent = parent.parentElement;
			}
		}
	}

	var answerCandidates = [
		'[class*=markdown]',
		'[class*=answer]',
		'[class*=message-content]',
		'[class*=assistant-message]',
		'[class*=chat-message]',
		'[class*=response]',
		'[class*=reply]',
		'[class*=ds-markdown]',
		'.message-content',
		'.answer-content',
		'[class*=answer-common-card]',
		'.ds-assistant-message-main-content',
		'[class*=bubble]',
		'[class*=content-card]',
		'article',
		'[class*=flow-markdown]'
	];
	for (var i = 0; i < answerCandidates.length; i++) {
		var els = document.querySelectorAll(answerCandidates[i]);
		if (els.length > 0) {
			var found = false;
			for (var k = 0; k < els.length; k++) {
				var rect = els[k].getBoundingClientRect();
				if (rect.width > 50) {
					result.answer = answerCandidates[i];
					found = true;
					break;
				}
			}
			if (found) break;
		}
	}

	var copyCandidates = [
		'button[class*=copy]',
		'[aria-label*=copy]',
		'[aria-label*=复制]',
		'button[title*=copy]',
		'button[title*=复制]'
	];
	for (var i = 0; i < copyCandidates.length; i++) {
		if (document.querySelector(copyCandidates[i])) {
			result.copy_button = copyCandidates[i];
			break;
		}
	}

	result.content_strategy = 'clipboard';
	return result;
}`
