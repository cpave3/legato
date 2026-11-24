const { chromium } = require(process.argv[2] + "/node_modules/playwright-core")
const out = process.argv[3]
;(async () => {
  const browser = await chromium.launch({ executablePath: process.env.HOME + "/.cache/ms-playwright/chromium-1247/chrome-linux64/chrome" })
  const page = await browser.newPage({ viewport: { width: 1500, height: 900 }, colorScheme: "dark" })
  const base = "http://127.0.0.1:5799/src/test/leg620-scratch-preview.html"
  await page.goto(base + "?view=tiles"); await page.waitForTimeout(1500)
  await page.screenshot({ path: out + "/tiles.png" })
  await page.getByRole("button", { name: /7 sub-agents/ }).first().click(); await page.waitForTimeout(400)
  await page.screenshot({ path: out + "/tree.png" })
  await page.goto(base + "?view=inline"); await page.waitForTimeout(1500)
  await page.getByTestId("subagent-card-call-a4").scrollIntoViewIfNeeded()
  await page.getByTestId("subagent-card-call-a4").screenshot({ path: out + "/card.png" })
  await page.goto(base + "?view=tiles&done"); await page.waitForTimeout(1500)
  await page.screenshot({ path: out + "/done.png" })
  await browser.close()
})().catch((e) => { console.error(e); process.exit(1) })
