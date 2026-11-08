// Structural scheduling control: code is evaluated before the first visit. Once
// the real primary response completes, two rendering frames must show its content,
// rather than an avoidable page-code fallback. This sets no millisecond SLO.
import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
const { chromium } = await import(process.env.PLAYWRIGHT_MODULE);
const chunks = JSON.parse(readFileSync(process.env.CHUNKS));
const browser = await chromium.launch({ executablePath:process.env.CHROME_PATH, headless:true, args:['--disable-background-networking'] });
const results=[];
try {
  for(const [step, method, module] of [['streams','ListStreams','Streams'], ['checks','ListCheckDefinitions','Checks']]) {
    const context = await browser.newContext({ viewport:{width:1280,height:800}, serviceWorkers:'block' });
    const page = await context.newPage();
    await page.goto(process.env.DEMO_URL+'/fleet?system=example-prod&range=6h');
    await page.getByText('3 systems, worst first').waitFor();
    const chunk=chunks.find(c=>c.modules.some(id=>id.endsWith('/pages/'+module+'.tsx')));
    assert.ok(chunk, 'recorded production graph must identify the route module');
    await page.evaluate(async file=>await import('/'+file),chunk.file);
    const response=page.waitForResponse(r=>r.url().endsWith('/'+method));
    const start=await page.evaluate(()=>performance.now());
    await page.locator(`nav a[href^="/${step}"]`).click();
    await (await response).finished();
    await page.evaluate(()=>new Promise(resolve=>requestAnimationFrame(()=>requestAnimationFrame(resolve))));
    const result=await page.evaluate(({step,start})=>{
      const main=document.querySelector('main');
      const marker=step==='checks'?main?.querySelector('input[placeholder="Code, name, or meaning"]'):[...main?.querySelectorAll('a')??[]].find(e=>e.textContent==='order-jobs');
      return {step,elapsed:performance.now()-start,visible:!!marker?.checkVisibility(),pending:main?.textContent.includes('Loading page…'),scripts:performance.getEntriesByType('resource').filter(r=>r.startTime>=start&&r.name.endsWith('.js')).map(r=>r.name)};
    },{step,start});
    results.push(result);
    await context.close();
  }
  console.log(JSON.stringify(results,null,2));
  assert.ok(results.every(r=>r.scripts.length===0), 'pre-import control must have no navigation code transfer');
  assert.ok(results.every(r=>r.visible&&!r.pending), 'ready code and primary data must render without a page-code fallback retry');
} finally { await browser.close(); }
