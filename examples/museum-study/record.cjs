const assert = require('node:assert/strict')
const fs = require('node:fs/promises')
const path = require('node:path')
const {pathToFileURL} = require('node:url')
const {chromium} = require('playwright-core')

async function main() {
  const [input, output] = process.argv.slice(2).map(p => path.resolve(p))
  const browser = await chromium.launch({channel:'chrome'})
  try {
    const context = await browser.newContext({offline:true,viewport:{width:1200,height:840}})
    const page = await context.newPage()
    const network = []
    page.on('request', req => {if (/^https?:/.test(req.url())) network.push(req.url())})
    await page.goto(pathToFileURL(input).href)
    const dimensions = await page.locator('figure img').evaluateAll(async imgs => {
      await Promise.all(imgs.map(img => img.decode()))
      return imgs.map(img => [img.naturalWidth,img.naturalHeight])
    })
    assert.deepEqual(dimensions, [[577,460],[585,460]])
    const titles = await page.locator('figcaption h2').allTextContents()
    assert.deepEqual(titles,['The Large Plane Trees (Road Menders at Saint-Rémy)','Irises'])
    await page.screenshot({path:path.join(output,'study.jpg'),quality:86,fullPage:true})
    const figures = page.locator('figure')
    let first = await figures.nth(0).boundingBox(), second = await figures.nth(1).boundingBox()
    assert.ok(second.x >= first.x + first.width - 1)
    await page.setViewportSize({width:390,height:844})
    first = await figures.nth(0).boundingBox()
    second = await figures.nth(1).boundingBox()
    assert.ok(second.y >= first.y + first.height - 1)
    assert.equal(await page.evaluate(() => document.documentElement.scrollWidth),390)
    await page.screenshot({path:path.join(output,'phone.jpg'),quality:80,fullPage:true})
    assert.deepEqual(network,[])
    await fs.writeFile(path.join(output,'browser.json'),JSON.stringify({titles,dimensions,
      desktop_columns:2,mobile_columns:1,mobile_width:390,external_requests:network},null,2)+'\n')
  } finally {await browser.close()}
}
main().catch(e=>{console.error(e);process.exitCode=1})
