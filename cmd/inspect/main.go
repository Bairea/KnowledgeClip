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
		log.Fatal("Usage: inspect <url-pattern> [duration-seconds]")
	}
	pattern := os.Args[1]
	duration := 30
	if len(os.Args) >= 3 {
		fmt.Sscanf(os.Args[2], "%d", &duration)
	}

	portData, _ := os.ReadFile("./.browser-data/DevToolsActivePort")
	lines := strings.Split(strings.TrimSpace(string(portData)), "\n")
	if len(lines) < 2 {
		log.Fatal("DevToolsActivePort file invalid")
	}
	controlURL := "ws://127.0.0.1:" + strings.TrimSpace(lines[0]) + strings.TrimSpace(lines[1])
	fmt.Printf("Connecting to: %s\nLooking for pages matching: %s\nDuration: %ds\n", controlURL, pattern, duration)

	browser := rod.New().ControlURL(controlURL)
	if err := browser.Connect(); err != nil {
		log.Fatal("connect: ", err)
	}
	defer browser.Close()

	// Selectors to test
	selectors := []string{
		`.ds-assistant-message-main-content`,
		`[class*=ds-markdown]`,
		`[class*=markdown]`,
		`[class*=message-content]`,
		`[class*=assistant]`,
		`[class*=answer]`,
		`[class*=bubble]`,
		`.ds-message__content`,
		`div[class*="ds-message"]`,
		`[class*=md-box-root]`,
		`[class*=flow-markdown]`,
		`[class*=prose]`,
		`[class*=rich-text]`,
		`[class*=text-content]`,
		`[class*=content-card]`,
		`[class*=chat-content]`,
		`[class*=markdown-body]`,
		`[class*=md-content]`,
		`[class*=md-body]`,
		`[class*=reply]`,
		`[class*=response]`,
		`div[class*="9a9a"]`,
	}

	deadline := time.Now().Add(time.Duration(duration) * time.Second)
	tick := 0
	for time.Now().Before(deadline) {
		tick++
		pages, err := browser.Pages()
		if err != nil {
			fmt.Printf("tick %d: pages error: %v\n", tick, err)
			time.Sleep(500 * time.Millisecond)
			continue
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

			// Test all selectors
			foundAny := false
			for _, sel := range selectors {
				selJs := fmt.Sprintf(`() => {
					var els = document.querySelectorAll(%q);
					if (els.length === 0) return null;
					var info = [];
					for (var i = 0; i < Math.min(els.length, 2); i++) {
						var el = els[i];
						var rect = el.getBoundingClientRect();
						var t = (el.innerText || el.textContent || '').trim();
						info.push({tag: el.tagName, cls: (el.getAttribute('class')||'').substring(0,80), len: t.length, preview: t.substring(0,100), w: Math.round(rect.width), h: Math.round(rect.height), x: Math.round(rect.x), y: Math.round(rect.y)});
					}
					return {count: els.length, samples: info};
				}`, sel)
				r, err := page.Timeout(3 * time.Second).Eval(selJs)
				if err != nil {
					continue
				}
				count := r.Value.Get("count").Int()
				if count == 0 {
					continue
				}
				foundAny = true
				samples := r.Value.Get("samples").Arr()
				ts := time.Now().Format("15:04:05.000")
				fmt.Printf("[%s tick=%d] sel=%q count=%d\n", ts, tick, sel, count)
				for i, s := range samples {
					fmt.Printf("  [%d] %s cls=%q len=%d w=%d h=%d pos=(%d,%d) text=%q\n",
						i, s.Get("tag").Str(), s.Get("cls").Str(),
						s.Get("len").Int(), s.Get("w").Int(), s.Get("h").Int(),
						s.Get("x").Int(), s.Get("y").Int(), s.Get("preview").Str())
				}
			}

			if !foundAny {
				// Dump all visible leaf elements with text > 30 chars
				leafJs := `() => {
					var results = [];
					var allEls = document.querySelectorAll('div, p, span, article, section, pre, td, li');
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
						if (text.length < 30 || text.length > 5000) continue;
						var rect = el.getBoundingClientRect();
						if (rect.width < 100 || rect.height < 10) continue;
						if (rect.x < 0) continue;
						var hasBlockChild = false;
						for (var c = 0; c < el.children.length; c++) {
							var ctag = el.children[c].tagName;
							if (ctag === 'DIV' || ctag === 'P' || ctag === 'ARTICLE' || ctag === 'SECTION' || ctag === 'UL' || ctag === 'OL' || ctag === 'PRE' || ctag === 'BLOCKQUOTE') { hasBlockChild = true; break; }
						}
						if (hasBlockChild) continue;
						results.push({tag: el.tagName, cls: (el.getAttribute('class') || '').substring(0, 80), len: text.length, preview: text.substring(0, 80), x: Math.round(rect.x), y: Math.round(rect.y), w: Math.round(rect.width)});
					}
					results.sort(function(a, b) { return a.y - b.y; });
					return results.slice(0, 8);
				}`
				if leafRes, leafErr := page.Timeout(3 * time.Second).Eval(leafJs); leafErr == nil {
					arr := leafRes.Value.Arr()
					if len(arr) > 0 {
						ts := time.Now().Format("15:04:05.000")
						fmt.Printf("[%s tick=%d] NO SELECTOR MATCHED, but %d leaf elements with text:\n", ts, tick, len(arr))
						for i, v := range arr {
							fmt.Printf("  leaf[%d]: tag=%s cls=%q len=%d pos=(%d,%d) w=%d text=%q\n",
								i, v.Get("tag").Str(), v.Get("cls").Str(),
								v.Get("len").Int(), v.Get("x").Int(), v.Get("y").Int(),
								v.Get("w").Int(), v.Get("preview").Str())
						}
					}
				}
			}
		}
		time.Sleep(500 * time.Millisecond)
	}
	fmt.Printf("Done after %d ticks\n", tick)
}
