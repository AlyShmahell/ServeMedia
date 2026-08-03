const { test, expect } = require('@playwright/test');

async function ensureAdmin(page) {
  await page.goto('/');
  const url = page.url();
  if (url.includes('/register')) {
    await page.fill('input[name="username"]', 'admin');
    await page.fill('input[name="password"]', 'adminpass');
    await page.fill('input[name="confirm"]', 'adminpass');
    await page.click('button[type="submit"]');
    await expect(page).toHaveURL(/\/$/);
    return;
  }
  if (url.includes('/login')) {
    await page.fill('input[name="username"]', 'admin');
    await page.fill('input[name="password"]', 'adminpass');
    await page.click('button[type="submit"]');
    await expect(page).toHaveURL(/\/$/);
  }
}

async function ensureMovieLibrary(page) {
  await page.goto('/');
  const movieCard = page.locator('a[href^="/movies/"]').first();
  if (await movieCard.count()) {
    return;
  }
  // Prefer an existing Movies library (TV may also be present from layout smoke).
  const moviesLib = page.locator('.library-card-title', { hasText: 'Movies' }).first();
  if (await moviesLib.count()) {
    await moviesLib.click();
    await expect(page.locator('a[href^="/movies/"]').first()).toBeVisible({ timeout: 90000 });
    return;
  }
  await page.click('#add-library-open');
  await page.fill('#library-name', 'Movies');
  await page.selectOption('select[name="type"]', 'movies');
  await expect(page.locator('#library-path')).toHaveValue('/media', { timeout: 15000 });
  await page.locator('button.media-browser-dir', { hasText: 'Movies' }).click();
  await expect(page.locator('#library-path')).toHaveValue('/media/Movies', { timeout: 15000 });
  await page.locator('#add-library-dialog button[type="submit"]').click();
  await expect(page).toHaveURL(/scan=/);
  await page.goto('/');
  await expect(page.locator('a[href^="/movies/"], .library-card').first()).toBeVisible({
    timeout: 90000,
  });
  if (await page.locator('a[href^="/movies/"]').count()) {
    return;
  }
  await page.locator('.library-card-title', { hasText: 'Movies' }).first().click();
  await expect(page.locator('a[href^="/movies/"]').first()).toBeVisible({ timeout: 90000 });
}

test.describe.configure({ mode: 'serial' });

test('stage fullscreen via custom control bar', async ({ page }) => {
  await ensureAdmin(page);
  await ensureMovieLibrary(page);

  await page.goto('/');
  if (!(await page.locator('a[href^="/movies/"]').count())) {
    await page.locator('.library-card-title', { hasText: 'Movies' }).first().click();
  }
  await page.locator('a[href^="/movies/"]').first().click();
  await page.locator('a[href^="/play/movie/"]').first().click();
  await expect(page.locator('#stage')).toBeVisible();
  await expect(page.locator('#controls')).toBeAttached();
  await expect(page.locator('#fs-btn')).toBeAttached();

  await page.locator('#stage').hover();
  await page.locator('#controls').evaluate((el) => el.classList.add('is-visible'));
  await page.locator('#fs-btn').click();

  await expect
    .poll(async () => page.evaluate(() => document.fullscreenElement && document.fullscreenElement.id), {
      timeout: 10000,
    })
    .toBe('stage');

  await expect(page.locator('#stage #controls')).toBeAttached();
  await expect(page.locator('#stage #fs-btn')).toBeAttached();
  await expect(page.locator('#stage #quality-sel')).toBeAttached();
});
