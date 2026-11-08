export function observe() {
  window.readiness = [];
  window.routeClicks = [];
  document.addEventListener('click', e => {
    const a = e.target.closest('a');
    if (a) window.routeClicks.push({ href:a.href, at:performance.now(), trusted:e.isTrusted });
  }, true);
  const seen = new Set();
  function check() {
    const p = location.pathname, main = document.querySelector('main');
    const el = p === '/fleet' && main?.textContent.includes('3 systems, worst first') ? main
      : p === '/streams' ? [...main?.querySelectorAll('a') ?? []].find(e => e.textContent === 'order-jobs')
      : p === '/checks' ? main?.querySelector('input[placeholder="Code, name, or meaning"]')
      : p === '/capacity' ? main?.querySelector('.cap-server')
      : p === '/topology' ? main?.querySelector('.tp-tables')
      : p === '/placement' ? main?.querySelector('.pm-cell')
      : p === '/replay' ? main?.querySelector('.rp-story')
      : p === '/accounts' ? main?.querySelector('.ac-cards')
      : p === '/applications' ? main?.querySelector('.ap-cards')
      : p === '/connections' && main?.querySelector('form[aria-label="Filter connections"]')?.checkVisibility()
        ? [...main.querySelectorAll('table')].find(t => [...t.querySelectorAll('thead th')].some(th => th.textContent.trim() === 'Server / CID'))?.querySelector('tbody tr')
      : p === '/data-history' ? main?.querySelector('.dh-summary')
      : p === '/micro-services' ? main?.querySelector('.mi-name') : null;
    if (!el?.checkVisibility() || seen.has(p)) return;
    seen.add(p);
    const mark = { path:p, dom:performance.now() };
    window.readiness.push(mark);
    requestAnimationFrame(() => requestAnimationFrame(() => { mark.frames = performance.now(); }));
  }
  new MutationObserver(check).observe(document, { subtree:true, childList:true, attributes:true, attributeFilter:['style','hidden'] });
  function tick() { check(); requestAnimationFrame(tick); }
  requestAnimationFrame(tick);
}
