const fs = require('node:fs');
const path = require('node:path');
const crypto = require('node:crypto');
const { chromium } = require('playwright-core');

async function main() {
  const [endpoint, output, mode = 'live'] = process.argv.slice(2);
  // Refuse to overwrite a retained capture or its provenance.
  fs.mkdirSync(output, { recursive: false });
  let browser = await chromium.connectOverCDP(endpoint);
  let context = browser.contexts()[0];
  const proxy = process.env.CLORO_MIMIC_PROXY;
  let proxyContextId;
  let proxyTargetId;
  if (proxy) {
    const session = await browser.newBrowserCDPSession();
    const created = await session.send('Mimic.createContext', {
      proxy: { server: proxy },
      disposeOnDetach: false,
    });
    proxyContextId = created.browserContextId;
    const target = await session.send('Target.createTarget', {
      url: 'about:blank',
      browserContextId: created.browserContextId,
    });
    proxyTargetId = target.targetId;
    await session.detach();
    await browser.close();
    browser = await chromium.connectOverCDP(endpoint);
    context = browser.contexts()[0];
  }
  let page;
  if (proxy) {
    // connectOverCDP presents pre-existing targets through its default client
    // context. Select by CDP target identity, preserving the server owner.
    for (const candidate of context.pages()) {
      const session = await context.newCDPSession(candidate);
      const { targetInfo } = await session.send('Target.getTargetInfo');
      await session.detach();
      if (targetInfo.targetId === proxyTargetId) page = candidate;
    }
    if (!page) throw new Error('Dedicated proxy target was not attached');
  } else page = await context.newPage();
  const cdp = await context.newCDPSession(page);
  const events = [];
  const pending = new Set();
  const bodies = [];
  for (const name of [
    'Network.requestWillBeSent',
    'Network.responseReceived',
    'Network.loadingFailed',
    'Runtime.exceptionThrown',
  ]) {
    cdp.on(name, (params) => events.push({ name, params }));
  }
  cdp.on('Network.loadingFinished', ({ requestId }) => {
    const job = cdp
      .send('Network.getResponseBody', { requestId })
      .then((body) => {
        const file = `body-${bodies.length}.json`;
        bodies.push({ requestId, file });
        fs.writeFileSync(path.join(output, file), JSON.stringify(body));
      })
      .catch((error) => bodies.push({ requestId, error: String(error) }));
    pending.add(job);
    job.finally(() => pending.delete(job));
  });
  await cdp.send('Network.enable');
  await cdp.send('Runtime.enable');
  const identity = await page.evaluate(() => ({
    webdriver: navigator.webdriver,
    descriptor: String(Object.getOwnPropertyDescriptor(Navigator.prototype, 'webdriver')?.get),
    userAgent: navigator.userAgent,
    languages: navigator.languages,
    inner: [innerWidth, innerHeight],
    outer: [outerWidth, outerHeight],
    visibility: document.visibilityState,
    focus: document.hasFocus(),
  }));
  const result = {
    mode,
    endpoint,
    timestamp: new Date().toISOString(),
    version: await browser.version(),
    identity,
    proxy: proxy || process.env.CLORO_PROXY_LABEL || null,
  };
  const url = 'https://www.google.com/search?q=why+is+the+sky+blue&hl=en&gl=us';
  result.query = mode === 'fixture' ? null : 'why is the sky blue';
  result.url = mode === 'fixture' ? process.env.CLORO_FIXTURE_URL : url;
  try {
    if (mode === 'fixture') {
      await page.goto(process.env.CLORO_FIXTURE_URL);
    } else {
      const response = await page.goto(url, { timeout: 20000, waitUntil: 'domcontentloaded' });
      result.status = response?.status();
    }
    await page.waitForSelector(
      "#m-x-content [data-container-id='main-col'], #m-x-content [data-rl]",
      { timeout: 10000, state: 'visible' },
    );
    result.layout = (await page.locator("#m-x-content [data-container-id='main-col']").count())
      ? "#m-x-content [data-container-id='main-col']"
      : '#m-x-content [data-rl]';
    const section = page.locator(result.layout).first();
    result.sectionHTML = await section.evaluate((el) => el.outerHTML);
    result.sectionText = await section.innerText();
    result.citations = [];
    for (const pill of await section.locator('[jsname="HtgYJd"]').all()) {
      await pill.dispatchEvent('click');
      await page.waitForTimeout(100);
      const links = [];
      for (const link of await page.locator('ul[jsname="Z3saHd"]').locator('a').all()) {
        if (await link.isVisible())
          links.push({
            href: await link.getAttribute('href'),
            label: await link.getAttribute('aria-label'),
          });
      }
      result.citations.push(links);
    }
    result.outcome =
      result.citations.length && result.citations.every((links) => links.length)
        ? 'interactive-citations-extracted'
        : 'aio-present-citation-flow-unverified';
  } catch (error) {
    result.error = String(error);
    result.outcome = 'blocked';
  }
  const readState = () =>
    page.evaluate(() => ({
      url: location.href,
      title: document.title,
      text: document.body?.innerText?.slice(0, 4000),
      h3: document.querySelectorAll('h3').length,
    }));
  for (let attempt = 0; attempt < 3; attempt++) {
    try {
      result.state = await readState();
      break;
    } catch (error) {
      result.terminalReadError = String(error);
      await page.waitForTimeout(500);
    }
  }
  result.state ||= { url: page.url(), text: null, title: null, h3: null };
  if (mode === 'fixture') {
    const expected = [
      [{ href: 'https://example.org/first', label: 'First reference' }],
      [{ href: 'https://example.org/second', label: 'Second reference' }],
    ];
    result.fixturePassed =
      JSON.stringify(result.citations) === JSON.stringify(expected) &&
      result.sectionText ===
        'A local fixture, not a captured Google AI Overview.\n\nFirst sources Second sources' &&
      result.sectionHTML?.includes('jsname="HtgYJd"') &&
      result.state.text ===
        'A local fixture, not a captured Google AI Overview.\n\nFirst sources Second sources\nSecond';
    if (!result.fixturePassed) process.exitCode = 1;
  }
  try {
    fs.writeFileSync(path.join(output, 'terminal.html'), await page.content());
  } catch (error) {
    result.terminalHTMLError = String(error);
  }
  await Promise.allSettled([...pending]);
  fs.writeFileSync(path.join(output, 'events.json'), JSON.stringify(events));
  fs.writeFileSync(path.join(output, 'bodies.json'), JSON.stringify(bodies));
  fs.writeFileSync(path.join(output, 'result.json'), JSON.stringify(result, null, 2));
  const inventory = fs
    .readdirSync(output)
    .filter((file) => fs.statSync(path.join(output, file)).isFile())
    .map((file) => ({
      file,
      sha256: crypto
        .createHash('sha256')
        .update(fs.readFileSync(path.join(output, file)))
        .digest('hex'),
    }));
  fs.writeFileSync(path.join(output, 'hashes.json'), JSON.stringify(inventory, null, 2));
  console.log(
    JSON.stringify(
      {
        ...result,
        sectionHTML: result.sectionHTML?.slice(0, 120),
        state: { ...result.state, text: result.state.text?.slice(0, 450) },
      },
      null,
      2,
    ),
  );
  // Bound client teardown separately from extraction and preserve its outcome.
  const cleanup = (async () => {
    if (proxy) {
      const session = await browser.newBrowserCDPSession();
      await session.send('Target.disposeBrowserContext', { browserContextId: proxyContextId });
      await session.detach();
    } else await page.close();
    await browser.close();
  })();
  let timer;
  const closed = await Promise.race([
    cleanup.then(() => true),
    new Promise((resolve) => {
      timer = setTimeout(() => resolve(false), 5000);
    }),
  ]);
  clearTimeout(timer);
  fs.writeFileSync(path.join(output, 'cleanup.json'), JSON.stringify({ closed }));
  if (!closed) {
    console.error('CDP client cleanup exceeded 5s');
    process.exit(2);
  }
}
main().catch((error) => {
  console.error(error);
  process.exitCode = 1;
});
