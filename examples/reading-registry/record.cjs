const {chromium}=require('playwright-core');
const {pathToFileURL}=require('node:url');
const path=require('node:path'),fs=require('node:fs'),assert=require('node:assert/strict');
(async()=>{
 const folder=path.resolve(process.argv[2]),media=path.resolve(process.argv[3]);
 fs.mkdirSync(media,{recursive:false});
 const browser=await chromium.launch({channel:'chrome',headless:true});
 const recordings=[];
 try {
  for(const [label,width,height] of [['desktop',1040,820],['phone',390,844]]){
   const ctx=await browser.newContext({viewport:{width,height},recordVideo:{dir:media,size:{width,height}}});
   const errors=[],outside=[];
   await ctx.route(/^https?:\/\//,r=>{outside.push(r.request().url());return r.abort()});
   const page=await ctx.newPage();page.on('pageerror',e=>errors.push(e.message));
   await page.goto(pathToFileURL(path.join(folder,'program.html')).href);
   const verifications=[];
   for(const [edition,expected] of [['original','17:30'],['amended','18:00']]){
    await page.getByLabel('Program edition').selectOption(edition);
    assert.equal(await page.locator('#time').textContent(),expected);
    if(edition==='original')await page.screenshot({path:path.join(media,label+'-original.png')});
    await page.waitForTimeout(850);
    for(const [id,filename] of [['calendar','reading-room.ics'],['book','four-moons.epub']]){
     const event=page.waitForEvent('download');await page.locator('#'+id).click();const dl=await event;
     const file=fs.readFileSync(await dl.path());assert.deepEqual(file,fs.readFileSync(path.join(folder,edition+'-download',filename)));
     verifications.push({edition,filename,bytes:file.length});
    }
   }
   await page.screenshot({path:path.join(media,label+'-amended.png')});
   await page.getByText('Registry digests').click();
   assert((await page.locator('#manifest').textContent()).startsWith('sha256:'));
   assert.equal(await page.evaluate(()=>document.documentElement.scrollWidth>innerWidth),false);
   assert.deepEqual(errors,[]);assert.deepEqual(outside,[]);
   await page.waitForTimeout(700);const movie=page.video();await ctx.close();
   fs.renameSync(await movie.path(),path.join(media,label+'.webm'));
   recordings.push({label,width,height,verifications,external_requests:outside,errors});
  }
 }finally{await browser.close()}
 fs.writeFileSync(path.join(media,'browser.json'),JSON.stringify(recordings,null,2)+'\n');
})().catch(e=>{console.error(e);process.exitCode=1});
