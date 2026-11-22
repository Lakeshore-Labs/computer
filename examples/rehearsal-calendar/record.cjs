const { chromium } = require("playwright-core")
const { existsSync, mkdirSync, readFileSync, writeFileSync, renameSync } = require("node:fs")
const path = require("node:path")
const { pathToFileURL } = require("node:url")
const { createHash } = require("node:crypto")

async function record() {
  const [source, target] = process.argv.slice(2).map(p => path.resolve(p))
  if (existsSync(target)) throw Error("Choose a new capture directory.")
  mkdirSync(target, { recursive: true })
  const browser = await chromium.launch({ channel: "chrome", headless: true })
  const results = []
  try {
    for (const [screen, width, height] of [["desktop", 1280, 840], ["phone", 390, 844]]) {
      const context = await browser.newContext({ viewport: { width, height }, offline: true,
        recordVideo: { dir: target, size: { width, height } } })
      const page = await context.newPage()
      await page.goto(pathToFileURL(path.join(source, "studio.html")).href)
      await page.locator("#decision").getByText("11:00–11:45 was the first shared opening. Computer booked it.").waitFor()
      await page.screenshot({ path: path.join(target, screen + ".png"), fullPage: true })
      const checks = []
      for (const [minutes, expected] of [[0, "Stage setup"], [75, "School ensemble"], [105, "Piano move"], [135, "was also available"]]) {
        await page.getByLabel("Inspect a start time").selectOption(String(minutes))
        await page.locator("#decision").getByText(expected, { exact: false }).waitFor()
        checks.push(await page.locator("#decision").innerText())
        await page.waitForTimeout(400)
      }
      await page.getByRole("button", { name: "Show the booked time" }).click()
      await page.locator("#decision").getByText("11:00–11:45 was the first shared opening. Computer booked it.").waitFor()
      const saved = page.waitForEvent("download")
      await page.getByText("Save the synced calendar entry").click()
      const file = await saved
      const actual = readFileSync(await file.path())
      if (!actual.equals(readFileSync(path.join(source, "rehearsal.ics")))) throw Error("Calendar download differs.")
      const widthRead = await page.evaluate(() => document.documentElement.scrollWidth)
      if (widthRead > width) throw Error("Horizontal overflow.")
      if (await page.locator("#agenda li").count() !== 4) throw Error("Expected four room agenda entries.")
      results.push({ screen, width, checks, agendaRows: 4, downloadSHA256: createHash("sha256").update(actual).digest("hex") })
      const video = page.video()
      await context.close()
      renameSync(await video.path(), path.join(target, screen + ".webm"))
    }
  } finally {
    await browser.close()
  }
  writeFileSync(path.join(target, "browser.json"), JSON.stringify(results, null, 2) + "\n")
}
record().catch(e => { console.error(e); process.exitCode = 1 })
