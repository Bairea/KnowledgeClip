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
	portData, _ := os.ReadFile("./.browser-data/DevToolsActivePort")
	lines := strings.Split(strings.TrimSpace(string(portData)), "\n")
	if len(lines) < 2 {
		log.Fatal("DevToolsActivePort file invalid")
	}
	controlURL := "ws://127.0.0.1:" + strings.TrimSpace(lines[0]) + strings.TrimSpace(lines[1])
	fmt.Println("Connecting to:", controlURL)
	browser := rod.New().ControlURL(controlURL)
	if err := browser.Connect(); err != nil {
		log.Fatal("connect: ", err)
	}
	defer browser.Close()

	pages, err := browser.Pages()
	if err != nil {
		log.Fatal("pages: ", err)
	}

	// Selectors to test per host pattern
	selectorTests := map[string][]string{
		"deepseek": {
			".ds-assistant-message-main-content",
			`[class*=ds-markdown]`,
			`[class*=markdown]`,
			`[class*=message-content]`,
			`[class*=assistant]`,
			`[class*=answer]`,
			`[class*=bubble]`,
			`.ds-message__content`,
			`.ds-message-content`,
			`div[class*="ds-message"]`,
		},
		"chatglm": {
			`[class*=markdown]`,
			`.markdown-body`,
			`[class*=message-content]`,
			`[class*=answer]`,
			`[class*=assistant]`,
			`[class*=bubble]`,
			`[class*=reply]`,
			`.msg-content`,
		},
		"doubao": {
			`.md-box-root`,
			`[class*=markdown]`,
			`[class*=message-content]`,
			`[class*=answer]`,
			`[class*=assistant]`,
			`[class*=bubble]`,
			`[class*=v_list_row]`,
			`[class*=flow-markdown]`,
		},
		"qianwen": {
			`[class*=answer-common-card]`,
			`[class*=markdown]`,
			`[class*=message-content]`,
			`[class*=answer]`,
			`[class*=assistant]`,
			`[class*=bubble]`,
			`[class*=answer-common]`,
		},
	}

	for _, page := range pages {
		info, err := page.Info()
		if err != nil {
			continue
		}
		url := info.URL
		if url == "" || url == "about:blank" || strings.Contains(url, "chrome://") || strings.Contains(url, "devtools://") {
			continue
		}
		fmt.Printf("\n========== Page: %s ==========\n", url)

		// First: test configured selectors
		for hostPattern, selectors := range selectorTests {
			if !strings.Contains(url, hostPattern) {
				continue
			}
			fmt.Printf("--- Selector tests for %s ---\n", hostPattern)
			for _, sel := range selectors {
				selJs := fmt.Sprintf(`() => {
					var els = document.querySelectorAll(%q);
					var info = [];
					for (var i = 0; i < Math.min(els.length, 3); i++) {
						var el = els[i];
						var rect = el.getBoundingClientRect();
						var t = (el.innerText || el.textContent || '').trim();
						info.push({tag: el.tagName, cls: (el.getAttribute('class')||'').substring(0,80), len: t.length, preview: t.substring(0,120), w: Math.round(rect.width), h: Math.round(rect.height), x: Math.round(rect.x), y: Math.round(rect.y)});
					}
					return {count: els.length, samples: info};
				}`, sel)
				r, err := page.Timeout(5 * time.Second).Eval(selJs)
				if err != nil {
					fmt.Printf("  sel=%-45s ERROR: %v\n", sel, err)
					continue
				}
				count := r.Value.Get("count").Int()
				samples := r.Value.Get("samples").Arr()
				fmt.Printf("  sel=%-45s count=%d\n", sel, count)
				for i, s := range samples {
					fmt.Printf("      [%d] %s cls=%q len=%d w=%d h=%d pos=(%d,%d)\n",
						i, s.Get("tag").Str(), s.Get("cls").Str(),
						s.Get("len").Int(), s.Get("w").Int(), s.Get("h").Int(),
						s.Get("x").Int(), s.Get("y").Int())
					if s.Get("len").Int() > 0 {
						fmt.Printf("          text=%q\n", s.Get("preview").Str())
					}
				}
			}
		}

		// Then: dump candidate containers (lower threshold = 80 chars)
		js := `() => {
			var results = {url: location.href, title: document.title, candidates: []};

			function isSidebar(el) {
				var parent = el;
				for (var i = 0; i < 5 && parent; i++) {
					var cls = (parent.getAttribute('class') || '').toLowerCase();
					var id = (parent.getAttribute('id') || '').toLowerCase();
					if (cls.indexOf('sidebar') >= 0 || cls.indexOf('nav') >= 0 ||
						cls.indexOf('menu') >= 0 || cls.indexOf('history') >= 0 ||
						cls.indexOf('aside') >= 0 || id.indexOf('sidebar') >= 0 ||
						id.indexOf('nav') >= 0) return true;
					parent = parent.parentElement;
				}
				return false;
			}

			function hasStructuredContent(el) {
				return el.querySelector('pre, code, table, ul, ol, blockquote, h1, h2, h3, h4, h5, h6') !== null;
			}

			function hasMarkdownClass(el) {
				var cls = (el.getAttribute('class') || '').toLowerCase();
				return cls.indexOf('markdown') >= 0 || cls.indexOf('md-') >= 0 ||
					cls.indexOf('content') >= 0 || cls.indexOf('answer') >= 0 ||
					cls.indexOf('message') >= 0 || cls.indexOf('response') >= 0 ||
					cls.indexOf('reply') >= 0 || cls.indexOf('bubble') >= 0 ||
					cls.indexOf('assistant') >= 0;
			}

			var allEls = document.querySelectorAll('div, article, section');
			var seen = {};
			var candidates = [];

			for (var i = 0; i < allEls.length; i++) {
				var el = allEls[i];
				var cls = el.getAttribute('class') || '';
				var text = (el.innerText || el.textContent || '').trim();
				if (text.length < 80) continue;
				if (isSidebar(el)) continue;

				var rect = el.getBoundingClientRect();
				if (rect.width < 100 || rect.height < 50) continue;

				var structured = hasStructuredContent(el);
				var mdClass = hasMarkdownClass(el);

				if (!structured && !mdClass) continue;

				var key = el.tagName + '|' + cls.substring(0, 60) + '|' + text.length;
				if (seen[key]) continue;
				seen[key] = true;

				var hasPre = el.querySelector('pre') !== null;
				var hasCode = el.querySelector('code') !== null;
				var hasTable = el.querySelector('table') !== null;
				var hasList = el.querySelector('ul, ol') !== null;
				var hasHeading = el.querySelector('h1, h2, h3, h4, h5, h6') !== null;
				var hasBlockquote = el.querySelector('blockquote') !== null;

				var ancestors = [];
				var p = el.parentElement;
				for (var a = 0; a < 3 && p; a++) {
					ancestors.push({
						tag: p.tagName,
						cls: (p.getAttribute('class') || '').substring(0, 80),
						id: (p.getAttribute('id') || '').substring(0, 40),
					});
					p = p.parentElement;
				}

				candidates.push({
					tag: el.tagName,
					cls: cls.substring(0, 120),
					id: (el.getAttribute('id') || '').substring(0, 60),
					textLen: text.length,
					textPreview: text.substring(0, 200),
					structured: structured,
					mdClass: mdClass,
					hasPre: hasPre, hasCode: hasCode, hasTable: hasTable,
					hasList: hasList, hasHeading: hasHeading, hasBlockquote: hasBlockquote,
					x: Math.round(rect.x), y: Math.round(rect.y),
					w: Math.round(rect.width), h: Math.round(rect.height),
					ancestors: ancestors,
				});
			}

			candidates.sort(function(a, b) {
				var aScore = (a.structured ? 1000 : 0) + (a.mdClass ? 100 : 0) + a.textLen;
				var bScore = (b.structured ? 1000 : 0) + (b.mdClass ? 100 : 0) + b.textLen;
				return bScore - aScore;
			});
			results.candidates = candidates.slice(0, 10);
			return results;
		}`

		result, err := page.Timeout(8 * time.Second).Eval(js)
		if err != nil {
			fmt.Printf("Error: %v\n", err)
			continue
		}

		fmt.Printf("Title: %s\n", result.Value.Get("title").Str())
		candidates := result.Value.Get("candidates").Arr()
		fmt.Printf("Found %d candidates:\n", len(candidates))
		for i, c := range candidates {
			fmt.Printf("\n  [%d] %s class=%q id=%q len=%d\n", i,
				c.Get("tag").Str(), c.Get("cls").Str(), c.Get("id").Str(),
				c.Get("textLen").Int())
			fmt.Printf("      structured=%v mdClass=%v pre=%v code=%v table=%v list=%v heading=%v quote=%v\n",
				c.Get("structured").Bool(), c.Get("mdClass").Bool(),
				c.Get("hasPre").Bool(), c.Get("hasCode").Bool(),
				c.Get("hasTable").Bool(), c.Get("hasList").Bool(),
				c.Get("hasHeading").Bool(), c.Get("hasBlockquote").Bool())
			fmt.Printf("      pos=(%d,%d) size=%dx%d\n",
				c.Get("x").Int(), c.Get("y").Int(), c.Get("w").Int(), c.Get("h").Int())
			fmt.Printf("      text=%q\n", c.Get("textPreview").Str())
			ancestors := c.Get("ancestors").Arr()
			for j, a := range ancestors {
				fmt.Printf("      ancestor[%d]: %s class=%q id=%q\n", j,
					a.Get("tag").Str(), a.Get("cls").Str(), a.Get("id").Str())
			}
		}
	}
}
