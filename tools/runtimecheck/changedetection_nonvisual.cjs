// Representative opt-in nonvisual fetch, not the stock screenshot fetcher.
const fs = require('node:fs');
const path = require('node:path');
const { performance } = require('node:perf_hooks');
const { chromium } = require('playwright-core');
const readline = require('node:readline');

async function main() {
  const [endpoint, fixture, output] = process.argv.slice(2);
  const connectStart = performance.now();
  const browser = await chromium.connectOverCDP(endpoint);
  const context = browser.contexts()[0];
  const initialPageCount = context.pages().length;
  const acknowledgements = readline.createInterface({ input: process.stdin });
  console.log(JSON.stringify({ type: 'connected', ms: performance.now() - connectStart }));
  const sequence = [
    [0, 0],
    [0, 1],
    [1, 2],
    [2, 3],
    [2, 4],
    [3, 5],
  ];
  for (const [index, [revision, noise]] of sequence.entries()) {
    const started = performance.now();
    const page = await context.newPage();
    await page.setViewportSize({ width: 1280, height: 720 });
    const errors = [];
    page.on('pageerror', (error) => errors.push(String(error)));
    const session = await context.newCDPSession(page);
    let requests = 0,
      encodedBytes = 0;
    await session.send('Network.enable');
    session.on('Network.requestWillBeSent', () => requests++);
    session.on('Network.loadingFinished', (event) => {
      encodedBytes += event.encodedDataLength;
    });
    const response = await page.goto(`${fixture}/watch?revision=${revision}&noise=${noise}`, {
      waitUntil: 'load',
      timeout: 15000,
    });
    // Both backends get the same explicit readiness condition. Stock default
    // 5/12-second waits and screenshots are intentionally outside this opt-in.
    await page.waitForSelector('#product[data-ready="true"]', { timeout: 5000 });
    const html = await page.content();
    const productText = await page.locator('#product').innerText();
    const identity = await page.evaluate(() => ({
      webdriver: navigator.webdriver,
      userAgent: navigator.userAgent,
      viewport: [innerWidth, innerHeight],
      outer: [outerWidth, outerHeight],
    }));
    fs.writeFileSync(path.join(output, `snapshot-${index}.html`), html);
    const extractMs = performance.now() - started;
    await session.detach();
    await page.close();
    const acknowledged = new Promise((resolve) => acknowledgements.once('line', resolve));
    console.log(
      JSON.stringify({
        type: 'snapshot',
        index,
        revision,
        noise,
        extractMs,
        totalMs: performance.now() - started,
        status: response?.status(),
        htmlBytes: Buffer.byteLength(html),
        requests,
        encodedBytes,
        errors,
        productText,
        identity,
      }),
    );
    await acknowledged;
  }
  console.log(
    JSON.stringify({
      type: 'teardown',
      initialPageCount,
      remainingPageCount: context.pages().length,
    }),
  );
  acknowledgements.close();
  await browser.close();
}
main().catch((error) => {
  console.error(error);
  process.exit(1);
});
