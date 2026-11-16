import { expect, test } from '@playwright/test';

test('the version selector stays on the current channel', async ({ page }) => {
  await page.goto('architecture/');
  const select = page.getByLabel('Documentation version').first();
  await expect(select).toBeVisible();
  await expect(select.locator('option')).toHaveCount(1);
  await expect(select.locator('option')).toHaveText('Current');
  await expect(select).toHaveValue('/conduit/architecture/');
  await select.selectOption('/conduit/architecture/');
  await expect(page).toHaveURL(/\/architecture\/$/);
});
