(() => {
  const isThinking = (el) => {
    let current = el;
    for (let i = 0; i < 5 && current; i += 1) {
      const cls = String(current.className || "").toLowerCase();
      if (cls.includes("thinking") || cls.includes("reasoning")) return true;
      current = current.parentElement;
    }
    return false;
  };

  const selectors = [
    '[class*="qk-markdown"]',
    'div[class*="markdown"]',
    '[class*="answer-common-card"]',
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

  const lastEl = answerEls[answerEls.length - 1];
  if (!lastEl) {
    return globalThis.__KC_LIB__.safeStringify({
      ok: false, error: "no answer element found", selector: null, answerCount: 0,
    });
  }

  return globalThis.__KC_LIB__.safeStringify({
    ok: true,
    selector: matchedSelector,
    answerCount: answerEls.length,
    text: globalThis.__KC_LIB__.cleanAnswerText(lastEl),
    htmlPreview: lastEl.outerHTML.slice(0, 5000),
    className: String(lastEl.className || "").slice(0, 200),
  });
})();
