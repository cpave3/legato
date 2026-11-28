// The agent chat header on a phone (LEG-705): with everything in it at once,
// nothing in it overlaps, nothing runs off the screen, the controls and the
// request are 44 px targets and the rest 32 px; and the way back to a
// workflow run covers nothing. Real AgentChatHeader and application CSS,
// fixture API only.
// Start web's Vite server on a dedicated loopback port, then pass its origin:
//   node scripts/verify-agent-header.mjs http://127.0.0.1:<port> [out-dir] [--capture-only]
// --capture-only records screenshots without asserting.
import assert from 'node:assert/strict'
import { mkdir } from 'node:fs/promises'
import { resolve } from 'node:path'
const { chromium } = await import(process.env.PLAYWRIGHT_MODULE || 'playwright')
const origin = process.argv[2]
assert.match(origin, /^http:\/\/127\.0\.0\.1:\d+$/, 'Use an isolated loopback fixture server')
const output = resolve(process.argv[3] && !process.argv[3].startsWith('--') ? process.argv[3] : '/tmp/agent-header')
await mkdir(output, { recursive: true })
const captureOnly = process.argv.includes('--capture-only')
const browser = await chromium.launch({ headless: true, executablePath: process.env.CHROMIUM_PATH })
const failures = []
try {
  for (const fixture of ['state=needs-you', 'state=running', 'state=running&few']) for (const width of [360, 390, 430, 700, 1024, 1440]) {
    const state = fixture.replace('state=', '').replace('&', '-')
    const phone = width < 640
    const page = await browser.newPage({ viewport: { width, height: phone ? 800 : 900 }, isMobile: phone, hasTouch: phone, deviceScaleFactor: phone ? 2 : 1 })
    const errors = []
    page.on('pageerror', error => errors.push(String(error)))
    await page.route('**/*', route => new URL(route.request().url()).origin === origin ? route.continue() : route.abort())
    await page.goto(`${origin}/src/test/agent-header-preview.html?${fixture}`)
    await page.locator('header').waitFor()
    await page.evaluate(() => document.fonts.ready)
    const found = await page.evaluate(() => {
      const header = document.querySelector('header')
      const shown = el => {
        const r = el.getBoundingClientRect(), s = getComputedStyle(el)
        return r.width > 1 && r.height > 1 && s.visibility !== 'hidden' && s.display !== 'none' && !el.closest('.sr-only')
      }
      const name = el => el.getAttribute('aria-label') || el.textContent.trim().slice(0, 40) || el.tagName
      // The things a person reads or taps: each control, chip and the title.
      const items = [...header.querySelectorAll('button, a, h2, [role=group], header > div > span')].filter(shown)
        .filter(el => !el.parentElement.closest('button, a, [role=group]'))
      const rects = items.map(el => ({ el, r: el.getBoundingClientRect() }))
      const problems = []
      const h = header.getBoundingClientRect()
      if (header.scrollWidth > header.clientWidth + 1) problems.push(`header overflows: ${header.scrollWidth} > ${header.clientWidth}`)
      if (document.documentElement.scrollWidth > innerWidth) problems.push('page scrolls sideways')
      for (const { el, r } of rects) {
        if (r.left < h.left - 0.5 || r.right > h.right + 0.5 || r.right > innerWidth + 0.5) problems.push(`off the header: ${name(el)} (${Math.round(r.left)}–${Math.round(r.right)})`)
        // Clipped by a squeezed parent: drawn narrower than its own content.
        if (el.scrollWidth > el.clientWidth + 1 && getComputedStyle(el).textOverflow !== 'ellipsis' && !el.querySelector('.truncate')) problems.push(`squeezed: ${name(el)} (${Math.round(r.width)} wide, needs ${el.scrollWidth})`)
      }
      for (let i = 0; i < rects.length; i++) for (let j = i + 1; j < rects.length; j++) {
        const a = rects[i].r, b = rects[j].r
        const x = Math.min(a.right, b.right) - Math.max(a.left, b.left), y = Math.min(a.bottom, b.bottom) - Math.max(a.top, b.top)
        if (x > 0.5 && y > 0.5) problems.push(`overlap: ${name(rects[i].el)} / ${name(rects[j].el)} (${Math.round(x)}×${Math.round(y)})`)
      }
      // The controls and the request are primary: 44 px. The stage and the
      // sub-agents open what they summarise: 32 px.
      const controls = header.children[1]
      const targets = [...header.querySelectorAll('button, a')].filter(shown).map(el => {
        const r = el.getBoundingClientRect()
        const primary = controls.contains(el) || el.getAttribute('aria-label') === 'Needs you' || /^Ask /.test(el.textContent.trim())
        return { name: name(el), w: Math.round(r.width), h: Math.round(r.height), min: primary ? 44 : 32 }
      })
      // Across the page: nothing a person taps lies over anything else they
      // tap, the way back to a workflow run included.
      const taps = [...document.querySelectorAll('button, a')].filter(shown).filter(el => !el.parentElement.closest('button, a'))
      for (let i = 0; i < taps.length; i++) for (let j = i + 1; j < taps.length; j++) {
        if (header.contains(taps[i]) && header.contains(taps[j])) continue
        const a = taps[i].getBoundingClientRect(), b = taps[j].getBoundingClientRect()
        const x = Math.min(a.right, b.right) - Math.max(a.left, b.left), y = Math.min(a.bottom, b.bottom) - Math.max(a.top, b.top)
        if (x > 0.5 && y > 0.5) problems.push(`overlap: ${name(taps[i])} / ${name(taps[j])} (${Math.round(x)}×${Math.round(y)})`)
      }
      const back = document.querySelector('a[href^="/workflows/runs/"]')
      if (back && shown(back)) {
        const r = back.getBoundingClientRect()
        targets.push({ name: name(back), w: Math.round(r.width), h: Math.round(r.height), min: 44 })
      }
      return { problems, targets, height: Math.round(h.height) }
    })
    if (phone) for (const t of found.targets) if (t.w < t.min || t.h < t.min) found.problems.push(`small target: ${t.name} ${t.w}×${t.h}, under ${t.min}`)
    await page.locator('header').screenshot({ path: `${output}/${state}-${width}-header.png` })
    await page.screenshot({ path: `${output}/${state}-${width}.png` })
    console.log(`${state} ${width}px: header ${found.height}px high, ${found.problems.length ? found.problems.join('; ') : 'ok'}`)
    if (errors.length) found.problems.push(...errors)
    for (const problem of found.problems) failures.push(`${state} ${width}px: ${problem}`)
    await page.close()
  }
} finally {
  await browser.close()
}
console.log(`screenshots in ${output}`)
if (!captureOnly) assert.deepEqual(failures, [], 'The agent header is crowded')
