// Run this expression with preview_evaluate on the synthetic demo server page
// at 360, 761 and 1280 CSS pixels, after its recent events have loaded.
// Set window.serverDetailLongContent = true first to stress wrapping.
(() => {
  const region = (id) => document.getElementById(id)?.closest('section');
  const cards = ['sd-where', 'sd-routes', 'sd-limits'].map(region).filter(Boolean);
  const recent = region('sd-recent');
  if (!recent || cards.length < 2) throw new Error('Open a populated demo server detail first');
  if (window.serverDetailLongContent) {
    const token = 'synthetic'.repeat(40);
    document.querySelectorAll('.sd-dl dd, .sd-recent li a').forEach((el) => {
      // Keep link semantics intact; change only text in this synthetic browser fixture.
      const target = el.querySelector('a') || el;
      target.textContent = `Long synthetic server detail ${token} with more words to wrap across lines.`;
    });
  }
  const bounds = (el) => {
    const r = el.getBoundingClientRect();
    return { left: r.left, right: r.right, top: r.top, bottom: r.bottom, width: r.width };
  };
  const grid = bounds(cards[0].parentElement);
  const events = bounds(recent);
  const failures = [];
  if (events.top < Math.max(...cards.map((el) => bounds(el).bottom)) + 11)
    failures.push('Recent events must follow all information cards with a 12px gap');
  if (Math.abs(events.left - grid.left) > 1 || Math.abs(events.width - grid.width) > 1)
    failures.push('Recent events must span the information grid width');
  for (const el of [...cards, recent]) {
    if (el.scrollWidth > el.clientWidth + 1) failures.push(`${el.getAttribute('aria-labelledby')} overflows`);
    const r = bounds(el);
    if (r.left < 0 || r.right > innerWidth + 1) failures.push('Card extends outside viewport');
  }
  // Check text fragments, not just their containing boxes: nowrap descendants
  // can overflow a zero-minimum grid cell while the cell itself still fits.
  document.querySelectorAll('.sd-dl dd, .sd-recent li a').forEach((el) => {
    const range = document.createRange();
    range.selectNodeContents(el);
    const card = bounds(el.closest('section'));
    for (const r of range.getClientRects()) {
      if (r.left < card.left || r.right > card.right) failures.push('Text extends outside its card');
    }
  });
  if (document.documentElement.scrollWidth > innerWidth + 1) failures.push('Page has horizontal overflow');
  const result = { width: innerWidth, longContent: !!window.serverDetailLongContent,
    cards: cards.map(bounds), events, failures: [...new Set(failures)] };
  if (failures.length) throw new Error(JSON.stringify(result));
  return result;
})()
