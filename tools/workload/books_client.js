// An ordinary Playwright extraction workload: the optimizer never edits it.
const assert = require('node:assert/strict');
const fs = require('node:fs');
const { chromium } = require('playwright-core');

(async () => {
  const browser = await chromium.connectOverCDP(process.env.MIMIC_ENDPOINT);
  const context = await browser.newContext();
  try {
    const page = await context.newPage();
    const response = await page.goto('https://books.toscrape.com/', { waitUntil: 'load' });
    assert.equal(response.status(), 200);
    const products = await page.locator('article.product_pod').evaluateAll((nodes) =>
      nodes.map((node) => ({
        title: node.querySelector('h3 a').getAttribute('title'),
        price: node.querySelector('.price_color').textContent.trim(),
        href: node.querySelector('h3 a').getAttribute('href'),
      })),
    );
    assert.equal(products.length, 20);
    assert.equal(products[0].title, 'A Light in the Attic');
    assert.equal(products[0].price, '£51.77');
    assert.ok(products.every((product) => product.title && /^£\d+\.\d{2}$/.test(product.price)));
    if (process.env.WORKLOAD_RESULT) {
      fs.writeFileSync(process.env.WORKLOAD_RESULT, JSON.stringify(products));
    }
    console.log('PASS', products.length, products[0].title);
  } finally {
    await context.close();
    await browser.close();
  }
})().catch((error) => {
  console.error(error.stack);
  process.exitCode = 1;
});
