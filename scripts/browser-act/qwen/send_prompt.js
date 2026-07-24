(() => {
  const prompt = String(globalThis.__PAYLOAD__.prompt || "");
  if (!prompt) {
    return globalThis.__KC_LIB__.safeStringify({ ok: false, error: "empty prompt" });
  }
  delete globalThis["__QWEN_WAIT_STATE__"];

  const input =
    document.querySelector('[data-slate-editor="true"]') ||
    document.querySelector('[role="textbox"]') ||
    document.querySelector('[contenteditable="true"]') ||
    document.querySelector("textarea");

  if (!input) {
    return globalThis.__KC_LIB__.safeStringify({ ok: false, error: "input not found" });
  }

  input.focus();

  let mode = "dom";
  let inputOk = false;

  if (input.getAttribute("data-slate-editor") === "true") {
    const reactKey = Object.keys(input).find((key) => key.indexOf("__reactFiber") === 0);
    if (reactKey) {
      let current = input[reactKey];
      let slateEditor = null;
      let onChange = null;
      for (let i = 0; i < 30 && current; i += 1) {
        const props = current.memoizedProps || {};
        if (props.editor && typeof props.editor.insertText === "function") {
          slateEditor = props.editor;
          onChange = props.onChange;
          break;
        }
        current = current.return;
      }
      if (slateEditor) {
        mode = "slate";
        try {
          if (typeof slateEditor.deleteBackward === "function") {
            const existing = (input.innerText || "").trim();
            for (let i = 0; i < existing.length; i += 1) {
              slateEditor.deleteBackward("character");
            }
          }
        } catch (_) {}
        slateEditor.insertText(prompt);
        if (typeof onChange === "function") {
          onChange(slateEditor.children);
        }
        inputOk = true;
      }
    }
  }

  if (!inputOk) {
    if (input.tagName === "TEXTAREA") {
      input.value = prompt;
    } else {
      input.textContent = prompt;
      input.innerText = prompt;
    }
    input.dispatchEvent(new Event("input", { bubbles: true }));
    input.dispatchEvent(new Event("change", { bubbles: true }));
    inputOk = true;
  }

  const sendButton =
    document.querySelector('button[aria-label="发送消息"]') ||
    Array.from(document.querySelectorAll("button")).find((btn) => {
      const text = (btn.innerText || btn.textContent || "").trim();
      const aria = btn.getAttribute("aria-label") || "";
      return text.includes("发送") || aria.includes("发送");
    });

  let submitMode = "enter";
  if (sendButton && !sendButton.disabled) {
    sendButton.click();
    submitMode = "button";
  } else {
    input.dispatchEvent(new KeyboardEvent("keydown", {
      key: "Enter", code: "Enter", which: 13, keyCode: 13, bubbles: true,
    }));
  }

  return globalThis.__KC_LIB__.safeStringify({
    ok: true, mode, submitMode,
    inputTag: input.tagName,
    sendButtonDisabled: sendButton ? !!sendButton.disabled : null,
    url: location.href,
  });
})();
