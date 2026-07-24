(() => {
  // Doubao input: textarea at the bottom
  const textareas = Array.from(document.querySelectorAll('textarea'));
  const inputs = textareas.map((el) => ({
    tag: el.tagName,
    placeholder: el.placeholder || "",
    className: String(el.className || "").slice(0, 200),
  }));

  const sendButton = document.querySelector('#flow-end-msg-send') ||
                     document.querySelector('div[class*="send-button"]') ||
                     document.querySelector('button[class*="send"]');
  const readyInput = inputs.length > 0;

  return globalThis.__KC_LIB__.safeStringify({
    url: location.href,
    title: document.title,
    ready: readyInput,
    inputCount: inputs.length,
    inputs,
    sendButton: sendButton ? { found: true, id: sendButton.id || "" } : null,
  });
})();
