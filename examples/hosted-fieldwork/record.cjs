const {chromium} = require('playwright-core');
const fs = require('node:fs/promises');
const path = require('node:path');
const {pathToFileURL} = require('node:url');
const assert = require('node:assert/strict');

async function main() {
  const pageUrl = pathToFileURL(path.resolve(process.argv[2])).href;
  const output = path.resolve(process.argv[3]);
  await fs.mkdir(output);
  const browser = await chromium.launch({channel:'chrome'});
  const observations = [];
  try {
    for (const [name, viewport] of Object.entries({desktop:{width:1280,height:840},phone:{width:390,height:844}})) {
      const context = await browser.newContext({viewport,recordVideo:{dir:output,size:viewport}});
      const external = [];
      await context.route(/^https?:/, route => {external.push(route.request().url());return route.abort();});
      const page = await context.newPage();
      const errors = [];
      page.on('pageerror', error => errors.push(error.message));
      await page.goto(pageUrl);
      const counts = await page.locator('.stats b').allTextContents();
      assert.deepEqual(counts, ['78','81','85']);
      assert.equal(await page.locator('details').count(),85);
      await page.getByRole('button',{name:'Zürich to the museum'}).click();
      assert.equal(await page.locator('details[open]').count(),1);
      assert.equal(await page.locator('details a').count(),2);
      await page.screenshot({path:path.join(output,name+'.png')});
      await page.getByRole('button',{name:'Clear filters'}).click();
      await page.locator('#filter').selectOption('miss');
      const missed = await page.locator('summary b').allTextContents();
      assert.equal(missed.length,4);
      await page.locator('#query').fill('Windsurf');
      assert.match(await page.locator('details[open]').innerText(),/Missed · site root/);
      assert.equal(await page.evaluate(()=>document.documentElement.scrollWidth>innerWidth),false);
      assert.deepEqual(errors,[]);
      assert.deepEqual(external,[]);
      observations.push({name,viewport,counts,missed,external,errors});
      const video = page.video();
      await context.close();
      await fs.rename(await video.path(),path.join(output,name+'.webm'));
    }
  } finally {await browser.close();}
  await fs.writeFile(path.join(output,'browser.json'),JSON.stringify(observations,null,2)+'\n');
}
main().catch(error=>{console.error(error);process.exitCode=1;});
