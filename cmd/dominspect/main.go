package main

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/go-rod/rod"
	"github.com/go-rod/rod/lib/launcher"
	"github.com/go-rod/rod/lib/proto"
)

func main() {
	if len(os.Args) < 2 {
		log.Fatal("Usage: dominspect <url> [mode]\n  mode: inspect (default) | monitor")
	}
	targetURL := os.Args[1]
	mode := "inspect"
	if len(os.Args) >= 3 {
		mode = os.Args[2]
	}

	userDataDir := "./.browser-data"
	controlURL := connectOrLaunch(userDataDir)
	fmt.Printf("Connected to Chrome: %s\n", controlURL)

	browser := rod.New().ControlURL(controlURL)
	if err := browser.Connect(); err != nil {
		log.Fatal("connect: ", err)
	}

	page, err := browser.Page(proto.TargetCreateTarget{URL: targetURL})
	if err != nil {
		log.Fatal("create page: ", err)
	}

	fmt.Printf("Navigated to: %s\n", targetURL)
	page.Timeout(30 * time.Second).WaitLoad()
	fmt.Printf("Page loaded, waiting 3s for SPA rendering...\n")
	time.Sleep(3 * time.Second)

	info, _ := page.Info()
	fmt.Printf("Page title: %s\n", info.Title)
	fmt.Printf("Page URL: %s\n", info.URL)

	switch mode {
	case "monitor":
		monitorDOM(page, 120)
	case "send":
		prompt := ""
		if len(os.Args) >= 4 {
			prompt = os.Args[3]
		}
		if prompt == "" {
			log.Fatal("send mode requires prompt as 3rd argument")
		}
		sendAndMonitor(page, prompt, 120)
	case "inspect":
		inspectDOM(page)
	default:
		inspectDOM(page)
	}
}

func connectOrLaunch(userDataDir string) string {
	portData, err := os.ReadFile(filepath.Join(userDataDir, "DevToolsActivePort"))
	if err == nil {
		lines := strings.Split(strings.TrimSpace(string(portData)), "\n")
		if len(lines) >= 2 {
			port := strings.TrimSpace(lines[0])
			path := strings.TrimSpace(lines[1])
			url := "ws://127.0.0.1:" + port + path
			browser := rod.New().ControlURL(url)
			if connectErr := browser.Connect(); connectErr == nil {
				if _, pagesErr := browser.Pages(); pagesErr == nil {
					return url
				}
			}
		}
	}

	l := launcher.New()
	chromePath := findChrome()
	if chromePath != "" {
		l = l.Bin(chromePath)
	}
	l = l.UserDataDir(userDataDir)
	l = l.Headless(false).Devtools(true)
	l = l.Delete("enable-automation")
	l = l.Set("disable-blink-features", "AutomationControlled")
	l = l.Set("no-first-run", "true")
	l = l.Set("no-default-browser-check", "true")

	url, err := l.Launch()
	if err != nil {
		log.Fatal("launch: ", err)
	}
	time.Sleep(2 * time.Second)
	return url
}

