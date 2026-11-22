// Shared helpers: dependency-free Chrome DevTools driver (Node >= 22) and the
// independent download verification used by capture.mjs and check.mjs.
import { spawn } from "node:child_process";
import crypto from "node:crypto";
import fs from "node:fs";
import os from "node:os";
import path from "node:path";

export const sleep = (ms) => new Promise((resolve) => setTimeout(resolve, ms));

function findChrome() {
  if (process.env.CHROME) return process.env.CHROME;
  const roots = [
    path.join(os.homedir(), "Library/Caches/ms-playwright"),
    path.join(os.homedir(), ".cache/ms-playwright"),
  ];
  const wanted = /\/(headless_shell|Chromium|Google Chrome for Testing)$/;
  for (const root of roots.filter((r) => fs.existsSync(r))) {
    for (const entry of fs.readdirSync(root).sort().reverse()) {
      if (!entry.startsWith("chromium")) continue;
      const stack = [path.join(root, entry)];
      while (stack.length) {
        const current = stack.pop();
        const stat = fs.statSync(current, { throwIfNoEntry: false });
        if (!stat) continue;
        if (stat.isDirectory()) {
          const depth = current.split(path.sep).length - root.split(path.sep).length;
          if (depth < 6) fs.readdirSync(current).forEach((c) => stack.push(path.join(current, c)));
        } else if (wanted.test(current) && stat.mode & 0o111) {
          return current;
        }
      }
    }
  }
  for (const candidate of ["/usr/bin/chromium", "/usr/bin/chromium-browser", "/usr/bin/google-chrome"]) {
    if (fs.existsSync(candidate)) return candidate;
  }
  throw new Error("No Chromium found; set CHROME=/path/to/chromium");
}

/** Start headless Chromium with the network emulated offline. */
export async function launch() {
  const tmp = fs.mkdtempSync(path.join(os.tmpdir(), "desk-"));
  const chrome = spawn(
    findChrome(),
    ["--headless=new", "--remote-debugging-port=0", `--user-data-dir=${path.join(tmp, "profile")}`,
     "--no-first-run", "--disable-gpu", "--hide-scrollbars", "about:blank"],
    { stdio: ["ignore", "ignore", "pipe"] },
  );
  const wsUrl = await new Promise((resolve, reject) => {
    let log = "";
    chrome.stderr.on("data", (chunk) => {
      log += chunk;
      const match = /DevTools listening on (ws:\/\/\S+)/.exec(log);
      if (match) resolve(match[1]);
    });
    setTimeout(() => reject(new Error("Chromium did not start")), 20000);
  });
  const port = new URL(wsUrl).port;
  const targets = await (await fetch(`http://127.0.0.1:${port}/json`)).json();
  const ws = new WebSocket(targets.find((t) => t.type === "page").webSocketDebuggerUrl);
  await new Promise((resolve) => (ws.onopen = resolve));

  let nextId = 0;
  const pending = new Map();
  const listeners = [];
  ws.onmessage = (event) => {
    const msg = JSON.parse(event.data);
    if (msg.id && pending.has(msg.id)) {
      const { resolve, reject } = pending.get(msg.id);
      pending.delete(msg.id);
      msg.error ? reject(new Error(msg.error.message)) : resolve(msg.result);
    } else {
      listeners.forEach((fn) => fn(msg));
    }
  };
  const send = (method, params = {}) =>
    new Promise((resolve, reject) => {
      const id = ++nextId;
      pending.set(id, { resolve, reject });
      ws.send(JSON.stringify({ id, method, params }));
    });

  const session = {
    send,
    on: (fn) => listeners.push(fn),
    requestSchemes: [],
    downloads: [],
    async evaluate(expression) {
      const { result, exceptionDetails } = await send("Runtime.evaluate", {
        expression, returnByValue: true, awaitPromise: true,
      });
      if (exceptionDetails) throw new Error(exceptionDetails.text);
      return result.value;
    },
    click: (selector) => session.evaluate(`document.querySelector(${JSON.stringify(selector)}).click()`),
    async view(width, height, mobile) {
      await send("Emulation.setDeviceMetricsOverride", { width, height, deviceScaleFactor: 1, mobile });
    },
    async open(readerDir) {
      await send("Page.navigate", { url: "file://" + path.resolve(readerDir, "index.html") });
      for (let i = 0; i < 50; i++) {
        if (await session.evaluate("document.body.dataset.ready === 'true'")) return;
        await sleep(100);
      }
      throw new Error("Reader did not finish starting");
    },
    async shot(file) {
      const { data } = await send("Page.captureScreenshot", { format: "png", captureBeyondViewport: true });
      fs.writeFileSync(file, Buffer.from(data, "base64"));
    },
    async close() {
      ws.close();
      chrome.kill();
      await sleep(800);
      fs.rmSync(tmp, { recursive: true, force: true, maxRetries: 5, retryDelay: 300 });
    },
    tmp,
  };
  listeners.push((msg) => {
    if (msg.method === "Network.requestWillBeSent") session.requestSchemes.push(msg.params.request.url.split(":")[0]);
    if (msg.method === "Browser.downloadWillBegin") {
      session.downloads.push({ name: msg.params.suggestedFilename, guid: msg.params.guid, done: false });
    }
    if (msg.method === "Browser.downloadProgress" && msg.params.state === "completed") {
      const item = session.downloads.find((d) => d.guid === msg.params.guid);
      if (item) item.done = true;
    }
  });
  await send("Page.enable");
  await send("Network.enable");
  await send("Runtime.enable");
  await send("Network.emulateNetworkConditions", { offline: true, latency: 0, downloadThroughput: 0, uploadThroughput: 0 });
  return session;
}

