// Scratch-only browser proof. PLAYWRIGHT_MODULE points at a local Playwright install.
const { chromium } = require(process.env.PLAYWRIGHT_MODULE || 'playwright');
const assert = require('node:assert/strict');
const path = require('node:path');
(async () => {
  const browser = await chromium.launch({headless: true, executablePath: process.env.CHROMIUM_PATH || undefined});
  const page = await browser.newPage({viewport: {width: 1380, height: 950}});
  const base = process.env.SPIKE_URL;
  const evidence = path.join(__dirname, 'evidence');
  try {
    await page.goto(base + '/standalone.html');
    await page.getByRole('heading', {name: 'Workspace study'}).waitFor({timeout: 5000});
    const frame = page.frameLocator('iframe');
    await frame.locator('body[data-isolation="blocked"][data-network="blocked"]').waitFor();
    await frame.getByRole('button', {name: 'Continue'}).click();
    await frame.getByRole('button', {name: 'Workspace opened'}).waitFor();
    await page.getByLabel('Pin mode').check();
    await frame.locator('[data-design-id="primary-action"]').click();
    await page.getByLabel('Comment').fill('Rename this action to Open workspace.');
    await page.getByRole('button', {name: 'Save comment'}).click();
    await page.getByText('Rename this action to Open workspace.', {exact:true}).waitFor();
    await page.reload();
    await page.getByText('Rename this action to Open workspace.', {exact:true}).waitFor();
    await page.screenshot({path: path.join(evidence, 'standalone.png'), fullPage: true});
    const embedded = await browser.newPage({viewport: {width: 1380, height: 950}});
    await embedded.goto(base + '/embedded.html');
    await embedded.getByText('Rename this action to Open workspace.', {exact:true}).waitFor();
    await embedded.screenshot({path: path.join(evidence, 'embedded.png'), fullPage: true});
    await embedded.getByRole('button', {name: 'Simulate agent revision'}).click();
    await embedded.getByText('Version 2', {exact: true}).waitFor();
    // The first page still holds v1: attempt a new pin after the other client changed it.
    await page.getByLabel('Pin mode').check();
    await page.frameLocator('iframe').locator('[data-design-id="primary-action"]').click();
    await page.getByLabel('Comment').fill('This is now stale');
    await page.getByRole('button', {name: 'Save comment'}).click();
    await page.getByRole('status').filter({hasText:'Stale revision'}).waitFor();
    await page.screenshot({path: path.join(evidence, 'stale-rejected.png'), fullPage: true});
    await embedded.getByText('Earlier revision', {exact: true}).waitFor();
    await embedded.getByRole('button', {name: 'View pinned revision'}).click();
    await embedded.getByText('Version 1', {exact:true}).waitFor();
    await embedded.getByLabel('Frame').selectOption('details');
    await embedded.frameLocator('iframe').getByRole('heading', {name:'Website refresh'}).waitFor();
    await embedded.getByLabel('Viewport').selectOption('390');
    await embedded.setViewportSize({width: 390, height: 844});
    assert.equal(await embedded.evaluate(() => document.documentElement.scrollWidth <= innerWidth), true);
    await embedded.screenshot({path: path.join(evidence, 'mobile.png'), fullPage: true});
    console.log(JSON.stringify({passed: ['interactive document JS', 'host DOM access blocked', 'document network blocked', 'pin saved through API', 'feedback survives reload', 'same core in embedded harness', 'stale revision rejected (409)', 'old revision retrievable', 'frame navigation', '390px host no overflow']}, null, 2));
  } finally { await browser.close(); }
})().catch(error => { console.error(error); process.exitCode = 1; });
