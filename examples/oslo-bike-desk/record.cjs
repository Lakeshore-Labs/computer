const {chromium}=require('playwright-core');
const fs=require('node:fs'),path=require('node:path'),assert=require('node:assert/strict'),crypto=require('node:crypto');
const {pathToFileURL}=require('node:url');
(async()=>{
 const edition=path.resolve(process.argv[2]),root=path.join(edition,'inputs','observations'),info=JSON.parse(fs.readFileSync(path.join(root,'station_information.json'))).data.stations;
 const capture=path.resolve(process.argv[3]);fs.mkdirSync(capture,{recursive:false});
 const receipts=fs.readdirSync(root).filter(n=>/^\d{8}T\d{6}Z$/.test(n)&&fs.existsSync(path.join(root,n,'receipt.json'))).sort();
 const product=JSON.parse(fs.readFileSync(path.join(edition,'product.json')));
 const rounds=receipts.slice(0,product.rounds).map(k=>JSON.parse(fs.readFileSync(path.join(root,k,'station_status.json'))));
 const displayed=new Set(info.map(s=>s.station_id));
 const browser=await chromium.launch({channel:'chrome',headless:true});
 const outputs=[];
 try {
  for(const [screen,width,height] of [['desktop',1300,840],['phone',390,844]]){
    const ctx=await browser.newContext({viewport:{width,height},deviceScaleFactor:1,recordVideo:{dir:capture,size:{width,height}}});const errors=[],external=[];
    await ctx.route(/^https?:\/\//,r=>{external.push(r.request().url());return r.abort()});
    const page=await ctx.newPage();page.on('pageerror',e=>errors.push(e.message));
    await page.goto(pathToFileURL(path.join(edition,'map.html')).href);
    await page.locator('.station').first().waitFor();
    const basemap=page.getByRole('checkbox',{name:'Street map'});
    assert(await basemap.isChecked());await basemap.uncheck();assert(!(await basemap.isChecked()));await basemap.check();
    const snapshots=[];
    for(const [index,round] of rounds.entries()){
      await page.selectOption('#time',String(index));
      const rows=round.data.stations.filter(s=>s.is_installed&&displayed.has(s.station_id));
      const named=rows.find(s=>s.station_id==='478')||rows[0];
      for(const [mode,field,enabled,phrase] of [
        ['borrow','num_bikes_available','is_renting','have a bike to borrow.'],
        ['return','num_docks_available','is_returning','can accept a bike.']
      ]){
        await page.locator('#'+mode).click();
        const expected=rows.filter(r=>r[enabled]&&r[field]>0).length;
        assert.equal(await page.locator('#summary').textContent(),expected+' of '+rows.length+' stations '+phrase);
        assert.equal(await page.locator('.station').count(),rows.length);
        await page.locator('.station[data-id="'+named.station_id+'"]').click();
        const detail=await page.locator('#detail').textContent();
        assert(detail.includes(named.num_bikes_available+' bikes · '+named.num_docks_available+' return spaces'));
        snapshots.push({index,mode,available:expected,displayed:rows.length,selected:named.station_id,detail});
      }
    }
    await page.reload();await page.locator('.station').first().waitFor();
    if(capture)await page.screenshot({path:path.join(capture,screen+'.png')});
    const file=page.waitForEvent('download');await page.getByText('Download GIS files').click();const link=await file;
    const bytes=fs.readFileSync(await link.path());assert.deepEqual(bytes,fs.readFileSync(path.join(edition,'oslo-stations.zip')));
    assert.equal(await page.evaluate(()=>document.documentElement.scrollWidth>innerWidth),false);
    assert.deepEqual(external,[]);assert.deepEqual(errors,[]);
    outputs.push({screen,width,height,rounds:rounds.length,snapshots,downloaded_sha256:crypto.createHash('sha256').update(bytes).digest('hex'),external_requests:external,page_errors:errors});
    const meeting=path.join(edition,'meeting.html');
    if(fs.existsSync(meeting)){
      await page.goto(pathToFileURL(meeting).href);
      const slider=page.locator('#group');
      const last=rounds.at(-1).data.stations;
      const geo=new Map(info.map(s=>[s.station_id,s])),from=geo.get('478');
      const rad=angle=>angle*Math.PI/180;
      const distance=s=>{
        const dLat=rad(s.lat-from.lat),dLon=rad(s.lon-from.lon);
        const a=Math.sin(dLat/2)**2+Math.cos(rad(s.lat))*Math.cos(rad(from.lat))*Math.sin(dLon/2)**2;
        return 2*6371000*Math.asin(Math.sqrt(a));
      };
      const decisions=[];
      for(const size of [1,10,30]){
        await slider.focus();
        await slider.press('Home');
        for(let i=1;i<size;i++) await slider.press('ArrowRight');
        assert.equal(await slider.inputValue(),String(size));
        const eligible=last.filter(s=>s.is_installed&&s.is_renting&&s.num_bikes_available>=size&&geo.has(s.station_id))
          .map(s=>({name:geo.get(s.station_id).name,bikes:s.num_bikes_available,d:distance(geo.get(s.station_id))}))
          .sort((a,b)=>a.d-b.d);
        const results=await page.locator('#list li').allTextContents();
        assert.equal(results.length,Math.min(3,eligible.length));
        for(const [i,line] of results.entries()){
          assert(line.startsWith((i+1)+'. '+eligible[i].name));
          const found=line.match(/(\d+) bikes · (\d+) m$/);
          assert(found);assert.equal(Number(found[1]),eligible[i].bikes);
          assert(Math.abs(Number(found[2])-eligible[i].d)<Math.max(2,eligible[i].d*.008));
        }
        decisions.push({size,eligible:eligible.length,results});
        if(size===10)await page.screenshot({path:path.join(capture,screen+'-group.png')});
      }
      outputs.at(-1).group_decisions=decisions;
      assert.equal(await page.evaluate(()=>document.documentElement.scrollWidth>innerWidth),false);
      assert.deepEqual(external,[]);assert.deepEqual(errors,[]);
    }
    const video=page.video();await ctx.close();
    fs.renameSync(await video.path(),path.join(capture,screen+'.webm'));
  }
 }finally{await browser.close()}
 fs.writeFileSync(path.join(edition,'browser.json'),JSON.stringify(outputs,null,2)+'\n');
})().catch(e=>{console.error(e);process.exitCode=1});
