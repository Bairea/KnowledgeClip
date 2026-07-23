(() => {
  const inputs = Array.from(
    document.querySelectorAll('[role="textbox"], [contenteditable="true"], textarea')
  )
    .map((el) => {
      const text = (el.innerText || el.textContent || "").trim();
      return {
        selectorHint:
          el.getAttribute("role") === "textbox"
            ? '[role="textbox"]'
            : el.getAttribute("data-slate-editor") === "true"
              ? '[data-slate-editor="true"]'
              : el.tagName.toLowerCase(),
        tag: el.tagName,
        textPreview: text.slice(0, 80),
        role: el.getAttribute("role"),
        contenteditable: el.getAttribute("contenteditable"),
        dataSlate: el.getAttribute("data-slate-editor"),
        className: String(el.className || "").slice(0, 200),
      };
    });

  const sendButton = document.querySelector('button[aria-label="发送消息"]');
  const readyInput = inputs.find(
    (item) =>
      item.role === "textbox" ||
      item.contenteditable === "true" ||
      item.dataSlate === "true"
  );

  return JSON.stringify({
    url: location.href,
    title: document.title,
    ready: Boolean(readyInput),
    inputCount: inputs.length,
    readyInput,
    sendButton: sendButton
      ? {
          disabled: !!sendButton.disabled,
          aria: sendButton.getAttribute("aria-label"),
          className: String(sendButton.className || "").slice(0, 160),
        }
      : null,
  });
})();
