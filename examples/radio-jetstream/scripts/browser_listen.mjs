// usage: node browser_listen.mjs CHROME FFMPEG PAGE.html OUTDIR  (Chrome DevTools protocol over loopback; Node >= 22 global WebSocket)
// Produces: listen.json (Web Audio proof), desktop.png, mobile.png, capture.mp4 (screencast of playback)
import { spawn, spawnSync } from 'node:child_process'; import { mkdtempSync, writeFileSync, mkdirSync, rmSync } from 'node:fs'; import { tmpdir } from 'node:os'; import { createServer } from 'node:net'; import path from 'node:path';
const [chrome, ffmpeg, page, out] = process.argv.slice(2); mkdirSync(out, { recursive: true });
const port = await new Promise(r => { const s = createServer().listen(0, '127.0.0.1', () => { const p = s.address().port; s.close(() => r(p)) }) });
const prof = mkdtempSync(path.join(tmpdir(), 'rj-chrome-'));
const ch = spawn(chrome, ['--headless=new', '--disable-gpu', '--no-first-run', '--autoplay-policy=no-user-gesture-required', `--remote-debugging-port=${port}`, '--remote-debugging-address=127.0.0.1', `--user-data-dir=${prof}`, 'about:blank'], { stdio: 'ignore' });
const sleep = ms => new Promise(r => setTimeout(r, ms));
const exited = new Promise(resolve => ch.once('close', resolve));
let spawnError;
ch.once('error', error => { spawnError = error; });
let ws, res;
const frames = [];
try {
  let tab;
  for (let i = 0; i < 100 && !tab; i++) {
    if (spawnError) throw spawnError;
    if (ch.exitCode !== null || ch.signalCode !== null) throw new Error('Chrome exited before connecting');
    try {
      const response = await fetch(`http://127.0.0.1:${port}/json`, {signal: AbortSignal.timeout(1000)});
      tab = (await response.json()).find(t => t.type === 'page');
    } catch {}
    if (!tab) await sleep(100);
  }
  if (!tab) throw new Error('Chrome did not expose a page');
  ws = new WebSocket(tab.webSocketDebuggerUrl);
  const pending = new Map();
  let id = 0, disconnect;
  await new Promise((resolve, reject) => {
    ws.onopen = resolve;
    ws.onclose = ws.onerror = () => {
      disconnect = new Error('Chrome debugger disconnected');
      reject(disconnect);
      for (const request of pending.values()) request.reject(disconnect);
      pending.clear();
    };
  });
  const send = (method, params = {}) => new Promise((resolve, reject) => {
    if (disconnect) return reject(disconnect);
    const requestId = ++id;
    pending.set(requestId, {resolve, reject});
    ws.send(JSON.stringify({id: requestId, method, params}));
  });
  ws.onmessage = event => {
    const message = JSON.parse(event.data);
    const request = pending.get(message.id);
    if (request) {
      pending.delete(message.id);
      if (message.error) request.reject(new Error(message.error.message));
      else request.resolve(message);
    } else if (message.method === 'Page.screencastFrame') {
      frames.push({t: message.params.metadata.timestamp, d: message.params.data});
      send('Page.screencastFrameAck', {sessionId: message.params.sessionId}).catch(() => {});
    }
  };
  const ev = async e => (await send('Runtime.evaluate', { expression: e, awaitPromise: true, returnByValue: true })).result.result.value;
  const shot = async (file) => writeFileSync(path.join(out, file), Buffer.from((await send('Page.captureScreenshot', { format: 'png' })).result.data, 'base64'));
  await send('Page.enable'); await send('Emulation.setDeviceMetricsOverride', { width: 900, height: 600, deviceScaleFactor: 1, mobile: false });
  await send('Page.navigate', { url: 'file://' + path.resolve(page) }); await sleep(600);
  await send('Page.startScreencast', { format: 'jpeg', quality: 60, everyNthFrame: 1 });
  await ev("document.getElementById('go').click(); true");
  for (let i = 0; i < 200; i++) { await sleep(100); res = await ev('window.__done ? JSON.stringify(window.__result) : null'); if (res) break }
  await sleep(400); await send('Page.stopScreencast'); await shot('desktop.png');
  await send('Emulation.setDeviceMetricsOverride', { width: 390, height: 780, deviceScaleFactor: 1, mobile: true }); await sleep(300); await shot('mobile.png');
} finally {
  ws?.close();
  if (ch.exitCode === null && ch.signalCode === null) ch.kill();
  const kill = setTimeout(() => ch.kill('SIGKILL'), 3000);
  try {
    await exited;
  } finally {
    clearTimeout(kill);
    rmSync(prof, {recursive: true, force: true});
  }
}
if (!res) throw new Error('page never finished');
const r = JSON.parse(res); const span = frames.length > 1 ? frames.at(-1).t - frames[0].t : 0;
// assemble capture with real frame timing (ffmpeg concat demuxer; frames in a temp dir)
const fd = mkdtempSync(path.join(tmpdir(), 'rj-frames-'));
try {
  let list = '';
  frames.forEach((f, i) => { const n = path.join(fd, `f${i}.jpg`); writeFileSync(n, Buffer.from(f.d, 'base64')); list += `file '${n}'\nduration ${Math.max(0.04, (frames[i + 1]?.t ?? f.t + 0.1) - f.t).toFixed(3)}\n` });
  list += `file '${path.join(fd, `f${frames.length - 1}.jpg`)}'\n`; writeFileSync(path.join(fd, 'list.txt'), list);
  const ff = spawnSync(ffmpeg, ['-y', '-loglevel', 'error', '-f', 'concat', '-safe', '0', '-i', path.join(fd, 'list.txt'), '-vf', 'scale=720:-2,fps=15', '-pix_fmt', 'yuv420p', '-an', path.join(out, 'capture.mp4')]);
  if (ff.status !== 0) throw new Error('ffmpeg failed: ' + ff.stderr);
} finally {
  rmSync(fd, {recursive: true, force: true});
}
r.capture = { frames: frames.length, seconds: +span.toFixed(2) }; writeFileSync(path.join(out, 'listen.json'), JSON.stringify(r, null, 1));
console.log(JSON.stringify({ pass: r.pass, capture: r.capture, plays: r.plays.map(p => [p.wall_ms, p.audio_clock_ms, p.active_ms]) })); process.exit(r.pass ? 0 : 1);
