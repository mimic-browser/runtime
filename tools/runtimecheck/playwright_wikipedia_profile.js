const assert = require('node:assert/strict');
const fs = require('node:fs');
const http = require('node:http');
const path = require('node:path');
const { chromium } = require('playwright-core');

const FIXTURE = path.join(__dirname, 'wikipedia-fixture');
const PAGE_MAP = {
  '/wiki/Main_Page': 'pages/main.html',
  '/wiki/JavaScript': 'pages/javascript.html',
  '/wiki/ECMAScript': 'pages/ecmascript.html',
};
const manifest = JSON.parse(fs.readFileSync(path.join(FIXTURE, 'manifest.json'), 'utf8'));
const outputPath = process.env.MIMIC_PROFILE_OUTPUT;
const stopAfter = process.env.MIMIC_PROFILE_STOP_AFTER || '';
const preventLinkNavigation = process.env.MIMIC_PROFILE_PREVENT_LINK_NAVIGATION === '1';
const chromeExecutable = process.env.PW_CHROME_EXECUTABLE;
const inspectBackIdentity = process.env.MIMIC_PROFILE_BACK_IDENTITY === '1';
const minimalProfile = process.env.MIMIC_PROFILE_MINIMAL === '1';
const splitBackNavigation = process.env.MIMIC_PROFILE_SPLIT_BACK === '1';
const startedAt = Date.now();

class EarlyStop extends Error {}

function contentType(headers) {
  return headers?.['content-type'] || headers?.['Content-Type'] || 'application/octet-stream';
}

async function startServer() {
  const server = http.createServer((request, response) => {
    const url = new URL(request.url, 'http://127.0.0.1');
    const pageFile = PAGE_MAP[url.pathname];
    if (!pageFile) {
      response.writeHead(404, { 'content-type': 'text/plain' });
      response.end('fixture resource not found');
      return;
    }
    response.writeHead(200, {
      'content-type': 'text/html; charset=utf-8',
      'cache-control': 'no-store',
    });
    response.end(fs.readFileSync(path.join(FIXTURE, pageFile)));
  });
  await new Promise((resolve, reject) => {
    server.once('error', reject);
    server.listen(0, '127.0.0.1', () => {
      server.off('error', reject);
      resolve();
    });
  });
  return server;
}

async function installFrozenRoutes(context) {
  await context.route(/^https?:\/\//, async (route) => {
    const request = route.request();
    if (request.method() !== 'GET') {
      await route.abort();
      return;
    }
    let url;
    try {
      url = new URL(request.url());
    } catch {
      await route.abort();
      return;
    }
    if (url.hostname === 'en.wikipedia.org' && PAGE_MAP[url.pathname]) {
      await route.fulfill({
        status: 200,
        contentType: 'text/html; charset=utf-8',
        body: fs.readFileSync(path.join(FIXTURE, PAGE_MAP[url.pathname])),
      });
      return;
    }
    if (
      url.hostname === 'en.wikipedia.org' &&
      url.pathname === '/w/index.php' &&
      url.searchParams.get('search') === 'JavaScript'
    ) {
      await route.fulfill({
        status: 302,
        headers: {
          location: 'https://en.wikipedia.org/wiki/JavaScript',
          'cache-control': 'no-store',
        },
        body: '',
      });
      return;
    }
    const entry = manifest.resources[request.url()];
    if (!entry) {
      await route.abort();
      return;
    }
    const headers = { ...entry.headers };
    delete headers['content-length'];
    delete headers['content-encoding'];
    delete headers['transfer-encoding'];
    delete headers.connection;
    await route.fulfill({
      status: entry.status,
      headers: { ...headers, 'content-type': contentType(entry.headers) },
      body: fs.readFileSync(path.join(FIXTURE, entry.file)),
    });
  });
}

function commandSummary(events) {
  const commands = events
    .filter((event) => event.name === 'commandTiming')
    .map((event) => ({
      commandId: event.data?.commandId,
      method: event.data?.method,
      totalMs: event.data?.totalMs || 0,
      workMs: event.data?.workMs || 0,
      queueMs: event.data?.queueMs || 0,
      pageWaitMs: event.data?.pageWaitMs || 0,
      sessionWaitMs: event.data?.sessionWaitMs || 0,
    }));
  const calls = events
    .filter((event) => event.name === 'callFunctionOn.end')
    .map((event) => ({
      callId: event.data?.callID,
      commandId: event.data?.commandId,
      durationMs: (event.data?.durationNs || 0) / 1e6,
    }))
    .sort((left, right) => right.durationMs - left.durationMs);
  return { commands, calls };
}

