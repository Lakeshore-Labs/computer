// usage: node listen_page.mjs RUNDIR OUT.html  -> offline page: control-room decision + Web Audio listening check of the DECODED receiver bytes
import { readFileSync, writeFileSync } from 'node:fs';
const [dir, out] = process.argv.slice(2); const r = JSON.parse(readFileSync(dir + '/receiver.json')); const v = JSON.parse(readFileSync(dir + '/verify.json'));
const wav = readFileSync(dir + '/cue.wav').toString('base64');
writeFileSync(out, `<!doctype html><meta charset=utf-8><meta name=viewport content="width=device-width,initial-scale=1"><title>Control room</title>
<style>body{font:14px/1.4 ui-monospace,Menlo,monospace;background:#14161a;color:#e6e6e6;margin:0;padding:14px;max-width:900px}
.lamp{display:inline-block;padding:8px 18px;font-size:24px;font-weight:700;border-radius:6px;background:${r.onair ? '#1f7a3a' : '#a33'};color:#fff;margin:8px 0}
.card{border:1px solid #333;border-radius:6px;padding:8px 12px;margin:8px 0;background:#1b1e24}td{padding:2px 6px;vertical-align:top}td:first-child{color:#9aa}
.ok{color:#6fdc8c}.bad{color:#f66}canvas{width:100%;height:80px;background:#0b0c0f;display:block}table{word-break:break-all}</style>
<h3 style="margin:0">Community radio control room (offline, synthetic)</h3>
<div class=lamp>${r.onair ? 'ON AIR' : 'HOLD'}</div><div>${r.decision}</div>
<div class=card><table><tr><td>decoded bytes</td><td>${r.decoded_bytes}</td></tr><tr><td>sha256</td><td>${r.sha256}</td></tr>
<tr><td>durable ack</td><td>seq ${r.seq}, floor ${v.consumer.ack_floor}, pending ${v.consumer.num_pending}</td></tr>
<tr><td>T1/T2/T3</td><td>${r.T1_amendment_published}<br>${r.T2_receiver_connected}<br>${r.T3_decision_acked}</td></tr></table></div>
<div class=card><b>Listening check (Web Audio)</b> <button id=go>play</button><canvas id=cv width=600 height=80></canvas><table id=lt><tr><td>status</td><td id=st>idle</td></tr></table></div>
<script>
const B64='${wav}', EXPECT='${r.sha256}', N=3;
const bytes=Uint8Array.from(atob(B64),c=>c.charCodeAt(0)); const cv=document.getElementById('cv'),g=cv.getContext('2d'),st=document.getElementById('st');
function gz(x,fs,f){let w=2*Math.PI*f/fs,c=2*Math.cos(w),a=0,b=0;for(const s of x){const t=s+c*a-b;b=a;a=t}return Math.sqrt(a*a+b*b-c*a*b)/x.length*2}
async function run(){
 const sha=[...new Uint8Array(await crypto.subtle.digest('SHA-256',bytes))].map(b=>b.toString(16).padStart(2,'0')).join('');
 const ctx=new AudioContext(); await ctx.resume(); const buf=await ctx.decodeAudioData(bytes.buffer.slice(0));
 const plays=[]; const fs=ctx.sampleRate;
 for(let k=0;k<N;k++){
  const cap=[]; const sp=ctx.createScriptProcessor(2048,1,1); const src=ctx.createBufferSource(); src.buffer=buf; src.connect(sp); sp.connect(ctx.destination);
  sp.onaudioprocess=e=>{const d=e.inputBuffer.getChannelData(0); cap.push(new Float32Array(d));
    g.fillStyle='#0b0c0f';g.fillRect(0,0,600,80);g.strokeStyle='#6fdc8c';g.beginPath();for(let i=0;i<d.length;i+=4){const x=i/d.length*600;i?g.lineTo(x,40-d[i]*70):g.moveTo(x,40)}g.stroke()};
  st.textContent='playing '+(k+1)+'/'+N;
  const p0=performance.now(), c0=ctx.currentTime; src.start();
  await new Promise(r=>src.onended=r); const wall=performance.now()-p0, clock=(ctx.currentTime-c0)*1000;
  await new Promise(r=>setTimeout(r,300)); src.disconnect(); sp.disconnect();
  const all=new Float32Array(cap.reduce((n,a)=>n+a.length,0)); let o=0; for(const a of cap){all.set(a,o);o+=a.length}
  let on=all.findIndex(v=>Math.abs(v)>0.01), off=all.length-1-[...all].reverse().findIndex(v=>Math.abs(v)>0.01);
  const notes=[0,0.4,0.8,1.2].map((t0,i)=>{const a=on+Math.round((t0+0.06)*fs),b=on+Math.round((t0+(i<3?0.34:0.24))*fs),x=all.subarray(a,b);
    const m={440:gz(x,fs,440),523.25:gz(x,fs,523.25),659.25:gz(x,fs,659.25)}; const best=Object.entries(m).sort((p,q)=>q[1]-p[1])[0]; return {win_s:t0,dominant_hz:+best[0],mag:+best[1].toFixed(3),m440:+m[440].toFixed(3),m523:+m[523.25].toFixed(3),m659:+m[659.25].toFixed(3)}});
  plays.push({wall_ms:Math.round(wall),audio_clock_ms:Math.round(clock),active_ms:Math.round((off-on)/fs*1000),notes});
  await new Promise(r=>setTimeout(r,700));
 }
 const ok=sha===EXPECT&&Math.abs(buf.duration-1.5)<0.01&&plays.every(p=>Math.abs(p.audio_clock_ms-1500)<150&&Math.abs(p.wall_ms-p.audio_clock_ms)<250&&Math.abs(p.active_ms-1500)<120&&[440,523.25,659.25].every((f,i)=>p.notes[i].dominant_hz===f)&&p.notes[3].m440>0.1&&p.notes[3].m523>0.1);
 window.__result={sha256_in_browser:sha,sha_matches:sha===EXPECT,decoded_duration_s:buf.duration,ctx_sample_rate:fs,ctx_state:ctx.state,plays,pass:ok};
 st.innerHTML='<span class='+(ok?'ok':'bad')+'>'+(ok?'PASS':'FAIL')+'</span> plays='+N+' clock '+plays.map(p=>p.audio_clock_ms).join('/')+' ms; tones '+plays[0].notes.map(n=>n.dominant_hz).join('/')+' Hz';
 window.__done=true;
}
document.getElementById('go').onclick=()=>run().catch(e=>{window.__result={error:String(e),pass:false};window.__done=true;st.textContent='error '+e});
</script>`);
