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

async function ensureTVLibrary(page) {
  await page.goto('/');
  const showLink = page.getByRole('link', { name: /Sample Show/i }).first();
  if (await showLink.count()) {
    return;
  }
  const tvLib = page.locator('.library-card').filter({ has: page.locator('.library-card-title', { hasText: 'TV' }) }).first();
  if (await tvLib.count()) {
    await expect(tvLib).not.toHaveClass(/is-scanning/, { timeout: 180000 });
    await tvLib.locator('.library-card-title').click();
    await expect(page.getByRole('link', { name: /Sample Show/i })).toBeVisible({ timeout: 90000 });
    return;
  }
  await page.click('#add-library-open');
  await page.fill('#library-name', 'TV');
  await expect(page.locator('#add-library-dialog select[name="type"]')).toHaveCount(0);
  await expect(page.locator('#library-path')).toHaveValue('/media', { timeout: 15000 });
  await page.locator('button.media-browser-dir', { hasText: 'TV' }).click();
  await expect(page.locator('#library-path')).toHaveValue('/media/TV', { timeout: 15000 });
  await page.locator('#add-library-dialog button[type="submit"]').click();
  await expect(page).toHaveURL(/scan=/);
  await page.goto('/');
  const created = page.locator('.library-card').filter({
    has: page.locator('.library-card-title', { hasText: 'TV' }),
  }).first();
  await expect(created).toBeVisible({ timeout: 30000 });
  await expect(created).not.toHaveClass(/is-scanning/, { timeout: 180000 });
  if (await page.getByRole('link', { name: /Sample Show/i }).count()) {
    return;
  }
  await created.locator('.library-card-title').click();
  await expect(page.getByRole('link', { name: /Sample Show/i })).toBeVisible({ timeout: 90000 });
}

test.describe.configure({ mode: 'serial' });

test('sidebar primary nav and home sections', async ({ page }) => {
  await ensureAdmin(page);
  await page.setViewportSize({ width: 1280, height: 800 });
  await page.goto('/');

  const sidebar = page.locator('.sidebar');
  await expect(sidebar.getByRole('link', { name: 'Home' }).first()).toBeVisible();
  await expect(sidebar.getByRole('link', { name: 'Settings' })).toBeVisible();
  await expect(sidebar.getByRole('link', { name: 'About' })).toBeVisible();
  await expect(page.locator('.nav-libraries-card')).toBeVisible();
  await expect(page.locator('.nav-libraries')).toBeVisible();

  await expect(page.locator('.home-section')).toHaveCount(3);
  await expect(page.locator('.home-split')).toBeVisible();
  await expect(page.locator('.home-section-continue')).toBeVisible();
  await expect(page.locator('.home-section-recent')).toBeVisible();
  await expect(page.locator('.home-section-libraries')).toBeVisible();
});

test('add library dialog has no type field', async ({ page }) => {
  await ensureAdmin(page);
  await page.goto('/');
  await page.click('#add-library-open');
  await expect(page.locator('#add-library-dialog')).toBeVisible();
  await expect(page.locator('#add-library-dialog select[name="type"]')).toHaveCount(0);
  await expect(page.locator('#library-name')).toBeVisible();
  await expect(page.locator('#library-path')).toHaveCount(1);
  await expect(page.locator('#library-path-display')).toBeVisible();
});

test('About shows version and license only', async ({ page }) => {
  await ensureAdmin(page);
  await page.goto('/about');
  await expect(page.locator('h1')).toHaveText('About');
  await expect(page.locator('.about-tile h2', { hasText: 'Version' })).toBeVisible();
  await expect(page.locator('.about-tile h2', { hasText: 'License' })).toBeVisible();
  await expect(page.locator('.about-tile')).toHaveCount(2);
  await expect(page.getByRole('heading', { name: 'Author' })).toHaveCount(0);
  await expect(page.getByRole('heading', { name: 'Copyright' })).toHaveCount(0);
  await expect(page.locator('.about-tile p').first()).toContainText(/\d+\.\d+\.\d+/);
});

test('library cards fit libraries section height', async ({ page }) => {
  await ensureAdmin(page);
  await ensureTVLibrary(page);
  await page.setViewportSize({ width: 1280, height: 800 });
  await page.goto('/');
  await expect(page.locator('.home-section-libraries .library-card').first()).toBeVisible({
    timeout: 30000,
  });

  const metrics = await page.evaluate(() => {
    const section = document.querySelector('.home-section-libraries');
    const card = section && section.querySelector('.library-card');
    if (!section || !card) {
      return { fullyVisible: false, slackBelow: -1 };
    }
    const s = section.getBoundingClientRect();
    const c = card.getBoundingClientRect();
    return {
      fullyVisible:
        c.top >= s.top - 2 &&
        c.bottom <= s.bottom + 2 &&
        c.left >= s.left - 2 &&
        c.right <= s.right + 2,
      slackBelow: s.bottom - c.bottom,
    };
  });
  expect(metrics.fullyVisible).toBe(true);
  expect(metrics.slackBelow).toBeGreaterThanOrEqual(8);
  expect(metrics.slackBelow).toBeLessThanOrEqual(64);
});

test('TV show season and episode cards', async ({ page }) => {
  await ensureAdmin(page);
  await ensureTVLibrary(page);

  await page.goto('/');
  let show = page.getByRole('link', { name: /Sample Show/i }).first();
  if (!(await show.count())) {
    await page.locator('.library-card-title', { hasText: 'TV' }).first().click();
    show = page.getByRole('link', { name: /Sample Show/i }).first();
  }
  await show.click();
  await expect(page).toHaveURL(/\/shows\/\d+$/);
  await expect(page.locator('.back-link a[href^="/libraries/"]')).toBeVisible();
  await expect(page.locator('.season-card .poster-progress').first()).toBeVisible();

  const seasonCard = page.locator('.season-card').first();
  await expect(seasonCard).toBeVisible({ timeout: 30000 });
  await expect(seasonCard).toContainText(/1 episode/);
  await expect(seasonCard).toContainText('Season one synopsis for smoke layout tests.');
  await expect(seasonCard.locator('.card-action')).toHaveCount(0);

  await seasonCard.click();
  await expect(page).toHaveURL(/\/seasons\/\d+/);
  await expect(page.locator('.season-main-card')).toBeVisible();
  await expect(page.locator('.season-sticky-head')).toBeVisible();
  await expect(page.locator('.card-action')).toHaveCount(0);

  const episodeCard = page.locator('.episode-card').first();
  await expect(episodeCard).toBeVisible();
  await expect(episodeCard).toContainText('Pilot');
  await expect(episodeCard).toContainText('Episode plot for the sample pilot.');
  await expect(episodeCard.locator('.poster-progress')).toBeVisible();
  await expect(episodeCard.locator('.card-action')).toHaveCount(0);
  const play = episodeCard.locator('a.episode-still').first();
  await expect(play).toHaveAttribute('href', /\/play\/episode\/\d+/);
  await expect(episodeCard.locator('.play-overlay')).toBeVisible();
  await expect(page.getByRole('button', { name: 'Play' })).toHaveCount(0);
});

test('library main card scrolls items', async ({ page }) => {
  await ensureAdmin(page);
  await ensureTVLibrary(page);
  await page.goto('/');
  await page.locator('.library-card-title', { hasText: 'TV' }).first().click();
  await expect(page).toHaveURL(/\/libraries\/\d+/);
  await expect(page.locator('.library-main-card')).toBeVisible();
  await expect(page.locator('#items .poster-progress').first()).toBeVisible();
});
