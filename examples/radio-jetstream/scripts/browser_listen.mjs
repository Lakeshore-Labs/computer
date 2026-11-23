// usage: node browser_listen.mjs CHROME FFMPEG PAGE.html OUTDIR  (Chrome DevTools protocol over loopback; Node >= 22 global WebSocket)
// Produces: listen.json (Web Audio proof), desktop.png, mobile.png, capture.mp4 (screencast of playback)
import { spawn, spawnSync } from 'node:child_process'; import { mkdtempSync, writeFileSync, mkdirSync, rmSync } from 'node:fs'; import { tmpdir } from 'node:os'; import { createServer } from 'node:net'; import path from 'node:path';
const [chrome, ffmpeg, page, out] = process.argv.slice(2); mkdirSync(out, { recursive: true });
const port = await new Promise(r => { const s = createServer().listen(0, '127.0.0.1', () => { const p = s.address().port; s.close(() => r(p)) }) });
const prof = mkdtempSync(path.join(tmpdir(), 'rj-chrome-'));
const ch = spawn(chrome, ['--headless=new', '--disable-gpu', '--no-first-run', '--autoplay-policy=no-user-gesture-required', `--remote-debugging-port=${port}`, '--remote-debugging-address=127.0.0.1', `--user-data-dir=${prof}`, 'about:blank'], { stdio: 'ignore' });
const sleep = ms => new Promise(r => setTimeout(r, ms));
let tabs; for (let i = 0; i < 100; i++) { try { tabs = await (await fetch(`http://127.0.0.1:${port}/json`)).json(); if (tabs.find(t => t.type === 'page')) break } catch {} await sleep(100) }
const ws = new WebSocket(tabs.find(t => t.type === 'page').webSocketDebuggerUrl); await new Promise(r => ws.onopen = r);
let id = 0; const pend = new Map(), frames = [];
ws.onmessage = ev => { const m = JSON.parse(ev.data); if (m.id && pend.has(m.id)) { pend.get(m.id)(m); pend.delete(m.id) }
  else if (m.method === 'Page.screencastFrame') { frames.push({ t: m.params.metadata.timestamp, d: m.params.data }); send('Page.screencastFrameAck', { sessionId: m.params.sessionId }) } };
const send = (method, params = {}) => new Promise(r => { const i = ++id; pend.set(i, r); ws.send(JSON.stringify({ id: i, method, params })) });
const ev = async e => (await send('Runtime.evaluate', { expression: e, awaitPromise: true, returnByValue: true })).result.result.value;
const shot = async (file) => writeFileSync(path.join(out, file), Buffer.from((await send('Page.captureScreenshot', { format: 'png' })).result.data, 'base64'));
await send('Page.enable'); await send('Emulation.setDeviceMetricsOverride', { width: 900, height: 600, deviceScaleFactor: 1, mobile: false });
await send('Page.navigate', { url: 'file://' + path.resolve(page) }); await sleep(600);
await send('Page.startScreencast', { format: 'jpeg', quality: 60, everyNthFrame: 1 });
await ev("document.getElementById('go').click(); true");
let res; for (let i = 0; i < 200; i++) { await sleep(100); res = await ev('window.__done ? JSON.stringify(window.__result) : null'); if (res) break }
await sleep(400); await send('Page.stopScreencast'); await shot('desktop.png');
await send('Emulation.setDeviceMetricsOverride', { width: 390, height: 780, deviceScaleFactor: 1, mobile: true }); await sleep(300); await shot('mobile.png');
ws.close(); ch.kill(); await sleep(300); try { rmSync(prof, { recursive: true, force: true }) } catch {}
if (!res) throw new Error('page never finished');
const r = JSON.parse(res); const span = frames.length > 1 ? frames.at(-1).t - frames[0].t : 0;
// assemble capture with real frame timing (ffmpeg concat demuxer; frames in a temp dir)
const fd = mkdtempSync(path.join(tmpdir(), 'rj-frames-')); let list = '';
frames.forEach((f, i) => { const n = path.join(fd, `f${i}.jpg`); writeFileSync(n, Buffer.from(f.d, 'base64')); list += `file '${n}'\nduration ${Math.max(0.04, (frames[i + 1]?.t ?? f.t + 0.1) - f.t).toFixed(3)}\n` });
list += `file '${path.join(fd, `f${frames.length - 1}.jpg`)}'\n`; writeFileSync(path.join(fd, 'list.txt'), list);
const ff = spawnSync(ffmpeg, ['-y', '-loglevel', 'error', '-f', 'concat', '-safe', '0', '-i', path.join(fd, 'list.txt'), '-vf', 'scale=720:-2,fps=15', '-pix_fmt', 'yuv420p', '-an', path.join(out, 'capture.mp4')]);
rmSync(fd, { recursive: true, force: true }); if (ff.status !== 0) throw new Error('ffmpeg failed: ' + ff.stderr);
r.capture = { frames: frames.length, seconds: +span.toFixed(2) }; writeFileSync(path.join(out, 'listen.json'), JSON.stringify(r, null, 1));
console.log(JSON.stringify({ pass: r.pass, capture: r.capture, plays: r.plays.map(p => [p.wall_ms, p.audio_clock_ms, p.active_ms]) })); process.exit(r.pass ? 0 : 1);
