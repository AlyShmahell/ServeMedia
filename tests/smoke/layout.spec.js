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

test('top bar primary nav and home sections', async ({ page }) => {
  await ensureAdmin(page);
  await page.setViewportSize({ width: 1280, height: 800 });
  await page.goto('/');

  const topbar = page.locator('.topbar');
  await expect(topbar).toBeVisible();
  await expect(topbar.locator('.topbar-title')).toHaveText('Home');
  await expect(topbar.locator('.topbar-back')).toHaveCount(0);
  await expect(topbar.getByRole('link', { name: 'Settings' })).toBeVisible();
  await expect(topbar.getByRole('link', { name: 'About' })).toBeVisible();
  await expect(topbar.locator('.topbar-logo')).toHaveAttribute('href', '/');
  await expect(topbar.locator('.topbar-logo')).toHaveAttribute('title', 'Return to Home Page');
  await expect(topbar.locator('.topbar-logo-name')).toHaveText('ServeMedia');
  await expect(topbar.locator('.topbar-signout')).toContainText('admin');
  await expect(page.getByRole('link', { name: 'Home', exact: true })).toHaveCount(0);
  await expect(page.locator('.sidebar')).toHaveCount(0);
  await expect(page.locator('.nav-libraries')).toHaveCount(0);

  await expect(page.locator('.home-section')).toHaveCount(3);
  await expect(page.locator('.home-split')).toBeVisible();
  await expect(page.locator('.home-section-continue')).toBeVisible();
  await expect(page.locator('.home-section-recent')).toBeVisible();
  await expect(page.locator('.home-section-libraries')).toBeVisible();
});

function isSpatialCandidate() {
  const el = document.activeElement;
  return !!(el && el.matches && el.matches('a:not(.card-action), button:not(.card-action), input, select, textarea'));
}

test('arrow keys move spatial focus on home', async ({ page }) => {
  await ensureAdmin(page);
  await page.setViewportSize({ width: 1280, height: 800 });
  await page.goto('/');
  await expect(page.locator('.topbar')).toBeVisible();
  await expect.poll(async () => page.evaluate(() => document.body.classList.contains('spatial-nav'))).toBe(false);
  const before = await page.evaluateHandle(() => document.activeElement);
  await page.keyboard.press('ArrowRight');
  await expect.poll(async () => page.evaluate(isSpatialCandidate)).toBe(true);
  await expect.poll(async () => page.evaluate(() => document.body.classList.contains('spatial-nav'))).toBe(true);
  const moved = await page.evaluate((el) => document.activeElement !== el, before);
  expect(moved).toBe(true);
});

test('gamepad hat axis moves spatial focus on home', async ({ page }) => {
  await ensureAdmin(page);
  await page.setViewportSize({ width: 1280, height: 800 });
  await page.goto('/');
  await expect(page.locator('.topbar')).toBeVisible();
  await page.evaluate(() => {
    window.__padCalls = 0;
    window.__padFirst = document.activeElement;
    Object.defineProperty(navigator, 'getGamepads', {
      configurable: true,
      value: function () {
        window.__padCalls += 1;
        const axes = window.__padCalls < 3
          ? [0, 0, 0, 0, 0, 0, 0, 0]
          : [0, 0, 0, 0, 0, 0, 1, 0];
        return [{
          id: '8BitDo',
          index: 0,
          connected: true,
          mapping: '',
          buttons: Array.from({ length: 16 }, () => ({ pressed: false, value: 0 })),
          axes,
        }];
      },
    });
  });
  await expect.poll(async () => page.evaluate(() => document.activeElement !== window.__padFirst), {
    timeout: 3000,
  }).toBe(true);
  await expect.poll(async () => page.evaluate(() => document.body.classList.contains('spatial-nav'))).toBe(true);
});

test('idle gamepad hat axis 0 does not move focus', async ({ page }) => {
  await ensureAdmin(page);
  await page.setViewportSize({ width: 1280, height: 800 });
  await page.goto('/');
  await expect(page.locator('.topbar')).toBeVisible();
  await page.evaluate(() => {
    window.__padFirst = document.activeElement;
    window.__padPolls = 0;
    Object.defineProperty(navigator, 'getGamepads', {
      configurable: true,
      value: function () {
        window.__padPolls += 1;
        return [{
          id: 'idle',
          index: 0,
          connected: true,
          mapping: '',
          buttons: Array.from({ length: 16 }, () => ({ pressed: false, value: 0 })),
          axes: [0, 0, 0, 0, 0, 0, 0, 0, 0, 0],
        }];
      },
    });
  });
  await expect.poll(async () => page.evaluate(() => window.__padPolls > 10), { timeout: 3000 }).toBe(true);
  expect(await page.evaluate(() => document.activeElement === window.__padFirst)).toBe(true);
  expect(await page.evaluate(() => document.body.classList.contains('spatial-nav'))).toBe(false);
});

