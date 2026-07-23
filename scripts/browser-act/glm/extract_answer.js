(() => {
  // GLM answer structure:
  // .answer-content (container)
  //   .advance-thinking (thinking section, should be excluded)
  //   .answer-content-wrap (actual answer)
  //     .markdown-body (answer text)
  const isThinking = (el) => {
    let current = el;
    for (let i = 0; i < 6 && current; i += 1) {
      const cls = String(current.className || "").toLowerCase();
      if (cls.indexOf("advance-thinking") >= 0 || cls.indexOf("think-block") >= 0 ||
          cls.indexOf("thinking-content") >= 0 || cls.indexOf("thinking-process") >= 0 ||
          cls.indexOf("text-advance-thinking") >= 0 || cls.indexOf("reason") >= 0) {
        return true;
      }
      current = current.parentElement;
    }
    return false;
  };

  // GLM-specific selectors
  const selectors = [
    '.answer-content-wrap',
    '.markdown-body',
    '.answer-content',
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
