// A real VitePress application: author event handling and SPA route loading.
// These assertions are the workload contract, not optimizer-side observations.
const assert = require('node:assert/strict');
const { chromium } = require('playwright-core');

(async () => {
  const browser = await chromium.connectOverCDP(process.env.MIMIC_CDP_URL);
  const context = await browser.newContext();
  try {
    const page = await context.newPage();
    page.setDefaultTimeout(15000);
    await page.goto('https://vuejs.org/guide/introduction.html', { waitUntil: 'load' });
    assert.match(await page.locator('h1').textContent(), /Introduction/);
    const appearance = page.getByRole('switch', { name: 'Toggle dark mode', exact: true }).first();
    const initiallyDark = await page.evaluate(() =>
      document.documentElement.classList.contains('dark'),
    );
    await appearance.evaluate((button) => button.click());
    await page.waitForFunction(
      (old) => document.documentElement.classList.contains('dark') !== old,
      initiallyDark,
    );
    await appearance.evaluate((button) => button.click());
    await page.waitForFunction(
      (old) => document.documentElement.classList.contains('dark') === old,
      initiallyDark,
    );
    await page
      .locator('a[href="/guide/quick-start.html"]')
      .first()
      .evaluate((link) => link.click());
    await page.waitForFunction(() => location.pathname === '/guide/quick-start.html');
    await page.waitForFunction(() =>
      document.querySelector('h1')?.textContent.includes('Quick Start'),
    );
    assert((await page.locator('main').innerText()).includes('Creating a Vue Application'));
    console.log('PASS: Vue guide theme round-trip and hydrated route navigation');
  } finally {
    await context.close();
    await browser.close();
  }
})().catch((error) => {
  console.error(error.stack);
  process.exitCode = 1;
});
