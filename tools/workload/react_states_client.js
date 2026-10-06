// One ordinary workload, with inputs supplied by its existing environment.
// Assertions are fixed before captures or optimization are evaluated.
const assert = require('node:assert/strict');
const { chromium } = require('playwright-core');

const articles = {
  quick: {
    path: '/learn',
    heading: 'Quick Start',
    content: 'Creating and nesting components',
    anchor: '#components',
  },
  interactivity: {
    path: '/learn/adding-interactivity',
    heading: 'Adding Interactivity',
    content: 'Responding to events',
    anchor: '#responding-to-events',
  },
  state: {
    path: '/learn/managing-state',
    heading: 'Managing State',
    content: 'Choosing the state structure',
    anchor: '#choosing-the-state-structure',
  },
  escape: {
    path: '/learn/escape-hatches',
    heading: 'Escape Hatches',
    content: 'Referencing values with refs',
    anchor: '#referencing-values-with-refs',
  },
};

(async () => {
  const input = process.env.REACT_ARTICLE || 'quick';
  const article = articles[input];
  assert(article, 'Unknown React article input');
  const browser = await chromium.connectOverCDP(process.env.MIMIC_CDP_URL);
  const context = await browser.newContext();
  try {
    const page = await context.newPage();
    page.setDefaultTimeout(15000);
    await page.goto(`https://react.dev${article.path}`, { waitUntil: 'load' });
    assert.equal((await page.locator('h1').first().textContent()).trim(), article.heading);
    assert((await page.locator('article').innerText()).includes(article.content));
    await page
      .getByRole('button', { name: 'Use Dark Mode', exact: true })
      .evaluate((b) => b.click());
    await page.waitForFunction(() => document.documentElement.classList.contains('dark'));
    await page
      .getByRole('button', { name: 'Use Light Mode', exact: true })
      .evaluate((b) => b.click());
    await page.waitForFunction(() => !document.documentElement.classList.contains('dark'));
    await page
      .locator(`a[href="${article.anchor}"]`)
      .first()
      .evaluate((a) => a.click());
    await page.waitForFunction((hash) => location.hash === hash, article.anchor);
    assert(await page.evaluate((hash) => !!document.getElementById(hash.slice(1)), article.anchor));
    console.log(`PASS: React ${input}, content, theme round-trip and article navigation`);
  } finally {
    await context.close();
    await browser.close();
  }
})().catch((error) => {
  console.error(error.stack);
  process.exitCode = 1;
});
