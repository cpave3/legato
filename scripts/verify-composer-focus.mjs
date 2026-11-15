// Real NativeAgentPanel and application CSS, with fixture API/WS only.
// Start web's Vite server on a dedicated loopback port, then pass its origin.
// --capture-only records the pre-fix appearance without asserting the new cue.
import assert from 'node:assert/strict'
import { mkdir, writeFile } from 'node:fs/promises'
import { resolve } from 'node:path'
const { chromium } = await import(process.env.PLAYWRIGHT_MODULE || 'playwright')
const origin = process.argv[2]
assert.match(origin, /^http:\/\/127\.0\.0\.1:\d+$/, 'Use an isolated loopback fixture server')
const output = resolve(process.argv[3] || '/tmp/composer-focus')
await mkdir(output, { recursive: true })
const browser = await chromium.launch({ headless: true, executablePath: process.env.CHROMIUM_PATH })
const results = []
const captureOnly = process.argv.includes('--capture-only')
try {
  for (const theme of ['legato', 'dracula', 'ocular-light', 'ocular-dark']) for (const width of [1280, 390, 320]) {
    const page = await browser.newPage({ viewport: { width, height: 900 }, isMobile: width < 640, hasTouch: width < 640 })
    const errors = []
    page.on('pageerror', error => errors.push(String(error)))
    await page.route('**/*', route => new URL(route.request().url()).origin === origin ? route.continue() : route.abort())
    await page.goto(`${origin}/src/test/recovery-preview.html?composer`)
    await page.evaluate(async id => {
      const { setAppTheme } = await import('/src/lib/appTheme.ts')
      await setAppTheme(id)
    }, theme)
    const input = page.getByRole('textbox', { name: 'Message', exact: true })
    await input.waitFor()
    // Enter via Tab, rather than synthesizing a CSS pseudo-class.
    await input.click()
    await page.keyboard.press('Shift+Tab')
    await page.keyboard.press('Tab')
    assert.equal(await input.evaluate(el => el === document.activeElement && el.matches(':focus-visible')), true)
    const appearance = await input.evaluate(el => {
      const box = el.parentElement.parentElement
      const s = getComputedStyle(el), b = getComputedStyle(box)
      const canvas = document.createElement('canvas'), ctx = canvas.getContext('2d')
      const lum = color => {
        ctx.fillStyle = color; ctx.fillRect(0, 0, 1, 1)
        return [...ctx.getImageData(0, 0, 1, 1).data].slice(0, 3).map(c => {
          const n = c / 255; return n <= .04045 ? n / 12.92 : ((n + .055) / 1.055) ** 2.4
        }).reduce((sum, n, i) => sum + n * [.2126, .7152, .0722][i], 0)
      }
      const a = lum(b.outlineColor), bg = lum(b.backgroundColor)
      return { inputOutline: s.outlineStyle, inputOutlineWidth: s.outlineWidth,
        outline: b.outlineStyle, outlineWidth: b.outlineWidth, offset: b.outlineOffset,
        radius: b.borderTopLeftRadius, color: b.outlineColor, contrast: (Math.max(a, bg) + .05) / (Math.min(a, bg) + .05),
        overflow: document.documentElement.scrollWidth > innerWidth }
    })
    await page.screenshot({ path: `${output}/${theme}-${width}-keyboard.png` })
    if (!captureOnly) {
      assert.equal(appearance.inputOutline, 'none', 'No separate square textarea outline')
      assert.equal(appearance.outline, 'solid', 'Rounded composer must indicate text focus')
      assert.equal(appearance.outlineWidth, '2px')
      assert.equal(appearance.offset, '-2px', 'Focus cue stays inside the control boundary')
      assert.ok(parseFloat(appearance.radius) >= 10)
      assert.ok(appearance.contrast >= 3, `${theme} focus contrast: ${appearance.contrast}`)
      assert.ok(!appearance.overflow, 'No horizontal overflow')
    }
    await page.keyboard.type('Keyboard draft')
    assert.equal(await input.inputValue(), 'Keyboard draft')
    await page.keyboard.press('Tab')
    const buttonFocus = await page.evaluate(() => {
      const el = document.activeElement, s = getComputedStyle(el)
      return { tag: el.tagName, visible: el.matches(':focus-visible'), style: s.outlineStyle, width: parseFloat(s.outlineWidth) }
    })
    assert.equal(buttonFocus.tag, 'BUTTON')
    assert.ok(buttonFocus.visible && buttonFocus.style !== 'none' && buttonFocus.width > 0, 'Toolbar keeps its own keyboard cue')
    if (!captureOnly) assert.equal(await input.evaluate(el => getComputedStyle(el.parentElement.parentElement).outlineStyle), 'none', 'Composer cue clears when textarea loses focus')
    // Touch/pointer text entry must retain the same aligned cue.
    if (width < 640) await input.tap(); else await input.click()
    await page.screenshot({ path: `${output}/${theme}-${width}-pointer.png` })
    if (!captureOnly) assert.equal(await input.evaluate(el => getComputedStyle(el.parentElement.parentElement).outlineOffset), '-2px')
    assert.deepEqual(errors, [])
    results.push({ theme, width, appearance, buttonFocus })
    await page.close()
  }
  await writeFile(`${output}/results.json`, JSON.stringify(results, null, 2) + '\n')
  console.log(`${results.length} composer cases passed; evidence: ${output}`)
} finally { await browser.close() }
