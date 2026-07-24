(() => {
  // DeepSeek new chat: click "开启新对话" button
  const keywords = ["开启新对话", "新对话", "New Chat", "new chat"];

  // Try to find the new chat button
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
