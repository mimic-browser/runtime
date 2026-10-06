// Ordinary assertions, fixed before training. Official Vue TodoMVC application.
const assert = require('node:assert/strict');
const { chromium } = require('playwright-core');
(async () => {
  const browser = await chromium.connectOverCDP(process.env.MIMIC_CDP_URL);
  const context = await browser.newContext();
  try {
    const page = await context.newPage();
    page.setDefaultTimeout(8000);
    await page.goto(process.env.WORKLOAD_URL || 'https://todomvc.com/examples/vue/dist/', {
      waitUntil: 'load',
    });
    const input = page.locator('.new-todo');
    await input.fill('Buy milk');
    await input.press('Enter');
    await input.fill('Review Mimic');
    await input.press('Enter');
    assert.equal(await page.locator('.todo-list li').count(), 2);
    assert.equal(await page.locator('.todo-list li label').first().textContent(), 'Buy milk');
    await page.locator('.todo-list li .toggle').first().check();
    await page.locator('a[href="#/active"]').click();
    assert.equal(await page.locator('.todo-list li').count(), 1);
    assert.equal(await page.locator('.todo-list li label').textContent(), 'Review Mimic');
    await page.locator('a[href="#/completed"]').click();
    assert.equal(await page.locator('.todo-list li label').textContent(), 'Buy milk');
    await page.locator('.clear-completed').click();
    assert.equal(await page.locator('.todo-list li').count(), 0);
    await page.locator('.filters a[href="#/"]').click();
    assert.equal(await page.locator('.todo-list li label').textContent(), 'Review Mimic');
    console.log('PASS: create, complete, filter and clear todos');
  } finally {
    await context.close();
    await browser.close();
  }
})().catch((error) => {
  console.error(error.stack);
  process.exitCode = 1;
});
