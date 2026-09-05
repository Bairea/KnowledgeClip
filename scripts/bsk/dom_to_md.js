// dom_to_md.js — DOM → Markdown converter executed via `bsk evaluate`.
//
// This is the extraction logic of the bsk PoC: it runs entirely inside the
// page via `bsk evaluate`, takes a CSS selector, and returns the element's
// content as Markdown. No LLM is involved anywhere in the pipeline.
//
// It mirrors the htmlToMd conversion the rod engine carries in
// internal/engine/content_extractor.go, deliberately kept self-contained so
// it can be embedded in a single `bsk evaluate` expression.

function htmlToMdEl(root) {
	function esc(s) { return (s || '').replace(/\|/g, '\\|'); }
function isNoise(node) {
	var tag = node.tagName.toLowerCase();
	if (tag === 'svg' || tag === 'path') return true;
	if (tag === 'button' || tag === 'a' || (node.getAttribute && node.getAttribute('role') === 'button')) return true;
	var cls = (node.getAttribute('class') || '').toLowerCase();
	// UI chrome: copy/download/toolbar chrome, code headers, language labels,
	// table captions. A chrome-named *container* that actually holds a code
	// block or table is content, not chrome — e.g. GitHub wraps each fenced
	// block in div.snippet-clipboard-content together with its copy button.
	var chrome = ['copy', 'download', 'clipboard', 'toolbar', 'code-header',
		'lang-label', 'code-lang', 'code-action', 'table-cap', 'table-label'];
	for (var i = 0; i < chrome.length; i++) {
		if (cls.indexOf(chrome[i]) >= 0) {
			if (node.querySelector && node.querySelector('pre, code, table')) return false;
			return true;
		}
	}
	// Thinking/reasoning blocks: filtered unconditionally so answer-site
	// reasoning text never leaks into the extracted answer.
	var alwaysNoise = ['think', 'advance-thinking', 'reasoning',
		'interact-container', 'assistant-name'];
	for (var j = 0; j < alwaysNoise.length; j++) {
		if (cls.indexOf(alwaysNoise[j]) >= 0) return true;
	}
	return false;
}

	function convert(node, depth) {
		if (depth > 15) return '';
		var out = '';
		for (var i = 0; i < node.childNodes.length; i++) {
			var child = node.childNodes[i];
			if (child.nodeType === 3) {
				out += child.textContent.replace(/\s+/g, ' ');
				continue;
			}
			if (child.nodeType !== 1) continue;
			if (isNoise(child)) continue;
			var tag = child.tagName.toLowerCase();
			switch (tag) {
				case 'h1': case 'h2': case 'h3': case 'h4': case 'h5': case 'h6': {
					var lvl = parseInt(tag[1], 10);
					var line = '';
					for (var h = 0; h < lvl; h++) line += '#';
					out += '\n' + line + ' ' + convert(child, depth + 1).trim() + '\n\n';
					break;
				}
				case 'p': out += '\n' + convert(child, depth + 1).trim() + '\n\n'; break;
				case 'br': out += '\n'; break;
				case 'hr': out += '\n---\n\n'; break;
				case 'strong': case 'b': out += '**' + convert(child, depth + 1).trim() + '**'; break;
				case 'em': case 'i': out += '*' + convert(child, depth + 1).trim() + '*'; break;
				case 'del': case 's': out += '~~' + convert(child, depth + 1).trim() + '~~'; break;
				case 'code': {
					var inPre = child.parentElement && child.parentElement.tagName.toLowerCase() === 'pre';
					if (inPre) out += convert(child, depth + 1);
					else out += '`' + (child.textContent || '') + '`';
					break;
				}
				case 'pre': {
					var codeEl = child.querySelector('code');
					var m1 = (child.getAttribute('class') || '').match(/language-(\w+)/);
					var m2 = codeEl ? (codeEl.getAttribute('class') || '').match(/language-(\w+)/) : null;
					var dataLang = codeEl ? (codeEl.getAttribute('data-language') || '') : '';
					var lang = (m1 ? m1[1] : '') || (m2 ? m2[1] : '') || dataLang;
					var clone = (codeEl || child).cloneNode(true);
					clone.querySelectorAll('[class*="line-number"],[class*="lineno"],[data-line-number]')
						.forEach(function (n) { n.remove(); });
					var codeText = (clone.textContent || '').trim();
					out += '\n```' + lang + '\n' + codeText + '\n```\n\n';
					break;
				}
				case 'blockquote': out += '\n> ' + convert(child, depth + 1).trim() + '\n\n'; break;
				case 'a': {
					var href = child.getAttribute('href') || '';
					var text = convert(child, depth + 1).trim();
					out += (href && text) ? ('[' + text + '](' + href + ')') : text;
					break;
				}
				case 'img': {
					var src = child.getAttribute('src') || '';
					var alt = child.getAttribute('alt') || '';
					// Skip data: URLs (avatars, logos, icons) — they bloat output.
					if (src && src.indexOf('data:') !== 0) out += '![' + alt + '](' + src + ')';
					break;
				}
				case 'ul': case 'ol': {
					var items = child.children;
					for (var k = 0; k < items.length; k++) {
						if (items[k].tagName.toLowerCase() !== 'li') continue;
						var prefix = tag === 'ol' ? (k + 1) + '. ' : '- ';
						out += prefix + convert(items[k], depth + 1).trim() + '\n';
					}
					out += '\n';
					break;
				}
				case 'li': out += convert(child, depth + 1).trim(); break;
				case 'table': {
					out += '\n';
					var rows = child.querySelectorAll('tr');
					var headerDone = false;
					for (var r = 0; r < rows.length; r++) {
						var cells = rows[r].querySelectorAll('th,td');
						var row = '';
						for (var c = 0; c < cells.length; c++) row += '| ' + convert(cells[c], depth + 1).trim() + ' ';
						row += '|\n';
						out += row;
						if (!headerDone && rows[r].querySelector('th')) {
							var sep = '';
							for (var s = 0; s < cells.length; s++) sep += '| --- ';
							sep += '|\n';
							out += sep;
							headerDone = true;
						}
					}
					out += '\n';
					break;
				}
				case 'thead': case 'tbody': case 'tfoot': case 'tr': case 'th': case 'td':
				case 'span': out += convert(child, depth + 1); break;
				case 'div': case 'section': case 'article': case 'main':
				case 'figure': case 'figcaption':
					out += '\n' + convert(child, depth + 1) + '\n';
					break;
				default: out += convert(child, depth + 1);
			}
		}
		return out;
	}

	return convert(root, 0)
		.replace(/\n[ \t]+\n/g, '\n\n')
		.replace(/\n{3,}/g, '\n\n')
		.replace(/[ \t]+\n/g, '\n')
		.replace(/\n{3,}/g, '\n\n')
		.trim();
}

// bskExtract is invoked by bsk_extract.sh as the evaluate expression:
//   bsk evaluate "$(cat dom_to_md.js); bskExtract('article')"
function bskExtract(sel) {
	var el = document.querySelector(sel);
	if (!el) return { ok: false, error: 'selector not found: ' + sel };
	var text = htmlToMdEl(el);
	if (!text) return { ok: false, error: 'empty content for selector: ' + sel };
	return {
		ok: true,
		text: text,
		len: text.length,
		title: document.title,
		url: location.href
	};
}