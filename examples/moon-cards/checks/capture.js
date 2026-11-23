// usage: NODE_PATH=<dir containing playwright> node capture.js DESK_DIR OUT_DIR
const { chromium } = require('playwright'); const path = require('path'), fs = require('fs');
const [desk, out] = process.argv.slice(2).map(p => path.resolve(p)); const url = 'file://' + desk + '/index.html';
const wait = ms => new Promise(r => setTimeout(r, ms));
(async () => {
  const b = await chromium.launch();
  for (const [name, w, h] of [['390', 390, 844], ['1280', 1280, 800]]) {
    const ctx = await b.newContext({ viewport: { width: w, height: h }, deviceScaleFactor: 1 }); const p = await ctx.newPage();
    const errs = []; p.on('pageerror', e => errs.push(String(e))); p.on('console', m => m.type() === 'error' && errs.push(m.text()));
    await p.goto(url); await wait(400);
    await p.screenshot({ path: `${out}/desk-${name}-1-question.png`, fullPage: true });
    await p.click('#rev'); await wait(400); await p.screenshot({ path: `${out}/desk-${name}-2-answer.png`, fullPage: true });
    await p.click('[data-t=compare]'); await wait(300); await p.screenshot({ path: `${out}/desk-${name}-3-compare.png`, fullPage: true });
    await p.click('[data-t=observed]'); await wait(300); await p.screenshot({ path: `${out}/desk-${name}-4-observed.png`, fullPage: true });
    const bad = await p.evaluate(() => document.documentElement.scrollWidth > innerWidth);
    console.log(name, 'horizontal overflow:', bad, 'errors:', errs); await ctx.close();
  }
  // film
  const ctx = await b.newContext({ viewport: { width: 1280, height: 720 }, recordVideo: { dir: `${out}/_vid`, size: { width: 1280, height: 720 } } });
  const p = await ctx.newPage(); await p.goto(url); await wait(1500);
  await p.click('[data-k="1"]'); await wait(1200); await p.keyboard.press('Space'); await wait(1800);
  await p.keyboard.press('3'); await wait(1200); await p.keyboard.press('Space'); await wait(1500); await p.keyboard.press('2'); await wait(1000);
  await p.click('[data-k="1"]'); await wait(800); await p.click('[data-e="1"]'); await wait(900); await p.click('#rev'); await wait(1500);
  await p.click('[data-e="2"]'); await wait(1500);
  await p.click('[data-t=compare]'); await wait(3000);
  await p.click('[data-t=observed]'); await wait(1500); await p.mouse.wheel(0, 500); await wait(2500);
  await ctx.close(); await b.close();
})();
