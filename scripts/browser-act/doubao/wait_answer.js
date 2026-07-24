(() => {
  const stableTarget = Number(globalThis.__PAYLOAD__.stableRounds || 3);
  const key = "__DOUBAO_WAIT_STATE__";
  const state = globalThis[key] || (globalThis[key] = { lastText: "", stableRounds: 0 });

  const isThinking = (el) => {
    let current = el;
    for (let i = 0; i < 5 && current; i += 1) {
      const cls = String(current.className || "").toLowerCase();
      if (cls.includes("thinking") || cls.includes("reasoning")) return true;
      current = current.parentElement;
    }
    return false;
  };

  // Doubao user messages are right-aligned (ancestor has "justify-end")
  const isUserMessage = (el) => {
    let current = el.parentElement;
    for (let i = 0; i < 5 && current; i += 1) {
      if (String(current.className || "").includes("justify-end")) return true;
      current = current.parentElement;
    }
    return false;
  };

  // Doubao answer selector: .md-box-root (filter out user messages)
  const answerSelectors = ['.md-box-root', '[class*="md-box"]'];

  let answerEls = [];
  for (const selector of answerSelectors) {
    answerEls = Array.from(document.querySelectorAll(selector)).filter((el) => {
      const text = (el.innerText || el.textContent || "").trim();
      return text.length > 0 && !isThinking(el) && !isUserMessage(el);
    });
    if (answerEls.length > 0) break;
  }

  // Get the last answer element
  const lastEl = answerEls[answerEls.length - 1] || null;
  const lastText = lastEl ? (lastEl.innerText || lastEl.textContent || "").trim() : "";
  if (lastText && lastText === state.lastText) {
    state.stableRounds += 1;
  } else if (lastText) {
    state.lastText = lastText;
    state.stableRounds = 0;
  }

  // Check if still generating (answer count hasn't changed)
  const done = Boolean(lastText) && state.stableRounds >= stableTarget;

  return JSON.stringify({
    answerCount: answerEls.length,
    lastTextLen: lastText.length,
    lastTextPreview: lastText.slice(0, 120),
    stableRounds: state.stableRounds,
    done,
    url: location.href,
  });
})();
