(() => {
  // DeepSeek answer structure:
  // .ds-message elements contain both user and assistant messages
  // Assistant messages have class "ds-message _63c77b1" (without "d29f3d7d" prefix)
  // The answer content is in .ds-assistant-message-main-content or similar
  const isThinking = (el) => {
    let current = el;
    for (let i = 0; i < 5 && current; i += 1) {
      const cls = String(current.className || "").toLowerCase();
      if (cls.includes("thinking") || cls.includes("reason")) return true;
      current = current.parentElement;
    }
    return false;
  };

  // DeepSeek-specific selectors for assistant messages
  const selectors = [
    '.ds-assistant-message-main-content',
    '.ds-message._63c77b1',
    '[class*="assistant-message"]',
    '.ds-message',
  ];

  let matchedSelector = null;
  let answerEls = [];
  for (const selector of selectors) {
    const els = Array.from(document.querySelectorAll(selector)).filter((el) => {
      const text = (el.innerText || el.textContent || "").trim();
      return text.length > 0 && !isThinking(el);
    });
    if (els.length > 0) {
      answerEls = els;
      matchedSelector = selector;
      break;
    }
  }

  // Get the last assistant message (the answer)
  const lastEl = answerEls[answerEls.length - 1];
  if (!lastEl) {
    return JSON.stringify({
      ok: false, error: "no answer element found", selector: null, answerCount: 0,
    });
  }

  return JSON.stringify({
    ok: true,
    selector: matchedSelector,
    answerCount: answerEls.length,
    text: (lastEl.innerText || lastEl.textContent || "").trim(),
    htmlPreview: lastEl.outerHTML.slice(0, 5000),
    className: String(lastEl.className || "").slice(0, 200),
  });
})();
