(() => {
  // Doubao input: tiptap/ProseMirror contenteditable editor in chat views
  // (legacy textarea still supported as a fallback).
  const editors = Array.from(
    document.querySelectorAll('div[contenteditable="true"], textarea')
  ).map((el) => ({
    tag: el.tagName,
    role: el.getAttribute("role"),
    placeholder: el.placeholder || "",
    contenteditable: el.getAttribute("contenteditable"),
    textPreview: (el.innerText || el.textContent || "").trim().slice(0, 80),
    className: String(el.className || "").slice(0, 200),
  }));

  const sendButton = document.querySelector('#flow-end-msg-send') ||
                     document.querySelector('div[class*="send-button"]') ||
                     document.querySelector('button[class*="send"]');
  const readyInput = editors.length > 0;

  return globalThis.__KC_LIB__.safeStringify({
    url: location.href,
    title: document.title,
    ready: readyInput,
    inputCount: editors.length,
    inputs: editors,
    sendButton: sendButton ? { found: true, id: sendButton.id || "" } : null,
  });
})();