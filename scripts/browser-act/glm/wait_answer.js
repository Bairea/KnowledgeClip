(() => {
  const stableTarget = Number(globalThis.__PAYLOAD__.stableRounds || 3);
  const key = "__GLM_WAIT_STATE__";
  const state = globalThis[key] || (globalThis[key] = { lastText: "", stableRounds: 0 });

  const isThinking = (el) => {
    let current = el;
    for (let i = 0; i < 6 && current; i += 1) {
      const cls = String(current.className || "").toLowerCase();
      if (cls.indexOf("advance-thinking") >= 0 || cls.indexOf("think-block") >= 0 ||
          cls.indexOf("thinking-content") >= 0 || cls.indexOf("thinking-process") >= 0 ||
          cls.indexOf("text-advance-thinking") >= 0 || cls.indexOf("reasoning") >= 0) {
        return true;
      }
      current = current.parentElement;
    }
    return false;
  };

  // GLM answer selector
  const answerSelectors = ['.answer-content-wrap', '.markdown-body', '.answer-content'];

  let answerEls = [];
  for (const selector of answerSelectors) {
    answerEls = Array.from(document.querySelectorAll(selector)).filter((el) => {
      const text = (el.innerText || el.textContent || "").trim();
      return text.length > 0 && !isThinking(el);
    });
    if (answerEls.length > 0) break;
  }

  const lastEl = answerEls[answerEls.length - 1] || null;
  const lastText = lastEl ? (lastEl.innerText || lastEl.textContent || "").trim() : "";
  if (lastText && lastText === state.lastText) {
    state.stableRounds += 1;
  } else if (lastText) {
    state.lastText = lastText;
    state.stableRounds = 0;
  }

  // Check if still generating (send button has disabled class or textarea is empty)
  const textarea = document.querySelector('textarea');
  const isGenerating = textarea ? (textarea.value === "" && lastText === "") : false;

  const done = Boolean(lastText) && state.stableRounds >= stableTarget;

  return JSON.stringify({
    answerCount: answerEls.length,
    lastTextLen: lastText.length,
    lastTextPreview: lastText.slice(0, 120),
    stableRounds: state.stableRounds,
    isGenerating,
    done,
    url: location.href,
  });
})();
