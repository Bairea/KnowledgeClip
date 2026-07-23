(() => {
  // Kimi input: div.chat-input-editor[role="textbox"]
  const inputs = Array.from(
    document.querySelectorAll('div.chat-input-editor, [role="textbox"], [contenteditable="true"], textarea')
  ).map((el) => {
    return {
      tag: el.tagName,
      role: el.getAttribute("role"),
      contenteditable: el.getAttribute("contenteditable"),
      className: String(el.className || "").slice(0, 200),
    };
  });

  const sendButton = document.querySelector('div.send-button-container');
  const readyInput = inputs.find(
    (item) => item.role === "textbox" || item.className.includes("chat-input")
  );

  return JSON.stringify({
    url: location.href,
    title: document.title,
    ready: Boolean(readyInput),
    inputCount: inputs.length,
    readyInput,
    sendButton: sendButton ? {
      disabled: sendButton.hasAttribute('disabled') || sendButton.classList.contains('disabled'),
      className: String(sendButton.className || "").slice(0, 160),
    } : null,
  });
})();