func findChrome() string {
	candidates := []string{
		`C:\Program Files\Google\Chrome\Application\chrome.exe`,
		`C:\Program Files (x86)\Google\Chrome\Application\chrome.exe`,
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

func inspectDOM(page *rod.Page) {
	fmt.Println("\n=== DOM Structure (elements with substantial text) ===")
	inspectJs := `() => {
		var results = [];
		var allEls = document.querySelectorAll('div, section, article, pre, [class*=markdown], [class*=answer], [class*=response], [class*=content], [class*=message], [class*=bubble], [class*=reply], [class*=chat], [class*=thread]');
		for (var i = 0; i < allEls.length; i++) {
			var el = allEls[i];
			var cls = (el.getAttribute('class') || '').toLowerCase();
			if (cls.indexOf('sidebar') >= 0 || cls.indexOf('nav') >= 0 || cls.indexOf('menu') >= 0 || cls.indexOf('history') >= 0) continue;
			var parent = el.parentElement;
			var skip = false;
			for (var j = 0; j < 3 && parent; j++) {
				var pcls = (parent.getAttribute('class') || '').toLowerCase();
				if (pcls.indexOf('sidebar') >= 0 || pcls.indexOf('nav') >= 0 || pcls.indexOf('menu') >= 0) { skip = true; break; }
				parent = parent.parentElement;
			}
			if (skip) continue;
			var text = (el.innerText || el.textContent || '').trim();
			if (text.length < 10) continue;
			var rect = el.getBoundingClientRect();
			if (rect.width < 50 || rect.height < 5) continue;
			var hasBlock = false;
			for (var c = 0; c < el.children.length; c++) {
				var ctag = el.children[c].tagName;
				if (ctag === 'DIV' || ctag === 'P' || ctag === 'ARTICLE' || ctag === 'SECTION' || ctag === 'UL' || ctag === 'OL' || ctag === 'PRE' || ctag === 'BLOCKQUOTE' || ctag === 'TABLE') { hasBlock = true; break; }
			}
			results.push({
				tag: el.tagName,
				cls: (el.getAttribute('class') || '').substring(0, 150),
				id: el.id || '',
				len: text.length,
				preview: text.substring(0, 200),
				x: Math.round(rect.x),
				y: Math.round(rect.y),
				w: Math.round(rect.width),
				h: Math.round(rect.height),
				children: el.children.length,
				hasBlock: hasBlock
			});
		}
		results.sort(function(a, b) { return b.len - a.len; });
		return JSON.stringify(results.slice(0, 30));
	}`
	if res, err := page.Timeout(5 * time.Second).Eval(inspectJs); err == nil {
		var results []map[string]interface{}
		if json.Unmarshal([]byte(res.Value.Str()), &results) == nil {
			for i, r := range results {
				fmt.Printf("  [%d] tag=%v cls=%v id=%v len=%v children=%v block=%v pos=(%v,%v) size=%vx%v\n  text=%v\n\n",
					i, r["tag"], r["cls"], r["id"], r["len"], r["children"],
					r["hasBlock"], r["x"], r["y"], r["w"], r["h"], r["preview"])
			}
		}
	}

	fmt.Println("\n=== Selector Tests ===")
	selectors := []string{
		`.ds-markdown`,
		`.ds-markdown--block`,
		`.ds-assistant-message-main-content`,
		`[class*=ds-markdown]`,
		`[class*=ds-message]`,
		`.answer-content`,
		`[class*=answer-content]`,
		`[class*=markdown]`,
		`[class*=message-content]`,
		`[class*=response]`,
		`[class*=assistant]`,
		`[class*=think]`,
		`[class*=reasoning]`,
		`[class*=thought]`,
		`[class*=bubble]`,
		`[class*=reply]`,
		`[class*=chat-content]`,
		`[class*=thread]`,
		`textarea`,
		`[contenteditable=true]`,
		`div[class*=send]`,
		`button[class*=send]`,
		`button[aria-label*=send]`,
		`[aria-label*=发送]`,
		`div.enter-icon-container`,
	}
	for _, sel := range selectors {
		testSelector(page, sel)
	}

	fmt.Println("\n=== Input Elements ===")
	inputJs := `() => {
		var results = [];
		var inputs = document.querySelectorAll('textarea, input[type=text], [contenteditable=true], [role=textbox]');
		for (var i = 0; i < inputs.length; i++) {
			var el = inputs[i];
			var rect = el.getBoundingClientRect();
			results.push({
				tag: el.tagName,
				cls: (el.getAttribute('class') || '').substring(0, 100),
				id: el.id || '',
				role: el.getAttribute('role') || '',
				contenteditable: el.getAttribute('contenteditable') || '',
				placeholder: el.getAttribute('placeholder') || '',
				visible: rect.width > 0 && rect.height > 0,
				pos: '(' + Math.round(rect.x) + ',' + Math.round(rect.y) + ')',
				size: Math.round(rect.width) + 'x' + Math.round(rect.height)
			});
		}
		return JSON.stringify(results);
	}`
	if res, err := page.Timeout(3 * time.Second).Eval(inputJs); err == nil {
		var results []map[string]interface{}
		if json.Unmarshal([]byte(res.Value.Str()), &results) == nil {
			for i, r := range results {
				fmt.Printf("  [%d] %v\n", i, r)
			}
		}
	}

	fmt.Println("\n=== Button Elements ===")
	btnJs := `() => {
		var results = [];
		var btns = document.querySelectorAll('button, [role=button], [class*=send], [class*=submit], [class*=btn]');
		for (var i = 0; i < btns.length; i++) {
			var el = btns[i];
			var rect = el.getBoundingClientRect();
			if (rect.width === 0 || rect.height === 0) continue;
			var text = (el.textContent || el.innerText || '').trim().substring(0, 50);
			var aria = el.getAttribute('aria-label') || '';
			var cls = (el.getAttribute('class') || '').substring(0, 100);
			results.push({tag: el.tagName, cls: cls, text: text, aria: aria, pos: '('+Math.round(rect.x)+','+Math.round(rect.y)+')', size: Math.round(rect.width)+'x'+Math.round(rect.height)});
		}
		return JSON.stringify(results.slice(0, 20));
	}`
	if res, err := page.Timeout(3 * time.Second).Eval(btnJs); err == nil {
		var results []map[string]interface{}
		if json.Unmarshal([]byte(res.Value.Str()), &results) == nil {
			for i, r := range results {
				fmt.Printf("  [%d] %v\n", i, r)
			}
		}
	}
}

func monitorDOM(page *rod.Page, durationSec int) {
	deadline := time.Now().Add(time.Duration(durationSec) * time.Second)
	tick := 0
	for time.Now().Before(deadline) {
		tick++
		fmt.Printf("\n=== tick %d (%s) ===\n", tick, time.Now().Format("15:04:05"))
		testSelector(page, `.ds-markdown`)
		testSelector(page, `.ds-assistant-message-main-content`)
		testSelector(page, `[class*=ds-markdown]`)
		testSelector(page, `.answer-content`)
		testSelector(page, `[class*=markdown]`)
		testSelector(page, `[class*=think]`)
		time.Sleep(2 * time.Second)
	}
}

func sendAndMonitor(page *rod.Page, prompt string, durationSec int) {
	fmt.Printf("Typing prompt: %s\n", prompt)

	typeJs := fmt.Sprintf(`() => {
		var ta = document.querySelector('textarea');
		if (!ta) return 'no textarea';
		var nativeSetter = Object.getOwnPropertyDescriptor(window.HTMLTextAreaElement.prototype, 'value').set;
		nativeSetter.call(ta, %q);
		ta.dispatchEvent(new Event('input', {bubbles: true}));
		ta.dispatchEvent(new Event('change', {bubbles: true}));
		return 'typed: ' + ta.value.substring(0, 50);
	}`, prompt)
	if res, err := page.Timeout(5 * time.Second).Eval(typeJs); err == nil {
		fmt.Printf("Type result: %s\n", res.Value.Str())
	} else {
		fmt.Printf("Type error: %v\n", err)
		return
	}

	time.Sleep(1 * time.Second)

	fmt.Println("Clicking send button...")
	clickJs := `() => {
		var btns = document.querySelectorAll('div[class*="ds-button"]');
		for (var i = 0; i < btns.length; i++) {
			var cls = btns[i].getAttribute('class') || '';
			if (cls.indexOf('ds-button--primary') >= 0 && cls.indexOf('ds-button--filled') >= 0) {
				btns[i].click();
				return 'clicked: ' + cls.substring(0, 80);
			}
		}
		var allBtns = document.querySelectorAll('div[class*="ds-button--primary"]');
		if (allBtns.length > 0) {
			allBtns[allBtns.length-1].click();
			return 'clicked last primary: ' + (allBtns[allBtns.length-1].getAttribute('class')||'').substring(0, 80);
		}
		return 'no send button found';
	}`
	if res, err := page.Timeout(5 * time.Second).Eval(clickJs); err == nil {
		fmt.Printf("Click result: %s\n", res.Value.Str())
	} else {
		fmt.Printf("Click error: %v\n", err)
		return
	}

	fmt.Println("Monitoring DOM for 120s...")
	deadline := time.Now().Add(time.Duration(durationSec) * time.Second)
	tick := 0
	for time.Now().Before(deadline) {
		tick++
		fmt.Printf("\n=== tick %d (%s) ===\n", tick, time.Now().Format("15:04:05"))

		// Get all potentially relevant selectors
		selectors := []string{
			`.ds-markdown`,
			`.ds-assistant-message-main-content`,
			`[class*=ds-markdown]`,
			`[class*=ds-message]`,
			`[class*=ds-bubble]`,
			`[class*=answer]`,
			`[class*=response]`,
			`[class*=markdown]`,
			`[class*=message]`,
			`[class*=think]`,
			`[class*=reasoning]`,
			`[class*=thought]`,
			`[class*=bubble]`,
			`[class*=reply]`,
			`[class*=chat-item]`,
			`[class*=conversation]`,
		}
		for _, sel := range selectors {
			testSelector(page, sel)
		}

		// Also dump the full DOM structure at key ticks
		if tick == 1 || tick == 5 || tick == 10 || tick == 15 || tick == 20 || tick == 30 || tick == 40 || tick == 50 {
			dumpDOM(page, tick)
		}

		time.Sleep(2 * time.Second)
	}
}

func dumpDOM(page *rod.Page, tick int) {
	fmt.Printf("\n--- DOM dump at tick %d ---\n", tick)
	inspectJs := `() => {
		var results = [];
		var allEls = document.querySelectorAll('div, section, article, pre, [class*=markdown], [class*=answer], [class*=response], [class*=message], [class*=bubble], [class*=reply]');
		for (var i = 0; i < allEls.length; i++) {
			var el = allEls[i];
			var cls = (el.getAttribute('class') || '').toLowerCase();
			if (cls.indexOf('sidebar') >= 0 || cls.indexOf('nav') >= 0 || cls.indexOf('menu') >= 0 || cls.indexOf('history') >= 0) continue;
			var text = (el.innerText || el.textContent || '').trim();
			if (text.length < 20) continue;
			var rect = el.getBoundingClientRect();
			if (rect.width < 50 || rect.height < 5) continue;
			results.push({
				tag: el.tagName,
				cls: (el.getAttribute('class') || '').substring(0, 150),
				len: text.length,
				preview: text.substring(0, 200),
				x: Math.round(rect.x),
				y: Math.round(rect.y),
				w: Math.round(rect.width),
				h: Math.round(rect.height),
				children: el.children.length
			});
		}
		results.sort(function(a, b) { return b.len - a.len; });
		return JSON.stringify(results.slice(0, 15));
	}`
	if res, err := page.Timeout(5 * time.Second).Eval(inspectJs); err == nil {
		var results []map[string]interface{}
		if json.Unmarshal([]byte(res.Value.Str()), &results) == nil {
			for i, r := range results {
				fmt.Printf("  [%d] tag=%v cls=%v len=%v children=%v pos=(%v,%v) size=%vx%v\n  text=%v\n",
					i, r["tag"], r["cls"], r["len"], r["children"],
					r["x"], r["y"], r["w"], r["h"], r["preview"])
			}
		}
	}
	fmt.Printf("--- end DOM dump ---\n")
}

func testSelector(page *rod.Page, sel string) {
	selJs := fmt.Sprintf(`() => {
		var els = document.querySelectorAll(%q);
		if (els.length === 0) return null;
		var info = [];
		for (var i = 0; i < Math.min(els.length, 5); i++) {
			var el = els[i];
			var t = (el.innerText || el.textContent || '').trim();
			var rect = el.getBoundingClientRect();
			info.push({tag: el.tagName, cls: (el.getAttribute('class')||'').substring(0,120), len: t.length, preview: t.substring(0,100), visible: rect.width>0&&rect.height>0, pos: '('+Math.round(rect.x)+','+Math.round(rect.y)+')'});
		}
		return {count: els.length, samples: info};
	}`, sel)
	if r, err := page.Timeout(3 * time.Second).Eval(selJs); err == nil {
		count := r.Value.Get("count").Int()
		if count > 0 {
			fmt.Printf("SELECTOR %q -> count=%d\n", sel, count)
			samples := r.Value.Get("samples").Arr()
			for i, s := range samples {
				fmt.Printf("  [%d] %s cls=%q len=%d vis=%v pos=%s text=%q\n",
					i, s.Get("tag").Str(), s.Get("cls").Str(),
					s.Get("len").Int(), s.Get("visible").Bool(),
					s.Get("pos").Str(), s.Get("preview").Str())
			}
		}
	}
}
