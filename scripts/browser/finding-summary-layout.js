// Evaluate in a browser on a finding detail page (open More details on phones).
// Throws if facts or actions touch; measures rendered geometry, not CSS rules.
(() => {
  const head = document.querySelector(".finding-h");
  if (!head || !head.getBoundingClientRect().height) throw new Error("Finding summary is not visible");
  const problems = [];
  const facts = [...head.querySelectorAll(".meta > div:not(.btns)")];
  for (const fact of facts) {
    const label = fact.querySelector(".lbl");
    const value = fact.querySelector(".v");
    if (!label || !value) continue;
    const l = label.getBoundingClientRect(), v = value.getBoundingClientRect();
    const stacked = v.top >= l.bottom;
    const gap = stacked ? v.top - l.bottom : v.left - l.right;
    if (gap < 4) problems.push(`${label.textContent}: label/value gap ${gap}px`);
  }
  const buttons = [...head.querySelectorAll(".btns button")];
  for (let i = 1; i < buttons.length; i++) {
    const a = buttons[i - 1].getBoundingClientRect(), b = buttons[i].getBoundingClientRect();
    const gap = b.top >= a.bottom ? b.top - a.bottom : b.left - a.right;
    if (gap < 8) problems.push(`Action button gap ${gap}px`);
  }
  if (document.documentElement.scrollWidth > innerWidth) problems.push("Page overflows viewport");
  if (problems.length) throw new Error(problems.join("; "));
  return { width: innerWidth, facts: facts.length, actions: buttons.length, result: "pass" };
})();
