// Deterministic control of the exact production measurement observer. Virtual
// HTML has no app code, API or timing benchmark: it exercises destination identity
// and visibility before permitting a marker to become ready.
import assert from 'node:assert/strict';
import { observe } from './readiness-observer.mjs';
const { chromium } = await import(process.env.PLAYWRIGHT_MODULE);
const browser=await chromium.launch({executablePath:process.env.CHROME_PATH,headless:true});
try {
  const page=await browser.newPage();
  await page.addInitScript(observe);
  await page.route('**/*', route=>route.fulfill({contentType:'text/html',body:'<main><table id="old"><thead><tr><th>Application</th></tr></thead><tbody><tr><td>Earlier Applications row</td></tr></tbody></table></main>'}));
  await page.goto('http://127.0.0.1:18575/connections');
  const frames=()=>page.evaluate(()=>new Promise(r=>requestAnimationFrame(()=>requestAnimationFrame(r))));
  const unseen=async()=>{await frames();assert.equal(await page.evaluate(()=>window.readiness.length),0);};
  await unseen();
  await page.evaluate(()=>document.querySelector('main').insertAdjacentHTML('afterbegin','<form aria-label="Filter connections"></form>'));
  await unseen(); // A new form plus an earlier table is still not Connections data.
  await page.evaluate(()=>{
    document.querySelector('form').style.display='none';
    document.querySelector('main').insertAdjacentHTML('beforeend','<table id="own"><thead><tr><th>Server / CID</th></tr></thead><tbody><tr><td>nats-1 / 123</td></tr></tbody></table>');
  });
  await unseen(); // Its form must be visible.
  await page.evaluate(()=>{document.querySelector('form').style.display='';document.querySelector('#own').style.display='none';});
  await unseen(); // Its own row must be visible too.
  await page.evaluate(()=>document.querySelector('#own').style.display='');
  await page.waitForFunction(()=>window.readiness[0]?.frames);
  assert.equal(await page.evaluate(()=>window.readiness[0].path),'/connections');
  console.log('PASS: prior Applications table, mixed old table/new form, hidden destination form and hidden destination row excluded; visible own Connections row accepted.');
} finally { await browser.close(); }
