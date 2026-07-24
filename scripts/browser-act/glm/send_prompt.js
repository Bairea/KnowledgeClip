(() => {
  const prompt = String(globalThis.__PAYLOAD__.prompt || "");
  if (!prompt) {
    return JSON.stringify({ ok: false, error: "empty prompt" });
  }
  delete globalThis["__GLM_WAIT_STATE__"];

  // GLM input: textarea inside #search-input-box
  const textarea = document.querySelector('textarea') || document.querySelector('#search-input-box textarea');
  if (!textarea) {
    return JSON.stringify({ ok: false, error: "input not found" });
  }

  textarea.focus();

  // Use native setter + InputEvent for Vue/Element Plus reactivity
  const nativeSetter = Object.getOwnPropertyDescriptor(window.HTMLTextAreaElement.prototype, 'value').set;
  nativeSetter.call(textarea, prompt);
  textarea.dispatchEvent(new InputEvent("input", { bubbles: true, data: prompt, inputType: "insertText" }));

  const urlBefore = location.href;

  return new Promise((resolve) => {
    setTimeout(() => {
      // Primary: Enter key on textarea (GLM's send button is a DIV, .click() does not trigger Vue submit)
      textarea.dispatchEvent(new KeyboardEvent("keydown", {
        key: "Enter", code: "Enter", which: 13, keyCode: 13, bubbles: true, cancelable: true,
      }));
      textarea.dispatchEvent(new KeyboardEvent("keypress", {
        key: "Enter", code: "Enter", which: 13, keyCode: 13, bubbles: true, cancelable: true,
      }));
      textarea.dispatchEvent(new KeyboardEvent("keyup", {
        key: "Enter", code: "Enter", which: 13, keyCode: 13, bubbles: true, cancelable: true,
      }));

      setTimeout(() => {
        // Verify: URL changed (cid param added) means submit succeeded
        if (location.href !== urlBefore) {
          resolve(JSON.stringify({ ok: true, mode: "enter", verified: "urlChanged" }));
          return;
        }
        // Fallback: click send button
        const sendBtn = document.querySelector('.enter-icon-container') ||
          document.querySelector('[class*="enter"]') ||
          document.querySelector('[class*="send"]');
        if (sendBtn) {
          sendBtn.dispatchEvent(new MouseEvent("mousedown", { bubbles: true }));
          sendBtn.dispatchEvent(new MouseEvent("mouseup", { bubbles: true }));
          sendBtn.click();
        }
        resolve(JSON.stringify({ ok: true, mode: "button-fallback" }));
      }, 1500);
    }, 500);
  });
})();
