const { chromium } = require("playwright-core");
const fs = require("node:fs/promises");
const path = require("node:path");
const os = require("node:os");
const { spawn } = require("node:child_process");
const { once } = require("node:events");
const readline = require("node:readline");
const { createHash } = require("node:crypto");
const assert = require("node:assert/strict");

async function files(page) {
  return page.evaluate(async () => {
    const root = await navigator.storage.getDirectory(),
      dir = await root.getDirectoryHandle("wardrobe");
    const result = {};
    for await (const [name, h] of dir.entries())
      result[name] = await (await h.getFile()).text();
    return result;
  });
}
async function main() {
  const [computer, python, bridge, outArg] = process.argv.slice(2),
    out = path.resolve(outArg);
  if (!outArg)
    throw new Error(
      "Expected Computer, WASI Python directory, bridge executable and a new output directory",
    );
  await fs.mkdir(out);
  const server = spawn(
    process.execPath,
    [path.join(__dirname, "app/desk.mjs"), computer, python, bridge],
    { stdio: ["ignore", "pipe", "pipe"] },
  );
  let diagnostics = "";
  server.stderr.on("data", (chunk) => (diagnostics += chunk));
  const lines = readline.createInterface({ input: server.stdout });
  const address = await Promise.race([
    once(lines, "line").then(([s]) => s),
    once(server, "exit").then(() => {
      throw new Error(diagnostics || "Desk exited");
    }),
  ]);
  const result = [];
  try {
    for (const [screen, viewport] of Object.entries({
      desktop: { width: 1260, height: 850 },
      phone: { width: 390, height: 844 },
    })) {
      const profile = await fs.mkdtemp(
        path.join(os.tmpdir(), "wardrobe-chrome-"),
      );
      let ctx;
      try {
        ctx = await chromium.launchPersistentContext(profile, {
          channel: "chrome",
          headless: true,
          viewport,
          recordVideo: { dir: out, size: viewport },
        });
        const page = ctx.pages()[0] || (await ctx.newPage());
        page.setDefaultTimeout(20000);
        const errors = [],
          origins = new Set();
        page.on("pageerror", (e) => errors.push(e.message));
        page.on("request", (request) => {
          if (/^https?:/.test(request.url()))
            origins.add(new URL(request.url()).origin);
        });
        // Hold the first OPFS open so the recorder can observe the startup controls.
        await page.addInitScript(() => {
          if (sessionStorage.getItem("wardrobe-initial-open")) return;
          sessionStorage.setItem("wardrobe-initial-open", "checked");
          const original = navigator.storage.getDirectory.bind(navigator.storage);
          const waiting = new Promise((release) => { window.releaseWardrobeStore = release; });
          navigator.storage.getDirectory = async () => {
            document.body.dataset.opening = "true";
            await waiting;
            return original();
          };
        });
        await page.goto(address, { waitUntil: "commit" });
        await page.locator('body[data-opening="true"]').waitFor();
        const connect = page.getByRole("button", { name: "Connect this tab" });
        const disabledDuringOpen = await connect.isDisabled();
        await page.evaluate(() => window.releaseWardrobeStore());
        assert.equal(disabledDuringOpen, true, "Connect must wait for the first OPFS open");
        await page.locator('body[data-ready="true"]').waitFor();
        await page.getByRole("button", { name: "Connect this tab" }).click();
        await page.getByRole("button", { name: "Run Computer" }).click();
        await page
          .getByText("5 fitted · 2 still need jackets", { exact: true })
          .waitFor();
        assert.equal(await page.locator(".tag.waiting").count(), 2);
        const before = await files(page);
        await page.screenshot({
          path: path.join(out, screen + "-before.png"),
          fullPage: screen === "phone",
        });
        await page.waitForTimeout(1000);
        await ctx.setOffline(true);
        assert.equal(
          await page.evaluate(() =>
            fetch("/style.css?check=offline", { cache: "no-store" }).then(
              () => false,
              () => true,
            ),
          ),
          true,
        );
        await page.getByLabel("Update medium stock").fill("8");
        await page.getByRole("button", { name: "Save count" }).click();
        await page
          .getByText(
            "Stock changed. Run Computer to update the fitting list.",
            { exact: true },
          )
          .waitFor();
        const offline = await files(page);
        assert.equal(JSON.parse(offline["inventory.json"]).M, 8);
        assert.equal(offline["allocation.json"], before["allocation.json"]);
        await page.screenshot({
          path: path.join(out, screen + "-offline.png"),
          fullPage: screen === "phone",
        });
        await page.waitForTimeout(1400);
        await ctx.setOffline(false);
        await page.reload();
        await page.locator('body[data-ready="true"]').waitFor();
        await page.getByRole("button", { name: "Connect this tab" }).click();
        await page.getByRole("button", { name: "Run Computer" }).click();
        await page
          .getByText("7 fitted · 1 spare on the rack", { exact: true })
          .waitFor();
        assert.equal(await page.locator(".tag.ready").count(), 7);
        const after = await files(page);
        await page.screenshot({
          path: path.join(out, screen + "-after.png"),
          fullPage: screen === "phone",
        });
        assert.equal(
          await page.evaluate(
            () => document.documentElement.scrollWidth > innerWidth,
          ),
          false,
        );
        assert.deepEqual(errors, []);
        assert.deepEqual([...origins], [address]);
        await page.waitForTimeout(1600);
        const film = page.video();
        await ctx.close();
        ctx = null;
        await fs.rename(await film.path(), path.join(out, screen + ".webm"));
        ctx = await chromium.launchPersistentContext(profile, {
          channel: "chrome",
          headless: true,
          viewport,
        });
        const reopened = ctx.pages()[0] || (await ctx.newPage());
        await reopened.goto(address);
        await reopened
          .getByText("7 fitted · 1 spare on the rack", { exact: true })
          .waitFor();
        const persisted = await files(reopened);
        assert.deepEqual(persisted, after);
        const stages = {};
        for (const [name, t] of Object.entries({ before, offline, after })) {
          const data = JSON.parse(t["inventory.json"]),
            alloc = JSON.parse(t["allocation.json"]);
          const rev = createHash("sha256")
            .update(JSON.stringify(data))
            .digest("hex");
          assert.equal(alloc.revision === rev, name !== "offline");
          stages[name] = { stock: data, allocation: alloc, revision: rev };
        }
        assert.deepEqual(
          [stages.before.allocation.short, stages.after.allocation.spare],
          [2, 1],
        );
        result.push({
          screen,
          viewport,
          stages,
          offline_fetch_rejected: true,
          reopened_files_match: true,
          local_origins: origins.size,
          errors,
        });
      } finally {
        if (ctx) await ctx.close();
        await fs.rm(profile, { recursive: true, force: true });
      }
    }
  } finally {
    lines.close();
    server.kill("SIGTERM");
    await once(server, "exit");
  }
  await fs.writeFile(
    path.join(out, "browser.json"),
    JSON.stringify(result, null, 2) + "\n",
  );
}
main().catch((error) => {
  console.error(error);
  process.exitCode = 1;
});
