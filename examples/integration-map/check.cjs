const assert = require("node:assert/strict"),
  fs = require("node:fs"),
  path = require("node:path");
const { pathToFileURL } = require("node:url"),
  { chromium } = require("playwright-core");
(async () => {
  const dir = path.join(__dirname, "sample"),
    rows = JSON.parse(fs.readFileSync(path.join(__dirname, "routes.json"))),
    checks = [];
  const browser = await chromium.launch({
    executablePath: process.env.CHROME,
    headless: true,
  });
  try {
    for (const width of [1280, 390]) {
      const c = await browser.newContext({
          offline: true,
          viewport: { width, height: 855 },
        }),
        p = await c.newPage();
      await p.goto(pathToFileURL(path.join(dir, "map.html")).href);
      await p.locator("#cover").evaluate((im) => im.decode());
      await p.screenshot({
        path: path.join(dir, width === 1280 ? "desktop.png" : "phone.png"),
      });
      for (const row of rows) {
        await p.getByRole("button", { name: row.role, exact: true }).click();
        await p.locator("select").selectOption({ label: row.name });
        await p.locator("#cover").evaluate((im) => im.decode());
        assert.equal(await p.locator("#outcome").innerText(), row.outcome);
        await p.waitForTimeout(550);
        const [dl] = await Promise.all([
          p.waitForEvent("download", { timeout: 6000 }),
          p.locator("#source").click(),
        ]);
        assert(
          fs
            .readFileSync(await dl.path())
            .equals(fs.readFileSync(path.join(__dirname, "..", row.receipt))),
        );
        assert(
          await p.evaluate(
            () => document.documentElement.scrollWidth <= innerWidth,
          ),
        );
        checks.push({ width, name: row.name, exact_download: true });
      }
      await c.close();
    }
  } finally {
    await browser.close();
  }
  fs.writeFileSync(
    path.join(dir, "browser.json"),
    JSON.stringify(checks, null, 2) + "\n",
  );
})().catch((e) => {
  console.error(e);
  process.exit(1);
});
