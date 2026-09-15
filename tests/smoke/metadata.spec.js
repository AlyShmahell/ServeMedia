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
    await expect(page.locator('#add-library-dialog select[name="type"]')).toHaveCount(0);
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

async function waitEntryScanSettled(page) {
  const panel = page.locator('#fetch-modal-body .job-progress');
  await expect(panel).toBeVisible({ timeout: 15000 });
  const footer = panel.locator('.job-footer');
  await Promise.race([
    footer.waitFor({ state: 'visible', timeout: 180000 }),
    page.waitForNavigation({ timeout: 180000, waitUntil: 'domcontentloaded' }),
    page.locator('#match-pick-dialog').waitFor({ state: 'visible', timeout: 180000 }),
  ]);
  if (await footer.count()) {
    await expect(footer).not.toContainText('Failed');
  }
}

async function pickMatchCandidate(page, yearHint) {
  const dlg = page.locator('#match-pick-dialog');
  await expect(dlg).toBeVisible({ timeout: 30000 });
  let cand = dlg.locator('button.cand');
  if (yearHint) {
    const withYear = cand.filter({ hasText: String(yearHint) });
    if (await withYear.count()) {
      cand = withYear;
    }
  }
  await cand.first().click();
}

async function entryScanRefetch(page) {
  const scanBtn = page.locator('.media-hero-poster .card-action, .show-hero-card .card-action', { hasText: 'Scan' }).first();
  await expect(scanBtn).toBeVisible({ timeout: 15000 });
  await scanBtn.click({ force: true });
  await expect(page.locator('#entry-scan-modal')).toBeVisible();
  await page.selectOption('#entry-scan-mode', 'matchmedia');
  const titleInput = page.locator('#entry-scan-title');
  await expect(titleInput).toBeVisible();
  await expect(titleInput).toHaveValue(/Film Title/i);
  await page.locator('#entry-scan-modal-form input[name="overwrite"]').check();
  await page.locator('#entry-scan-modal-form button[type="submit"]').click();
  await waitEntryScanSettled(page);
  await pickMatchCandidate(page, 2016);
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
  await expect(page.locator('.media-meta h1')).toContainText(/Film Title/i);
  await expect(page.locator('.media-meta h1')).toContainText('2016');
  await expect(page.locator('.media-meta h1')).not.toContainText(/Longer Variant/i);
  await expect(page.locator('.media-meta h1')).not.toContainText('2015');

  const poster = page.locator('.media-hero img.poster').first();
  await expect(poster).toBeVisible();
  const src = await poster.getAttribute('src');
  expect(src).toMatch(/\/metadata\//);
  expect(src).not.toMatch(/placeholder/);
  expect(src).toMatch(/[?&]m=/);
});

test('MatchMedia rescan sends an edited title', async ({ page }) => {
  await ensureAdmin(page);
  await ensureAnimeLibrary(page);

  const card = page.locator('#items a.card').filter({
    has: page.locator('.t', { hasText: /Stray NFO Film/i }),
  }).first();
  await expect(card).toBeVisible({ timeout: 30000 });
  await card.click();
  await expect(page).toHaveURL(/\/movies\/\d+/);

  const scanBtn = page.locator('.media-hero-poster .card-action', { hasText: 'Scan' }).first();
  await scanBtn.click({ force: true });
  await expect(page.locator('#entry-scan-modal')).toBeVisible();
  await page.selectOption('#entry-scan-mode', 'matchmedia');
  const titleInput = page.locator('#entry-scan-title');
  await expect(titleInput).toBeVisible();
  await expect(titleInput).toHaveValue(/Stray NFO Film/i);
  await titleInput.fill('Custom Query Title');
  await page.locator('#entry-scan-modal-form input[name="overwrite"]').check();
  await page.locator('#entry-scan-modal-form button[type="submit"]').click();
  await waitEntryScanSettled(page);

  await page.reload();
  await expect(page.locator('.media-meta h1')).toContainText('Custom Query Title');
});
