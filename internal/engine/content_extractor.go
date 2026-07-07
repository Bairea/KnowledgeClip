package engine

import (
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/go-rod/rod"
)

type ContentExtractor interface {
	Extract(page *rod.Page, answerSelector string, beforeCount int, expectedLength int, prompt string) (string, error)
}

type ClipboardExtractor struct {
	CopyButtonSelector string
}

const clipboardOverrideJs = `
	() => {
		window.__capturedMarkdown = '';
		if (navigator.clipboard && navigator.clipboard.writeText) {
			navigator.clipboard.writeText = function(text) {
				window.__capturedMarkdown = text;
				return Promise.resolve();
			};
		}
		document.addEventListener('copy', function(e) {
			var text = '';
			if (e.clipboardData && e.clipboardData.getData) {
				text = e.clipboardData.getData('text/plain');
			}
			if (!text) {
				text = window.getSelection().toString();
			}
			if (text) window.__capturedMarkdown = text;
		});
		var origSetData = DataTransfer.prototype.setData;
		DataTransfer.prototype.setData = function(format, data) {
			if (format === 'text/plain' || format === 'text') {
				window.__capturedMarkdown = data;
			}
			return origSetData.call(this, format, data);
		};
		document.execCommand = function(cmd) {
			if (cmd === 'copy') {
				var text = window.getSelection().toString();
				if (text) window.__capturedMarkdown = text;
			}
			return true;
		};
		return 'ok';
	}
`

func (e *ClipboardExtractor) Extract(page *rod.Page, answerSelector string, beforeCount int, expectedLength int, prompt string) (string, error) {
	if _, err := page.Eval(clipboardOverrideJs); err != nil {
		return "", fmt.Errorf("override clipboard: %w", err)
	}

	if e.CopyButtonSelector != "" {
		content := e.tryClickBySelector(page, e.CopyButtonSelector)
		if len(content) > 100 {
			log.Printf("[rod] clipboard extraction via configured selector succeeded: %d chars", len(content))
			return content, nil
		}
		log.Printf("[rod] configured selector got %d chars, trying generic search", len(content))
	}

	return e.tryGenericCandidates(page, answerSelector, beforeCount, expectedLength, prompt)
}

func (e *ClipboardExtractor) tryClickBySelector(page *rod.Page, selector string) string {
	page.Eval(`() => { window.__capturedMarkdown = ''; }`)

	clickJs := fmt.Sprintf(`
		() => {
			var btn = document.querySelector(%q);
			if (!btn) return 'not found';
			btn.scrollIntoView({block: 'center'});
			btn.click();
			return 'clicked';
		}
	`, selector)
	page.Eval(clickJs)

	time.Sleep(800 * time.Millisecond)

	result, _ := page.Eval(`() => { return window.__capturedMarkdown || ''; }`)
	return result.Value.Str()
}

