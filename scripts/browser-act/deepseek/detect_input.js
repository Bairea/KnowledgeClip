(() => {
  // DeepSeek input: textarea[name="search"]
  const textareas = Array.from(document.querySelectorAll('textarea'));
  const inputs = textareas.map((el) => ({
    tag: el.tagName,
    name: el.name || "",
    placeholder: el.placeholder || "",
    className: String(el.className || "").slice(0, 200),
  }));

  const readyInput = inputs.length > 0;

  return JSON.stringify({
    url: location.href,
    title: document.title,
    ready: readyInput,
    inputCount: inputs.length,
    inputs,
  });
})();
