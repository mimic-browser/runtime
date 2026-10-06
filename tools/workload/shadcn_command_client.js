// Extract the installation command for a chosen package manager from the live
// DOM, after author tab activation. This contract is DOM-based, not a rendering
// or pointer-actionability claim. It is fixed before optimization.
const assert = require('node:assert/strict');
const { chromium } = require('playwright-core');

(async () => {
  const browser = await chromium.connectOverCDP(process.env.MIMIC_CDP_URL);
  const context = await browser.newContext();
  try {
    const page = await context.newPage();
    page.setDefaultTimeout(15000);
    page.on('pageerror', (error) => console.error(`AUTHOR ERROR: ${error.message}`));
    await page.goto('https://ui.shadcn.com/docs/installation', {
      waitUntil: 'load',
      timeout: 45000,
    });
    assert.equal((await page.locator('h1').first().textContent()).trim(), 'Installation');
    const theme = page.locator('button').filter({ hasText: 'Toggle theme' }).first();
    const initial = await page.evaluate(() => document.documentElement.classList.contains('dark'));
    await theme.evaluate((button) => button.click());
    await page.waitForFunction(
      (initial) => document.documentElement.classList.contains('dark') !== initial,
      initial,
    );
    await theme.evaluate((button) => button.click());
    await page.waitForFunction(
      (initial) => document.documentElement.classList.contains('dark') === initial,
      initial,
    );
    const tab = page.locator('[role="tab"]').filter({ hasText: /^npm$/ }).first();
    await tab.evaluate((button) => button.click());
    await page.waitForFunction(() =>
      Array.from(document.querySelectorAll('[role="tab"]')).some(
        (button) =>
          button.textContent.trim() === 'npm' && button.getAttribute('aria-selected') === 'true',
      ),
    );
    const command = (
      await page.locator('[role="tabpanel"][data-state="active"]').first().textContent()
    ).trim();
    assert(
      command.includes('npx shadcn@latest init -t [framework]'),
      'Expected the selected npm command, not a loading/error placeholder',
    );
    console.log(JSON.stringify({ packageManager: 'npm', command }));
  } finally {
    await context.close();
    await browser.close();
  }
})().catch((error) => {
  console.error(error.stack);
  process.exitCode = 1;
});