func (e *ClipboardExtractor) tryGenericCandidates(page *rod.Page, answerSelector string, beforeCount int, expectedLength int, prompt string) (string, error) {
	js := fmt.Sprintf(`
		async () => {
			function isCodeBlockButton(btn) {
				if (btn.closest('pre')) return true;
				if (btn.closest('table')) return true;
				if (btn.closest('figure')) return true;
				if (btn.closest('[class*="code-header"]')) return true;
				if (btn.closest('[class*="code-action"]')) return true;
				if (btn.closest('[class*="code-toolbar"]')) return true;
				if (btn.closest('[class*="code-lang"]')) return true;
				if (btn.closest('[class*="image-viewer"]')) return true;
				if (btn.closest('[class*="image-preview"]')) return true;
				if (btn.closest('[class*="img-zoom"]')) return true;
				if (btn.closest('[class*="zoom-container"]')) return true;
				if (btn.closest('[class*="preview-wrap"]')) return true;
				if (btn.closest('[class*="table-wrap"]')) return true;
				if (btn.closest('[class*="table-scroll"]')) return true;
				if (btn.closest('[class*="chart"]')) return true;
				if (btn.closest('[class*="mermaid"]')) return true;
				var cls = (btn.getAttribute('class') || '').toLowerCase();
				if (cls.indexOf('md-copy') >= 0 || cls.indexOf('code-copy') >= 0 ||
					cls.indexOf('block-copy') >= 0 || cls.indexOf('snippet') >= 0) return true;
				if (cls.indexOf('image') >= 0 || cls.indexOf('picture') >= 0 ||
					cls.indexOf('zoom') >= 0 || cls.indexOf('preview') >= 0 ||
					cls.indexOf('lightbox') >= 0 || cls.indexOf('modal') >= 0 ||
					cls.indexOf('enlarge') >= 0 || cls.indexOf('fullscreen') >= 0 ||
					cls.indexOf('expand') >= 0 || cls.indexOf('scale') >= 0) return true;
				var aria = (btn.getAttribute('aria-label') || '').toLowerCase();
				var title = (btn.getAttribute('title') || '').toLowerCase();
				if (aria.indexOf('zoom') >= 0 || aria.indexOf('preview') >= 0 ||
					aria.indexOf('enlarge') >= 0 || aria.indexOf('expand') >= 0 ||
					aria.indexOf('fullscreen') >= 0 || title.indexOf('zoom') >= 0 ||
					title.indexOf('preview') >= 0 || title.indexOf('enlarge') >= 0) return true;
				var p = btn.parentElement;
				if (p) {
					for (var i = 0; i < p.children.length; i++) {
						if (p.children[i].tagName === 'PRE') return true;
					}
					if (p.nextElementSibling && p.nextElementSibling.tagName === 'PRE') return true;
					if (p.previousElementSibling && p.previousElementSibling.tagName === 'PRE') return true;
				}
				var gp = p ? p.parentElement : null;
				if (gp) {
					if (gp.nextElementSibling && gp.nextElementSibling.tagName === 'PRE') return true;
					if (gp.previousElementSibling && gp.previousElementSibling.tagName === 'PRE') return true;
				}
				return false;
			}

			function isInThinking(el) {
				var parent = el.parentElement;
				for (var i = 0; i < 6 && parent; i++) {
					var pcls = (parent.getAttribute('class') || '').toLowerCase();
					// Only match explicit thinking/reasoning container classes.
					// Avoid broad words like 'mind'/'analysis'/'reflect' that match
					// legitimate answer containers and cause the answer to be filtered.
					if (pcls.indexOf('think-block') >= 0 || pcls.indexOf('think-content') >= 0 ||
						pcls.indexOf('think_process') >= 0 || pcls.indexOf('thinking-block') >= 0 ||
						pcls.indexOf('thinking-content') >= 0 || pcls.indexOf('thinking-process') >= 0 ||
						pcls.indexOf('reasoning-block') >= 0 || pcls.indexOf('reasoning-content') >= 0 ||
						pcls.indexOf('reasoning-text') >= 0 || pcls.indexOf('thought-block') >= 0 ||
						pcls.indexOf('thought-content') >= 0 || pcls.indexOf('thought-process') >= 0 ||
						pcls.indexOf('chain-of-thought') >= 0 || pcls.indexOf('cot-block') >= 0 ||
						pcls.indexOf('inner-mono') >= 0 || pcls.indexOf('deep-think') >= 0 ||
						pcls.indexOf('pre-think') >= 0) {
						return true;
					}
					var ptag = parent.tagName;
					if (ptag === 'DETAILS') {
						var psum = parent.querySelector('summary');
						if (psum) {
							var pst = (psum.textContent || '').toLowerCase();
							if (pst.indexOf('思考') >= 0 || pst.indexOf('think') >= 0) {
								return true;
							}
						}
					}
					parent = parent.parentElement;
				}
				return false;
			}

			function scoreButton(btn) {
				var text = (btn.textContent || btn.innerText || '').trim().toLowerCase();
				var cls = (btn.getAttribute('class') || '').toLowerCase();
				var aria = (btn.getAttribute('aria-label') || '').toLowerCase();
				var title = (btn.getAttribute('title') || '').toLowerCase();
				var rect = btn.getBoundingClientRect();
				if (rect.width === 0 || rect.height === 0) return 0;
				if (cls.indexOf('copyright') >= 0) return 0;
				if (isInThinking(btn)) return 0;

				var score = 0;
				if (text === '\u590d\u5236' || text === 'copy' || text === '\u590d\u5236\u5185\u5bb9') score += 100;
				if (aria.indexOf('copy') >= 0 || aria.indexOf('\u590d\u5236') >= 0) score += 80;
				if (title.indexOf('copy') >= 0 || title.indexOf('\u590d\u5236') >= 0) score += 80;
				if (cls.indexOf('copy') >= 0 && cls.indexOf('copyright') < 0 &&
					cls.indexOf('md-copy') < 0 && cls.indexOf('code-copy') < 0) score += 60;
				if (cls.indexOf('clipboard') >= 0) score += 60;

				if (score === 0) {
					if (cls.indexOf('action') >= 0 || cls.indexOf('toolbar') >= 0 ||
						cls.indexOf('operate') >= 0 || cls.indexOf('footer') >= 0) {
						var svg = btn.querySelector('svg');
						if (svg) {
							var svgCls = (svg.getAttribute('class') || '').toLowerCase();
							if (svgCls.indexOf('copy') >= 0 || svgCls.indexOf('clipboard') >= 0) {
								score += 50;
							} else {
								score += 10;
							}
						}
					}
				}
				return score;
			}

			var allAnswerEls = document.querySelectorAll(%q);
			if (allAnswerEls.length === 0) return {status: 'no answer', text: ''};
			var answerEls = [];
			for (var fi = 0; fi < allAnswerEls.length; fi++) {
				if (!isInThinking(allAnswerEls[fi])) answerEls.push(allAnswerEls[fi]);
			}
			if (answerEls.length === 0) answerEls = Array.prototype.slice.call(allAnswerEls);

			// Target the LAST answer element (the newest). For multi-turn, this is the
			// current turn's answer. Do NOT scan the whole conversation tree, which would
			// pick up copy buttons of previous turns and copy stale content.
			var answerEl = answerEls[answerEls.length - 1];

			// Search copy buttons INSIDE the answer element first, then its immediate
			// message container (up to 2 ancestors). Going further up merges siblings and
			// causes the extractor to click older messages' copy buttons.
			var searchRoots = [answerEl];
			var ancestor = answerEl.parentElement;
			for (var ai = 0; ai < 2 && ancestor; ai++) {
				searchRoots.push(ancestor);
				ancestor = ancestor.parentElement;
			}

			var insideCandidates = [];
			var outsideCandidates = [];
			var seen = {};
			for (var ri = 0; ri < searchRoots.length; ri++) {
				var root = searchRoots[ri];
				var btns = root.querySelectorAll('button, [role="button"], [class*="copy"], [class*="action"], [class*="toolbar"], [class*="operate"]');
				for (var i = 0; i < btns.length; i++) {
					var btn = btns[i];
					if (seen[btn]) continue;
					// skip buttons that belong to a DIFFERENT answer element (older turn)
					if (ri === 0 || answerEl.contains(btn)) {
						seen[btn] = true;
					} else {
						// for ancestor searches, only accept buttons contained in answerEl
						// (already covered above) - skip sibling-message buttons
						continue;
					}
					if (isCodeBlockButton(btn)) continue;
					var score = scoreButton(btn);
					if (score === 0) continue;
					var cls = (btn.getAttribute('class') || '').toLowerCase();
					insideCandidates.push({el: btn, score: score, cls: cls.substring(0, 60)});
				}
			}

			// Only if nothing inside the answer element, look in the single parent that
			// wraps the answer + its action bar (common pattern: action bar is a sibling
			// of the answer content inside one message container).
			if (insideCandidates.length === 0 && answerEl.parentElement) {
				var msgRoot = answerEl.parentElement;
				var msgBtns = msgRoot.querySelectorAll('button, [role="button"], [class*="copy"], [class*="action"], [class*="toolbar"], [class*="operate"]');
				for (var i = 0; i < msgBtns.length; i++) {
					var btn = msgBtns[i];
					if (seen[btn]) continue;
					// skip buttons clearly belonging to a sibling message (older turn)
					if (!msgRoot.contains(answerEl)) continue;
					if (isCodeBlockButton(btn)) continue;
					var score = scoreButton(btn);
					if (score === 0) continue;
					outsideCandidates.push({el: btn, score: score, cls: (btn.getAttribute('class') || '').toLowerCase().substring(0, 60)});
				}
			}

			var candidates = insideCandidates.concat(outsideCandidates);
			candidates.sort(function(a, b) { return b.score - a.score; });

			if (candidates.length === 0) {
				var actionContainers = answerEl.querySelectorAll('[class*="action"], [class*="toolbar"], [class*="operate"], [class*="footer"]');
				for (var i = 0; i < actionContainers.length; i++) {
					if (actionContainers[i].closest('pre')) continue;
					if (isCodeBlockButton(actionContainers[i])) continue;
					var innerBtns = actionContainers[i].querySelectorAll('button, [role="button"], svg, [class*="icon"]');
					if (innerBtns.length > 0) {
						var rect = innerBtns[0].getBoundingClientRect();
						if (rect.width > 0 && rect.height > 0) {
							candidates.push({el: innerBtns[0], score: 1, cls: (innerBtns[0].getAttribute('class') || '').substring(0, 60)});
						}
					}
				}
			}

			if (candidates.length === 0) return {status: 'no candidates', text: ''};

			// Capture the answer element's own text so we can validate the clipboard
			// result actually came from THIS message, not a previous turn.
			var answerOwnText = (answerEl.innerText || answerEl.textContent || '').trim().substring(0, 200);
			var bestText = '';
		var bestInfo = 'none';
		var earlyExitThreshold = Math.max(500, Math.floor(%d * 0.5));
		var promptSnippet = %q;
		function closeOverlaysInline() {
			var closeEls = document.querySelectorAll('[class*="close"], [aria-label*="close"], [class*="close-btn"], [class*="closeBtn"]');
			for (var ci = 0; ci < closeEls.length; ci++) {
				var r = closeEls[ci].getBoundingClientRect();
				if (r.width < 5 || r.height < 5) continue;
				var st = window.getComputedStyle(closeEls[ci]);
				if (st.display === 'none' || st.visibility === 'hidden' || st.opacity === '0') continue;
				try { closeEls[ci].click(); } catch(ce) { closeEls[ci].dispatchEvent(new MouseEvent('click', {bubbles: true})); }
			}
			var overlays = document.querySelectorAll('[class*="modal"], [class*="lightbox"], [class*="enlarge"], [class*="fullscreen"], [class*="zoom-overlay"], [class*="image-viewer"], [class*="preview-wrap"], [role="dialog"]');
			for (var oi = 0; oi < overlays.length; oi++) {
				var st = window.getComputedStyle(overlays[oi]);
				if (st.display === 'none' || st.visibility === 'hidden' || st.opacity === '0') continue;
				var z = parseInt(st.zIndex) || 0;
				if (z > 100) {
					var cb = overlays[oi].querySelector('[class*="close"], button, [aria-label*="close"]');
					if (cb) { try { cb.click(); } catch(ce2) {} }
				}
			}
			for (var ei = 0; ei < 2; ei++) {
				document.dispatchEvent(new KeyboardEvent('keydown', {key: 'Escape', code: 'Escape', keyCode: 27, bubbles: true, cancelable: true}));
				document.dispatchEvent(new KeyboardEvent('keyup', {key: 'Escape', code: 'Escape', keyCode: 27, bubbles: true, cancelable: true}));
			}
		}
		for (var i = 0; i < candidates.length; i++) {
		closeOverlaysInline();
		window.__capturedMarkdown = '';
		candidates[i].el.scrollIntoView({block: 'center'});
		try { candidates[i].el.click(); } catch(e) { candidates[i].el.dispatchEvent(new MouseEvent('click', {bubbles: true})); }

			await new Promise(function(r) { setTimeout(r, 800); });

			var captured = window.__capturedMarkdown || '';
			if (promptSnippet && captured.indexOf(promptSnippet) >= 0 && captured.length < promptSnippet.length + 200) {
				continue;
			}
			if (captured.length > bestText.length) {
				bestText = captured;
				bestInfo = 'success:' + i + ':' + candidates[i].score + ':' + candidates[i].cls;
			}
			if (bestText.length > earlyExitThreshold) break;
		}

		if (bestText.length > 100) {
			return {status: bestInfo, text: bestText, answerOwnText: answerOwnText};
		}

		return {status: 'all failed: ' + candidates.length + ' tried, best=' + bestText.length, text: bestText, answerOwnText: answerOwnText};
	}
	`, answerSelector, expectedLength, prompt)

	result, err := page.Timeout(30 * time.Second).Eval(js)
	if err != nil {
		return "", fmt.Errorf("generic clipboard search: %w", err)
	}

	status := result.Value.Get("status").Str()
	text := result.Value.Get("text").Str()

	if len(text) > 100 && (expectedLength == 0 || len(text) >= expectedLength/2) {
		log.Printf("[rod] clipboard extraction succeeded: %s, %d chars (expected ~%d)", status, len(text), expectedLength)
		return text, nil
	}

	return "", fmt.Errorf("clipboard extraction too short: %s, got %d chars (expected ~%d)", status, len(text), expectedLength)
}

