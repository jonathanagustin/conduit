import { expect, test } from '@playwright/test';

test('sidebar groups keep the active section open', async ({ page }) => {
  await page.goto('architecture/');
  const sidebar = page.locator('.sidebar-content');
  const labels = await sidebar.locator('ul.top-level > li > details > summary').allTextContents();
  expect(labels.map((label) => label.trim())).toEqual([
    'Getting started', 'Concepts', 'Reference', 'Project',
  ]);
  const concepts = sidebar.locator('details').filter({
    has: page.locator('summary').filter({ hasText: /^Concepts$/ }),
  });
  await expect.poll(() => concepts.evaluate((element) => element.hasAttribute('open'))).toBe(true);
  await expect(concepts.getByRole('link', { name: 'Architecture', exact: true })).toBeVisible();
  const project = sidebar.locator('details').filter({
    has: page.locator('summary').filter({ hasText: /^Project$/ }),
  });
  await expect.poll(() => project.evaluate((element) => element.hasAttribute('open'))).toBe(false);
  await project.locator('summary').first().click();
  await project.getByRole('link', { name: 'Roadmap', exact: true }).click();
  await expect(page).toHaveURL(/\/roadmap\/$/);
  await expect.poll(() => project.evaluate((element) => element.hasAttribute('open'))).toBe(true);
});

test('quick start expands Getting started', async ({ page }) => {
  await page.goto('getting-started/');
  const sidebar = page.locator('.sidebar-content');
  const gettingStarted = sidebar.locator('details').filter({
    has: page.locator('summary').filter({ hasText: /^Getting started$/ }),
  });
  await expect.poll(() => gettingStarted.evaluate((element) => element.hasAttribute('open'))).toBe(true);
  await expect(gettingStarted.getByRole('link', { name: 'Quick start', exact: true })).toHaveAttribute('aria-current', 'page');
});

test('search opens from the keyboard shortcut and Escape returns focus', async ({ page }) => {
  await page.goto('architecture/');
  const search = page.getByRole('button', { name: 'Search', exact: true });
  await search.focus();
  await page.keyboard.press('Control+k');
  const dialog = page.getByRole('dialog', { name: /Search Current/ });
  await expect(dialog).toBeVisible();
  await expect(dialog.getByRole('textbox')).toBeFocused();
  await page.keyboard.press('Escape');
  await expect(dialog).not.toBeVisible();
  await expect(search).toBeFocused();
});

for (const width of [320, 390]) {
  test(`navigation, search, and version selection fit a ${width}px screen`, async ({ page }, testInfo) => {
    await page.setViewportSize({ width, height: 844 });
    await page.goto('architecture/');
    await expect(page.getByRole('button', { name: 'Search', exact: true })).toBeVisible();
    expect(await page.evaluate(() => document.documentElement.scrollWidth)).toBeLessThanOrEqual(width);
    await page.getByRole('button', { name: 'Menu', exact: true }).click();
    await expect(page.getByLabel('Documentation version').filter({ visible: true })).toBeVisible();
    await expect(page.locator('.sidebar-content').getByRole('link', { name: 'Architecture', exact: true })).toBeVisible();
    expect(await page.evaluate(() => document.documentElement.scrollWidth)).toBeLessThanOrEqual(width);
    await page.screenshot({ path: testInfo.outputPath(`mobile-navigation-${width}.png`) });
  });
}
