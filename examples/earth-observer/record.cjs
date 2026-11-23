/* Record real offline browser states and verify the date selectors and keyboard-operated divider. */
const assert = require("node:assert/strict");
const { execFileSync } = require("node:child_process");
const fs = require("node:fs");
const os = require("node:os");
const path = require("node:path");
const { pathToFileURL } = require("node:url");
const { chromium } = require("playwright-core");

async function record(input, output) {
  fs.mkdirSync(output, { recursive: false });
  const frames = fs.mkdtempSync(path.join(os.tmpdir(), "computer-earth-frames-"));
  const browser = await chromium.launch({ channel: "chrome", headless: true });
  try {
    const context = await browser.newContext({ viewport: { width: 1100, height: 980 }, offline: true });
    const page = await context.newPage();
    await page.goto(pathToFileURL(input).href);
    const slider = page.locator("#slide");
    const position = page.locator("#position");
    const photo = page.locator(".scene img");
    const dimensions = await photo.evaluateAll(async (images) => {
      await Promise.all(images.map((image) => image.decode()));
      return images.map((image) => [image.naturalWidth, image.naturalHeight]);
    });
    assert.deepEqual(dimensions, [[680, 400], [680, 400]]);
    await page.screenshot({ path: path.join(output, "desk.jpg"), type: "jpeg", quality: 86 });
    await slider.focus();
    await page.keyboard.press("ArrowRight");
    assert.equal(await position.textContent(), "51%");
    const frameValues = [];
    for (const left of [0, 1]) {
      await page.locator("#a").selectOption(String(left));
      for (let split = 0; split <= 100; split += 5) {
        await slider.fill(String(split));
        assert.equal(await position.textContent(), split + "%");
        const n = frameValues.length;
        frameValues.push({ left, split });
        await page.screenshot({ path: path.join(frames, String(n).padStart(3, "0") + ".png") });
      }
    }
    assert.equal(await page.locator("#ad").textContent(), "2024-04-06");
    await page.locator("#b").selectOption("3");
    assert.equal(await page.locator("#bd").textContent(), "2024-10-05");
    await page.setViewportSize({ width: 390, height: 844 });
    assert.equal(await page.evaluate(() => document.documentElement.scrollWidth), 390);
    await page.screenshot({ path: path.join(output, "phone.jpg"), type: "jpeg", quality: 86 });
    execFileSync("ffmpeg", ["-v", "error", "-framerate", "8", "-i", path.join(frames, "%03d.png"),
      "-c:v", "libx264", "-pix_fmt", "yuv420p", "-movflags", "+faststart", path.join(output, "desk.mp4")]);
    fs.writeFileSync(path.join(output, "browser.json"), JSON.stringify({
      offline: true, dimensions, frames: frameValues.length, keyboard_position: 51,
      left_date: "2024-04-06", right_date: "2024-10-05", mobile_width: 390,
    }, null, 2) + "\n");
  } finally {
    await browser.close();
    fs.rmSync(frames, { recursive: true, force: true });
  }
}

const [input, output] = process.argv.slice(2);
if (!input || !output) throw new Error("Pass the path to desk.html and a fresh output directory.");
record(path.resolve(input), path.resolve(output)).catch((error) => {
  console.error(error.message);
  process.exitCode = 1;
});
