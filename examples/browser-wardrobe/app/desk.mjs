import http from "node:http";
import { randomBytes, randomUUID } from "node:crypto";
import { readFile, writeFile, mkdtemp, mkdir, rm } from "node:fs/promises";
import { spawn } from "node:child_process";
import { constants } from "node:fs";
import { access } from "node:fs/promises";
import { tmpdir } from "node:os";
import path from "node:path";
import { fileURLToPath } from "node:url";

const [computerArg, pythonArg, bridgeArg] = process.argv.slice(2);
if (!bridgeArg)
  throw new Error(
    "Expected Computer executable, WASI Python directory and bridge executable",
  );
const computer = path.resolve(computerArg),
  python = path.resolve(pythonArg),
  bridge = path.resolve(bridgeArg);
const app = path.dirname(fileURLToPath(import.meta.url)),
  recipe = path.resolve(app, "../recipe");
await Promise.all([
  access(computer, constants.X_OK),
  access(bridge, constants.X_OK),
  access(path.join(python, "python.wasm"), constants.R_OK),
  access(path.join(python, "lib"), constants.R_OK),
]);
const secret = randomBytes(24).toString("hex");
let origin,
  channel,
  running = false;
const pending = new Map();
function send(res, status, obj) {
  res.writeHead(status, {
    "Content-Type": "application/json",
    "Cache-Control": "no-store",
  });
  res.end(JSON.stringify(obj));
}
async function body(req) {
  const chunks = [];
  let count = 0;
  for await (const chunk of req) {
    count += chunk.length;
    if (count > 8192) throw new Error("Request exceeds 8 KiB");
    chunks.push(chunk);
  }
  return JSON.parse(Buffer.concat(chunks).toString());
}
async function runComputer() {
  const work = await mkdtemp(path.join(tmpdir(), "wardrobe-plan-"));
  try {
    const file = path.join(work, "config.json");
    await mkdir(path.join(work, "home"));
    await mkdir(path.join(work, "work"));
    await writeFile(
      file,
      JSON.stringify({
        mcpServers: {
          wardrobe: {
            command: bridge,
            args: [origin],
            env: { WARDROBE_LINK: "${WARDROBE_LINK}" },
          },
        },
      }),
    );
    const args = [
      "-config",
      file,
      "-mount",
      path.join(work, "work") + ":/work",
      "-mount",
      recipe + ":/recipe:ro",
      "-workdir",
      "/work",
      "-python",
      path.join(python, "python.wasm"),
      "-python-lib",
      path.join(python, "lib"),
      "-c",
      "python3 /recipe/allocate.py",
    ];
    const text = await new Promise((resolve, reject) => {
      const child = spawn(computer, args, {
        env: {
          HOME: path.join(work, "home"),
          PATH: "/usr/bin:/bin",
          LANG: "en_US.UTF-8",
          WARDROBE_LINK: secret,
        },
        timeout: 30000,
      });
      let stdout = "",
        stderr = "";
      child.stdout.on("data", (b) => (stdout += b));
      child.stderr.on("data", (b) => (stderr += b));
      child.on("error", reject);
      child.on("close", (code) =>
        code === 0
          ? resolve(stdout)
          : reject(new Error(stderr.trim() || "Computer failed")),
      );
    });
    return JSON.parse(text);
  } finally {
    await rm(work, { recursive: true, force: true });
  }
}
const server = http.createServer(async (req, res) => {
  try {
    if (req.headers.host !== new URL(origin).host)
      return send(res, 400, { error: "Unexpected host" });
    const url = new URL(req.url, origin);
    if (
      req.method === "GET" &&
      ["/", "/wardrobe.js", "/style.css"].includes(url.pathname)
    ) {
      const f = {
        "/": ["index.html", "text/html"],
        "/wardrobe.js": ["wardrobe.js", "application/javascript"],
        "/style.css": ["style.css", "text/css"],
      }[url.pathname];
      const data = await readFile(path.join(app, f[0]));
      res.writeHead(200, {
        "Content-Type": f[1] + "; charset=utf-8",
        "Cache-Control": "no-store",
      });
      return res.end(data);
    }
    if (req.method === "GET" && url.pathname === "/events") {
      if (req.headers.origin && req.headers.origin !== origin)
        return send(res, 403, { error: "Use this desk tab" });
      if (channel)
        return send(res, 409, { error: "Another wardrobe tab is connected" });
      res.writeHead(200, {
        "Content-Type": "text/event-stream",
        "Cache-Control": "no-cache",
        Connection: "keep-alive",
      });
      res.write(": ready\n\n");
      channel = res;
      res.on("close", () => {
        if (channel === res) channel = null;
      });
      return;
    }
    if (req.method !== "POST")
      return send(res, 404, { error: "Unknown route" });
    if (url.pathname === "/operation") {
      if (req.headers["x-wardrobe-link"] !== secret)
        return send(res, 403, { error: "Unknown bridge" });
      if (!channel)
        return send(res, 409, { error: "Connect the wardrobe tab first" });
      const message = await body(req);
      if (!["save", "read"].includes(message.op))
        return send(res, 400, { error: "Unknown operation" });
      const id = randomUUID();
      const result = await new Promise((resolve, reject) => {
        const timer = setTimeout(() => {
          pending.delete(id);
          reject(new Error("Browser did not respond"));
        }, 9000);
        pending.set(id, {
          resolve: (value) => {
            clearTimeout(timer);
            resolve(value);
          },
        });
        channel.write("data: " + JSON.stringify({ id, ...message }) + "\n\n");
      });
      return send(
        res,
        result.ok ? 200 : 409,
        result.ok ? result.value : { error: result.error },
      );
    }
    if (req.headers.origin !== origin)
      return send(res, 403, { error: "Use this desk tab" });
    if (url.pathname === "/reply") {
      const msg = await body(req),
        task = pending.get(msg.id);
      if (!task) return send(res, 404, { error: "Unknown request" });
      pending.delete(msg.id);
      task.resolve(msg);
      return send(res, 200, { ok: true });
    }
    if (url.pathname === "/plan") {
      if (running || !channel)
        return send(res, 409, {
          error: "Connect one tab and finish the current plan",
        });
      running = true;
      try {
        return send(res, 200, await runComputer());
      } finally {
        running = false;
      }
    }
    return send(res, 404, { error: "Unknown operation" });
  } catch (error) {
    return send(res, 500, { error: error.message });
  }
});
server.listen(0, "127.0.0.1", () => {
  origin = "http://127.0.0.1:" + server.address().port;
  console.log(origin);
});
