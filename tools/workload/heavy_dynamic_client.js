// Fixed semantic contracts for the acquisition-heavy corpus. Ordinary CDP
// interactions execute author handlers; no Mimic-specific assertion API is used.
const assert = require('node:assert/strict');
const { chromium } = require('playwright-core');

const cases = {
  supabase: { url: 'https://supabase.com/docs/guides/database/overview', heading: 'Database' },
  nextjs: {
    url: 'https://nextjs.org/docs/app/getting-started/installation',
    heading: 'Installation',
  },
  gitlab: { url: 'https://gitlab.com/gitlab-org/gitlab', heading: 'GitLab' },
  shadcn: { url: 'https://ui.shadcn.com/docs/installation', heading: 'Installation' },
  discourse: { url: 'https://meta.discourse.org/latest' },
  'react-native': { url: 'https://reactnative.dev/docs/getting-started', heading: 'Introduction' },
};

async function activate(locator) {
  await locator.first().evaluate((element) => element.click());
}

(async () => {
  const name = process.argv[2];
  const selected = cases[name];
  assert(selected, 'Choose a corpus workload');
  const browser = await chromium.connectOverCDP(process.env.MIMIC_CDP_URL);
  const context = await browser.newContext();
  try {
    const page = await context.newPage();
    page.setDefaultTimeout(15000);
    page.on('pageerror', (error) => console.error(`AUTHOR ERROR: ${error.message}`));
    await page.goto(selected.url, { waitUntil: 'load', timeout: 45000 });
    if (selected.heading) {
      assert(
        (await page.locator('h1').allTextContents()).some(
          (text) => text.trim() === selected.heading,
        ),
      );
    }
    if (name === 'shadcn') {
      assert((await page.locator('body').innerText()).includes('How to install dependencies'));
      const before = await page.evaluate(() => document.documentElement.classList.contains('dark'));
      await activate(page.getByRole('button', { name: 'Toggle theme', exact: true }));
      await page.waitForFunction(
        (initial) => document.documentElement.classList.contains('dark') !== initial,
        before,
      );
      await activate(page.getByRole('button', { name: 'Toggle theme', exact: true }));
      await page.waitForFunction(
        (initial) => document.documentElement.classList.contains('dark') === initial,
        before,
      );
      const tab = page.getByRole('tab', { name: 'npm', exact: true }).first();
      await activate(tab);
      await page.waitForFunction(() =>
        Array.from(document.querySelectorAll('[role="tab"]')).some(
          (tab) => tab.textContent.trim() === 'npm' && tab.getAttribute('aria-selected') === 'true',
        ),
      );
      assert(
        (await page.locator('[role="tabpanel"][data-state="active"]').first().innerText()).includes(
          'npx shadcn@latest init',
        ),
      );
    } else if (name === 'supabase') {
      assert((await page.locator('body').innerText()).includes('Postgres'));
      const section = page.getByRole('button', {
        name: 'Connecting to your database',
        exact: true,
      });
      await activate(section);
      await page.waitForFunction(() =>
        Array.from(document.querySelectorAll('button')).some(
          (button) =>
            button.textContent.trim() === 'Connecting to your database' &&
            button.getAttribute('aria-expanded') === 'true',
        ),
      );
      await activate(section);
      await page.waitForFunction(() =>
        Array.from(document.querySelectorAll('button')).some(
          (button) =>
            button.textContent.trim() === 'Connecting to your database' &&
            button.getAttribute('aria-expanded') === 'false',
        ),
      );
    } else if (name === 'gitlab') {
      const code = page.getByRole('button', { name: 'Code', exact: true });
      await activate(code);
      await page.waitForFunction(
        () =>
          document.querySelector('button[aria-label="Code"]')?.getAttribute('aria-expanded') ===
          'true',
      );
      assert((await page.locator('body').innerText()).includes('Repository'));
      await activate(code);
      await page.waitForFunction(
        () =>
          document.querySelector('button[aria-label="Code"]')?.getAttribute('aria-expanded') ===
          'false',
      );
    } else if (name === 'nextjs') {
      assert((await page.locator('body').innerText()).includes('create-next-app'));
      await activate(page.getByRole('radio', { name: 'Switch to dark theme', exact: true }));
      await page.waitForFunction(() => document.documentElement.classList.contains('dark'));
      await activate(page.getByRole('radio', { name: 'Switch to light theme', exact: true }));
      await page.waitForFunction(() => !document.documentElement.classList.contains('dark'));
    } else if (name === 'discourse') {
      await activate(page.locator('.d-header #search-button'));
      await page.waitForFunction(() => !!document.querySelector('.search-menu input.search-term'));
      assert((await page.locator('.topic-list .topic-list-item').count()) >= 10);
    } else {
      assert((await page.locator('body').innerText()).includes('Interactive examples'));
      await activate(page.getByRole('button', { name: /Switch between dark and light mode/ }));
      await page.waitForFunction(
        () => document.documentElement.getAttribute('data-theme') === 'dark',
      );
      await activate(page.getByRole('button', { name: /Switch between dark and light mode/ }));
      await page.waitForFunction(
        () => document.documentElement.getAttribute('data-theme') === 'light',
      );
    }
    console.log(`PASS: ${name} content and required hydrated interaction`);
  } finally {
    await context.close();
    await browser.close();
  }
})().catch((error) => {
  console.error(error.stack);
  process.exitCode = 1;
});
