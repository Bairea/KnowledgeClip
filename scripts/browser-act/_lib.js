// Shared utility functions for browser-act scripts.
// Loaded by the engine and prepended to every eval script.
globalThis.__KC_LIB__ = globalThis.__KC_LIB__ || {};

// Convert an answer element's HTML to Markdown, filtering out UI controls
// (buttons, icons, copy/download buttons, code headers, line numbers) and
// thinking/reasoning regions. Preserves structural formatting: headings,
// paragraphs, lists, tables, code blocks, blockquotes, links, images.
//
// Usage: const md = globalThis.__KC_LIB__.htmlToMarkdown(el);
globalThis.__KC_LIB__.htmlToMarkdown = function (el) {
  if (!el) return "";

  function esc(s) { return (s || "").replace(/\|/g, "\\|"); }

  function convert(node, depth) {
    if (depth > 15) return "";
    var result = "";
    for (var i = 0; i < node.childNodes.length; i++) {
      var child = node.childNodes[i];
      if (child.nodeType === 3) {
        result += child.textContent.replace(/\s+/g, " ");
      } else if (child.nodeType === 1) {
        var tag = child.tagName.toLowerCase();
        var cls = (child.getAttribute("class") || "").toLowerCase();

        // Skip UI control elements
        if (tag === "button" || tag === "svg" || tag === "path" ||
            cls.indexOf("copy") >= 0 || cls.indexOf("download") >= 0 ||
            cls.indexOf("clipboard") >= 0 || cls.indexOf("toolbar") >= 0 ||
            cls.indexOf("action") >= 0 || cls.indexOf("code-header") >= 0 ||
            cls.indexOf("table-cap") >= 0 || cls.indexOf("table-label") >= 0 ||
            cls.indexOf("lang-label") >= 0 || cls.indexOf("code-lang") >= 0 ||
            cls.indexOf("code-action") >= 0 || cls.indexOf("header-row") >= 0) {
          continue;
        }

        // Skip thinking/reasoning regions and their whole subtree
        if (cls.indexOf("advance-thinking") >= 0 || cls.indexOf("think-block") >= 0 ||
            cls.indexOf("thinking-block") >= 0 || cls.indexOf("thinking-item") >= 0 ||
            cls.indexOf("thinking-area") >= 0 || cls.indexOf("thinking-content") >= 0 ||
            cls.indexOf("thinking-process") >= 0 || cls.indexOf("reasoning-block") >= 0 ||
            cls.indexOf("reasoning-content") >= 0 || cls.indexOf("reasoning-text") >= 0 ||
            cls.indexOf("thought-block") >= 0 || cls.indexOf("thought-content") >= 0 ||
            cls.indexOf("text-advance-thinking") >= 0 || cls.indexOf("cot-block") >= 0) {
          continue;
        }

        // Skip assistant name label and "AI generated" footer
        if (cls.indexOf("assistant-name") >= 0 || cls.indexOf("interact-container") >= 0) {
          continue;
        }

        // Skip leaf elements whose own text matches common UI labels
        var ownText = "";
        for (var j = 0; j < child.childNodes.length; j++) {
          if (child.childNodes[j].nodeType === 3) ownText += child.childNodes[j].textContent;
        }
        ownText = ownText.trim().toLowerCase();
        if (ownText.length > 0 && ownText.length < 20 &&
            child.querySelectorAll("p,h1,h2,h3,h4,h5,h6,ul,ol,table,pre,blockquote,div").length === 0) {
          var uiLabels = ["copy", "download", "复制", "下载", "share", "分享", "regenerate", "重新生成",
            "table", "表格", "python", "javascript", "java", "go", "golang", "rust", "typescript",
            "cpp", "c++", "c#", "c", "sql", "html", "css", "bash", "shell", "sh", "json", "yaml",
            "xml", "markdown", "md", "text", "plain", "code", "代码", "typescript", "tsx", "jsx",
            "kotlin", "swift", "ruby", "php", "perl", "scala", "dart", "r", "matlab", "lua",
            "运行", "执行", "执行结果", "输出文本", "内容", "编辑", "导出"];
          if (uiLabels.indexOf(ownText) >= 0) continue;
        }

        switch (tag) {
          case "h1": result += "\n# " + convert(child, depth + 1).trim() + "\n\n"; break;
          case "h2": result += "\n## " + convert(child, depth + 1).trim() + "\n\n"; break;
          case "h3": result += "\n### " + convert(child, depth + 1).trim() + "\n\n"; break;
          case "h4": result += "\n#### " + convert(child, depth + 1).trim() + "\n\n"; break;
          case "h5": result += "\n##### " + convert(child, depth + 1).trim() + "\n\n"; break;
          case "h6": result += "\n###### " + convert(child, depth + 1).trim() + "\n\n"; break;
          case "p": result += "\n" + convert(child, depth + 1).trim() + "\n\n"; break;
          case "br": result += "\n"; break;
          case "hr": result += "\n---\n\n"; break;
          case "strong": case "b": result += "**" + convert(child, depth + 1).trim() + "**"; break;
          case "em": case "i": result += "*" + convert(child, depth + 1).trim() + "*"; break;
          case "del": case "s": result += "~~" + convert(child, depth + 1).trim() + "~~"; break;
          case "code":
            var codeCls = child.getAttribute("class") || "";
            var langMatch = codeCls.match(/language-(\w+)/);
            if (langMatch && child.parentElement && child.parentElement.tagName.toLowerCase() === "pre") {
              result += convert(child, depth + 1);
            } else {
              result += "`" + (child.textContent || "") + "`";
            }
            break;
          case "pre":
            var preCls = child.getAttribute("class") || "";
            var preLang = preCls.match(/language-(\w+)/);
            var codeEl = child.querySelector("code");
            var codeLang = "";
            if (codeEl) {
              var codeCls2 = codeEl.getAttribute("class") || "";
              var m = codeCls2.match(/language-(\w+)/);
              if (m) codeLang = m[1];
            }
            if (!preLang && !codeLang) {
              var allCls = child.getAttribute("class") || "";
              var m2 = allCls.match(/(?:lang|language)-(\w+)/);
              if (m2) codeLang = m2[1];
            }
            if (!codeLang) {
              var dataLang = child.getAttribute("data-language") || (codeEl ? codeEl.getAttribute("data-language") : "") || "";
              if (dataLang) codeLang = dataLang;
            }
            if (!codeLang) {
              var prev = child.previousElementSibling;
              for (var pi = 0; pi < 3 && prev; pi++) {
                var prevText = (prev.innerText || prev.textContent || "").trim().toLowerCase();
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
            var codeText = (codeClone.textContent || "").trim();
            if (codeText.length > 0) {
              var codeLines = codeText.split("\n");
              var hasLineNums = codeLines.length > 3;
              for (var li = 0; hasLineNums && li < Math.min(5, codeLines.length); li++) {
                if (!codeLines[li].match(new RegExp("^\\s*" + (li + 1) + "(?![0-9])"))) hasLineNums = false;
              }
              if (hasLineNums) {
                codeText = codeLines.map(function (line) {
                  return line.replace(/^\s*\d+(?![0-9])/, "");
                }).join("\n").trim();
              }
            }
            if (!codeLang && codeText.length > 0) {
              if (/^\s*(def |import |from |print\(|class |if __name__)/.test(codeText)) codeLang = "python";
              else if (/^\s*(function |const |let |var |import |export )/.test(codeText)) codeLang = "javascript";
              else if (/^\s*(func |package )/.test(codeText)) codeLang = "go";
              else if (/^\s*(pub fn |fn |use |mod )/.test(codeText)) codeLang = "rust";
              else if (/^\s*(#include|#define|#ifndef)/.test(codeText)) codeLang = "cpp";
              else if (/^\s*<\?php/.test(codeText)) codeLang = "php";
            }
            // Strip non-informative language tags that sites emit as placeholders
            // (e.g. Doubao marks plain text code blocks as language-plaintext).
            if (/^(plaintext|plain|text|txt|null|none|undefined|default|raw)$/i.test(codeLang)) {
              codeLang = "";
            }
            result += "\n```" + codeLang + "\n" + codeText + "\n```\n\n";
            break;
          case "blockquote": result += "\n> " + convert(child, depth + 1).trim() + "\n\n"; break;
          case "a":
            var href = child.getAttribute("href") || "";
            var linkText = convert(child, depth + 1).trim();
            if (href && linkText) result += "[" + linkText + "](" + href + ")";
            else result += linkText;
            break;
          case "img":
            var src = child.getAttribute("src") || "";
            var alt = child.getAttribute("alt") || "";
            if (src.indexOf("data:") === 0) break;
            if (src) result += "![" + alt + "](" + src + ")";
            break;
          case "ul": case "ol":
            var items = child.children;
            for (var k = 0; k < items.length; k++) {
              if (items[k].tagName.toLowerCase() === "li") {
                var prefix = tag === "ol" ? (k + 1) + ". " : "- ";
                result += prefix + convert(items[k], depth + 1).trim() + "\n";
              }
            }
            result += "\n";
            break;
          case "li": result += convert(child, depth + 1).trim(); break;
          case "table":
            result += "\n";
            var rows = child.querySelectorAll("tr");
            var headerProcessed = false;
            for (var r = 0; r < rows.length; r++) {
              var cells = rows[r].querySelectorAll("th,td");
              var rowText = "";
              for (var c = 0; c < cells.length; c++) {
                rowText += "| " + convert(cells[c], depth + 1).trim() + " ";
              }
              rowText += "|\n";
              result += rowText;
              if (!headerProcessed && rows[r].querySelector("th")) {
                var sep = "";
                for (var c2 = 0; c2 < cells.length; c2++) sep += "| --- ";
                sep += "|\n";
                result += sep;
                headerProcessed = true;
              }
            }
            result += "\n";
            break;
          case "thead": case "tbody": case "tfoot": case "tr": case "th": case "td":
            result += convert(child, depth + 1);
            break;
          case "span": result += convert(child, depth + 1); break;
          case "div": case "section": case "article":
          case "header": case "footer": case "main":
          case "figure": case "figcaption":
            result += "\n" + convert(child, depth + 1) + "\n";
            break;
          default: result += convert(child, depth + 1); break;
        }
      }
    }
    return result;
  }

  return convert(el, 0)
    .replace(/\n[ \t]+\n/g, "\n\n")
    .replace(/\n{3,}/g, "\n\n")
    .trim();
};

// Backward-compatible alias: existing extract_answer.js scripts call
// cleanAnswerText. Route it to htmlToMarkdown so they get structured Markdown
// instead of flattened plain text.
globalThis.__KC_LIB__.cleanAnswerText = globalThis.__KC_LIB__.htmlToMarkdown;