test('gamepad A clicks focused control', async ({ page }) => {
  await ensureAdmin(page);
  await page.setViewportSize({ width: 1280, height: 800 });
  await page.goto('/');
  await expect(page.locator('.topbar')).toBeVisible();
  await page.keyboard.press('ArrowRight');
  await expect.poll(async () => page.evaluate(isSpatialCandidate)).toBe(true);
  await page.evaluate(() => {
    const el = document.activeElement;
    window.__padClicks = 0;
    window.__padCalls = 0;
    if (el && typeof el.click === 'function') {
      el.addEventListener('click', (e) => {
        e.preventDefault();
        e.stopPropagation();
        window.__padClicks += 1;
      }, true);
    }
    Object.defineProperty(navigator, 'getGamepads', {
      configurable: true,
      value: function () {
        window.__padCalls += 1;
        const pressed = window.__padCalls >= 3;
        return [{
          id: '8BitDo',
          index: 0,
          connected: true,
          mapping: '',
          buttons: Array.from({ length: 16 }, (_, i) => ({
            pressed: pressed && i === 0,
            value: pressed && i === 0 ? 1 : 0,
          })),
          axes: [0, 0, 0, 0, 0, 0, 0, 0],
        }];
      },
    });
  });
  await expect.poll(async () => page.evaluate(() => window.__padClicks > 0), {
    timeout: 3000,
  }).toBe(true);
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

test('add library dialog focuses a folder not Close', async ({ page }) => {
  await ensureAdmin(page);
  await page.goto('/');
  await page.click('#add-library-open');
  await expect(page.locator('#add-library-dialog')).toBeVisible();
  await expect(page.locator('#add-library-dialog .media-browser-dir').first()).toBeVisible({
    timeout: 15000,
  });
  await expect.poll(async () => page.evaluate(() => document.body.classList.contains('spatial-nav'))).toBe(false);
  await page.keyboard.press('ArrowDown');
  await expect.poll(async () => page.evaluate(() => {
    const el = document.activeElement;
    return !!(el && el.classList && el.classList.contains('media-browser-dir'));
  }), {
    timeout: 5000,
  }).toBe(true);
  await expect.poll(async () => page.evaluate(() => document.body.classList.contains('spatial-nav'))).toBe(true);
});

test('add library folder swap keeps focus on a folder', async ({ page }) => {
  await ensureAdmin(page);
  await page.goto('/');
  await page.click('#add-library-open');
  await expect(page.locator('#add-library-dialog .media-browser-dir').first()).toBeVisible({
    timeout: 15000,
  });
  const before = await page.locator('#library-path').inputValue();
  const tv = page.locator('#add-library-dialog .media-browser-dir', { hasText: 'TV' });
  if (await tv.count()) {
    await tv.click();
  } else {
    await page.locator('#add-library-dialog .media-browser-dir').first().click();
  }
  await expect.poll(async () => page.locator('#library-path').inputValue(), {
    timeout: 15000,
  }).not.toBe(before);
  await expect.poll(async () => page.evaluate(() => {
    const el = document.activeElement;
    if (el && el.id === 'add-library-close') return false;
    if (el && el.getAttribute && el.getAttribute('aria-label') === 'Close') return false;
    return !document.body.classList.contains('spatial-nav');
  }), {
    timeout: 5000,
  }).toBe(true);
});

test('inner pages focus Back', async ({ page }) => {
  await ensureAdmin(page);
  await ensureTVLibrary(page);
  await page.goto('/');
  await page.locator('.library-card-title', { hasText: 'TV' }).first().click();
  await expect(page).toHaveURL(/\/libraries\/\d+/);
  await expect(page.locator('.topbar-back')).toBeVisible();
  await expect.poll(async () => page.evaluate(() => {
    const el = document.activeElement;
    return !(el && el.classList && el.classList.contains('topbar-back'));
  }), {
    timeout: 5000,
  }).toBe(true);
  await page.keyboard.press('ArrowLeft');
  await expect.poll(async () => page.evaluate(() => {
    const el = document.activeElement;
    return !!(el && el.classList && el.classList.contains('topbar-back'));
  }), {
    timeout: 5000,
  }).toBe(true);
  await expect.poll(async () => page.evaluate(() => document.body.classList.contains('spatial-nav'))).toBe(true);
});

test('About shows version and license only', async ({ page }) => {
  await ensureAdmin(page);
  await page.goto('/about');
  await expect(page.locator('.topbar-title')).toHaveText('About');
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
  await expect(page.locator('.topbar-back[href^="/libraries/"]')).toBeVisible();
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
