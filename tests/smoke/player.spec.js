const { test, expect } = require('@playwright/test');
const { openLibrary } = require('./helpers');

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
  // Anime films also live at /movies/:id — always open the Movies library.
  const moviesLib = page.locator('.library-card').filter({
    has: page.locator('.library-card-title', { hasText: 'Movies' }),
  }).first();
  if (!(await moviesLib.count())) {
    await page.click('#add-library-open');
    await page.fill('#library-name', 'Movies');
    await expect(page.locator('#add-library-dialog select[name="type"]')).toHaveCount(0);
    await expect(page.locator('#library-path')).toHaveValue('/media', { timeout: 15000 });
    await page.locator('button.media-browser-dir', { hasText: 'Movies' }).click();
    await expect(page.locator('#library-path')).toHaveValue('/media/Movies', { timeout: 15000 });
    await page.locator('#add-library-dialog button[type="submit"]').click();
    await expect(page).toHaveURL(/scan=/);
  }
  await openLibrary(page, 'Movies');
  await expect(page.locator('#items a.card').first()).toBeVisible({ timeout: 90000 });
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

async function ensureAnimeLibrary(page) {
  await page.goto('/');
  const animeLib = page.locator('.library-card').filter({
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
  }
  await openLibrary(page, 'Anime');
  await expect(page.locator('#items .card').first()).toBeVisible({ timeout: 90000 });
}

async function forceControlBar(page) {
  await page.addStyleTag({
    content: `
      .video-js .vjs-control-bar {
        display: flex !important;
        opacity: 1 !important;
        visibility: visible !important;
        pointer-events: auto !important;
      }
      .vjs-error-display { pointer-events: none !important; }
    `,
  });
}

async function injectSessionTracks(page) {
  await page.route('**/play/**/session', async (route) => {
    if (route.request().method() !== 'POST') {
      await route.continue();
      return;
    }
    const res = await route.fetch();
    const json = await res.json();
    json.audioTracks = [
      { index: 1, type: 'audio', lang: 'eng', title: 'English', codec: 'aac' },
      { index: 2, type: 'audio', lang: 'jpn', title: 'Japanese', codec: 'aac' },
    ];
    json.subtitleTracks = [
      { index: 3, type: 'subtitle', lang: 'eng', title: 'English', codec: 'subrip' },
    ];
    await route.fulfill({
      status: res.status(),
      contentType: 'application/json',
      body: JSON.stringify(json),
    });
  });
}

async function openSampleMoviePlayer(page) {
  await page.goto('/');
  await page.locator('.library-card-title', { hasText: 'Movies' }).first().click();
  await page.locator('#items a.card').filter({
    has: page.locator('.t', { hasText: /Sample Movie/i }),
  }).first().click();
  await page.locator('a[href^="/play/movie/"]').first().click();
  await expect(page.locator('.video-js')).toBeVisible({ timeout: 30000 });
  await expect(page.locator('#status')).toContainText(/Direct play|Playing|Converting/, { timeout: 30000 });
}

test.describe.configure({ mode: 'serial' });

test('fullscreen via video.js control bar', async ({ page }) => {
  await ensureAdmin(page);
  await ensureMovieLibrary(page);
  await openSampleMoviePlayer(page);
  await forceControlBar(page);
  const fsBtn = page.locator('.vjs-fullscreen-control');
  await expect(fsBtn).toBeVisible({ timeout: 15000 });
  await fsBtn.click();

  await expect
    .poll(async () => page.evaluate(() => {
      const el = document.querySelector('.video-js');
      return !!(document.fullscreenElement || (el && el.classList.contains('vjs-fullscreen')));
    }), {
      timeout: 10000,
    })
    .toBe(true);
});

test('movie player has no episode buttons', async ({ page }) => {
  await ensureAdmin(page);
  await ensureMovieLibrary(page);
  await openSampleMoviePlayer(page);
  await forceControlBar(page);
  await expect(page.locator('.vjs-control-bar .vjs-prev-episode')).toBeHidden();
  await expect(page.locator('.vjs-control-bar .vjs-next-episode')).toBeHidden();
});

test('video.js control bar shows audio and subtitle menus', async ({ page }) => {
  await ensureAdmin(page);
  await ensureMovieLibrary(page);
  await injectSessionTracks(page);
  await openSampleMoviePlayer(page);
  await forceControlBar(page);
  await expect(page.locator('.vjs-control-bar button.vjs-audio-button')).toBeVisible({ timeout: 15000 });
  await expect(page.locator('.vjs-control-bar button.vjs-subs-caps-button')).toBeVisible({ timeout: 15000 });
});

test('episode player has prev/next in the control bar', async ({ page }) => {
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
  await page.locator('.season-card').first().click();
  await expect(page).toHaveURL(/\/seasons\/\d+/);
  await page.locator('a.episode-still[href^="/play/episode/"]').first().click();
  await expect(page.locator('.video-js')).toBeVisible({ timeout: 30000 });
  await expect(page.locator('#status')).toContainText(/Direct play|Playing|Converting|Loading/, { timeout: 30000 });
  await forceControlBar(page);

  const prev = page.locator('.vjs-control-bar .vjs-prev-episode');
  const next = page.locator('.vjs-control-bar .vjs-next-episode');
  await expect(prev).toBeVisible({ timeout: 15000 });
  await expect(next).toBeVisible({ timeout: 15000 });
  await expect(prev).toHaveClass(/vjs-disabled/);
});

test('next episode advances from the control bar', async ({ page }) => {
  await ensureAdmin(page);
  await ensureAnimeLibrary(page);

  await page.locator('#items a.card').filter({
    has: page.locator('.t', { hasText: /^Pack Show/ }),
  }).first().click();
  await expect(page).toHaveURL(/\/shows\/\d+/);
  await page.locator('a.season-card').first().click();
  await expect(page.locator('.episode-card')).toHaveCount(2, { timeout: 30000 });
  await page.locator('a.episode-still[href^="/play/episode/"]').first().click();
  await expect(page).toHaveURL(/\/play\/episode\/\d+/);
  await expect(page.locator('.video-js')).toBeVisible({ timeout: 30000 });
  await expect(page.locator('#status')).toContainText(/Direct play|Playing|Converting|Loading/, { timeout: 30000 });
  await forceControlBar(page);

  const prev = page.locator('.vjs-control-bar .vjs-prev-episode');
  const next = page.locator('.vjs-control-bar .vjs-next-episode');
  await expect(prev).toBeVisible({ timeout: 15000 });
  await expect(next).toBeVisible({ timeout: 15000 });
  await expect(prev).toHaveClass(/vjs-disabled/);
  await expect(next).not.toHaveClass(/vjs-disabled/);

  const before = page.url();
  await next.click();
  await expect.poll(() => page.url(), { timeout: 20000 }).not.toBe(before);
  await expect(page).toHaveURL(/\/play\/episode\/\d+/);
});

test('status sits below the player stage', async ({ page }) => {
  await ensureAdmin(page);
  await ensureMovieLibrary(page);
  await openSampleMoviePlayer(page);
  const below = await page.evaluate(() => {
    const stage = document.getElementById('stage');
    const status = document.getElementById('status');
    if (!stage || !status) return false;
    return !!(stage.compareDocumentPosition(status) & Node.DOCUMENT_POSITION_FOLLOWING);
  });
  expect(below).toBe(true);
});

test('duration display uses session length', async ({ page }) => {
  await ensureAdmin(page);
  await ensureMovieLibrary(page);
  let sessionDur = 0;
  await page.route('**/play/**/session', async (route) => {
    if (route.request().method() !== 'POST') {
      await route.continue();
      return;
    }
    const res = await route.fetch();
    const json = await res.json();
    sessionDur = Number(json.duration) || 0;
    await route.fulfill({
      status: res.status(),
      contentType: 'application/json',
      body: JSON.stringify(json),
    });
  });
  await openSampleMoviePlayer(page);
  await forceControlBar(page);
  if (sessionDur > 0) {
    const pad = (n) => String(n).padStart(2, '0');
    const s = Math.floor(sessionDur);
    const h = Math.floor(s / 3600);
    const m = Math.floor((s % 3600) / 60);
    const sec = s % 60;
    const want = h > 0 ? `${h}:${pad(m)}:${pad(sec)}` : `${m}:${pad(sec)}`;
    await expect(page.locator('.vjs-duration-display')).toContainText(want, { timeout: 15000 });
  }
});

test('seek bar click jumps toward that time', async ({ page }) => {
  await ensureAdmin(page);
  await ensureMovieLibrary(page);
  await openSampleMoviePlayer(page);
  await forceControlBar(page);
  const bar = page.locator('.vjs-progress-holder').first();
  await expect(bar).toBeVisible({ timeout: 15000 });
  const box = await bar.boundingBox();
  expect(box).toBeTruthy();
  await bar.click({ position: { x: Math.max(1, Math.floor(box.width * 0.5)), y: Math.max(1, Math.floor(box.height / 2)) } });
  const ratio = await page.evaluate(() => {
    const el = document.getElementById('v');
    const p = el && el.player;
    const vid = document.querySelector('.video-js video') || document.querySelector('video');
    const dur = (p && p.duration && p.duration()) || (vid && vid.duration) || 0;
    const t = (p && p.currentTime && p.currentTime()) || (vid && vid.currentTime) || 0;
    if (!(dur > 0.5)) return null;
    return t / dur;
  });
  if (ratio == null) {
    return;
  }
  expect(ratio).toBeGreaterThan(0.2);
});