/** Facts that the evidence files say the reader must show, read from the run directory itself. */
export function expectedFor(runDir) {
  const read = (name) => {
    const file = path.join(runDir, "desk", name);
    return fs.existsSync(file) ? fs.readFileSync(file) : null;
  };
  const inspect = JSON.parse(read("inspect.json"));
  const correct = read("correct.json") && JSON.parse(read("correct.json"));
  return {
    ticket: read("ticket.zpl"),
    stages: {
      inspect: { decision: inspect.decision, queued: inspect.queue_file_exists },
      issue: correct ? { decision: correct.decision, queued: correct.queue_file_exists } : null,
    },
  };
}

async function clickAndWaitForDownload(session, dir) {
  await session.send("Browser.setDownloadBehavior", { behavior: "allow", downloadPath: dir, eventsEnabled: true });
  const before = session.downloads.length;
  await session.click("#dl");
  for (let i = 0; i < 50 && !(session.downloads[before] && session.downloads[before].done); i++) await sleep(100);
  const item = session.downloads[before];
  return item && item.done ? { name: item.name, bytes: fs.readFileSync(path.join(dir, item.name)) } : null;
}

/**
 * For every run and every stage: drive the radios, compare what the page says with the
 * evidence files, and (when the page offers a download) compare the saved file's bytes
 * with desk/ticket.zpl and the gate's recorded sha-256. Nothing from the page's own data
 * is trusted. Returns one record per run/stage.
 */
export async function verifyRuns(session, runDirs, tag) {
  const records = [];
  for (let j = 0; j < runDirs.length; j++) {
    const expected = expectedFor(runDirs[j]);
    for (const key of ["inspect", "issue"]) {
      const want = expected.stages[key];
      const radio = `input[name=stage][value=${key}]`;
      await session.click(`input[name=job][value="${j}"]`);
      const radioDisabled = await session.evaluate(`document.querySelector('${radio}').disabled`);
      const rec = { run: path.basename(runDirs[j]), stage: key, viewport: tag, stageAvailable: !radioDisabled };
      rec.stageMatchesEvidence = radioDisabled === (want === null);
      if (want === null) {
        rec.pass = rec.stageMatchesEvidence;
        records.push(rec);
        continue;
      }
      await session.click(radio);
      await sleep(200);
      const page = await session.evaluate(`({
        decision: document.querySelector('[data-fact=decision]').textContent,
        queue: document.querySelector('[data-fact=queue]').textContent,
        disabled: document.getElementById('dl').disabled,
        sha: (document.querySelector('[data-fact=sha]') || {}).textContent || null
      })`);
      rec.decision = page.decision;
      rec.decisionMatches = page.decision === want.decision;
      rec.queueMatches = page.queue === (want.queued ? "present" : "none");
      rec.downloadOffered = !page.disabled;
      rec.downloadExpected = want.queued && expected.ticket !== null;
      rec.offerMatches = rec.downloadOffered === rec.downloadExpected;
      rec.shaShownVerified = want.queued ? /verified$/.test(page.sha || "") : page.sha === null;
      if (rec.downloadOffered) {
        const dir = fs.mkdtempSync(path.join(session.tmp, "dl-"));
        const saved = await clickAndWaitForDownload(session, dir);
        rec.saved = saved !== null;
        rec.bytes = saved ? saved.bytes.length : 0;
        rec.bytesEqualEvidence = !!saved && Buffer.compare(saved.bytes, expected.ticket) === 0;
        rec.sha256Equal = !!saved && crypto.createHash("sha256").update(saved.bytes).digest("hex") === crypto.createHash("sha256").update(expected.ticket).digest("hex");
        rec.startsWithXA = !!saved && saved.bytes.toString("utf8").startsWith("^XA");
        rec.filenameOk = !!saved && /^wardrobe-.*\.zpl$/.test(saved.name);
      }
      rec.pass = [rec.stageMatchesEvidence, rec.decisionMatches, rec.queueMatches, rec.offerMatches, rec.shaShownVerified,
        !rec.downloadOffered || (rec.saved && rec.bytesEqualEvidence && rec.sha256Equal && rec.startsWithXA && rec.filenameOk)].every(Boolean);
      records.push(rec);
    }
  }
  return records;
}
