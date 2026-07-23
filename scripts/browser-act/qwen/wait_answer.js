(() => {
  const stableTarget = Number(globalThis.__PAYLOAD__.stableRounds || 3);
  const key = "__QWEN_WAIT_STATE__";
  const state = globalThis[key] || (globalThis[key] = { lastText: "", stableRounds: 0 });

  const isThinking = (el) => {
    let current = el;
    for (let i = 0; i < 5 && current; i += 1) {
      const cls = String(current.className || "").toLowerCase();
      if (cls.includes("thinking") || cls.includes("reason")) return true;
      current = current.parentElement;
    }
    return false;
  };

  const answerSelectors = [
    '[class*="qk-markdown"]',
    '[class*="markdown"]',
    '[class*="answer-common-card"]',
  ];

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

  const sendButton = document.querySelector('button[aria-label="发送消息"]');
  const done = Boolean(lastText) && (
    (state.stableRounds >= stableTarget && (!sendButton || sendButton.disabled === false)) ||
    state.stableRounds >= stableTarget + 3
  );

  return JSON.stringify({
    answerCount: answerEls.length,
    lastTextLen: lastText.length,
    lastTextPreview: lastText.slice(0, 120),
    stableRounds: state.stableRounds,
    sendDisabled: sendButton ? !!sendButton.disabled : null,
    done,
    url: location.href,
  });
})();
