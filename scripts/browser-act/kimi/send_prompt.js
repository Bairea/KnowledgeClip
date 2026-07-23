(() => {
  const prompt = String(globalThis.__PAYLOAD__.prompt || "");
  if (!prompt) {
    return JSON.stringify({ ok: false, error: "empty prompt" });
  }

  // Kimi input: div.chat-input-editor[role="textbox"]
  const input = document.querySelector('div.chat-input-editor') || document.querySelector('[role="textbox"]');
  if (!input) {
    return JSON.stringify({ ok: false, error: "input not found" });
  }

  input.focus();

  // Kimi uses a rich text editor - use textContent + input event
  input.textContent = prompt;
  input.dispatchEvent(new Event("input", { bubbles: true }));
  input.dispatchEvent(new Event("change", { bubbles: true }));

  // Small delay for React state to update
  return new Promise((resolve) => {
    setTimeout(() => {
      // Try to find and click the send button
      const sendBtn = document.querySelector('div.send-button-container');
      if (sendBtn && !sendBtn.hasAttribute('disabled') && !sendBtn.classList.contains('disabled')) {
        sendBtn.click();
        resolve(JSON.stringify({ ok: true, mode: "button" }));
      } else {
        // Fallback to Enter key
        input.dispatchEvent(new KeyboardEvent("keydown", {
          key: "Enter", code: "Enter", which: 13, keyCode: 13, bubbles: true,
        }));
        resolve(JSON.stringify({ ok: true, mode: "enter" }));
      }
    }, 300);
  });
})();
