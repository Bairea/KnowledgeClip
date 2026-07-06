package main

import (
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"github.com/go-rod/rod"
)

func main() {
	if len(os.Args) < 2 {
		log.Fatal("Usage: dumpbuttons <url-pattern>")
	}
	pattern := os.Args[1]

	portData, _ := os.ReadFile("./.browser-data/DevToolsActivePort")
	lines := strings.Split(strings.TrimSpace(string(portData)), "\n")
	if len(lines) < 2 {
		log.Fatal("DevToolsActivePort file invalid")
	}
	controlURL := "ws://127.0.0.1:" + strings.TrimSpace(lines[0]) + strings.TrimSpace(lines[1])
	fmt.Printf("Connecting to: %s\nLooking for pages matching: %s\n", controlURL, pattern)

	browser := rod.New().ControlURL(controlURL)
	if err := browser.Connect(); err != nil {
		log.Fatal("connect: ", err)
	}
	defer browser.Close()

	pages, err := browser.Pages()
	if err != nil {
		log.Fatal("pages: ", err)
	}

	for _, page := range pages {
		info, err := page.Info()
		if err != nil {
			continue
		}
		url := info.URL
		if !strings.Contains(url, pattern) {
			continue
		}
		if strings.Contains(url, "devtools://") || strings.Contains(url, "chrome://") {
			continue
		}
		fmt.Printf("\n=== Page: %s ===\n", url)

		// Dump all buttons and clickable elements
		btnJs := `() => {
			var results = [];
			var btns = document.querySelectorAll('button, [role="button"], [class*="send"], [class*="submit"], [class*="btn"], [aria-label*="发送"], [aria-label*="send"], div[class*="icon"], svg[class*="icon"]');
			for (var i = 0; i < btns.length; i++) {
				var btn = btns[i];
				var rect = btn.getBoundingClientRect();
				if (rect.width === 0 || rect.height === 0) continue;
				var cls = (btn.getAttribute('class') || '').substring(0, 80);
				var aria = btn.getAttribute('aria-label') || '';
				var title = btn.getAttribute('title') || '';
				var text = (btn.innerText || btn.textContent || '').trim().substring(0, 30);
				var tag = btn.tagName;
				var type = btn.getAttribute('type') || '';
				var disabled = btn.disabled || (cls.indexOf('disabled') >= 0);
				results.push({tag: tag, cls: cls, aria: aria, title: title, text: text, type: type, disabled: disabled, x: Math.round(rect.x), y: Math.round(rect.y), w: Math.round(rect.width), h: Math.round(rect.height)});
			}
			return results;
		}`
		r, err := page.Timeout(5 * time.Second).Eval(btnJs)
		if err != nil {
			fmt.Printf("Error: %v\n", err)
			continue
		}
		arr := r.Value.Arr()
		fmt.Printf("Found %d clickable elements:\n", len(arr))
		for i, v := range arr {
			disabledStr := ""
			if v.Get("disabled").Bool() {
				disabledStr = " [DISABLED]"
			}
			fmt.Printf("  [%d] %s cls=%q aria=%q title=%q text=%q type=%q%s pos=(%d,%d) size=%dx%d\n",
				i, v.Get("tag").Str(), v.Get("cls").Str(), v.Get("aria").Str(),
				v.Get("title").Str(), v.Get("text").Str(), v.Get("type").Str(),
				disabledStr,
				v.Get("x").Int(), v.Get("y").Int(),
				v.Get("w").Int(), v.Get("h").Int())
		}

		// Also dump the textarea's parent structure
		parentJs := `() => {
			var ta = document.querySelector('textarea');
			if (!ta) return {found: false};
			var results = [];
			var el = ta;
			for (var i = 0; i < 5 && el; i++) {
				var rect = el.getBoundingClientRect();
				results.push({
					level: i,
					tag: el.tagName,
					cls: (el.getAttribute('class') || '').substring(0, 80),
					id: el.id || '',
					childCount: el.children.length,
					x: Math.round(rect.x), y: Math.round(rect.y),
					w: Math.round(rect.width), h: Math.round(rect.height)
				});
				el = el.parentElement;
			}
			return {found: true, parents: results};
		}`
		pr, err := page.Timeout(5 * time.Second).Eval(parentJs)
		if err != nil {
			fmt.Printf("Parent error: %v\n", err)
			continue
		}
		if !pr.Value.Get("found").Bool() {
			fmt.Println("No textarea found")
			continue
		}
		parents := pr.Value.Get("parents").Arr()
		fmt.Println("\nTextarea parent hierarchy:")
		for _, p := range parents {
			fmt.Printf("  L%d: %s cls=%q id=%q children=%d pos=(%d,%d) size=%dx%d\n",
				p.Get("level").Int(), p.Get("tag").Str(), p.Get("cls").Str(),
				p.Get("id").Str(), p.Get("childCount").Int(),
				p.Get("x").Int(), p.Get("y").Int(),
				p.Get("w").Int(), p.Get("h").Int())
		}
	}
}
