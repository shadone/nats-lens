// Evaluate on Capacity after resources load. Checks physical geometry with real fonts.
(() => {
  const filter = document.querySelector(".cap-resource-filter");
  const note = filter?.querySelector("p");
  const select = filter?.querySelector("select");
  if (!note || !select) throw new Error("Resource filter is not loaded");
  const box = filter.getBoundingClientRect();
  const explanation = note.getBoundingClientRect();
  if (explanation.width < Math.min(240, box.width - 32)) {
    throw new Error(`Count explanation cramped: ${explanation.width}px at ${innerWidth}px`);
  }
  if (document.documentElement.scrollWidth > innerWidth) throw new Error("Page overflows viewport");
  return { viewport: innerWidth, explanationWidth: explanation.width, result: "pass" };
})();
