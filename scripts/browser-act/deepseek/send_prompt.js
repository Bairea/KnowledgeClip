(() => {
  const prompt = String(globalThis.__PAYLOAD__.prompt || "");
  if (!prompt) {
    return globalThis.__KC_LIB__.safeStringify({ ok: false, error: "empty prompt" });
  }
  delete globalThis["__DEEPSEEK_WAIT_STATE__"];

  // DeepSeek input: textarea[name="search"]
  const textarea = document.querySelector('textarea[name="search"]') || document.querySelector('textarea');
  if (!textarea) {
    return globalThis.__KC_LIB__.safeStringify({ ok: false, error: "input not found" });
  }

  textarea.focus();

  // Use native setter for React controlled textarea
  const nativeSetter = Object.getOwnPropertyDescriptor(window.HTMLTextAreaElement.prototype, 'value').set;
  nativeSetter.call(textarea, prompt);
  textarea.dispatchEvent(new Event("input", { bubbles: true }));
  textarea.dispatchEvent(new Event("change", { bubbles: true }));

  // DeepSeek: use Enter key to send
  textarea.dispatchEvent(new KeyboardEvent("keydown", {
    key: "Enter", code: "Enter", which: 13, keyCode: 13, bubbles: true,
  }));
  return globalThis.__KC_LIB__.safeStringify({ ok: true, mode: "enter" });
})();
