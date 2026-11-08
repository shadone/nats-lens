// Production regression: hold every new JS request after Fleet. A route's primary
// API must start without waiting for presentation code. No performance SLO here.
// Use an installed Playwright Core module; do not install project dependencies.
import assert from 'node:assert/strict';
const { chromium } = await import(process.env.PLAYWRIGHT_MODULE);
const base = process.env.DEMO_URL;
const browser = await chromium.launch({ executablePath: process.env.CHROME_PATH, headless: true, args: ['--disable-background-networking'] });
const results = [];
try {
  for (const [path, method] of [['/capacity','GetCapacity'], ['/topology','GetTopology'], ['/placement','GetPlacement'], ['/replay','GetStatusTimeline']]) {
    const context = await browser.newContext({ viewport: { width:1280, height:800 }, serviceWorkers:'block' });
    const page = await context.newPage();
    await page.goto(base + '/fleet?system=example-prod&range=6h&account=APP');
    await page.getByText('3 systems, worst first').waitFor();
    const held = [];
    await page.route('**/assets/*.js', route => { held.push(route); });
    const request = page.waitForRequest(r => r.url().endsWith('/' + method), { timeout:2000 });
    await page.locator(`nav a[href^="${path}"]`).click();
    try {
      const r = await request;
      const body = r.postDataJSON();
      assert.equal(body.system, 'example-prod');
      assert.ok(body.range, 'explicit range must be retained');
      results.push({ path, method, held:held.length, request:body, passed:true });
    } catch (error) { results.push({ path, method, held:held.length, passed:false, error:error.message }); }
    await context.close();
  }
  console.log(JSON.stringify(results, null, 2));
  assert.ok(results.every(r => r.passed), 'primary reads must begin while any presentation code is held');
} finally { await browser.close(); }
