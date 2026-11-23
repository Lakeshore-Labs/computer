// Usage: node capture/capture.mjs [address] [outDir]   (address: arg, PRESS_ADDRESS, or ../address)
import { chromium } from "playwright-core";
import { readFileSync, mkdirSync, writeFileSync, readdirSync } from "node:fs";
import { createHash } from "node:crypto";
import { execFileSync } from "node:child_process";
import path from "node:path";

const here = path.dirname(new URL(import.meta.url).pathname);
const addr = process.argv[2] || process.env.PRESS_ADDRESS;
if (!addr) throw new Error("Supply the printed local press address");
const out = path.resolve(process.argv[3] || path.join(here, "out"));
const exe =
  process.env.CHROME ||
  "/Applications/Google Chrome.app/Contents/MacOS/Google Chrome";
mkdirSync(out, { recursive: true });
const log = {
  transport: "loopback Streamable HTTP",
  started: new Date().toISOString(),
  sessions: [],
};
const sha = (b) => createHash("sha256").update(b).digest("hex");
const pause = (ms) => new Promise((r) => setTimeout(r, ms));

const browser = await chromium.launch({ executablePath: exe, headless: true });

async function session(name, viewport, steps, video) {
  const ctx = await browser.newContext({
    viewport,
    acceptDownloads: true,
    deviceScaleFactor: name === "phone" ? 2 : 1,
    isMobile: name === "phone",
    hasTouch: name === "phone",
    ...(video
      ? {
          recordVideo: { dir: path.join(out, "video-" + name), size: viewport },
        }
      : {}),
  });
  const page = await ctx.newPage();
  const s = { name, viewport, mcp: [], issued: [], consoleErrors: [] };
  page.on(
    "console",
    (m) => m.type() === "error" && s.consoleErrors.push(m.text()),
  );
  page.on("request", (r) => {
    if (!r.url().endsWith("/mcp") || r.method() !== "POST") return;
    try {
      const j = JSON.parse(r.postData());
      for (const m of [].concat(j)) {
        const a = m.params?.arguments;
        s.mcp.push({
          at: new Date().toISOString(),
          method: m.method,
          tool: m.params?.name,
          args:
            a &&
            Object.fromEntries(
              Object.entries(a).map(([k, v]) => [
                k,
                typeof v === "string" && v.length > 60
                  ? v.slice(0, 57) + `… (${v.length} chars)`
                  : v,
              ]),
            ),
        });
      }
    } catch {}
  });
  await page.goto(addr + "/");
  await page.waitForSelector("#status[data-state=ready]");
  s.status = await page.textContent("#status-text");
  s.tools = await page.evaluate(() => window.pressTools);
  await page.screenshot({ path: path.join(out, `${name}-1-ready.png`) });
  const issue = async (label) => {
    const t0 = new Date().toISOString();
    await page.click("#issue");
    await page.waitForSelector("#actions:not([hidden])", { timeout: 90000 });
    await page.waitForFunction(
      (n) => document.querySelectorAll("#issued li:not(.muted)").length >= n,
      s.issued.length + 1,
    );
    const [dl] = await Promise.all([
      page.waitForEvent("download"),
      page.click("#download"),
    ]);
    const file = path.join(out, `${name}-${label}.svg`);
    await dl.saveAs(file);
    const bytes = readFileSync(file);
    const receipt = Object.fromEntries(
      await page.$$eval("#receipt dt", (ts) =>
        ts.map((t) => [t.textContent, t.nextElementSibling.textContent]),
      ),
    );
    const calls = await page.evaluate(() =>
      window.pressTrace
        .slice(-3)
        .map((t) => ({ tool: t.tool, args: t.args, ok: t.ok })),
    );
    s.issued.push({
      label,
      t0,
      t1: new Date().toISOString(),
      file: path.basename(file),
      downloaded_bytes: bytes.length,
      downloaded_sha256: sha(bytes),
      receipt_sha256: receipt["SHA-256"],
      receipt_bytes: receipt["SVG bytes"],
      match: sha(bytes) === receipt["SHA-256"],
      longest_line: Math.max(
        ...bytes
          .toString()
          .split("\n")
          .map((l) => l.length),
      ),
      receipt,
      calls,
      facts: await page.textContent("#facts"),
    });
    await page.screenshot({ path: path.join(out, `${name}-${label}.png`) });
    await pause(2200);
  };
  await steps(page, issue);
  s.allTools = [
    ...new Set(
      s.mcp.filter((m) => m.method === "tools/call").map((m) => m.tool),
    ),
  ];
  if (
    s.consoleErrors.length ||
    s.issued.some((i) => !i.match) ||
    !s.allTools.includes("read")
  )
    throw new Error("The recorded issue failed a browser check");
  if (
    name === "desktop" &&
    !["write", "edit", "bash", "read"].every((t) => s.allTools.includes(t))
  )
    throw new Error("Missing live tool call");
  log.sessions.push(s);
  await ctx.close();
  if (video) {
    const f = readdirSync(path.join(out, "video-" + name)).find((x) =>
      x.endsWith(".webm"),
    );
    s.video = path.join("video-" + name, f);
  }
}

await session(
  "desktop",
  { width: 1280, height: 800 },
  async (page, issue) => {
    await pause(3000);
    await issue("card1-getty");
    await page.click(".work[data-museum=cleveland]");
    await page.fill("#inscription", "Wish you were here");
    await pause(1500);
    await issue("card2-cleveland");
  },
  true,
);

await session(
  "phone",
  { width: 390, height: 844 },
  async (page, issue) => {
    await page.click(".work[data-museum=cleveland]");
    await page.fill("#inscription", "Greetings from Saint-Rémy");
    await issue("card1-cleveland");
    await page.screenshot({
      path: path.join(out, "phone-full.png"),
      fullPage: true,
    });
  },
  false,
);

log.finished = new Date().toISOString();
await browser.close();
const v = log.sessions[0].video;
if (v)
  execFileSync("ffmpeg", [
    "-y",
    "-loglevel",
    "error",
    "-i",
    path.join(out, v),
    "-c:v",
    "libx264",
    "-pix_fmt",
    "yuv420p",
    path.join(out, "desktop-film.mp4"),
  ]);
console.log(
  JSON.stringify(
    log.sessions.map((s) => ({
      name: s.name,
      status: s.status,
      tools: s.allTools,
      issued: s.issued.map((i) => [i.label, i.downloaded_bytes, i.match]),
    })),
    null,
    1,
  ),
);

if (v) log.sessions[0].video = "desktop-film.mp4";
writeFileSync(
  path.join(out, "receipt.json"),
  JSON.stringify(log, null, 2) + "\n",
);
