(() => {
  const stableTarget = Number(globalThis.__PAYLOAD__.stableRounds || 3);
  const key = "__KIMI_WAIT_STATE__";
  const state = globalThis[key] || (globalThis[key] = { lastText: "", stableRounds: 0, assistantCount: 0 });

  // Track stability of the whole last assistant message, not the last
  // .markdown-container on the page. Kimi renders code blocks in separate
  // duplicate containers; polling those would declare the answer done while
  // the text after the code block is still streaming.
  const assistants = Array.from(document.querySelectorAll('.chat-content-item-assistant'));
  const lastMsg = assistants[assistants.length - 1] || null;

  // Only consider this a new answer once a message beyond the one present
  // at send time appears. Otherwise the previous turn's answer could be
  // mistaken for the new one before Kimi starts rendering.
  const newAnswerSeen = assistants.length > state.assistantCount;
  const lastText = lastMsg ? (lastMsg.innerText || lastMsg.textContent || "").trim() : "";

  if (newAnswerSeen && lastText && lastText === state.lastText) {
    state.stableRounds += 1;
  } else if (newAnswerSeen && lastText) {
    state.lastText = lastText;
    state.stableRounds = 0;
  }

  const done = newAnswerSeen && Boolean(lastText) && state.stableRounds >= stableTarget;

  return globalThis.__KC_LIB__.safeStringify({
    answerCount: assistants.length,
    lastTextLen: lastText.length,
    lastTextPreview: lastText.slice(0, 120),
    stableRounds: state.stableRounds,
    done,
    url: location.href,
  });
})();
