(() => {
  // MiniMax new chat: click "新建任务" button
  const keywords = ["新建任务", "新任务", "New Task", "new task"];

  const candidates = Array.from(document.querySelectorAll("button, a, div[role=button]"));
  for (const el of candidates) {
    const text = (el.innerText || el.textContent || "").trim();
    if (keywords.some(kw => text.includes(kw))) {
      el.click();
      return globalThis.__KC_LIB__.safeStringify({ ok: true, method: "keyword", text: text });
    }
  }

  return globalThis.__KC_LIB__.safeStringify({ ok: false, error: "new task button not found" });
})();
