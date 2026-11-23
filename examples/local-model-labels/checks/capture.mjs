#!/usr/bin/env node
// Final capture: node capture.mjs READER_DIR OUT_DIR RUN_DIR [RUN_DIR ...]
// Screenshots at 1280 and 390 px, a ~10 s screencast, and independent download checks
// at both sizes (bytes compared with each run's desk/ticket.zpl). Writes verify.json.
import { spawnSync } from "node:child_process";
import fs from "node:fs";
import path from "node:path";
import { launch, sleep, verifyRuns } from "./lib.mjs";

const [readerDir, outDir, ...runs] = process.argv.slice(2);
if (!readerDir || !outDir || runs.length === 0) {
  console.error("usage: node capture.mjs READER_DIR OUT_DIR RUN_DIR [RUN_DIR ...]");
  process.exit(2);
}
fs.mkdirSync(outDir, { recursive: true });
const names = runs.map((r) => path.basename(r));
const session = await launch();
const pick = async (job, stage) => {
  await session.click(`input[name=job][value="${job}"]`);
  await session.click(`input[name=stage][value=${stage}]`);
  await sleep(350);
};
const hasStage = (job, stage) => session.evaluate(`!document.querySelector('input[name=stage][value=${stage}]').disabled`);

// ---- desktop: screenshots + screencast
await session.view(1280, 800, false);
await session.open(readerDir);
const frames = [];
session.on((msg) => {
  if (msg.method !== "Page.screencastFrame") return;
  frames.push({ t: msg.params.metadata.timestamp, data: msg.params.data });
  session.send("Page.screencastFrameAck", { sessionId: msg.params.sessionId });
});
await session.send("Page.startScreencast", { format: "jpeg", quality: 80, maxWidth: 1280, maxHeight: 800, everyNthFrame: 1 });
await sleep(1000);
for (let j = 0; j < runs.length; j++) {
  await pick(j, "inspect");
  await session.shot(path.join(outDir, `reader-1280-${names[j]}-inspect.png`));
  await sleep(1700);
  if (await hasStage(j, "issue")) {
    await pick(j, "issue");
    await session.shot(path.join(outDir, `reader-1280-${names[j]}-issue.png`));
    await sleep(1900);
  }
}
await sleep(600);
await session.send("Page.stopScreencast");
const desktop = await verifyRuns(session, runs, "desktop");

// ---- phone
await session.view(390, 844, true);
await session.open(readerDir);
for (let j = 0; j < runs.length; j++) {
  await pick(j, (await hasStage(j, "issue")) ? "issue" : "inspect");
  await session.shot(path.join(outDir, `reader-390-${names[j]}.png`));
}
const noOverflow = await session.evaluate("document.documentElement.scrollWidth <= document.documentElement.clientWidth");
const phone = await verifyRuns(session, runs, "phone");

// ---- movie
const frameDir = path.join(session.tmp, "frames");
fs.mkdirSync(frameDir);
let list = "";
frames.forEach((f, i) => {
  const file = path.join(frameDir, `f${String(i).padStart(4, "0")}.jpg`);
  fs.writeFileSync(file, Buffer.from(f.data, "base64"));
  const dur = i + 1 < frames.length ? Math.max(0.04, frames[i + 1].t - f.t) : 1;
  list += `file '${file}'\nduration ${dur.toFixed(3)}\n`;
});
const lastFile = path.join(frameDir, `f${String(frames.length - 1).padStart(4, "0")}.jpg`);
fs.writeFileSync(path.join(session.tmp, "list.txt"), list + `file '${lastFile}'\n`);
const ffmpeg = spawnSync("ffmpeg", ["-y", "-v", "error", "-f", "concat", "-safe", "0", "-i", path.join(session.tmp, "list.txt"),
  "-vf", "fps=15,scale=1280:-2,format=yuv420p", "-c:v", "libx264", "-movflags", "+faststart", path.join(outDir, "screencast.mp4")]);
const seconds = frames.length ? Number((frames.at(-1).t - frames[0].t + 1).toFixed(1)) : 0;

const external = session.requestSchemes.filter((s) => !["file", "data", "blob"].includes(s));
const result = {
  runs: names,
  offline: true,
  externalRequests: external.length,
  noHorizontalOverflow390: noOverflow,
  screencast: { frames: frames.length, seconds, ffmpegStatus: ffmpeg.status },
  desktop,
  phone,
};
result.pass = [...desktop, ...phone].every((r) => r.pass) && external.length === 0 && noOverflow && ffmpeg.status === 0 && seconds >= 8 && seconds <= 15;
fs.writeFileSync(path.join(outDir, "verify.json"), JSON.stringify(result, null, 2) + "\n");
console.log(`${result.pass ? "PASS" : "FAIL"}: ${desktop.length + phone.length} checks, screencast ${seconds}s`);
await session.close();
process.exit(result.pass ? 0 : 1);
