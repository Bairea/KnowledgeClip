(() => {
  // Kimi new chat: click the "新建会话" button
  // It can be: a.new-chat-btn, div.sidebar-new-chat, or a[aria-label="新建会话"]
  const keywords = ["新建会话", "新对话", "新聊天", "New Chat", "new chat"];

  // Try specific Kimi selectors first
  const newChatBtn = document.querySelector('a.new-chat-btn') ||
                     document.querySelector('div.sidebar-new-chat') ||
                     document.querySelector('a[aria-label="新建会话"]');

  if (newChatBtn) {
    newChatBtn.click();
    return JSON.stringify({ ok: true, method: "selector", text: "新建会话" });
  }

  // Fallback: search by text content
  const candidates = Array.from(document.querySelectorAll("button, a, div[role=button]"));
  for (const el of candidates) {
    const text = (el.innerText || el.textContent || "").trim();
    if (keywords.some(kw => text.includes(kw))) {
      el.click();
      return JSON.stringify({ ok: true, method: "keyword", text: text });
    }
  }

  return JSON.stringify({ ok: false, error: "new chat button not found" });
})();
