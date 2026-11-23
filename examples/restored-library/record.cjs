const assert = require('node:assert/strict');
const {chromium} = require('playwright-core');
const {createHash} = require('node:crypto');
const fs = require('node:fs/promises');
const path = require('node:path');
const {pathToFileURL} = require('node:url');

async function main() {
  const root=path.resolve(process.argv[2]), dest=path.resolve(process.argv[3]);
  const evidence=JSON.parse(await fs.readFile(path.join(root,'report','comparison.json')));
  await fs.mkdir(dest);
  const browser=await chromium.launch({channel:'chrome'});
  const screens=[];
  try {
    for(const [screen,viewport] of Object.entries({desktop:{width:1280,height:830},phone:{width:390,height:844}})) {
      const context=await browser.newContext({viewport,recordVideo:{dir:dest,size:viewport}});
      const outside=[];await context.route(/^https?:/,r=>{outside.push(r.request().url());return r.abort();});
      const page=await context.newPage();const errors=[];page.on('pageerror',e=>errors.push(e.message));
      await page.goto(pathToFileURL(path.join(root,'report','index.html')).href);
      assert.equal(await page.getByText('✔ matches source',{exact:true}).count(),6);
      assert.equal(await page.locator('.day:visible').count(),14);
      await page.waitForTimeout(1100);
      await page.getByRole('checkbox').check();
      assert.equal(await page.locator('.day:visible').count(),6);
      assert.deepEqual(await page.locator('.original .day:visible b').allTextContents(),['Fri','Sat','Sun']);
      await page.screenshot({path:path.join(dest,screen+'.png'),fullPage:screen==='phone'});
      await page.waitForTimeout(1400);
      const downloads={};
      for(const edition of ['original','changed']) {
        const [download]=await Promise.all([page.waitForEvent('download'),page.getByRole('link',{name:'Download '+edition+' hours'}).click()]);
        const saved=await fs.readFile(await download.path());
        assert.deepEqual(saved,await fs.readFile(path.join(root,'restored',edition,'hours.csv')));
        downloads[edition]=createHash('sha256').update(saved).digest('hex');
        assert.equal(downloads[edition],evidence.sha256[edition]['hours.csv']);
      }
      await page.getByRole('checkbox').uncheck();
      assert.equal(await page.locator('.day:visible').count(),14);
      assert.equal(await page.evaluate(()=>document.documentElement.scrollWidth>innerWidth),false);
      assert.deepEqual(outside,[]);assert.deepEqual(errors,[]);
      const video=page.video();await context.close();await fs.rename(await video.path(),path.join(dest,screen+'.webm'));
      screens.push({screen,viewport,changed_days:evidence.changed_days,downloads,outside,errors});
    }
  } finally {await browser.close();}
  await fs.writeFile(path.join(dest,'browser.json'),JSON.stringify(screens,null,2)+'\n');
}
main().catch(error=>{console.error(error);process.exitCode=1;});
