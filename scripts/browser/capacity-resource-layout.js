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
  const charts = [...document.querySelectorAll('.cap-tbl tbody th[scope="row"] .cap-history-compact')];
  for (const chart of charts) {
    const cell = chart.closest('th').getBoundingClientRect();
    const svg = chart.querySelector('svg').getBoundingClientRect();
    if (svg.left < cell.left || svg.right > cell.right) {
      throw new Error(`Compact history crosses its resource cell: chart ${svg.left}..${svg.right}, cell ${cell.left}..${cell.right}`);
    }
    if (svg.width < Math.min(80, cell.width * 0.8)) throw new Error("Compact history has no usable width");
  }
  return { viewport: innerWidth, explanationWidth: explanation.width, compactCharts: charts.length, result: "pass" };
})();
