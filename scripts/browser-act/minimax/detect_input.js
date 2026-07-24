(() => {
  // MiniMax input: .tiptap.ProseMirror[contenteditable="true"]
  const editors = document.querySelectorAll('[contenteditable="true"]');
  const inputs = Array.from(editors).map((el) => ({
    tag: el.tagName,
    className: String(el.className || "").slice(0, 200),
    text: (el.innerText || "").trim().slice(0, 80),
  }));

  const readyInput = inputs.length > 0;

  return globalThis.__KC_LIB__.safeStringify({
    url: location.href,
    title: document.title,
    ready: readyInput,
    inputCount: inputs.length,
    inputs,
  });
})();
