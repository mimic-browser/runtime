// Ordinary CDP workload. Assertions are fixed before optimization runs.
const assert = require('node:assert/strict');
const { chromium } = require('playwright-core');

const cases = {
  react: {
    url: 'https://react.dev/learn',
    heading: 'Quick Start',
    requiredText: 'Creating and nesting components',
    anchor: '#components',
  },
  mdn: {
    url: 'https://developer.mozilla.org/en-US/docs/Web/JavaScript',
    heading: 'JavaScript',
    requiredText: 'JavaScript Guide',
    anchor: '#reference',
  },
  wikipedia: {
    url: 'https://en.wikipedia.org/wiki/JavaScript',
    heading: 'JavaScript',
    requiredText: 'programming language',
  },
};

(async () => {
  const selected = cases[process.argv[2]];
  assert(selected, 'Choose react, mdn or wikipedia');
  const browser = await chromium.connectOverCDP(process.env.MIMIC_CDP_URL);
  const context = await browser.newContext();
  try {
    const page = await context.newPage();
    page.setDefaultTimeout(12000);
    await page.goto(selected.url, { waitUntil: 'load' });
    assert.match(await page.locator('h1').first().textContent(), new RegExp(selected.heading));
    assert((await page.locator('body').innerText()).includes(selected.requiredText));
    // Follow a real, same-document article link through the client interaction
    // path. Verify its target exists and that the click commits the fragment.
    if (process.argv[2] === 'react') {
      await page
        .getByRole('button', { name: 'Use Dark Mode', exact: true })
        .evaluate((button) => button.click());
      await page.waitForFunction(() => document.documentElement.classList.contains('dark'));
      await page
        .getByRole('button', { name: 'Use Light Mode', exact: true })
        .evaluate((button) => button.click());
      await page.waitForFunction(() => !document.documentElement.classList.contains('dark'));
    }
    const anchor = selected.anchor
      ? page.locator(`a[href="${selected.anchor}"]`).first()
      : page.locator('a[href^="#"]:not([href="#"])').first();
    const href = await anchor.getAttribute('href');
    assert(href && href.length > 1);
    await anchor.evaluate((link) => {
      try {
        link.click();
      } catch (error) {
        throw new Error(error.stack);
      }
    });
    await page.waitForFunction((hash) => location.hash === hash, href);
    assert(
      await page.evaluate(
        (hash) => !!document.getElementById(decodeURIComponent(hash.slice(1))),
        href,
      ),
    );
    console.log(`PASS: ${process.argv[2]} article and anchor interaction`);
  } finally {
    await context.close();
    await browser.close();
  }
})().catch((error) => {
  console.error(error.stack);
  process.exitCode = 1;
});
