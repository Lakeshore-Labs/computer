const {chromium} = require('playwright-core');
const {createHash} = require('node:crypto');
const fs = require('node:fs/promises');
const path = require('node:path');
const {pathToFileURL} = require('node:url');
const assert = require('node:assert/strict');

const hash = bytes => createHash('sha256').update(bytes).digest('hex');
async function run() {
  const root = path.resolve(process.argv[2] || 'run1');
  const models = JSON.parse(await fs.readFile(path.join(root, 'models.json'))).models;
  const visual = path.join(root, 'visual');
  await fs.mkdir(visual,{recursive:true});
  const browser = await chromium.launch({channel:'chrome',args:['--enable-unsafe-swiftshader','--use-gl=angle','--use-angle=swiftshader']});
  const checks = {};
  try {
    for (const [name, viewport] of Object.entries({desktop:{width:1280,height:800},phone:{width:390,height:844}})) {
      const context = await browser.newContext({viewport,recordVideo:{dir:visual,size:viewport},acceptDownloads:true});
      const external = [], errors = [];
      await context.route(/^https?:/, route => { external.push(route.request().url());return route.abort(); });
      const page = await context.newPage();
      page.on('pageerror', error => errors.push(error.message));
      await page.goto(pathToFileURL(path.join(root,'model.html')).href);
      await page.locator('#stage[data-ready=true]').waitFor();
      const canvas = page.locator('#stage canvas');
      const box = await canvas.boundingBox();
      const frameHash = () => canvas.evaluate(c => c.toDataURL()).then(hash);
      const before = await frameHash();
      await page.screenshot({path:path.join(visual,name+'.png')});
      await page.mouse.move(box.x+box.width*.55,box.y+box.height*.48);
      await page.mouse.down();
      await page.mouse.move(box.x+box.width*.24,box.y+box.height*.59,{steps:20});
      await page.mouse.up();
      const rotated = await frameHash();
      assert.notEqual(rotated,before);
      await page.locator('#reset').click();
      const reset = await frameHash();
      assert.equal(reset,before);
      const receipts = [];
      for (const model of models) {
        await page.locator('#scale').selectOption(String(model.vertical_exaggeration));
        assert.equal(await page.locator('#stage').getAttribute('data-triangles'),String(model.triangles));
        const metric = await page.locator('#metrics').textContent();
        assert.equal(metric, '110 × 118.8 × '+model.height_mm.toFixed(1)+' mm');
        const [download] = await Promise.all([page.waitForEvent('download'),page.locator('#download').click()]);
        const digest = hash(await fs.readFile(await download.path()));
        assert.equal(digest,model.sha256);
        const savedFrame = await frameHash();
        receipts.push({name:download.suggestedFilename(),metric,triangles:model.triangles,sha256:digest,frame:savedFrame});
      }
      assert.notEqual(receipts[0].frame,receipts[1].frame);
      assert.deepEqual(errors,[]);assert.deepEqual(external,[]);
      let layers;
      try { await fs.access(path.join(root,'layers.html')); layers=true; } catch { layers=false; }
      if (layers) {
        await page.goto(pathToFileURL(path.join(root,'layers.html')).href);
        await page.locator('#cut').focus();
        await page.screenshot({path:path.join(visual,name+'-layers.png')});
        const at = async key => { await page.locator('#cut').press(key); return page.locator('#height').textContent(); };
        assert.equal(await at('Home'),'0.2 mm');
        assert.notEqual(await page.locator('#natural').getAttribute('d'),'');
        assert.equal(await at('End'),'16.8 mm');
        assert.equal(await page.locator('#natural').getAttribute('d'),'');
        assert.equal(await page.locator('#one').textContent(),'Above the model');
        assert.notEqual(await page.locator('#exaggerated').getAttribute('d'),'');
        assert.equal(await at('ArrowLeft'),'16.6 mm');
        const overflows = await page.evaluate(()=>document.documentElement.scrollWidth>innerWidth);
        assert.equal(overflows,false);
      }
      const video = page.video();
      await context.close();
      await fs.rename(await video.path(),path.join(visual,name+'.webm'));
      checks[name]={viewport,external,errors,before,rotated,reset,downloads:receipts,layer_reader:layers};
    }
  } finally { await browser.close(); }
  await fs.writeFile(path.join(root,'browser.json'),JSON.stringify(checks,null,2)+'\n');
  console.log(Object.keys(checks));
}
run().catch(e=>{console.error(e);process.exitCode=1});
