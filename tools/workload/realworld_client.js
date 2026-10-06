// RealWorld's public dynamic application; assert consistency across API-fed views.
const assert = require('node:assert/strict');
const { chromium } = require('playwright-core');

(async () => {
  const browser = await chromium.connectOverCDP(process.env.MIMIC_CDP_URL);
  const context = await browser.newContext();
  try {
    const page = await context.newPage();
    page.setDefaultTimeout(20000);
    await page.goto(process.env.WORKLOAD_URL || 'https://demo.realworld.show/', {
      waitUntil: 'load',
    });
    await page.waitForFunction(() => document.querySelectorAll('.article-preview').length > 0);
    const preview = page.locator('.article-preview').first();
    const title = (await preview.locator('h1').textContent()).trim();
    const author = (await preview.locator('.author').textContent()).trim();
    assert(title.length > 0 && author.length > 0);
    const detail = page.waitForResponse((response) =>
      /\/api\/articles\/[^/?]+$/.test(response.url()),
    );
    await preview.locator('.preview-link').evaluate((link) => link.click());
    await page.waitForFunction(() => document.querySelector('.article-page h1') !== null);
    assert.equal((await page.locator('.article-page h1').textContent()).trim(), title);
    assert.equal(
      (await page.locator('.article-page .author').first().textContent()).trim(),
      author,
    );
    const article = (await (await detail).json()).article;
    assert.equal(article.title, title);
    assert.equal(article.author.username, author);
    const expectedText = article.body.replace(/\s+/g, ' ').trim();
    assert(expectedText.length >= 80, 'The article fixture must have substantive content');
    await page.waitForFunction(
      (expected) =>
        (document.querySelector('.article-content')?.innerText || '')
          .replace(/\s+/g, ' ')
          .trim()
          .includes(expected),
      expectedText.slice(0, 80),
    );
    const bodyText = (await page.locator('.article-content').innerText())
      .replace(/\s+/g, ' ')
      .trim();
    assert(
      bodyText.includes(expectedText.slice(0, 80)),
      'Article text must contain the API content, not a loading/error placeholder',
    );
    await page.locator('a.navbar-brand').evaluate((link) => link.click());
    await page.waitForFunction(() => document.querySelectorAll('.article-preview').length > 0);
    assert.equal((await page.locator('.article-preview h1').first().textContent()).trim(), title);
    console.log('PASS: live feed, article/author/content consistency and return navigation');
  } finally {
    await context.close();
    await browser.close();
  }
})().catch((error) => {
  console.error(error.stack);
  process.exitCode = 1;
});
