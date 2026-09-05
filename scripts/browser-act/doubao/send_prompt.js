(() => {
  const prompt = String(globalThis.__PAYLOAD__.prompt || "");
  if (!prompt) {
    return globalThis.__KC_LIB__.safeStringify({ ok: false, error: "empty prompt" });
  }
  delete globalThis["__DOUBAO_WAIT_STATE__"];

  // Doubao input: tiptap/ProseMirror contenteditable editor (legacy textarea
  // still supported as a fallback).
  const textarea = document.querySelector('textarea');
  const editor = document.querySelector('div[contenteditable="true"]');
  const input = editor || textarea;
  if (!input) {
    return globalThis.__KC_LIB__.safeStringify({ ok: false, error: "input not found" });
  }

  input.focus();

  let mode = "pmView";
  let inputOk = false;

  if (editor) {
    // Preferred: drive the ProseMirror view directly (no window focus or
    // execCommand needed, works from a background agent tab).
    try {
      if (editor.pmViewDesc && editor.pmViewDesc.view) {
        const view = editor.pmViewDesc.view;
        view.dispatch(view.state.tr.insertText(prompt));
        inputOk = true;
      }
    } catch (e) {}
    if (!inputOk) {
      try {
        document.execCommand("selectAll", false, null);
        document.execCommand("insertText", false, prompt);
        inputOk = true;
        mode = "execCommand";
      } catch (e) {}
    }
    if (!inputOk) {
      editor.textContent = prompt;
      editor.dispatchEvent(new InputEvent("input", {
        bubbles: true, inputType: "insertText", data: prompt,
      }));
      mode = "textContent";
      inputOk = true;
    }
  } else {
    // Legacy textarea: native setter for React controlled components.
    const nativeSetter = Object.getOwnPropertyDescriptor(window.HTMLTextAreaElement.prototype, 'value').set;
    nativeSetter.call(textarea, prompt);
    textarea.dispatchEvent(new Event("input", { bubbles: true }));
    textarea.dispatchEvent(new Event("change", { bubbles: true }));
    inputOk = true;
    mode = "textarea";
  }

  const gotText = (input.innerText || input.textContent || input.value || "").trim();
  const landed = gotText.includes(prompt.slice(0, 20));

  // The send button appears/enables after the editor state settles; wait a
  // beat, then click it (Enter as fallback for the guidance input).
  return new Promise((resolve) => {
    setTimeout(() => {
      const sendBtn = document.querySelector('#flow-end-msg-send') ||
                      document.querySelector('div[class*="send-button"]') ||
                      document.querySelector('button[class*="send"]') ||
                      document.querySelector('div[class*="send-btn"]');
      if (sendBtn && !sendBtn.disabled) {
        sendBtn.click();
        resolve(globalThis.__KC_LIB__.safeStringify({
          ok: true, mode, submitMode: "button", landed, textLen: gotText.length,
        }));
        return;
      }
      input.dispatchEvent(new KeyboardEvent("keydown", {
        key: "Enter", code: "Enter", which: 13, keyCode: 13, bubbles: true,
      }));
      resolve(globalThis.__KC_LIB__.safeStringify({
        ok: true, mode, submitMode: "enter", landed, textLen: gotText.length,
      }));
    }, 600);
  });
})();