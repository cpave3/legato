// Run against an isolated Vite fixture server, never a running Legato.
import assert from 'node:assert/strict'
import { mkdir, readFile, writeFile } from 'node:fs/promises'
import { resolve } from 'node:path'
const { chromium } = await import(process.env.PLAYWRIGHT_MODULE || 'playwright')
const origin = process.argv[2]
assert.match(origin, /^http:\/\/127\.0\.0\.1:\d+$/)
const output = resolve(process.argv[3] || '/tmp/native-message-order')
const broken = process.argv.includes('--expect-broken')
await mkdir(output, { recursive: true })
const font = (await readFile(new URL('../web/node_modules/@fontsource/geist-sans/files/geist-sans-latin-400-normal.woff2', import.meta.url))).toString('base64')
const browser = await chromium.launch({ headless: true, executablePath: process.env.CHROMIUM_PATH })
const results = []
try {
  for (const width of [1280, 390]) {
    const page = await browser.newPage({ viewport: { width, height: 800 } })
    const errors = []
    page.on('pageerror', error => errors.push(String(error)))
    await page.route('**/*', route => new URL(route.request().url()).origin === origin ? route.continue() : route.abort())
    // Use the app's own font even on hosts where Chromium has no system fonts.
    await page.addInitScript(font => window.addEventListener('DOMContentLoaded', async () => {
      for (const family of ['Geist Sans', 'Geist Mono']) {
        const face = await new FontFace(family, `url(data:font/woff2;base64,${font})`).load()
        document.fonts.add(face)
      }
    }), font)
    const settle = () => page.evaluate(() => new Promise(resolve => requestAnimationFrame(() => requestAnimationFrame(resolve))))
    const measure = () => page.evaluate(() => {
      const column = document.querySelector('[data-conversation-column]')
      const viewport = document.querySelector('[data-conversation-scroll]').getBoundingClientRect()
      const reply = [...column.querySelectorAll('p')].find(item => item.textContent === 'Implementation is done.')
      const box = reply.getBoundingClientRect()
      return { lastRowIsReply: column.lastElementChild.contains(reply), replyVisible: box.top >= viewport.top && box.bottom <= viewport.bottom }
    })
    await page.goto(`${origin}/src/test/native-order-preview.html`)
    await page.getByRole('button', { name: 'Stream exchange', exact: true }).click()
    await page.getByText('Implementation is done.', { exact: true }).waitFor()
    // Arrange the reported starting point: the operator has seen the reply.
    await page.locator('[data-conversation-scroll]').evaluate(element => { element.scrollTop = element.scrollHeight })
    await settle()
    const before = await measure()
    assert.ok(before.lastRowIsReply && before.replyVisible)
    await page.screenshot({ path: `${output}/${width}-before.png` })
    await page.getByRole('button', { name: 'Finish and refresh', exact: true }).click()
    await page.waitForFunction(() => Number(document.body.dataset.snapshots) >= 2)
    await settle()
    const after = await measure()
    await page.screenshot({ path: `${output}/${width}-after.png` })
    assert.equal(after.lastRowIsReply, !broken)
    assert.equal(after.replyVisible, !broken)
    await page.goto(`${origin}/src/test/native-order-preview.html?finished`)
    await page.getByText('Implementation is done.', { exact: true }).waitFor({ state: 'attached' })
    await settle()
    const reload = await measure()
    assert.equal(reload.lastRowIsReply, !broken)
    assert.equal(reload.replyVisible, !broken)
    assert.deepEqual(errors, [])
    results.push({ width, before, after, reload })
    await page.close()
  }
  await writeFile(`${output}/results.json`, JSON.stringify({ expectedBroken: broken, results }, null, 2) + '\n')
  console.log(`${results.length} isolated panel/scroll cases passed (${broken ? 'known-broken baseline' : 'fixed'})`)
} finally { await browser.close() }
