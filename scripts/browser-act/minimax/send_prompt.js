(() => {
  const prompt = String(globalThis.__PAYLOAD__.prompt || "");
  if (!prompt) {
    return JSON.stringify({ ok: false, error: "empty prompt" });
  }

  // MiniMax input: .tiptap.ProseMirror[contenteditable="true"]
  const editor = document.querySelector('.tiptap.ProseMirror') || document.querySelector('[contenteditable="true"]');
  if (!editor) {
    return JSON.stringify({ ok: false, error: "input not found" });
  }

  editor.focus();

  // For ProseMirror, use textContent + input event
  editor.textContent = prompt;
  editor.dispatchEvent(new Event("input", { bubbles: true }));

  // MiniMax: use Enter key to send
  editor.dispatchEvent(new KeyboardEvent("keydown", {
    key: "Enter", code: "Enter", which: 13, keyCode: 13, bubbles: true,
  }));
  return JSON.stringify({ ok: true, mode: "enter" });
})();
