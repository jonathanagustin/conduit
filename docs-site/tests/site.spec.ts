import { expect, test, type APIResponse, type Page } from '@playwright/test';

const tagline = 'Self-hosted delivery control plane that takes an upstream app from source to a governed UDS package. Every step is planned, accepted, validated, and receipted.';

async function head(page: Page, path: string): Promise<APIResponse> {
  return page.request.fetch(path, { method: 'HEAD' });
}

test('home page shows the tagline, the demo, and the four guides', async ({ page }) => {
  await page.goto('');
  await expect(page.getByRole('heading', { level: 1, name: 'Conduit' })).toBeVisible();
  await expect(page.locator('.hero .tagline')).toHaveText(tagline);
  await expect(page.locator('.hero .project-wordmark')).toBeVisible();
  await expect(page.getByRole('link', { name: 'Quick start', exact: true }).first()).toHaveAttribute('href', '/conduit/getting-started/');
  await expect(page.getByRole('link', { name: 'How it works', exact: true })).toHaveAttribute('href', '/conduit/how-it-works/');
  await expect(page.getByRole('link', { name: 'Architecture', exact: true })).toHaveAttribute('href', '/conduit/architecture/');
  await expect(page.getByRole('link', { name: 'Packaging profiles', exact: true })).toHaveAttribute('href', '/conduit/packaging/');

  const packaging = page.locator('#demo-packaging img');
  await expect(packaging).toBeVisible();
  const src = await packaging.getAttribute('src');
  expect(src).toMatch(/conduit-excalidraw-demo\.webp$/);
  const image = await head(page, src!);
  expect(image.status()).toBe(200);
  expect(image.headers()['content-type'] ?? '').toContain('image/');

  const theme = page.getByLabel('Select theme').first();
  await theme.selectOption('dark');
  await expect(page.locator('html')).toHaveAttribute('data-theme', 'dark');
  await theme.selectOption('light');
  await expect(page.locator('html')).toHaveAttribute('data-theme', 'light');
});

test('an old home-page anchor opens Quick start', async ({ page }) => {
  await page.goto('#quick-start', { waitUntil: 'domcontentloaded' });
  await expect(page).toHaveURL(/\/getting-started\/#quick-start$/);
  await expect(page.getByRole('heading', { level: 2, name: 'Quick start' })).toBeVisible();
});

test('an unknown page is a 404 with a link home', async ({ page }) => {
  const response = await page.goto('this-page-does-not-exist/');
  expect(response?.status()).toBe(404);
  await expect(page.getByRole('heading', { level: 1, name: 'Page not found' })).toBeVisible();
  await expect(page.getByText('will not be included in production')).toHaveCount(0);
  const home = page.getByRole('link', { name: 'home page', exact: true });
  await expect(home).toHaveAttribute('href', '/conduit/');
  await home.click();
  await expect(page.getByRole('heading', { level: 1, name: 'Conduit' })).toBeVisible();
});

test('the favicon is the console mark', async ({ page }) => {
  await page.goto('');
  const href = await page.locator('link[rel*="icon"]').first().getAttribute('href');
  expect(href).toMatch(/favicon\.svg$/);
  const icon = await page.request.get(new URL(href!, page.url()).href);
  expect(icon.status()).toBe(200);
  expect(await icon.text()).toContain('#215db0');
});

test('architecture diagrams render the Hub and the Spoke', async ({ page }) => {
  await page.goto('architecture/', { waitUntil: 'domcontentloaded' });
  const diagrams = page.locator('pre.mermaid svg, .mermaid svg');
  await expect(diagrams.first()).toBeVisible({ timeout: 15_000 });
  await expect(diagrams.first()).toContainText('Hub');
  await expect(diagrams.first()).toContainText('Spoke');
});

test('the quick start command block shows its title', async ({ page }) => {
  await page.goto('getting-started/');
  await expect(page.locator('.expressive-code figure.has-title .title', { hasText: 'terminal' })).toBeVisible();
});

test('llms.txt lists the guides', async ({ page }) => {
  const listing = await page.request.get('/conduit/llms.txt');
  expect(listing.status()).toBe(200);
  const text = await listing.text();
  expect(text).toContain('# Conduit');
  expect(text).toContain('llms-full.txt');
  const full = await page.request.get('/conduit/llms-full.txt');
  expect(full.status()).toBe(200);
  expect(full.headers()['content-type'] ?? '').toContain('text/plain');
  const body = await full.text();
  expect(body).toContain('Quick start');
  expect(body).toContain('COMPUTE_BEFORE_ACCEPT');
});

test('starting-point tabs move by keyboard and stay on this site', async ({ page }) => {
  await page.goto('');
  const local = page.getByRole('tab', { name: 'Local demo', exact: true });
  const recorded = page.getByRole('tab', { name: 'Recorded proposals', exact: true });
  await local.focus();
  await page.keyboard.press('ArrowRight');
  await expect(recorded).toBeFocused();
  await expect(recorded).toHaveAttribute('aria-selected', 'true');
  await expect(page.getByRole('tabpanel').getByRole('link', { name: 'Quick start', exact: true }))
    .toHaveAttribute('href', /\/getting-started\/#recorded-proposals$/);
  await page.keyboard.press('ArrowLeft');
  await expect(local).toBeFocused();
  await expect(page.getByRole('tabpanel').getByRole('link', { name: 'Quick start', exact: true }))
    .toHaveAttribute('href', /\/getting-started\/#quick-start$/);
});

test('home page screenshots', async ({ page }, testInfo) => {
  await page.setViewportSize({ width: 1440, height: 900 });
  await page.goto('');
  await page.screenshot({ path: testInfo.outputPath('home-desktop-light.png') });
  await page.getByLabel('Select theme').first().selectOption('dark');
  await expect(page.locator('html')).toHaveAttribute('data-theme', 'dark');
  await page.screenshot({ path: testInfo.outputPath('home-desktop-dark.png') });
  const demo = page.locator('#demo-packaging img');
  await demo.scrollIntoViewIfNeeded();
  await expect.poll(() => demo.evaluate((img: HTMLImageElement) => img.naturalWidth)).toBeGreaterThan(1000);
  // The demo is an animated WebP. Wait until a frame has painted before capturing it.
  await page.waitForTimeout(1200);
  await page.screenshot({ path: testInfo.outputPath('home-demo-dark.png') });
  await page.setViewportSize({ width: 390, height: 844 });
  await page.screenshot({ path: testInfo.outputPath('home-mobile-dark.png'), fullPage: true });
});
