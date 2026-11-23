import assert from "node:assert/strict";
import { readFileSync, writeFileSync } from "node:fs";
import { fileURLToPath, pathToFileURL } from "node:url";
import path from "node:path";
import { chromium } from "playwright-core";
const sample = fileURLToPath(new URL("../../sample/", import.meta.url));
const hits = [];
const browser = await chromium.launch({
  executablePath: process.env.CHROME,
  headless: true,
});
try {
  for (const width of [1280, 390]) {
    const ctx = await browser.newContext({
        offline: true,
        viewport: { width, height: 840 },
      }),
      p = await ctx.newPage();
    await p.goto(pathToFileURL(path.join(sample, "proofs.html")).href);
    for (const name of [
      "Download the Getty postcard",
      "Download the Cleveland postcard",
      "third postcard",
    ]) {
      const [download] = await Promise.all([
        p.waitForEvent("download"),
        p.getByRole("link", { name, exact: true }).click(),
      ]);
      const bytes = readFileSync(await download.path());
      assert(
        bytes.equals(
          readFileSync(path.join(sample, download.suggestedFilename())),
        ),
      );
      hits.push({
        width,
        file: download.suggestedFilename(),
        exact_bytes: bytes.length,
      });
    }
    await ctx.close();
  }
} finally {
  await browser.close();
}
writeFileSync(
  path.join(sample, "proof-downloads.json"),
  JSON.stringify(hits, null, 2) + "\n",
);
