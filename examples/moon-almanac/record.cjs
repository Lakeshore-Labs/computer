const {chromium} = require('playwright-core');
const fs = require('node:fs');
const http = require('node:http');
const path = require('node:path');
const crypto = require('node:crypto');
const assert = require('node:assert/strict');
const here = __dirname;
const run = path.resolve(process.argv[2]);
const media = path.resolve(process.argv[3]);
fs.mkdirSync(media, {recursive:false});
const assets = new Map([
 ['/', ['text/html',path.join(here,'reader/index.html')]],
 ['/jszip.min.js',['text/javascript',require.resolve('jszip/dist/jszip.min.js')]],
 ['/epub.min.js',['text/javascript',require.resolve('epubjs/dist/epub.min.js')]],
 ['/four-moons.epub',['application/epub+zip',path.join(run,'four-moons.epub')]],
]);
const server = http.createServer((req,res)=>{
 const item = assets.get(req.url);
 if (!item) {res.writeHead(404);res.end();return;}
 res.setHeader('Content-Type',item[0]);res.end(fs.readFileSync(item[1]));
});
(async()=>{
 await new Promise(ok=>server.listen(0,'127.0.0.1',ok));
 const origin='http://127.0.0.1:'+server.address().port;
 const browser=await chromium.launch({channel:'chrome',headless:true});
 const evidence=[];
 try {
  for(const [label,width,height] of [['desktop',1120,760],['phone',390,844]]) {
   const context=await browser.newContext({viewport:{width,height},deviceScaleFactor:1,recordVideo:{dir:media,size:{width,height}}});
   const external=[];
   await context.route(/^https?:\/\//,route=>{
    if(!route.request().url().startsWith(origin+'/')){external.push(route.request().url());return route.abort();}
    return route.continue();
   });
   const page=await context.newPage(),errors=[];
   page.on('pageerror',e=>errors.push(e.message));
   const bookResponse=page.waitForResponse(r=>r.url()===origin+'/four-moons.epub');
   await page.goto(origin);
   const returned=await (await bookResponse).body();
   assert.deepEqual(returned,fs.readFileSync(path.join(run,'four-moons.epub')));
   const inner=page.frameLocator('#reader iframe').last();
   const title=inner.locator('h1');
   await title.waitFor();
   assert.equal((await title.innerText()).replace(/\s+/g,' '),'Four moons of Jupiter');
   await inner.locator('img').evaluate(el=>el.decode());
   const photo=await inner.locator('img').evaluate(el=>[el.naturalWidth,el.naturalHeight]);
   assert.deepEqual(photo,[1870,635]);
   await page.screenshot({path:path.join(media,label+'-cover.png')});
   await page.waitForTimeout(1300);
   await page.getByLabel('Chapter',{exact:true}).selectOption({label:'Measure the moons'});
   await inner.getByRole('heading',{name:'Measure the moons'}).waitFor();
   const rows=await inner.locator('tbody tr').allTextContents();
   assert(rows.some(row=>row.includes('Ganymede')&&row.includes('2,631.20')));
   assert.equal(rows.length,4);
   await page.screenshot({path:path.join(media,label+'-table.png')});
   await page.waitForTimeout(1600);
   await page.getByLabel('Chapter',{exact:true}).selectOption({label:'Sources and credits'});
   await inner.getByRole('heading',{name:'Sources and credits'}).waitFor();
   const links=await inner.locator('main li a').count();
   assert.equal(links,6);
   await page.screenshot({path:path.join(media,label+'-credits.png')});
   await page.waitForTimeout(900);
   const overflow=await page.evaluate(()=>document.documentElement.scrollWidth>innerWidth);
   assert.equal(overflow,false);
   assert.deepEqual(errors,[]);assert.deepEqual(external,[]);
   const video=page.video();
   await context.close();
   await fs.promises.rename(await video.path(),path.join(media,label+'.webm'));
   evidence.push({label,width,height,reader:'EPUB.js 0.3.93',book_sha256:crypto.createHash('sha256').update(returned).digest('hex'),photo,rows,source_links:links,page_errors:errors,external_requests:external});
  }
  fs.writeFileSync(path.join(media,'browser.json'),JSON.stringify(evidence,null,2)+'\n');
 } finally {await browser.close();server.close();}
})().catch(e=>{console.error(e);server.close();process.exitCode=1;});
