// Start web's Vite server, then run with PLAYWRIGHT_MODULE (if not installed
// locally) and CHROMIUM_PATH. Pass the isolated origin and output folder.
import assert from 'node:assert/strict'
import { mkdir, readFile, writeFile } from 'node:fs/promises'
import { resolve } from 'node:path'
const { chromium } = await import(process.env.PLAYWRIGHT_MODULE || 'playwright')
const origin = process.argv[2]
assert.match(origin, /^http:\/\/127\.0\.0\.1:\d+$/, 'Use an isolated loopback fixture server')
const output = resolve(process.argv[3] || '/tmp/recovery-evidence')
await mkdir(output, { recursive: true })
const fixture = JSON.parse(await readFile(new URL('../contracts/chimera-recovery/fixture.json', import.meta.url), 'utf8'))
const browser = await chromium.launch({ headless: true, executablePath: process.env.CHROMIUM_PATH })
const results = []
try {
  const themes = process.argv.includes('--themes') ? ['legato', 'dracula', 'ocular-light', 'ocular-dark'] : ['legato']
  for (const theme of themes) for (const width of [1280, 390, 320]) {
    const page = await browser.newPage({ viewport: { width, height: 900 } })
    const failures = []
    page.on('pageerror', error => failures.push(String(error)))
    // Nothing in this check may contact a real backend or an external host.
    await page.route('**/*', route => new URL(route.request().url()).origin === origin ? route.continue() : route.abort())
    await page.goto(`${origin}/src/test/recovery-preview.html?truncated`)
    if (theme !== 'legato') await page.evaluate(async id => {
      const { setAppTheme } = await import('/src/lib/appTheme.ts')
      await setAppTheme(id)
    }, theme)
    const toggle = page.getByRole('button', { name: 'Compaction recovery', exact: true })
    await toggle.waitFor()
    assert.equal(await toggle.getAttribute('aria-expanded'), 'false')
    assert.equal(await page.getByRole('region', { name: 'Recovery excerpt', exact: true }).count(), 0)
    await page.screenshot({ path: `${output}/${theme}-${width}-collapsed.png` })
    await toggle.focus()
    const focus = await toggle.evaluate(el => ({ color: getComputedStyle(el).outlineColor, width: parseFloat(getComputedStyle(el).outlineWidth) }))
    assert.ok(focus.width >= 2, 'Keyboard focus must have a visible outline')
    await page.keyboard.press('Enter')
    assert.equal(await toggle.getAttribute('aria-expanded'), 'true')
    const region = page.getByRole('region', { name: 'Recovery excerpt', exact: true })
    await region.getByText('Older context was truncated by Chimera.').waitFor()
    await region.getByText('I will inspect the failing test.').waitFor()
    assert.equal(await region.locator('details[open]').count(), 0)
    const records = region.getByRole('region', { name: 'Recovered records', exact: true })
    const bounds = await records.evaluate(el => ({ height: el.clientHeight, width: el.clientWidth, scrollWidth: el.scrollWidth }))
    assert.ok(bounds.height <= 540, 'Recovery records must have bounded height')
    assert.ok(bounds.scrollWidth <= bounds.width + 1, 'Recovery records overflow horizontally')
    const downloaded = page.waitForEvent('download')
    await region.getByRole('button', { name: 'Download original excerpt' }).click()
    const download = await downloaded
    assert.equal(await readFile(await download.path(), 'utf8'), fixture.truncated.summary)
    await records.evaluate(el => { el.scrollTop = 0 })
    await toggle.scrollIntoViewIfNeeded()
    await page.screenshot({ path: `${output}/${theme}-${width}-expanded.png` })
    const partial = region.locator('summary').first()
    await partial.click()
    assert.equal(await region.locator('details[open]').count(), 1)
    assert.ok(await region.locator('details[open] pre').innerText())
    const appearance = await region.evaluate((el, focus) => {
      const canvas = document.createElement('canvas')
      const ctx = canvas.getContext('2d')
      function rgb(color) { ctx.clearRect(0, 0, 1, 1); ctx.fillStyle = color; ctx.fillRect(0, 0, 1, 1); return [...ctx.getImageData(0, 0, 1, 1).data].slice(0, 3) }
      function luminance(channels) { return channels.map(c => { const n = c / 255; return n <= .04045 ? n / 12.92 : ((n + .055) / 1.055) ** 2.4 }).reduce((sum, n, i) => sum + n * [.2126, .7152, .0722][i], 0) }
      const bg = getComputedStyle(el.closest('section')).backgroundColor
      const ratio = color => { const a = luminance(rgb(color)); const b = luminance(rgb(bg)); return (Math.max(a, b) + .05) / (Math.min(a, b) + .05) }
      return { body: ratio(getComputedStyle(el).color), muted: ratio(getComputedStyle(el.querySelector('p')).color), link: ratio(getComputedStyle(el.querySelector('button')).color), focus: ratio(focus.color), overflow: document.documentElement.scrollWidth > innerWidth }
    }, focus)
    assert.ok(!appearance.overflow, 'Page overflows horizontally')
    for (const name of ['body', 'muted', 'link']) assert.ok(appearance[name] >= 4.5, `${theme} ${name} contrast ${appearance[name]}`)
    assert.ok(appearance.focus >= 3, `${theme} focus contrast ${appearance.focus}`)
    assert.deepEqual(failures, [])
    results.push({ theme, width, bounds, contrast: appearance, download: 'exact', keyboardToggle: 'passed' })
    await page.close()
  }
  // Complete (untruncated) records must also render without raw serialization.
  const page = await browser.newPage()
  await page.goto(`${origin}/src/test/recovery-preview.html`)
  await page.getByRole('button', { name: 'Compaction recovery', exact: true }).click()
  assert.ok((await page.getByRole('region', { name: 'Recovery excerpt', exact: true }).innerText()).includes('Keep the existing theme semantics.'))
  await page.close()
  await writeFile(`${output}/results.json`, JSON.stringify(results, null, 2) + '\n')
  console.log(`${results.length} rendering cases passed; screenshots and results: ${output}`)
} finally { await browser.close() }