type HtmlToMarkdownExtractor struct{}

// ExtractFromHTML converts a saved HTML string (the outerHTML of an answer
// element captured during streaming) to Markdown. It injects the HTML into a
// hidden temp div and runs the same htmlToMd conversion used by Extract.
// This is used when the live answer element has been recycled by a virtual
// list (e.g. DeepSeek's ds-virtual-list-visible-items) and is no longer in
// the DOM at extraction time.
func (e *HtmlToMarkdownExtractor) ExtractFromHTML(page *rod.Page, html string, prompt string) (string, error) {
	if strings.TrimSpace(html) == "" {
		return "", fmt.Errorf("empty snapshot html")
	}
	encodedHTML, err := json.Marshal(html)
	if err != nil {
		return "", fmt.Errorf("encode snapshot html: %w", err)
	}
	injectJs := fmt.Sprintf(`() => {
		var existing = document.getElementById('__snapshot_answer');
		if (existing) existing.remove();
		var wrapper = document.createElement('div');
		wrapper.id = '__snapshot_answer';
		wrapper.style.cssText = 'position:absolute;left:-9999px;top:0;width:800px;visibility:hidden;';
		wrapper.innerHTML = %s;
		document.body.appendChild(wrapper);
		return 'ok';
	}`, string(encodedHTML))
	if _, err := page.Timeout(5 * time.Second).Eval(injectJs); err != nil {
		return "", fmt.Errorf("inject snapshot html: %w", err)
	}
	defer page.Timeout(2 * time.Second).Eval(`() => { var el = document.getElementById('__snapshot_answer'); if (el) el.remove(); }`)
	return e.Extract(page, "#__snapshot_answer > *", 0, len(html), prompt)
}

