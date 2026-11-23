const assert = require('node:assert/strict')
const crypto = require('node:crypto')
const fs = require('node:fs/promises')
const path = require('node:path')
const {pathToFileURL} = require('node:url')
const {chromium} = require('playwright-core')

async function main() {
  const [workspace, output] = process.argv.slice(2).map(p => path.resolve(p))
  await fs.mkdir(output, {recursive:false})
  const browser = await chromium.launch({channel:'chrome'})
  try {
    const context = await browser.newContext({offline:true, viewport:{width:1120,height:640}})
    const page = await context.newPage()
    const requests = []
    page.on('request', req => {if (/^https?:/.test(req.url())) requests.push(req.url())})
    await page.goto(pathToFileURL(path.join(workspace,'original.html')).href)
    const original = await page.locator('.time').allTextContents()
    assert.deepEqual(original, ['18:00','18:30','19:07'])
    await page.screenshot({path:path.join(output,'original.png')})
    await page.getByRole('link',{name:'Amended',exact:true}).click()
    const amended = await page.locator('.time').allTextContents()
    assert.deepEqual(amended,['18:00','18:45','19:22'])
    await page.screenshot({path:path.join(output,'amended.png')})
    const pending = page.waitForEvent('download')
    await page.getByRole('link',{name:'Download this calendar'}).click()
    const download = await pending
    assert.equal(download.suggestedFilename(),'amended.ics')
    const bytes = await fs.readFile(await download.path())
    assert.ok(bytes.equals(await fs.readFile(path.join(workspace,'amended.ics'))))
    await page.setViewportSize({width:390,height:844})
    assert.equal(await page.evaluate(() => document.documentElement.scrollWidth),390)
    await page.screenshot({path:path.join(output,'phone.png'),fullPage:true})
    await page.getByRole('link',{name:'Original',exact:true}).click()
    assert.deepEqual(await page.locator('.time').allTextContents(),original)
    assert.deepEqual(requests,[])
    const receipt = {original,amended,download:download.suggestedFilename(),
      sha256:crypto.createHash('sha256').update(bytes).digest('hex'),mobile_width:390,external_requests:requests}
    await fs.writeFile(path.join(output,'browser.json'),JSON.stringify(receipt,null,2)+'\n')
  } finally {await browser.close()}
}
main().catch(error=>{console.error(error);process.exitCode=1})
