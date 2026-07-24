(() => {
  const keywords = ["新建对话", "新建会话", "新对话", "新聊天", "New Chat", "new chat"];

  const candidates = Array.from(
    document.querySelectorAll("button, a, div[role=button]")
  ).map((el) => {
    const text = (el.innerText || el.textContent || "").trim();
    const aria = (el.getAttribute("aria-label") || "").trim();
    return {
      el, text, aria,
      className: String(el.className || "").slice(0, 160),
      matched: keywords.some((kw) => text.includes(kw) || aria.includes(kw)),
    };
  });

  const target = candidates.find((item) => item.matched);
  if (!target) {
    return globalThis.__KC_LIB__.safeStringify({
      ok: false, error: "new chat button not found",
      candidates: candidates.filter((item) => item.text || item.aria).slice(0, 20)
        .map((item) => ({ text: item.text, aria: item.aria, className: item.className })),
    });
  }

  target.el.scrollIntoView({ block: "center" });
  target.el.click();

  return globalThis.__KC_LIB__.safeStringify({
    ok: true, text: target.text, aria: target.aria, className: target.className, url: location.href,
  });
})();
