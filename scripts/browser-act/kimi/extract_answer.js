(() => {
  // Kimi renders each assistant reply as a .chat-content-item-assistant
  // element containing several sibling .markdown-container blocks: an
  // optional thinking block, the main answer, and duplicate code-block
  // containers (markdown-code, used for full-screen/teleport rendering).
  // Taking the last .markdown-container on the page returns only the code
  // block, so we must extract from the last assistant message instead.
  const isThinking = (el) => {
    let current = el;
    for (let i = 0; i < 5 && current; i += 1) {
      const cls = String(current.className || "").toLowerCase();
      if (cls.includes("thinking") || cls.includes("reasoning")) return true;
      current = current.parentElement;
    }
    return false;
  };

  const assistants = Array.from(document.querySelectorAll('.chat-content-item-assistant'));
  const lastMsg = assistants[assistants.length - 1];

  // Fallback: previous selector-based extraction if the message wrapper
  // is not found (e.g. Kimi UI changed).
  if (!lastMsg) {
    const selectors = [
      '.markdown-container',
      '.markdown',
      '[class*="markdown"]',
      '[class*="message-content"]',
      '[class*="assistant-message"]',
    ];
    for (const selector of selectors) {
      const els = Array.from(document.querySelectorAll(selector)).filter((el) => {
        const text = (el.innerText || el.textContent || "").trim();
        return text.length > 0 && !isThinking(el);
      });
      if (els.length > 0) {
        const lastEl = els[els.length - 1];
        return globalThis.__KC_LIB__.safeStringify({
          ok: true, selector, answerCount: els.length,
          text: globalThis.__KC_LIB__.cleanAnswerText(lastEl),
          htmlPreview: lastEl.outerHTML.slice(0, 5000),
          className: String(lastEl.className || "").slice(0, 200),
        });
      }
    }
    return globalThis.__KC_LIB__.safeStringify({
      ok: false, error: "no answer element found", selector: null, answerCount: 0,
    });
  }

  const parts = [];
  lastMsg.querySelectorAll('.markdown-container').forEach((el) => {
    if (isThinking(el)) return;
    const cls = String(el.className || "").toLowerCase();
    // Code-block containers duplicate code already rendered inside the main
    // answer container; skip them to avoid doubling the code.
    if (cls.indexOf("markdown-code") >= 0) return;
    const text = (el.innerText || el.textContent || "").trim();
    if (!text) return;
    const md = globalThis.__KC_LIB__.htmlToMarkdown(el);
    if (md) parts.push(md);
  });

  let text = parts.join("\n\n");
  // Kimi sometimes answers without markdown containers (e.g. rate-limit or
  // error messages rendered as plain text); fall back to the message text.
  if (!text) {
    text = (lastMsg.innerText || lastMsg.textContent || "").trim();
  }
  return globalThis.__KC_LIB__.safeStringify({
    ok: Boolean(text),
    selector: ".chat-content-item-assistant",
    answerCount: assistants.length,
    text,
    htmlPreview: lastMsg.outerHTML.slice(0, 5000),
    className: String(lastMsg.className || "").slice(0, 200),
  });
})();
