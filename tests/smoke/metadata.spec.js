const { test, expect } = require('@playwright/test');
const { ensureAdmin } = require('./helpers');

async function ensureAnimeLibrary(page) {
  await page.goto('/');
  let animeLib = page.locator('.library-card').filter({
    has: page.locator('.library-card-title', { hasText: 'Anime' }),
  }).first();
  if (!(await animeLib.count())) {
    await page.click('#add-library-open');
    await page.fill('#library-name', 'Anime');
    await page.selectOption('select[name="type"]', 'anime');
    await expect(page.locator('#library-path')).toHaveValue('/media', { timeout: 15000 });
    await page.locator('button.media-browser-dir', { hasText: 'Anime' }).click();
    await expect(page.locator('#library-path')).toHaveValue('/media/Anime', { timeout: 15000 });
    await page.locator('#add-library-dialog button[type="submit"]').click();
    await expect(page).toHaveURL(/scan=/);
    await page.goto('/');
    animeLib = page.locator('.library-card').filter({
      has: page.locator('.library-card-title', { hasText: 'Anime' }),
    }).first();
  }
  await expect(animeLib).toBeVisible({ timeout: 30000 });
  await expect(animeLib).not.toHaveClass(/is-scanning/, { timeout: 180000 });
  await animeLib.locator('.library-card-title').click();
  await expect(page).toHaveURL(/\/libraries\/\d+/);
  await expect(page.locator('#items .card').first()).toBeVisible({ timeout: 90000 });
}

async function entryScanRefetch(page) {
  const scanBtn = page.locator('.media-hero-poster .card-action, .show-hero-card .card-action', { hasText: 'Scan' }).first();
  await expect(scanBtn).toBeVisible({ timeout: 15000 });
  await scanBtn.click({ force: true });
  await expect(page.locator('#entry-scan-modal')).toBeVisible();
  await page.selectOption('#entry-scan-mode', 'refetch_all');
  await page.locator('#entry-scan-modal-form button[type="submit"]').click();
  const panel = page.locator('#fetch-modal-body .job-progress');
  await expect(panel).toBeVisible({ timeout: 15000 });
  await expect(panel.locator('.job-footer')).toContainText(/Finished|Failed/, { timeout: 180000 });
  await expect(panel.locator('.job-footer')).toContainText('Finished');
}

test.describe.configure({ mode: 'serial' });

test('Film Title entry Scan refetch matches 2016 anime', async ({ page }) => {
  await ensureAdmin(page);
  await ensureAnimeLibrary(page);

  const card = page.locator('#items a.card').filter({
    has: page.locator('.t', { hasText: /Film Title/i }),
  }).first();
  await expect(card).toBeVisible({ timeout: 30000 });
  await card.click();
  await expect(page).toHaveURL(/\/movies\/\d+/);

  await entryScanRefetch(page);

  await page.reload();
  await expect(page.locator('h1')).toContainText(/Film Title/i);
  await expect(page.locator('h1')).toContainText('2016');
  await expect(page.locator('h1')).not.toContainText(/Longer Variant/i);
  await expect(page.locator('h1')).not.toContainText('2015');

  const poster = page.locator('.media-hero img.poster').first();
  await expect(poster).toBeVisible();
  const src = await poster.getAttribute('src');
  expect(src).toMatch(/\/metadata\//);
  expect(src).not.toMatch(/placeholder/);
  expect(src).toMatch(/[?&]m=/);
});
