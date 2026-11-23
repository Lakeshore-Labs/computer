const assert = require("node:assert/strict");
const fs = require("node:fs");
const path = require("node:path");
const { pathToFileURL } = require("node:url");
const { chromium } = require("playwright-core");
(async () => {
  const [source, target] = process.argv.slice(2).map((p) => path.resolve(p));
  const start = pathToFileURL(path.join(source, "index.html")).href;
  const checks = [];
  const browser = await chromium.launch({
    executablePath: process.env.CHROME,
    headless: true,
  });
  try {
    for (const width of [1280, 390]) {
      const ctx = await browser.newContext({
          offline: true,
          viewport: { width, height: 850 },
        }),
        p = await ctx.newPage();
      const outside = [];
      p.on("request", (req) => {
        if (/^https?:/.test(req.url())) outside.push(req.url());
      });
      await p.goto(start);
      await p
        .locator("img")
        .evaluateAll((ims) => Promise.all(ims.map((im) => im.decode())));
      assert(
        await p.evaluate(
          () => document.documentElement.scrollWidth <= innerWidth,
        ),
      );
      await p.screenshot({
        path: path.join(target, width === 1280 ? "desktop.png" : "phone.png"),
      });
      const films = [];
      for (const link of ["Inside Computer", "Where the files go"]) {
        await p.getByRole("link", { name: new RegExp(link) }).click();
        const buttons = p.getByRole("button");
        assert.equal(await buttons.count(), 6);
        await buttons.last().click();
        await p.waitForFunction(() => {
          const v = document.querySelector("video");
          return !v.paused && !v.seeking && v.currentTime > 55;
        });
        const facts = await p
          .locator("video")
          .evaluate((v) => ({
            duration: v.duration,
            position: v.currentTime,
            caption_tracks: v.textTracks.length,
          }));
        assert.equal(facts.caption_tracks, 1);
        films.push({ film: link, ...facts });
        await p.goto(start);
      }
      await p
        .getByRole("link", { name: "Explore 23 integration paths" })
        .click();
      assert.equal(await p.locator("select option").count(), 14);
      await p.goto(start);
      assert.equal(
        await p
          .getByRole("link", { name: "Open the one-page print guide" })
          .getAttribute("href"),
        "field-guide.pdf",
      );
      assert.deepEqual(outside, []);
      checks.push({
        width,
        films,
        integration_map_opened: true,
        external_requests: outside,
      });
      await ctx.close();
    }
  } finally {
    await browser.close();
  }
  fs.writeFileSync(
    path.join(target, "browser.json"),
    JSON.stringify(checks, null, 2) + "\n",
  );
})().catch((e) => {
  console.error(e);
  process.exit(1);
});
