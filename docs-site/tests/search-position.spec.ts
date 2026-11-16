import { expect, test } from '@playwright/test';

test('desktop search opens above the fold and centered in the viewport after scrolling', async ({ page }, testInfo) => {
  await page.setViewportSize({ width: 1440, height: 900 });
  await page.goto('architecture/');
  await page.evaluate(() => window.scrollTo(0, 900));
  await page.getByRole('button', { name: 'Search', exact: true }).click();
  const dialog = page.getByRole('dialog');
  await expect(dialog).toBeVisible();
  const box = (await dialog.boundingBox())!;
  expect(box.x).toBeGreaterThan(0);
  expect(Math.abs(box.x + box.width / 2 - 720)).toBeLessThan(2);
  expect(box.y).toBeGreaterThan(0);
  expect(box.y).toBeLessThan(300);
  expect(box.y + box.height).toBeLessThanOrEqual(900);
  await page.screenshot({ path: testInfo.outputPath('desktop-search-position.png') });
  await page.mouse.click(box.x + box.width / 2, box.y + 2);
  await expect(dialog).toBeVisible();
  await page.mouse.click(box.x - 10, box.y + 10);
  await expect(dialog).not.toBeVisible();
  await expect(page.getByRole('button', { name: 'Search', exact: true })).toBeFocused();
});

for (const viewport of [{ width: 390, height: 844 }, { width: 844, height: 390 }]) {
  test(`search stays usable with results at ${viewport.width}×${viewport.height}`, async ({ page }, testInfo) => {
    await page.setViewportSize(viewport);
    await page.goto('architecture/');
    await page.getByRole('button', { name: 'Search', exact: true }).click();
    const dialog = page.getByRole('dialog');
    await dialog.getByRole('textbox').fill('Spoke');
    await expect(dialog.locator('.pagefind-ui__result').first()).toBeVisible();
    await expect(dialog.getByRole('checkbox')).toHaveCount(0);
    const box = (await dialog.boundingBox())!;
    expect(box.x).toBeGreaterThanOrEqual(0);
    expect(box.y).toBeGreaterThanOrEqual(0);
    expect(box.x + box.width).toBeLessThanOrEqual(viewport.width + 1);
    expect(box.y + box.height).toBeLessThanOrEqual(viewport.height + 1);
    await page.screenshot({ path: testInfo.outputPath(`search-${viewport.width}x${viewport.height}.png`) });
  });
}
