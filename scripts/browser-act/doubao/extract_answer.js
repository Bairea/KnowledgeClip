(() => {
  // Doubao answer structure:
  // .md-box-root elements: user messages (ancestor has "justify-end") and assistant answers
  const isThinking = (el) => {
    let current = el;
    for (let i = 0; i < 5 && current; i += 1) {
      const cls = String(current.className || "").toLowerCase();
      if (cls.includes("thinking") || cls.includes("reasoning")) return true;
      current = current.parentElement;
    }
    return false;
  };

  // User messages are right-aligned (ancestor has "justify-end")
  const isUserMessage = (el) => {
    let current = el.parentElement;
    for (let i = 0; i < 5 && current; i += 1) {
      if (String(current.className || "").includes("justify-end")) return true;
      current = current.parentElement;
    }
    return false;
  };

  // Doubao-specific selectors
  const selectors = [
    '.md-box-root',
    '[class*="md-box"]',
    '[class*="message-content"]',
    '[class*="assistant-message"]',
  ];

  let matchedSelector = null;
  let answerEls = [];
  for (const selector of selectors) {
    const els = Array.from(document.querySelectorAll(selector)).filter((el) => {
      const text = (el.innerText || el.textContent || "").trim();
      return text.length > 0 && !isThinking(el) && !isUserMessage(el);
    });
    if (els.length > 0) {
      answerEls = els;
      matchedSelector = selector;
      break;
    }
  }

  // For Doubao, the answer is the LAST md-box-root (first is user message)
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
    text: globalThis.__KC_LIB__.cleanAnswerText(lastEl),
    htmlPreview: lastEl.outerHTML.slice(0, 5000),
    className: String(lastEl.className || "").slice(0, 200),
  });
})();
