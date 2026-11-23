const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const { pathToFileURL } = require('node:url');
const { chromium } = require('playwright-core');

async function run() {
  const dir = path.join(__dirname, 'sample');
  const cases = JSON.parse(fs.readFileSync(path.join(dir, 'checks.json')));
  const results = [];
  const browser = await chromium.launch({ executablePath: process.env.CHROME, headless: true });
  try {
    for (const width of [1280, 390]) {
      const context = await browser.newContext({ viewport: { width, height: 900 }, offline: true });
      const page = await context.newPage();
      await page.goto(pathToFileURL(path.join(dir, 'four-cohorts.html')).href);
      assert.equal(await page.locator('#count').innerText(), '107 cases');
      await page.screenshot({ path: path.join(dir, width === 1280 ? 'desktop.png' : 'phone.png') });
      await page.getByRole('button', {name:'Inspect the repeated hour'}).click();
      assert.match(await page.locator('details[open]').innerText(), /zoneinfo/);
      await page.getByRole('button', {name:'Greater Accra'}).click();
      assert.match(await page.locator('details[open]').innerText(), /160,446 \(2\.94%\)/);
      for (const r of cases) {
        await page.locator('#query').fill(r.id);
        assert.equal(await page.locator('details[open] .body li').count(), Object.keys(r.checks).length);
        assert.match(await page.locator('details[open]').innerText(), new RegExp(r.stdout_sha256));
        assert(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth));
      }
      await page.getByRole('button', { name:'Clear filters' }).click();
      await page.locator('#filter').selectOption('miss');
      assert.equal(await page.locator('#count').innerText(), '4 cases');
      const missed = await page.locator('details summary b').allTextContents();
      results.push({width, checked_ids: cases.map(r => r.id), missed, offline:true});
      await context.close();
    }
  } finally { await browser.close(); }
  fs.writeFileSync(path.join(dir,'browser.json'),JSON.stringify(results,null,2)+'\n');
}
run().catch(e => { console.error(e); process.exitCode=1; });