(async () => {
  let browser;
  let server;
  let page;
  let cdp;
  let diagnosticsBefore;
  let backIdentity;
  let currentStage = 'initialization';
  let outcome = 'PASS';
  const stages = [];

  async function trace() {
    if (chromeExecutable || minimalProfile) return [];
    return (await cdp.send('Mimic.getTrace')).events || [];
  }

  async function stage(name, callback) {
    currentStage = name;
    const before = await trace();
    const stageDiagnosticsBefore =
      process.env.MIMIC_PROFILE_STAGE_DIAGNOSTICS && !chromeExecutable && !minimalProfile
        ? await cdp.send('Mimic.getDiagnostics')
        : undefined;
    const stageStarted = performance.now();
    const value = await callback();
    const elapsedMs = performance.now() - stageStarted;
    const stageDiagnosticsAfter =
      process.env.MIMIC_PROFILE_STAGE_DIAGNOSTICS && !chromeExecutable && !minimalProfile
        ? await cdp.send('Mimic.getDiagnostics')
        : undefined;
    const after = await trace();
    const events = after.slice(before.length);
    const evaluation = process.env.MIMIC_PROFILE_EXPRESSION
      ? await cdp.send('Runtime.evaluate', {
          expression: process.env.MIMIC_PROFILE_EXPRESSION,
          returnByValue: true,
        })
      : undefined;
    stages.push({
      name,
      elapsedMs,
      ...commandSummary(events),
      events,
      evaluation,
      diagnosticsBefore: stageDiagnosticsBefore,
      diagnosticsAfter: stageDiagnosticsAfter,
    });
    console.log(`[STAGE] ${name}: ${elapsedMs.toFixed(2)} ms`);
    if (stopAfter === name) throw new EarlyStop(name);
    return value;
  }

  async function writeProfile() {
    if (!outputPath || !cdp) return;
    const diagnostics =
      chromeExecutable || minimalProfile
        ? null
        : await cdp.send('Mimic.getDiagnostics').catch(() => null);
    const finalTrace =
      chromeExecutable || minimalProfile
        ? null
        : await cdp.send('Mimic.getTrace').catch(() => null);
    fs.writeFileSync(
      outputPath,
      JSON.stringify(
        {
          source: 'playwright_wikipedia_profile.js',
          startedAt: new Date(startedAt).toISOString(),
          elapsedMs: Date.now() - startedAt,
          outcome,
          currentStage,
          stopAfter: stopAfter || null,
          diagnosticsBefore,
          diagnostics,
          backIdentity,
          stages,
          trace: finalTrace,
        },
        null,
        2,
      ),
    );
  }

  try {
    server = await startServer();
    browser = chromeExecutable
      ? await chromium.launch({ executablePath: chromeExecutable, headless: true })
      : await chromium.connectOverCDP(process.env.PW_MIMIC_ENDPOINT || 'http://127.0.0.1:9222');
    const context = browser.contexts()[0] ?? (await browser.newContext());
    await installFrozenRoutes(context);
    page = await context.newPage();
    cdp = await context.newCDPSession(page);
    if (!chromeExecutable && !minimalProfile) {
      await cdp.send('Mimic.startTrace');
    }
    diagnosticsBefore =
      chromeExecutable || minimalProfile
        ? null
        : await cdp.send('Mimic.getDiagnostics').catch(() => null);
    page.setDefaultTimeout(15_000);
    page.setDefaultNavigationTimeout(30_000);

    const mainResponse = await stage('main_navigation', async () => {
      const response = await page.goto('https://en.wikipedia.org/wiki/Main_Page', {
        waitUntil: 'domcontentloaded',
      });
      assert.ok(response);
      assert.equal(response.status(), 200);
      await page.locator('#firstHeading').waitFor({ state: 'attached' });
      assert.equal(await page.title(), 'Wikipedia, the free encyclopedia');
      await page.locator('#firstHeading').innerText();
      return response;
    });
    assert.equal(mainResponse.status(), 200);

    const searchInput = page.locator('input[name="search"]').first();
    await stage('search_fill', () => searchInput.fill('JavaScript'));
    assert.equal(await stage('search_input_value', () => searchInput.inputValue()), 'JavaScript');
    await stage('search_navigation', () =>
      Promise.all([page.waitForURL(/\/wiki\/JavaScript(?:$|[#?])/), searchInput.press('Enter')]),
    );
    await stage('javascript_heading_visible', () =>
      page.locator('#firstHeading').waitFor({ state: 'visible' }),
    );

    let paragraphText;
    await stage('article_inspection', async () => {
      assert.equal((await page.locator('#firstHeading').innerText()).trim(), 'JavaScript');
      const paragraph = page
        .locator('#mw-content-text p')
        .filter({ hasText: /programming language/i })
        .first();
      await paragraph.waitFor({ state: 'visible' });
      paragraphText = (await paragraph.innerText()).replace(/\s+/g, ' ').trim();
      assert.match(paragraphText, /programming language/i);
      await page.locator('body').innerText();
    });

    const link = page.getByRole('link', { name: 'ECMAScript', exact: true }).first();
    await stage('ecmascript_scroll', () => link.scrollIntoViewIfNeeded());
    assert.equal(
      (await stage('ecmascript_inner_text', () => link.innerText())).trim(),
      'ECMAScript',
    );
    const href = await stage('ecmascript_href', () => link.getAttribute('href'));
    assert.match(href, /\/wiki\/ECMAScript$/);
    if (inspectBackIdentity)
      await page.evaluate(() => {
        globalThis.__mimicBackIdentity = 'javascript-document';
      });
    if (preventLinkNavigation) {
      await page.evaluate(() => {
        document.addEventListener('click', (event) => event.preventDefault(), {
          capture: true,
          once: true,
        });
      });
      await stage('ecmascript_click_actionability', () => link.click());
      assert.match(page.url(), /\/wiki\/JavaScript(?:$|[#?])/);
      throw new EarlyStop('ecmascript_click_actionability');
    }
    await stage('ecmascript_click_navigation', () =>
      Promise.all([
        page.waitForURL(/\/wiki\/ECMAScript(?:$|[#?])/, { waitUntil: 'domcontentloaded' }),
        link.click(),
      ]),
    );
    await stage('ecmascript_heading_visible', () =>
      page.locator('#firstHeading').waitFor({ state: 'visible' }),
    );
    assert.equal(
      (
        await stage('ecmascript_heading_inner_text', () =>
          page.locator('#firstHeading').innerText(),
        )
      ).trim(),
      'ECMAScript',
    );
    assert.equal(await stage('ecmascript_title', () => page.title()), 'ECMAScript - Wikipedia');
    const finishBack = async () => {
      if (inspectBackIdentity)
        backIdentity = await page.evaluate(() => ({
          marker: globalThis.__mimicBackIdentity,
          navigationType: performance.getEntriesByType('navigation')[0]?.type,
        }));
    };
    if (splitBackNavigation) {
      await stage('back_navigation', async () => {
        await page.goBack({ waitUntil: 'domcontentloaded' });
        await page.waitForURL(/\/wiki\/JavaScript(?:$|[#?])/);
      });
      assert.equal(
        (
          await stage('back_heading_inner_text', () => page.locator('#firstHeading').innerText())
        ).trim(),
        'JavaScript',
      );
      await finishBack();
    } else {
      await stage('back_navigation', async () => {
        await page.goBack({ waitUntil: 'domcontentloaded' });
        await page.waitForURL(/\/wiki\/JavaScript(?:$|[#?])/);
        assert.equal((await page.locator('#firstHeading').innerText()).trim(), 'JavaScript');
        await finishBack();
      });
    }
  } catch (error) {
    if (error instanceof EarlyStop) {
      outcome = 'EARLY_STOP';
    } else {
      outcome = 'FAIL';
      console.error(`RESULT: FAIL at stage: ${currentStage}`);
      console.error(error?.stack || error);
      process.exitCode = 1;
    }
  } finally {
    await writeProfile().catch((error) => {
      console.error(`Unable to write profile: ${error.stack || error}`);
      process.exitCode = 1;
    });
    if (page) await page.close().catch(() => {});
    if (browser) await browser.close().catch(() => {});
    if (server) await new Promise((resolve) => server.close(resolve));
  }

  console.log(`RESULT: ${outcome} (${Date.now() - startedAt} ms)`);
})();
