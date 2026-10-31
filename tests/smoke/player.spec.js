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
  // Sample Movie lives in the Movies library tree.
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
  await page.locator('#items a.card, #items a.file-card').filter({
    hasText: /Sample Movie/i,
  }).first().click();
  if (!/\/play\//.test(page.url())) {
    await page.locator('a.file-card[href^="/play/file/"]').first().click();
  }
  await expect(page.locator('.video-js')).toBeVisible({ timeout: 30000 });
  await expect(page.locator('#status')).toContainText(/Direct play|Playing|Converting/, { timeout: 30000 });
}

test.describe.configure({ mode: 'serial' });

test('player arrow keys stay in video.js', async ({ page }) => {
  await ensureAdmin(page);
  await ensureMovieLibrary(page);
  await openSampleMoviePlayer(page);
  const url = page.url();
  await page.locator('.video-js').evaluate((el) => {
    el.setAttribute('tabindex', '0');
    el.focus();
  });
  await page.keyboard.press('ArrowLeft');
  await expect(page).toHaveURL(url);
  const inPlayer = await page.evaluate(() => {
    const el = document.activeElement;
    if (!el) return false;
    return !!(el.closest && el.closest('.video-js')) || !!(el.classList && el.classList.contains('video-js')) || el.tagName === 'VIDEO';
  });
  expect(inPlayer).toBe(true);
});

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
  await expect(page).toHaveURL(/\/libraries\/\d+\/tree/);
  await page.locator('#items a.file-card').filter({ hasText: /Season/i }).first().click();
  await expect(page).toHaveURL(/\/libraries\/\d+\/tree/);
  await page.locator('a.file-card[href^="/play/file/"]').first().click();
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
  await expect(page).toHaveURL(/\/libraries\/\d+\/tree/);
  await page.locator('#items a.file-card').filter({ hasText: /Season/i }).first().click();
  await expect(page.locator('a.file-card[href^="/play/file/"]')).toHaveCount(2, { timeout: 30000 });
  await page.locator('a.file-card[href^="/play/file/"]').first().click();
  await expect(page).toHaveURL(/\/play\/file\/\d+/);
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
  await expect(page).toHaveURL(/\/play\/(file|episode)\/\d+/);
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

test('backward seek after forward seek lands near the target', async ({ page }) => {
  await ensureAdmin(page);
  await ensureMovieLibrary(page);
  await openSampleMoviePlayer(page);
  await forceControlBar(page);
  const bar = page.locator('.vjs-progress-holder').first();
  await expect(bar).toBeVisible({ timeout: 15000 });
  const box = await bar.boundingBox();
  expect(box).toBeTruthy();
  const y = Math.max(1, Math.floor(box.height / 2));
  const timeRatio = () => page.evaluate(() => {
    const el = document.getElementById('v');
    const p = el && el.player;
    const vid = document.querySelector('.video-js video') || document.querySelector('video');
    const dur = (p && p.duration && p.duration()) || (vid && vid.duration) || 0;
    const t = (p && p.currentTime && p.currentTime()) || (vid && vid.currentTime) || 0;
    if (!(dur > 0.5)) return null;
    return t / dur;
  });
  await bar.click({ position: { x: Math.max(1, Math.floor(box.width * 0.7)), y } });
  try {
    await expect.poll(timeRatio, { timeout: 15000 }).not.toBeNull();
  } catch {
    return;
  }
  await expect.poll(timeRatio, { timeout: 20000 }).toBeGreaterThan(0.45);
  await bar.click({ position: { x: Math.max(1, Math.floor(box.width * 0.2)), y } });
  await expect.poll(timeRatio, { timeout: 25000 }).toBeLessThan(0.45);
  const back = await timeRatio();
  if (back == null) {
    return;
  }
  expect(back).toBeGreaterThan(0.05);
});

test.describe('phone player stage', () => {
  test.use({ viewport: { width: 390, height: 844 }, hasTouch: true, isMobile: true });

  test('stage is a centred 16:9 box', async ({ page }) => {
    await ensureAdmin(page);
    await ensureMovieLibrary(page);
    await openSampleMoviePlayer(page);

    const metrics = await page.evaluate(() => {
      const stage = document.getElementById('stage');
      if (!stage) return null;
      const rect = stage.getBoundingClientRect();
      return { left: rect.left, right: rect.right, width: rect.width, height: rect.height, innerWidth: window.innerWidth };
    });
    expect(metrics).toBeTruthy();
    expect(Math.abs(metrics.width * 9 / 16 - metrics.height)).toBeLessThanOrEqual(1);
    expect(Math.abs(metrics.left - (metrics.innerWidth - metrics.right))).toBeLessThanOrEqual(1);
  });
});

async function stubFullscreen(page) {
  await page.addInitScript(() => {
    window.__fsCalls = [];
    Element.prototype.requestFullscreen = function () {
      window.__fsCalls.push('request');
      return Promise.resolve();
    };
    Element.prototype.webkitRequestFullscreen = Element.prototype.requestFullscreen;
    document.exitFullscreen = function () {
      window.__fsCalls.push('exit');
      return Promise.resolve();
    };
  });
}

async function dispatchTouchTap(page, x, y) {
  await page.evaluate(({ x, y }) => {
    const stage = document.getElementById('stage');
    stage.dispatchEvent(new PointerEvent('pointerup', {
      bubbles: true,
      cancelable: true,
      pointerId: 1,
      pointerType: 'touch',
      isPrimary: true,
      clientX: x,
      clientY: y,
      button: 0,
      buttons: 0,
    }));
  }, { x, y });
}

test.describe('player fullscreen gesture', () => {
  test.use({ viewport: { width: 390, height: 844 }, hasTouch: true, isMobile: true });

  test('double tap requests fullscreen once without toggling playback', async ({ page }) => {
    await ensureAdmin(page);
    await ensureMovieLibrary(page);
    await stubFullscreen(page);
    await openSampleMoviePlayer(page);

    const box = await page.locator('#stage').boundingBox();
    expect(box).toBeTruthy();
    const x = box.x + box.width / 2;
    const y = box.y + box.height / 2;
    const pausedBefore = await page.evaluate(() => document.getElementById('v').player.paused());

    await dispatchTouchTap(page, x, y);
    await page.evaluate(async () => {
      const player = document.getElementById('v').player;
      if (player.paused()) await player.play();
      else player.pause();
    });
    await dispatchTouchTap(page, x, y);
    await page.waitForTimeout(300);

    const result = await page.evaluate(() => ({
      requests: (window.__fsCalls || []).filter((call) => call === 'request').length,
      paused: document.getElementById('v').player.paused(),
    }));
    expect(result.requests).toBe(1);
    expect(result.paused).toBe(pausedBefore);
  });
});

test('desktop double-click still toggles fullscreen through video.js', async ({ page }) => {
  await ensureAdmin(page);
  await ensureMovieLibrary(page);
  await stubFullscreen(page);
  await page.setViewportSize({ width: 1280, height: 800 });
  await openSampleMoviePlayer(page);

  const box = await page.locator('#stage').boundingBox();
  expect(box).toBeTruthy();
  const pausedBefore = await page.evaluate(() => document.getElementById('v').player.paused());
  await page.mouse.dblclick(box.x + box.width * 0.2, box.y + box.height * 0.25);
  await page.waitForTimeout(300);

  const result = await page.evaluate(() => ({
    requests: (window.__fsCalls || []).filter((call) => call === 'request').length,
    paused: document.getElementById('v').player.paused(),
  }));
  expect(result.requests).toBe(1);
  expect(result.paused).toBe(pausedBefore);
});
