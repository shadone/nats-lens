// Run in the synthetic UI after hovering a chart. No navigation or data access.
(() => {
  const results = [...document.querySelectorAll('.chart-inspection-wrap')].flatMap(wrapper => {
    const button = [...wrapper.children].find(node => node.matches('.investigation-copy'));
    const svg = wrapper.querySelector('.chart-inspection svg');
    if (!button || !svg || getComputedStyle(button).opacity === '0') return [];
    const plot = svg.getBoundingClientRect(), action = button.getBoundingClientRect();
    if (!plot.width || !plot.height) return [];
    const overlap = Math.max(0, Math.min(plot.right, action.right) - Math.max(plot.left, action.left)) *
      Math.max(0, Math.min(plot.bottom, action.bottom) - Math.max(plot.top, action.top));
    return [{ label: wrapper.querySelector('[role=slider]')?.getAttribute('aria-label'), width: plot.width, overlap }];
  });
  if (!results.length) throw new Error('Hover a visible shared chart before checking copy geometry');
  if (results.some(result => result.overlap > 0.1)) throw new Error(`Copy action obstructs chart inspection: ${JSON.stringify(results)}`);
  return results;
})()
