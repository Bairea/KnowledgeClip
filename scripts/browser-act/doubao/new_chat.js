(() => {
  // Doubao new chat: click "新建对话" button
  const keywords = ["新建对话", "新对话", "新聊天", "New Chat", "new chat"];

  // Try specific Doubao selectors first
  const newChatBtn = document.querySelector('[class*="new-session"]') ||
                     document.querySelector('[class*="new-chat"]') ||
                     document.querySelector('div[role="button"]');

  if (newChatBtn) {
    newChatBtn.click();
    return globalThis.__KC_LIB__.safeStringify({ ok: true, method: "selector", text: "新建对话" });
  }

  // Fallback: search by text content
  const candidates = Array.from(document.querySelectorAll("button, a, div[role=button]"));
  for (const el of candidates) {
    const text = (el.innerText || el.textContent || "").trim();
    if (keywords.some(kw => text.includes(kw))) {
      el.click();
      return globalThis.__KC_LIB__.safeStringify({ ok: true, method: "keyword", text: text });
    }
  }

  return globalThis.__KC_LIB__.safeStringify({ ok: false, error: "new chat button not found" });
})();
