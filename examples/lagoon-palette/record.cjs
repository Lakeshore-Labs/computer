const {chromium} = require('playwright-core');
const fs = require('node:fs/promises');
const path = require('node:path');
const {pathToFileURL} = require('node:url');
const assert = require('node:assert/strict');
const {createHash} = require('node:crypto');

async function main() {
  const root = path.resolve(process.argv[2]), dest = path.resolve(process.argv[3]);
  const selected = JSON.parse(await fs.readFile(path.join(root,'selection.json')));
  await fs.mkdir(dest);
  const browser = await chromium.launch({channel:'chrome'});
  const records = [];
  try {
    for (const [name,viewport] of Object.entries({desktop:{width:1280,height:850},phone:{width:390,height:844}})) {
      const context = await browser.newContext({viewport,recordVideo:{dir:dest,size:viewport}});
      const outside = [], errors = [], colors = [], downloads = {};
      await context.route(/^https?:/, route=>{outside.push(route.request().url());return route.abort();});
      const page = await context.newPage();
      page.on('pageerror',e=>errors.push(e.message));
      await page.goto(pathToFileURL(path.join(root,'palette.html')).href);
      for (const color of selected) {
        await page.getByRole('button',{name:color.name}).click();
        assert.equal(await page.locator('#name').textContent(),color.name);
        const pixel = await page.locator('#image').evaluate(async (image, c)=>{
          await image.decode();
          const canvas=document.createElement('canvas');canvas.width=image.naturalWidth;canvas.height=image.naturalHeight;
          const ctx=canvas.getContext('2d');ctx.drawImage(image,0,0);
          return Array.from(ctx.getImageData(c.x,c.y,1,1).data).slice(0,3);
        },color);
        assert.deepEqual(pixel,color.rgb);
        const hex='#'+Buffer.from(pixel).toString('hex').toUpperCase();
        assert.match(await page.locator('#detail').textContent(),new RegExp(hex));
        colors.push({name:color.name,pixel,hex});
        // Keep the selected source pixel visible long enough to read in the film.
        await page.waitForTimeout(1350);
        if (color.name==='Roof terracotta' || color.name==='Lido sand') {
          await page.screenshot({path:path.join(dest,name+'-'+color.site+'.png'),fullPage:name==='phone'});
        }
      }
      for(const [ext,label] of Object.entries({ase:'Adobe swatches',gpl:'GIMP palette',css:'CSS colors'})) {
        const [d]=await Promise.all([page.waitForEvent('download'),page.getByRole('link',{name:label}).click()]);
        const saved=await fs.readFile(await d.path());
        assert.deepEqual(saved,await fs.readFile(path.join(root,d.suggestedFilename())));
        downloads[ext]=createHash('sha256').update(saved).digest('hex');
      }
      assert.equal(await page.evaluate(()=>document.documentElement.scrollWidth>innerWidth),false);
      assert.deepEqual(errors,[]);assert.deepEqual(outside,[]);
      const video=page.video();await context.close();await fs.rename(await video.path(),path.join(dest,name+'.webm'));
      records.push({name,viewport,colors,downloads,errors,outside});
    }
  } finally {await browser.close();}
  await fs.writeFile(path.join(dest,'browser.json'),JSON.stringify(records,null,2)+'\n');
}
main().catch(error=>{console.error(error);process.exitCode=1;});
