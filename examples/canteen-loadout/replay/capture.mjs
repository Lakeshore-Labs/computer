// node capture.mjs BOARD_INDEX_HTML OUTDIR  (needs playwright-core + a Chromium; board is opened as file://, no network)
import { chromium } from 'playwright-core';
import { pathToFileURL } from 'node:url';
const [html, out] = process.argv.slice(2);
const url = pathToFileURL(html).href;
const b = await chromium.launch();
const requests = [];
for (const [name, vp] of [['desktop', {width:1280,height:900}], ['mobile', {width:390,height:844}]]) {
  const ctx = await b.newContext({ viewport: vp, deviceScaleFactor: name==='mobile'?2:1, isMobile: name==='mobile' });
  const p = await ctx.newPage(); p.on('request', r => requests.push(r.url()));
  await p.goto(url); await p.selectOption('#hall','Quay');
  await p.screenshot({ path:`${out}/${name}-quay-before.png`, fullPage:true });
  await p.click('#a'); await p.waitForTimeout(500);
  await p.screenshot({ path:`${out}/${name}-quay-after.png`, fullPage:true });
  await ctx.close();
}
const ctx = await b.newContext({ viewport:{width:1100,height:760}, recordVideo:{ dir: out+'/video-raw', size:{width:1100,height:760} } });
const p = await ctx.newPage(); await p.goto(url); await p.waitForTimeout(1200);
for (const h of ['Quay','Orchard','Hill']) { await p.selectOption('#hall',h); await p.waitForTimeout(900); }
await p.selectOption('#hall','Quay'); await p.waitForTimeout(900);
await p.click('#a'); await p.waitForTimeout(1500);
for (const h of ['Orchard','Hill','Quay']) { await p.selectOption('#hall',h); await p.waitForTimeout(1100); }
await ctx.close(); await b.close();
console.log(JSON.stringify({ non_file_requests: requests.filter(u=>!u.startsWith('file:')) }));
