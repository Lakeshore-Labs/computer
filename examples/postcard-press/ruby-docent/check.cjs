const assert = require("node:assert/strict");
const fs = require("node:fs");
const path = require("node:path");
const { pathToFileURL } = require("node:url");
const { createHash } = require("node:crypto");
const { chromium } = require("playwright-core");
(async () => {
  const dir = path.join(__dirname, "sample");
  const rows = JSON.parse(
    fs.readFileSync(path.join(dir, "operator-receipt.json")),
  ).cards;
  const results = [];
  const browser = await chromium.launch({
    executablePath: process.env.CHROME,
    headless: true,
  });
  try {
    for (const width of [1280, 390]) {
      const ctx = await browser.newContext({
        offline: true,
        viewport: { width, height: 850 },
      });
      const page = await ctx.newPage();
      await page.goto(pathToFileURL(path.join(dir, "index.html")).href);
      for (const c of rows) {
        await page.getByRole("button", { name: c.visitor }).click();
        await page.locator("#card").evaluate((im) => im.decode());
        assert.equal(
          await page.locator("#inscription").innerText(),
          c.inscription,
        );
        const [file] = await Promise.all([
          page.waitForEvent("download"),
          page.locator("#keep").click(),
        ]);
        const bytes = fs.readFileSync(await file.path());
        assert.equal(
          createHash("sha256").update(bytes).digest("hex"),
          c.svg_sha256,
        );
        assert(
          await page.evaluate(
            () => document.documentElement.scrollWidth <= innerWidth,
          ),
        );
        results.push({
          width,
          visitor: c.visitor,
          file: file.suggestedFilename(),
          bytes: bytes.length,
          sha256: c.svg_sha256,
        });
      }
      await page.screenshot({
        path: path.join(dir, width === 1280 ? "desktop.png" : "phone.png"),
      });
      await ctx.close();
    }
  } finally {
    await browser.close();
  }
  fs.writeFileSync(
    path.join(dir, "browser.json"),
    JSON.stringify(results, null, 2) + "\n",
  );
})().catch((e) => {
  console.error(e);
  process.exit(1);
});
