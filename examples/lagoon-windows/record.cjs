const {chromium}=require('playwright-core');
const fs=require('node:fs/promises'),path=require('node:path'),assert=require('node:assert/strict'),{createHash}=require('node:crypto'),{pathToFileURL}=require('node:url');
const hash=data=>createHash('sha256').update(data).digest('hex');
(async()=>{
  const root=path.resolve(process.argv[2]),dest=path.resolve(process.argv[3]);
  const info=JSON.parse(await fs.readFile(path.join(root,'windows.json')));
  const ref=JSON.parse(await fs.readFile(path.join(root,'independent.json')));
  await fs.mkdir(dest,{recursive:false});
  const browser=await chromium.launch({channel:'chrome'});const all=[];
  try{
    for(const [name,viewport] of Object.entries({desktop:{width:1320,height:850},phone:{width:390,height:844}})){
      const ctx=await browser.newContext({viewport,recordVideo:{dir:dest,size:viewport}});
      const outside=[],errors=[],images=[];
      await ctx.route(/^https?:/,r=>{outside.push(r.request().url());return r.abort()});
      const page=await ctx.newPage();page.on('pageerror',e=>errors.push(e.message));
      await page.goto(pathToFileURL(path.join(root,'lagoon.html')).href);
      for(const w of info.windows){
        await page.getByRole('button',{name:w.name}).click();
        await page.locator('#photo[data-ready="'+w.id+'"]').waitFor();
        const rgb=await page.locator('canvas').evaluate(c=>{
          const rgba=c.getContext('2d').getImageData(0,0,c.width,c.height).data;
          const rgb=new Uint8Array(c.width*c.height*3);
          for(let i=0,j=0;i<rgba.length;i+=4){rgb[j++]=rgba[i];rgb[j++]=rgba[i+1];rgb[j++]=rgba[i+2];}
          return btoa(Array.from(rgb, octet=>String.fromCharCode(octet)).join(''));
        });
        const rgbBytes=Buffer.from(rgb,'base64');
        assert.equal(hash(rgbBytes),ref.images.find(o=>o.place===w.name).rgb_sha256);
        const holder=page.locator('#photo');
        await holder.focus();await holder.press('ArrowRight');await holder.press('ArrowDown');
        const values=await page.locator('#values').textContent(),idx=(129*256+129)*3;
        assert.equal(values,'(129, 129) · RGB '+Array.from(rgbBytes.subarray(idx,idx+3)).join(', '));
        if(w.id==='san-marco')await page.screenshot({path:path.join(dest,name+'.png')});
        const [d]=await Promise.all([page.waitForEvent('download'),page.locator('#save').click()]);
        const sha=hash(await fs.readFile(await d.path()));assert.equal(sha,w.png_sha256);
        images.push({place:w.name,actual_canvas_rgb_sha256:hash(rgbBytes),verified_pixel:values,download_sha256:sha});
      }
      assert.equal(await page.locator('#fraction').textContent(),'3.65%');
      await page.getByText('Which bytes did Computer use?').click();
      assert.match(await page.locator('#ranges').textContent(),/Block 72/);
      assert.equal(await page.evaluate(()=>document.documentElement.scrollWidth>innerWidth),false);
      assert.deepEqual(outside,[]);assert.deepEqual(errors,[]);
      const video=page.video();await ctx.close();await fs.rename(await video.path(),path.join(dest,name+'.webm'));
      all.push({name,viewport,images,outside,errors});
    }
  }finally{await browser.close();}
  await fs.writeFile(path.join(dest,'browser.json'),JSON.stringify(all,null,2)+'\n');
})().catch(e=>{console.error(e);process.exitCode=1;});
