const {chromium} = require('playwright-core');
const fs = require('node:fs/promises');
const path = require('node:path');
const {pathToFileURL} = require('node:url');
const assert = require('node:assert/strict');

async function record() {
  const sample=path.resolve(process.argv[2]);
  const output=path.resolve(process.argv[3]);
  await fs.mkdir(output,{recursive:true});
  const checks={};
  const browser=await chromium.launch({channel:'chrome'});
  try {
    for(const [name,viewport] of Object.entries({desktop:{width:1280,height:800},phone:{width:390,height:844}})) {
      const context=await browser.newContext({viewport,recordVideo:{dir:output,size:viewport}});
      const requests=[], errors=[];
      await context.route(/^https?:/,route=>{requests.push(route.request().url());return route.abort();});
      const page=await context.newPage();
      page.on('pageerror',error=>errors.push(error.message));
      await page.goto(pathToFileURL(path.join(sample,'claim.html')).href);
      await page.locator('#value').waitFor();
      assert.equal(await page.locator('#value').textContent(),'LF-042 IVORY');
      await page.screenshot({path:path.join(output,name+'.png')});
      const actual=[];
      for(const button of await page.locator('#timeline button').all()) {
        await button.click();
        actual.push(await page.locator('#value').textContent());
      }
      assert.deepEqual(actual,['Process 1','LF-042','Exit -9','Process 2','LF-042 IVORY']);
      if(name==='desktop') {
        await page.getByRole('button',{name:'Replay recorded timing'}).click();
        assert.equal(await page.locator('#value').textContent(),'Process 1');
        await page.waitForFunction(()=>document.querySelector('#value').textContent==='LF-042 IVORY',{},{timeout:120000});
      }
      assert.deepEqual(requests,[]);assert.deepEqual(errors,[]);
      assert.equal(await page.evaluate(()=>document.documentElement.scrollWidth>innerWidth),false);
      const video=page.video();
      await context.close();
      await fs.rename(await video.path(),path.join(output,name+'.webm'));
      checks[name]={viewport,actual,requests,errors,recordedTimingPlayed:name==='desktop'};
    }
  } finally { await browser.close(); }
  await fs.writeFile(path.join(output,'browser.json'),JSON.stringify(checks,null,2)+'\n');
}
record().catch(err=>{console.error(err);process.exitCode=1;});
