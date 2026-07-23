(() => {
  // GLM input: textarea inside #search-input-box
  const textareas = Array.from(document.querySelectorAll('textarea'));
  const inputs = textareas.map((el) => ({
    tag: el.tagName,
    className: String(el.className || "").slice(0, 200),
    id: el.id || "",
    value: (el.value || "").slice(0, 80),
  }));

  const sendButton = document.querySelector('.enter-icon-container');
  const readyInput = inputs.length > 0;

  return JSON.stringify({
    url: location.href,
    title: document.title,
    ready: readyInput,
    inputCount: inputs.length,
    inputs,
    sendButton: sendButton ? {
      found: true,
      className: String(sendButton.className || "").slice(0, 160),
    } : null,
  });
})();
