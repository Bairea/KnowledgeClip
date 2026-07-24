(() => {
  const prompt = String(globalThis.__PAYLOAD__.prompt || "");
  if (!prompt) {
    return JSON.stringify({ ok: false, error: "empty prompt" });
  }
  delete globalThis["__MINIMAX_WAIT_STATE__"];

  const editor = document.querySelector('.tiptap.ProseMirror') ||
    document.querySelector('[contenteditable="true"]') ||
    document.querySelector('[data-testid="message-textarea"]');
  if (!editor) {
    return JSON.stringify({ ok: false, error: "input not found" });
  }

  editor.focus();

  let inputOk = false;
  let mode = "execCommand";

  try {
    if (document.execCommand) {
      document.execCommand("selectAll", false, null);
      document.execCommand("insertText", false, prompt);
      inputOk = true;
    }
  } catch (e) {}

  if (!inputOk) {
    editor.textContent = prompt;
    editor.dispatchEvent(new Event("input", { bubbles: true }));
    mode = "textContent";
  }

  return new Promise((resolve) => {
    setTimeout(() => {
      const sendBtn = document.querySelector('button[class*="send"]') ||
        document.querySelector('button[type="submit"]') ||
        document.querySelector('[class*="send"] button') ||
        document.querySelector('[aria-label*="发送"]');
      if (sendBtn && !sendBtn.disabled) {
        sendBtn.click();
        resolve(JSON.stringify({ ok: true, mode, submitMode: "button" }));
        return;
      }
      editor.dispatchEvent(new KeyboardEvent("keydown", {
        key: "Enter", code: "Enter", which: 13, keyCode: 13, bubbles: true,
      }));
      resolve(JSON.stringify({ ok: true, mode, submitMode: "enter" }));
    }, 500);
  });
})();
