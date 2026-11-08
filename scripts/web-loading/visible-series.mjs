// Compare owned embedded production demos. Requires an existing Playwright Core
// module and Chrome. Run only in a coordinated quiet measurement window.
import { readFileSync, writeFileSync } from 'node:fs';
const { chromium } = await import(process.env.PLAYWRIGHT_MODULE);
const variants = JSON.parse(process.env.VARIANTS);
const browser = await chromium.launch({ executablePath:process.env.CHROME_PATH, headless:true, args:['--disable-background-networking'] });
const samples = [];
function observe() {
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
      : p === '/replay' ? main?.querySelector('.rp-story') : null;
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
async function context(constrained) {
  const ctx = await browser.newContext({ viewport:{width:1280,height:800}, serviceWorkers:'block' });
  const page = await ctx.newPage(), cdp = await ctx.newCDPSession(page);
  await cdp.send('Network.enable');
  await cdp.send('Network.setCacheDisabled', { cacheDisabled:true });
  await cdp.send('Emulation.setCPUThrottlingRate', { rate:constrained?4:1 });
  await cdp.send('Network.emulateNetworkConditions', { offline:false, latency:constrained?150:0, downloadThroughput:constrained?200000:-1, uploadThroughput:constrained?93750:-1 });
  await cdp.send('Performance.enable');
  await page.addInitScript(observe);
  return { ctx, page, cdp };
}
async function collect(page, cdp, step, start) {
  await page.waitForFunction(path => window.readiness?.some(x => x.path === path && x.frames), '/' + step, {timeout:20000});
  const evidence = await page.evaluate(({ step,start }) => ({ step,start,observed:performance.now(), probe:window.readiness.find(x=>x.path==='/'+step), click:window.routeClicks.at(-1), resources:performance.getEntriesByType('resource').filter(x=>x.startTime>=start).map(x=>x.toJSON()), navigation:performance.getEntriesByType('navigation').map(x=>x.toJSON()), paints:performance.getEntriesByType('paint').map(x=>x.toJSON()) }), { step,start });
  evidence.metrics = (await cdp.send('Performance.getMetrics')).metrics;
  return evidence;
}
try {
  for (const constrained of [false,true]) for(let run=0;run<3;run++) for(const [variant,base] of variants) {
    const {ctx,page,cdp} = await context(constrained), steps=[];
    for(const step of ['fleet','streams','checks','capacity','topology','placement','replay']) {
      const start=step==='fleet'?0:await page.evaluate(()=>performance.now());
      if(step==='fleet') await page.goto(base+'/fleet?system=example-prod&range=6h');
      else await page.locator(`nav a[href^="/${step}"]`).click();
      steps.push(await collect(page,cdp,step,start));
    }
    samples.push({variant,constrained,run,kind:'navigation',steps});
    console.log(variant,constrained,run,steps.map(s=>[s.step,Math.round(s.probe.dom-s.start),Math.round(s.probe.frames-s.start)]));
    await ctx.close();
  }
  // Cold/direct is a separate series: it includes all initial shared/preloads.
  for(const step of ['checks','capacity','topology','placement','replay']) for(const [variant,base] of variants) {
    const {ctx,page,cdp}=await context(true);
    await page.goto(base+'/'+step+'?system=example-prod&range=6h');
    samples.push({variant,constrained:true,kind:'direct',steps:[await collect(page,cdp,step,0)]});
    await ctx.close();
  }
  // First view visit after ESM evaluation: it is diagnostic, not a cold sample.
  const [variant,base]=variants.at(-1), chunks=JSON.parse(readFileSync(process.env.CHUNKS));
  const {ctx,page,cdp}=await context(false);
  await page.goto(base+'/fleet?system=example-prod&range=6h');
  await collect(page,cdp,'fleet',0);
  for(const step of ['streams','checks','topology','placement','replay']) {
    const chunk=chunks.find(c=>c.modules.some(id=>id.endsWith('/views/'+step[0].toUpperCase()+step.slice(1)+'.tsx')));
    if(chunk) await page.evaluate(async file=>await import('/'+file),chunk.file);
    const start=await page.evaluate(()=>performance.now());
    await page.locator(`nav a[href^="/${step}"]`).click();
    samples.push({variant,constrained:false,kind:'preimported',imported:chunk?.file??'eager',steps:[await collect(page,cdp,step,start)]});
  }
  await ctx.close();
  writeFileSync(process.env.RESULT,JSON.stringify({browser:browser.version(),samples},null,2));
} finally { await browser.close(); }