func (e *HtmlToMarkdownExtractor) Extract(page *rod.Page, answerSelector string, beforeCount int, expectedLength int, prompt string) (string, error) {
	js := fmt.Sprintf(`
		() => {
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
							cls.indexOf('action') >= 0 || cls.indexOf('code-header') >= 0 ||
							cls.indexOf('table-cap') >= 0 || cls.indexOf('table-label') >= 0 ||
							cls.indexOf('lang-label') >= 0 || cls.indexOf('code-lang') >= 0 ||
							cls.indexOf('code-action') >= 0 || cls.indexOf('header-row') >= 0) {
							continue;
						}
						var ownText = '';
						for (var j = 0; j < child.childNodes.length; j++) {
							if (child.childNodes[j].nodeType === 3) ownText += child.childNodes[j].textContent;
						}
						ownText = ownText.trim().toLowerCase();
						if (ownText.length > 0 && ownText.length < 20 &&
							child.querySelectorAll('p,h1,h2,h3,h4,h5,h6,ul,ol,table,pre,blockquote,div').length === 0) {
							var uiLabels = ['copy','download','\u590d\u5236','\u4e0b\u8f7d','share','\u5206\u4eab','regenerate','\u91cd\u65b0\u751f\u6210',
								'table','\u8868\u683c','python','javascript','java','go','golang','rust','typescript',
								'cpp','c++','c#','c','sql','html','css','bash','shell','sh','json','yaml',
								'xml','markdown','md','text','plain','code','\u4ee3\u7801','typescript','tsx','jsx',
								'kotlin','swift','ruby','php','perl','scala','dart','r','matlab','lua'];
							if (uiLabels.indexOf(ownText) >= 0) continue;
						}

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
						var preCls = child.getAttribute('class') || '';
						var preLang = preCls.match(/language-(\w+)/);
						var codeEl = child.querySelector('code');
						var codeLang = '';
						if (codeEl) {
							var codeCls = codeEl.getAttribute('class') || '';
							var m = codeCls.match(/language-(\w+)/);
							if (m) codeLang = m[1];
						}
						if (!preLang && !codeLang) {
							var allCls = child.getAttribute('class') || '';
							var m2 = allCls.match(/(?:lang|language)-(\w+)/);
							if (m2) codeLang = m2[1];
						}
						if (!codeLang) {
							var dataLang = child.getAttribute('data-language') || (codeEl ? codeEl.getAttribute('data-language') : '') || '';
							if (dataLang) codeLang = dataLang;
						}
						if (!codeLang) {
							var prev = child.previousElementSibling;
							for (var pi = 0; pi < 3 && prev; pi++) {
								var prevText = (prev.innerText || prev.textContent || '').trim().toLowerCase();
								if (/^(python|javascript|js|java|go|golang|rust|typescript|ts|cpp|c\+\+|c#|c|sql|html|css|bash|shell|sh|json|yaml|xml|markdown|md|kotlin|swift|ruby|php|perl|scala|dart|lua|matlab|r)$/.test(prevText)) {
									codeLang = prevText;
									break;
								}
								prev = prev.previousElementSibling;
							}
						}
						var codeSource = codeEl || child;
						var codeClone = codeSource.cloneNode(true);
						var lineNumEls = codeClone.querySelectorAll('[class*="line-number"], [class*="lineno"], [class*="line-num"], [data-line-number]');
						for (var ln = 0; ln < lineNumEls.length; ln++) lineNumEls[ln].remove();
						var codeText = (codeClone.textContent || '').trim();
						if (codeText.length > 0) {
							var codeLines = codeText.split('\n');
							var hasLineNums = codeLines.length > 3;
							for (var li = 0; hasLineNums && li < Math.min(5, codeLines.length); li++) {
								if (!codeLines[li].match(new RegExp('^\\s*' + (li + 1) + '(?![0-9])'))) hasLineNums = false;
							}
							if (hasLineNums) {
								codeText = codeLines.map(function(line) {
									return line.replace(/^\s*\d+(?![0-9])/, '');
								}).join('\n').trim();
							}
						}
						if (!codeLang && codeText.length > 0) {
						if (/^\s*(def |import |from |print\(|class |if __name__)/.test(codeText)) codeLang = 'python';
						else if (/^\s*(function |const |let |var |import |export )/.test(codeText)) codeLang = 'javascript';
						else if (/^\s*(func |package )/.test(codeText)) codeLang = 'go';
						else if (/^\s*(pub fn |fn |use |mod )/.test(codeText)) codeLang = 'rust';
						else if (/^\s*(#include|#define|#ifndef)/.test(codeText)) codeLang = 'cpp';
						else if (/^\s*<\?php/.test(codeText)) codeLang = 'php';
						else if (/\b(graph TD|graph LR|flowchart|sequenceDiagram|classDiagram|stateDiagram|erDiagram|gantt|journey|pie)\b/.test(codeText)) codeLang = 'mermaid';
					}
						result += '\n\x60\x60\x60' + codeLang + '\n' + codeText + '\n\x60\x60\x60\n\n';
						break;
						case 'blockquote': result += '\n> ' + convert(child, depth+1).trim() + '\n\n'; break;
						case 'a':
							var href = child.getAttribute('href') || '';
							var linkText = convert(child, depth+1).trim();
							if (href && linkText) result += '[' + linkText + '](' + href + ')';
							else result += linkText;
							break;
						case 'img':
						var src = child.getAttribute('src') || '';
						var alt = child.getAttribute('alt') || '';
						// Skip base64 data URLs (logos, icons, avatars) - they bloat
						// output with thousands of chars of useless data
						if (src.indexOf('data:') === 0) break;
						if (src) result += '![' + alt + '](' + src + ')';
						break;
						case 'ul': case 'ol':
							var items = child.children;
							for (var k = 0; k < items.length; k++) {
								if (items[k].tagName.toLowerCase() === 'li') {
									var prefix = tag === 'ol' ? (k+1) + '. ' : '- ';
									result += prefix + convert(items[k], depth+1).trim() + '\n';
								}
							}
							result += '\n';
							break;
						case 'li': result += convert(child, depth+1).trim(); break;
						case 'table':
							result += '\n';
							var rows = child.querySelectorAll('tr');
							var headerProcessed = false;
							for (var r = 0; r < rows.length; r++) {
								var cells = rows[r].querySelectorAll('th,td');
								var rowText = '';
								for (var c = 0; c < cells.length; c++) {
									rowText += '| ' + convert(cells[c], depth+1).trim() + ' ';
								}
								rowText += '|\n';
								result += rowText;
								if (!headerProcessed && rows[r].querySelector('th')) {
									var sep = '';
									for (var c = 0; c < cells.length; c++) sep += '| --- ';
									sep += '|\n';
									result += sep;
									headerProcessed = true;
								}
							}
							result += '\n';
							break;
						case 'thead': case 'tbody': case 'tfoot': case 'tr': case 'th': case 'td':
							result += convert(child, depth+1);
							break;
						case 'span': result += convert(child, depth+1); break;
						case 'div': case 'section': case 'article':
						case 'header': case 'footer': case 'main':
						case 'figure': case 'figcaption':
							result += '\n' + convert(child, depth+1) + '\n';
							break;
						default: result += convert(child, depth+1); break;
					}
						}
					}
					return result;
				}
				return convert(el, 0)
					.replace(/\n[ \t]+\n/g, '\n\n')
					.replace(/\n{3,}/g, '\n\n')
					.replace(/^\s*(Table|\u8868\u683c|Python|JavaScript|Java|Go|Golang|Rust|TypeScript|C\+\+|C#|SQL|HTML|CSS|Bash|Shell|JSON|YAML|XML|Markdown|Code|\u4ee3\u7801|Copy|Download|\u590d\u5236|\u4e0b\u8f7d|Share|\u5206\u4eab|Regenerate|\u91cd\u65b0\u751f\u6210|Kotlin|Swift|Ruby|PHP|Perl|Scala|Dart|Lua|Matlab)\s*$/gim, '')
					.replace(/\n{3,}/g, '\n\n')
					.replace(/[ \t]+\n/g, '\n')
					.replace(/\n{3,}/g, '\n\n')
					.trim();
			}

			var els = document.querySelectorAll(%q);
			if (els.length === 0) return {count: 0, text: ''};

			function isInThinking(el) {
				var parent = el.parentElement;
				for (var i = 0; i < 5 && parent; i++) {
					var cls = (parent.getAttribute('class') || '').toLowerCase();
					if (cls.indexOf('think-block') >= 0 || cls.indexOf('think-content') >= 0 ||
						cls.indexOf('think_process') >= 0 || cls.indexOf('thinking-block') >= 0 ||
						cls.indexOf('thinking-content') >= 0 || cls.indexOf('thinking-process') >= 0 ||
						cls.indexOf('reasoning-block') >= 0 || cls.indexOf('reasoning-content') >= 0 ||
						cls.indexOf('reasoning-text') >= 0 || cls.indexOf('thought-block') >= 0 ||
						cls.indexOf('thought-content') >= 0 || cls.indexOf('thought-process') >= 0 ||
						cls.indexOf('chain-of-thought') >= 0 || cls.indexOf('cot-block') >= 0) {
						return true;
					}
					parent = parent.parentElement;
				}
				return false;
			}

			function filterOutNested(els) {
			var result = [];
			for (var i = 0; i < els.length; i++) {
				var isChild = false;
				for (var j = 0; j < els.length; j++) {
					if (i !== j && els[j].contains(els[i])) {
						isChild = true;
						break;
					}
				}
				if (!isChild) result.push(els[i]);
			}
			return result;
		}

		var promptSnippet = %q;
		var startIdx = %d;
		var candidateEls = [];
		for (var i = startIdx; i < els.length; i++) {
			if (isInThinking(els[i])) continue;
			candidateEls.push(els[i]);
		}
		candidateEls = filterOutNested(candidateEls);

		var parts = [];
		for (var i = 0; i < candidateEls.length; i++) {
			var md = htmlToMd(candidateEls[i]).trim();
			var raw = (candidateEls[i].innerText || candidateEls[i].textContent || '').trim();
			if (promptSnippet && raw.indexOf(promptSnippet) >= 0 && raw.length < promptSnippet.length + 200) continue;
			raw = raw.replace(/^\s*(Table|\u8868\u683c|Python|JavaScript|Java|Go|Golang|Rust|TypeScript|C\+\+|C#|SQL|HTML|CSS|Bash|Shell|JSON|YAML|XML|Markdown|Code|\u4ee3\u7801|Copy|Download|\u590d\u5236|\u4e0b\u8f7d|Share|\u5206\u4eab|Regenerate|\u91cd\u65b0\u751f\u6210|Kotlin|Swift|Ruby|PHP|Perl|Scala|Dart|Lua|Matlab|\u8fd0\u884c|\u8fd0\u884c\u8f93\u51fa\u793a\u4f8b|plaintext|Mermaid|Preview|Fullscreen)\s*$/gim, '').replace(/\n{3,}/g, '\n\n').trim();
			if (md.length < raw.length * 0.7 && raw.length > md.length + 50) {
				md = raw;
			}
			if (md) parts.push(md);
		}
		var maxText = parts.join('\n\n');
	var diag = {
		totalEls: els.length,
		startIdx: startIdx,
		candidateEls: candidateEls.length,
		partsCount: parts.length,
		partDetails: parts.map(function(p, i) { return {idx: i, len: p.length}; }),
		fallbackUsed: false,
		skippedFallback: false
	};
	// When startIdx >= totalEls, ALL matched elements existed before the answer was
	// sent (beforeCount too high). This happens when the selector is too broad
	// (matches sidebar/nav) OR the answer rendered inside an existing element
	// (Kimi-style multi-turn). In both cases, blindly taking the last element
	// risks extracting stale/sidebar content (e.g. a logo). Return empty and let
	// the caller fall back to polling text, which is the actual answer.
	if (maxText === '' && els.length > 0 && startIdx < els.length) {
		diag.fallbackUsed = true;
		var fallbackEls = filterOutNested(els);
		diag.fallbackEls = fallbackEls.length;
		for (var fi = fallbackEls.length - 1; fi >= 0; fi--) {
			if (isInThinking(fallbackEls[fi])) continue;
			var md = htmlToMd(fallbackEls[fi]).trim();
			var raw = (fallbackEls[fi].innerText || fallbackEls[fi].textContent || '').trim();
			if (promptSnippet && raw.indexOf(promptSnippet) >= 0 && raw.length < promptSnippet.length + 200) continue;
			raw = raw.replace(/^\s*(Table|\u8868\u683c|Python|JavaScript|Java|Go|Golang|Rust|TypeScript|C\+\+|C#|SQL|HTML|CSS|Bash|Shell|JSON|YAML|XML|Markdown|Code|\u4ee3\u7801|Copy|Download|\u590d\u5236|\u4e0b\u8f7d|Share|\u5206\u4eab|Regenerate|\u91cd\u65b0\u751f\u6210|Kotlin|Swift|Ruby|PHP|Perl|Scala|Dart|Lua|Matlab|\u8fd0\u884c|\u8fd0\u884c\u8f93\u51fa\u793a\u4f8b|plaintext|Mermaid|Preview|Fullscreen)\s*$/gim, '').replace(/\n{3,}/g, '\n\n').trim();
			if (md.length < raw.length * 0.7 && raw.length > md.length + 50) {
				md = raw;
			}
			if (md) {
				maxText = md;
				diag.fallbackFound = true;
				diag.fallbackMdLen = md.length;
				diag.fallbackRawLen = raw.length;
				break;
			}
		}
	} else if (maxText === '' && startIdx >= els.length && els.length > 0) {
		diag.skippedFallback = true;
	}
		if (maxText.length > 50000) maxText = maxText.substring(0, 50000);
		return {count: els.length, text: maxText, diag: diag};
	}
`, answerSelector, prompt, beforeCount)

	result, err := page.Timeout(10 * time.Second).Eval(js)
	if err != nil {
		return "", fmt.Errorf("html2md eval: %w", err)
	}

	text := result.Value.Get("text").Str()
	diag := result.Value.Get("diag")
	totalEls := diag.Get("totalEls").Int()
	startIdx := diag.Get("startIdx").Int()
	candidateEls := diag.Get("candidateEls").Int()
	partsCount := diag.Get("partsCount").Int()
	fallbackUsed := diag.Get("fallbackUsed").Bool()
	if text == "" {
		partDetails := diag.Get("partDetails").Arr()
		partSummary := make([]string, len(partDetails))
		for i, p := range partDetails {
			partSummary[i] = fmt.Sprintf("[%d]=%d", p.Get("idx").Int(), p.Get("len").Int())
		}
		skippedFallback := diag.Get("skippedFallback").Bool()
		log.Printf("[rod] html2md returned EMPTY: totalEls=%d startIdx=%d candidateEls=%d partsCount=%d fallbackUsed=%v skippedFallback=%v fallbackEls=%d parts=[%s]",
			totalEls, startIdx, candidateEls, partsCount, fallbackUsed, skippedFallback, diag.Get("fallbackEls").Int(), strings.Join(partSummary, ", "))
		if skippedFallback {
			log.Printf("[rod] html2md: startIdx(%d) >= totalEls(%d), selector matches only pre-existing elements - returning empty to avoid stale content", startIdx, totalEls)
		}
		return "", fmt.Errorf("html2md returned empty text (totalEls=%d startIdx=%d candidateEls=%d skippedFallback=%v)", totalEls, startIdx, candidateEls, skippedFallback)
	}

	log.Printf("[rod] html2md extraction succeeded: %d chars (totalEls=%d startIdx=%d candidateEls=%d parts=%d fallback=%v)", len(text), totalEls, startIdx, candidateEls, partsCount, fallbackUsed)
	return text, nil
}

type HybridExtractor struct {
	Primary  ContentExtractor
	Fallback ContentExtractor
}

func (e *HybridExtractor) Extract(page *rod.Page, answerSelector string, beforeCount int, expectedLength int, prompt string) (string, error) {
	text, err := e.Primary.Extract(page, answerSelector, beforeCount, expectedLength, prompt)
	if err == nil && len(text) > 50 {
		return text, nil
	}
	if err != nil {
		log.Printf("[rod] primary extraction failed: %v, trying fallback", err)
	} else {
		log.Printf("[rod] primary extraction too short (%d chars), trying fallback", len(text))
	}
	return e.Fallback.Extract(page, answerSelector, beforeCount, expectedLength, prompt)
}

func NewContentExtractor(strategy string, copyButtonSelector string) ContentExtractor {
	html2md := &HtmlToMarkdownExtractor{}
	if strategy == "clipboard" {
		return &HybridExtractor{
			Primary:  &ClipboardExtractor{CopyButtonSelector: copyButtonSelector},
			Fallback: html2md,
		}
	}
	return html2md
}
