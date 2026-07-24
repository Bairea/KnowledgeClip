(() => {
  const prompt = String(globalThis.__PAYLOAD__.prompt || "");
  if (!prompt) {
    return globalThis.__KC_LIB__.safeStringify({ ok: false, error: "empty prompt" });
  }
  delete globalThis["__KIMI_WAIT_STATE__"];

  const input = document.querySelector('div.chat-input-editor') ||
    document.querySelector('[role="textbox"]') ||
    document.querySelector('[contenteditable="true"]');
  if (!input) {
    return globalThis.__KC_LIB__.safeStringify({ ok: false, error: "input not found" });
  }

  input.focus();

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
    input.textContent = prompt;
    input.dispatchEvent(new Event("input", { bubbles: true }));
    input.dispatchEvent(new Event("change", { bubbles: true }));
    mode = "textContent";
  }

  return new Promise((resolve) => {
    setTimeout(() => {
      const sendBtn = document.querySelector('div.send-button-container') ||
        document.querySelector('[class*="send"]');
      if (sendBtn) {
        const disabled = sendBtn.hasAttribute("disabled") ||
          sendBtn.classList.contains("disabled");
        if (!disabled) {
          sendBtn.click();
          resolve(globalThis.__KC_LIB__.safeStringify({ ok: true, mode, submitMode: "button" }));
          return;
        }
      }
      input.dispatchEvent(new KeyboardEvent("keydown", {
        key: "Enter", code: "Enter", which: 13, keyCode: 13, bubbles: true,
      }));
      resolve(globalThis.__KC_LIB__.safeStringify({ ok: true, mode, submitMode: "enter" }));
    }, 500);
  });
})();
