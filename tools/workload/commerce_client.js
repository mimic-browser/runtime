// Public Next.js commerce application: product navigation and asserted cart state.
const assert = require('node:assert/strict');
const { chromium } = require('playwright-core');

(async () => {
  const browser = await chromium.connectOverCDP(process.env.MIMIC_CDP_URL);
  const context = await browser.newContext();
  try {
    const page = await context.newPage();
    page.setDefaultTimeout(20000);
    await page.goto(process.env.WORKLOAD_URL || 'https://demo.vercel.store/', {
      waitUntil: 'load',
    });
    const products = page.locator('a[href^="/product/"]');
    assert((await products.count()) >= 3, 'The storefront must expose multiple products');
    const productURL = await products.first().getAttribute('href');
    await products.first().evaluate((link) => link.click());
    await page.waitForFunction((path) => location.pathname === path, productURL);
    const title = (await page.locator('h1').first().textContent()).trim();
    assert(title.length > 0, 'A product title is required');
    const add = page.getByRole('button', { name: /Add to cart/i });
    await add.waitFor();
    await add.evaluate((button) => button.click());
    await page.waitForFunction((title) => {
      const dialog = document.querySelector('[role="dialog"]');
      return (
        dialog &&
        dialog.textContent.includes(title) &&
        !dialog.textContent.includes('Your cart is empty')
      );
    }, title);
    const cart = page.getByRole('dialog');
    assert(
      (await cart.textContent()).includes(title),
      'The cart must contain the selected product',
    );
    console.log(`PASS: product ${productURL}, title and cart contents`);
  } finally {
    await context.close();
    await browser.close();
  }
})().catch((error) => {
  console.error(error.stack);
  process.exitCode = 1;
});
