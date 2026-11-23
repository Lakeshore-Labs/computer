const assert = require("node:assert/strict");
const path = require("node:path");
const fs = require("node:fs");
const { chromium } = require("playwright-core");

(async () => {
  const [url, out] = process.argv.slice(2);
  const browser = await chromium.launch({
    executablePath: process.env.CHROME,
    headless: true,
  });
  try {
    const page = await browser.newPage({
      viewport: { width: 1000, height: 440 },
    });
    await page.goto(url);
    const text = await page.locator("body").innerText();
    assert.match(text, /Museum proof press Jobs/);
    assert.match(text, /Amended-postcards/);
    assert.match(text, /museum-desk/);
    assert.match(text, /completed/i);
    await page.screenshot({ path: path.join(out, "queue.png") });
    fs.writeFileSync(
      path.join(out, "queue-browser.json"),
      JSON.stringify({ title: await page.title(), text }, null, 2) + "\n",
    );
  } finally {
    await browser.close();
  }
})().catch((e) => {
  console.error(e);
  process.exit(1);
});
