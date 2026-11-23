const fs = require("node:fs");
const path = require("node:path");
const { pathToFileURL } = require("node:url");
const { createHash } = require("node:crypto");
const assert = require("node:assert/strict");
const { chromium } = require("playwright-core");

(async () => {
  const out = path.resolve(process.argv[2]);
  const receipt = JSON.parse(fs.readFileSync(path.join(out, "receipt.json")));
  const results = [];
  const browser = await chromium.launch({
    executablePath: process.env.CHROME,
    headless: true,
  });
  try {
    for (const width of [1280, 390]) {
      const context = await browser.newContext({
        offline: true,
        viewport: { width, height: 860 },
      });
      const page = await context.newPage();
      await page.goto(pathToFileURL(path.join(out, "index.html")).href);
      for (const id of ["first", "amended"]) {
        await page.locator("input[value=" + id + "]").check();
        await page.locator("#proof").evaluate((el) => el.decode());
        assert.equal(
          await page.locator("#counts").innerText(),
          receipt[id].copies.getty +
            " Getty · " +
            receipt[id].copies.cleveland +
            " Cleveland",
        );
        const [file] = await Promise.all([
          page.waitForEvent("download"),
          page.locator("#download").click(),
        ]);
        const pdf = fs.readFileSync(await file.path());
        assert.equal(
          createHash("sha256").update(pdf).digest("hex"),
          receipt[id].pdf_sha256,
        );
        assert(
          await page.evaluate(
            () => document.documentElement.scrollWidth <= innerWidth,
          ),
        );
        results.push({
          width,
          edition: id,
          download_bytes: pdf.length,
          sha256: receipt[id].pdf_sha256,
        });
      }
      await page.locator("summary").click();
      assert.match(
        await page.locator("#queue").innerText(),
        /completed \(IPP state 9\)/,
      );
      await page.screenshot({
        path: path.join(out, width === 1280 ? "desktop.png" : "phone.png"),
      });
      await context.close();
    }
  } finally {
    await browser.close();
  }
  fs.writeFileSync(
    path.join(out, "desk-browser.json"),
    JSON.stringify(results, null, 2) + "\n",
  );
})().catch((e) => {
  console.error(e);
  process.exit(1);
});
