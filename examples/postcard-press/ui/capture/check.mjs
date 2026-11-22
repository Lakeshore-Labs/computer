import assert from "node:assert/strict";
import { execFileSync } from "node:child_process";
import { mkdirSync, readFileSync, writeFileSync } from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";
import { chromium } from "playwright-core";

const [address, output, python] = process.argv.slice(2);
if (!address || !output || !python)
  throw new Error(
    "Supply the press address, a fresh output directory and the MCP Python executable.",
  );
const example = fileURLToPath(new URL("../../", import.meta.url));
mkdirSync(output, { recursive: false });
const browser = await chromium.launch({
  executablePath: process.env.CHROME,
  headless: true,
});
const results = [];
try {
  const page = await browser.newPage({
    viewport: { width: 390, height: 844 },
    acceptDownloads: true,
  });
  await page.goto(address);
  await page.waitForSelector("#status[data-state=ready]");
  await page.locator("#inscription").fill(" ");
  assert(await page.locator("#issue").isDisabled());
  const inscription = "A print for Zoë & René <3";
  for (const [i, museum] of ["getty", "cleveland", "cleveland"].entries()) {
    await page.locator(".work[data-museum=" + museum + "]").click();
    await page.locator("#inscription").fill(inscription);
    await page.locator("#issue").click();
    await page.waitForFunction(
      (n) =>
        document.querySelectorAll("#issued li:not(.muted)").length === n ||
        !!document.querySelector("#stage .error"),
      i + 1,
    );
    assert.equal(
      await page.locator("#stage .error").count(),
      0,
      await page.locator("#stage").innerText(),
    );
    const [dl] = await Promise.all([
      page.waitForEvent("download"),
      page.locator("#download").click(),
    ]);
    const svg = path.join(output, "order-" + i + ".svg");
    await dl.saveAs(svg);
    const witness = path.join(output, "independent-" + i);
    execFileSync(python, [
      path.join(example, "observe.py"),
      "--endpoint",
      address,
      "--output",
      witness,
      "--download",
      svg,
    ]);
    const proof = JSON.parse(readFileSync(path.join(witness, "receipt.json")));
    assert.deepEqual(proof.order, { museum, inscription });
    assert(
      await page.evaluate(
        () => document.documentElement.scrollWidth <= innerWidth,
      ),
    );
    results.push({
      museum,
      inscription,
      download_bytes: proof.guest.svg_bytes,
      sha256: proof.independent_svg_sha256,
      native_raster: proof.raster_sha256,
    });
  }
  assert.equal(results[1].sha256, results[2].sha256);
  writeFileSync(
    path.join(output, "results.json"),
    JSON.stringify(results, null, 2) + "\n",
  );
} finally {
  await browser.close();
}
