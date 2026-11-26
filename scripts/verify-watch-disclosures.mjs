// Run only against the isolated Vite fixture, never a running Legato.
import assert from 'node:assert/strict'
import { mkdir, writeFile } from 'node:fs/promises'
import { resolve } from 'node:path'
const { chromium } = await import(process.env.PLAYWRIGHT_MODULE || 'playwright')
const origin = process.argv[2]
assert.match(origin, /^http:\/\/127\.0\.0\.1:\d+$/)
const output = resolve(process.argv[3] || '/tmp/watch-disclosures')
await mkdir(output, { recursive: true })
const browser = await chromium.launch({ headless: true, executablePath: process.env.CHROMIUM_PATH })
const results = []
try {
  for (const width of [1280, 390, 320]) {
    const page = await browser.newPage({ viewport: { width, height: 900 } })
    const errors = []
    page.on('pageerror', error => errors.push(String(error)))
    await page.route('**/*', route => {
      const url = new URL(route.request().url())
      return url.origin === origin && !url.pathname.startsWith('/api/') ? route.continue() : route.abort()
    })
    await page.routeWebSocket('**/scratch-watch-ws', () => {})
    await page.goto(`${origin}/src/test/watch-preview.html`)
    const single = page.getByRole('button', { name: /Legato · agent T-3 · 1 event/ })
    const batch = page.getByRole('button', { name: /Legato · agent 2 agents · 3 events/ })
    await batch.waitFor()
    assert.equal(await single.getAttribute('aria-expanded'), 'false')
    assert.equal(await batch.getAttribute('aria-expanded'), 'false')
    await page.getByRole('region', { name: 'Permission request for bash' }).waitFor()
    await page.getByRole('region', { name: 'Question from Chimera' }).waitFor()
    assert.equal(await page.getByText(/message_reeve/).count(), 0)
    const noOverflow = () => page.evaluate(() => document.documentElement.scrollWidth <= innerWidth &&
      [...document.querySelectorAll('[data-conversation-column] button')].every(el => el.getBoundingClientRect().right <= innerWidth))
    assert.ok(await noOverflow())
    // Reach the disclosure using the browser's native keyboard navigation.
    await page.keyboard.press('Tab')
    for (let i = 0; i < 12 && !await batch.evaluate(el => el === document.activeElement); i++) await page.keyboard.press('Tab')
    assert.ok(await batch.evaluate(el => el === document.activeElement && el.matches(':focus-visible')))
    const outline = await batch.evaluate(el => getComputedStyle(el).outlineStyle)
    assert.notEqual(outline, 'none')
    await page.screenshot({ path: `${output}/${width}-collapsed.png` })
    await page.keyboard.press('Enter')
    assert.equal(await batch.getAttribute('aria-expanded'), 'true')
    const detail = page.getByText(/message_reeve/)
    const text = await detail.textContent()
    assert.ok(text.includes('https://example.test/pr/42 and **keep** <source> literal.'))
    assert.ok(text.includes('Never answer a permission prompt yourself'))
    assert.ok(text.includes('long-title'.repeat(25)))
    assert.equal(await detail.locator('source, img').count(), 0)
    assert.ok(await noOverflow())
    const wraps = await detail.evaluate(el => el.scrollWidth <= el.clientWidth)
    assert.ok(wraps)
    await page.screenshot({ path: `${output}/${width}-expanded.png` })
    await page.keyboard.press('Space')
    assert.equal(await batch.getAttribute('aria-expanded'), 'false')
    assert.equal(await detail.count(), 0)
    await single.click()
    await page.getByText(/Focused checks passed/).waitFor()
    assert.deepEqual(errors, [])
    results.push({ width, keyboard: 'Tab/Enter/Space passed', focusOutline: outline, noOverflow: true, fullLiteralDetails: true, separatePromptsVisible: true })
    await page.close()
  }
  await writeFile(`${output}/results.json`, JSON.stringify(results, null, 2) + '\n')
  console.log('3 isolated disclosure layouts passed')
} finally { await browser.close() }
