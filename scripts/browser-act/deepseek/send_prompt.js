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

  // Verify the editor consumed the prompt; on a freshly mounted editor the
  // synthetic Enter is sometimes swallowed. Fall back to clicking the
  // composer's send button (a div.ds-button--circle, not a <button>).
  return new Promise((resolve) => {
    setTimeout(() => {
      if ((textarea.value || "").trim().length === 0) {
        resolve(globalThis.__KC_LIB__.safeStringify({ ok: true, mode: "enter" }));
        return;
      }
      const candidates = [
        '.ds-button--primary.ds-button--circle',
        'div[class*="ds-button"][class*="circle"]',
      ];
      let clicked = false;
      for (const sel of candidates) {
        const btn = Array.from(document.querySelectorAll(sel)).find((el) => {
          const r = el.getBoundingClientRect();
          return r.width > 0 && r.height > 0;
        });
        if (btn) {
          btn.click();
          clicked = true;
          break;
        }
      }
      setTimeout(() => {
        const consumed = (textarea.value || "").trim().length === 0;
        resolve(globalThis.__KC_LIB__.safeStringify({
          ok: consumed, mode: clicked ? "button" : "enter", consumed,
          error: consumed ? undefined : "prompt not consumed after enter+button",
        }));
      }, 800);
    }, 800);
  });
})();
