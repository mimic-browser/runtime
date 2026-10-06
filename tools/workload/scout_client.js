// Research inventory only. This is not a validated optimization workload.
// Preserve its capture, then define interaction assertions before training.
const assert = require('node:assert/strict');
const { chromium } = require('playwright-core');

(async () => {
  const url = process.argv[2];
  assert(url && url.startsWith('https://'));
  const browser = await chromium.connectOverCDP(process.env.MIMIC_CDP_URL);
  const context = await browser.newContext();
  try {
    const page = await context.newPage();
    page.setDefaultTimeout(20000);
    await page.goto(url, { waitUntil: 'load', timeout: 45000 });
    console.log(
      JSON.stringify(
        await page.evaluate(() => ({
          title: document.title,
          headings: Array.from(document.querySelectorAll('h1,h2'))
            .slice(0, 25)
            .map((n) => n.textContent),
          buttons: Array.from(document.querySelectorAll('button'))
            .slice(0, 100)
            .map((n) => ({
              text: n.textContent?.trim().slice(0, 100),
              aria: n.getAttribute('aria-label'),
              title: n.getAttribute('title'),
              role: n.getAttribute('role'),
              state: n.getAttribute('aria-expanded'),
            })),
          inputs: Array.from(document.querySelectorAll('input'))
            .slice(0, 20)
            .map((n) => ({
              type: n.type,
              placeholder: n.getAttribute('placeholder'),
              aria: n.getAttribute('aria-label'),
            })),
          body: document.body?.innerText.slice(0, 2500),
        })),
        null,
        2,
      ),
    );
  } finally {
    await context.close();
    await browser.close();
  }
})().catch((error) => {
  console.error(error.stack);
  process.exitCode = 1;
});
