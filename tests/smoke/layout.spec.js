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
  await expect(topbar.locator('#topbar-menu')).toHaveCount(0);
  await expect(topbar.getByRole('button', { name: 'Settings' })).toBeVisible();
  await expect(topbar.getByRole('button', { name: 'About' })).toBeVisible();
  await expect(topbar.locator('#settings-open')).toHaveAttribute('title', 'Settings');
  await expect(topbar.locator('#about-open')).toHaveAttribute('title', 'About');
  await expect(topbar.locator('.topbar-logo')).toHaveAttribute('href', '/');
  await expect(topbar.locator('.topbar-logo')).toHaveAttribute('title', 'Return to Home Page');
  await expect(topbar.locator('.topbar-logo-name')).toHaveText('ServeMedia');
  await expect(topbar.locator('.topbar-signout')).toContainText('admin');
  await expect(page.getByRole('link', { name: 'Home', exact: true })).toHaveCount(0);
  await expect(page.locator('.sidebar')).toHaveCount(0);
  await expect(page.locator('.nav-libraries')).toHaveCount(0);

  await expect(page.locator('.home-section')).toHaveCount(3);
  await expect(page.locator('.home-split')).toHaveCount(0);
  await expect(page.locator('.home-section-continue')).toBeVisible();
  await expect(page.locator('.home-section-recent')).toBeVisible();
  await expect(page.locator('.home-section-libraries')).toBeVisible();

  const layout = await page.evaluate(() => {
    const pageEl = document.querySelector('.home-page');
    const libs = document.querySelector('.home-section-libraries');
    const cont = document.querySelector('.home-section-continue');
    const recent = document.querySelector('.home-section-recent');
    const pr = pageEl.getBoundingClientRect();
    const pad = parseFloat(getComputedStyle(pageEl).paddingTop);
    const gap = parseFloat(getComputedStyle(pageEl).rowGap);
    const innerW = pr.width - pad * 2;
    const innerH = pr.height - pad * 2;
    const col1 = (innerW - gap) * 0.6;
    const col2 = (innerW - gap) * 0.4;
    const row1 = (innerH - gap) * 0.4;
    const row2 = (innerH - gap) * 0.6;
    function box(el) {
      const r = el.getBoundingClientRect();
      return {
        x: r.left - pr.left,
        y: r.top - pr.top,
        w: r.width,
        h: r.height,
        right: pr.right - r.right,
        bottom: pr.bottom - r.bottom,
      };
    }
    return {
      pad,
      gap,
      col1,
      col2,
      row1,
      row2,
      libs: box(libs),
      cont: box(cont),
      recent: box(recent),
    };
  });
  const slack = 4;
  expect(layout.pad).toBeGreaterThanOrEqual(7);
  expect(layout.pad).toBeLessThanOrEqual(9);
  expect(layout.gap).toBeGreaterThanOrEqual(7);
  expect(layout.gap).toBeLessThanOrEqual(9);
  expect(Math.abs(layout.libs.w - layout.col1)).toBeLessThanOrEqual(slack);
  expect(Math.abs(layout.libs.h - layout.row1)).toBeLessThanOrEqual(slack);
  expect(Math.abs(layout.cont.w - layout.col1)).toBeLessThanOrEqual(slack);
  expect(Math.abs(layout.cont.h - layout.row2)).toBeLessThanOrEqual(slack);
  expect(Math.abs(layout.recent.w - layout.col2)).toBeLessThanOrEqual(slack);
  expect(Math.abs(layout.recent.h - (layout.row1 + layout.gap + layout.row2))).toBeLessThanOrEqual(slack);
  expect(Math.abs(layout.libs.x - layout.pad)).toBeLessThanOrEqual(slack);
  expect(Math.abs(layout.libs.y - layout.pad)).toBeLessThanOrEqual(slack);
  expect(Math.abs(layout.cont.x - layout.pad)).toBeLessThanOrEqual(slack);
  expect(Math.abs(layout.cont.y - (layout.pad + layout.row1 + layout.gap))).toBeLessThanOrEqual(slack);
  expect(Math.abs(layout.recent.x - (layout.pad + layout.col1 + layout.gap))).toBeLessThanOrEqual(slack);
  expect(Math.abs(layout.recent.y - layout.pad)).toBeLessThanOrEqual(slack);
  expect(Math.abs(layout.libs.right - (layout.pad + layout.gap + layout.col2))).toBeLessThanOrEqual(slack);
  expect(Math.abs(layout.cont.right - (layout.pad + layout.gap + layout.col2))).toBeLessThanOrEqual(slack);
  expect(Math.abs(layout.recent.right - layout.pad)).toBeLessThanOrEqual(slack);
  expect(Math.abs(layout.libs.bottom - (layout.pad + layout.gap + layout.row2))).toBeLessThanOrEqual(slack);
  expect(Math.abs(layout.cont.bottom - layout.pad)).toBeLessThanOrEqual(slack);
  expect(Math.abs(layout.recent.bottom - layout.pad)).toBeLessThanOrEqual(slack);
  expect(Math.abs((layout.cont.y - layout.libs.h - layout.libs.y) - layout.gap)).toBeLessThanOrEqual(slack);
  expect(Math.abs((layout.recent.x - layout.libs.w - layout.libs.x) - layout.gap)).toBeLessThanOrEqual(slack);
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

test('About opens as a modal and version lives under the logo', async ({ page }) => {
  await ensureAdmin(page);
  await page.goto('/about');
  await expect(page).toHaveURL(/\/$/);
  await expect(page.locator('.topbar-title')).toHaveText('Home');
  await page.locator('#about-open').click();
  await expect(page.locator('#about-modal')).toHaveAttribute('open', '');
  await expect(page).toHaveURL(/\/$/);
  await expect(page.locator('.about-panel h2', { hasText: 'Version' })).toHaveCount(0);
  await expect(page.locator('.about-panel h2', { hasText: 'License' })).toBeVisible();
  await expect(page.locator('.about-panel h2', { hasText: 'Attribution' })).toBeHidden();
  await page.getByRole('button', { name: 'Attribution', exact: true }).click();
  await expect(page.locator('.about-panel h2', { hasText: 'Attribution' })).toBeVisible();
  await expect(page.locator('.about-panel h2', { hasText: 'License' })).toBeHidden();
  await expect(page.locator('.settings-nav-link.active')).toHaveText('Attribution');
  await expect(page.getByRole('heading', { name: 'Author' })).toHaveCount(0);
  await expect(page.getByRole('heading', { name: 'Copyright' })).toHaveCount(0);
  await expect(page.locator('.topbar-logo-version')).toContainText(/\d+\.\d+\.\d+/);
});

test('Support card and heart', async ({ page }) => {
  await ensureAdmin(page);
  await page.goto('/');
  await expect(page.locator('.topbar-heart')).toBeVisible();
  await page.locator('.topbar-heart').click();
  await expect(page).toHaveURL(/\/$/);
  const dialog = page.locator('#support-modal');
  await expect(dialog).toHaveAttribute('open', '');
  await expect(page.locator('.support-card h2')).toHaveText('Support ServeMedia');
  await expect(page.locator('.support-qr')).toBeVisible();
  await expect(page.getByRole('link', { name: 'Sponsor Me' })).toHaveAttribute('href', /github.com\/sponsors\/AlyShmahell/);
  await expect(page.getByRole('link', { name: 'Star ServeMedia' })).toHaveAttribute('href', /github.com\/AlyShmahell\/ServeMedia/);
  await expect(page.getByRole('link', { name: 'Contributors' })).toHaveAttribute('href', /contributors/);
  await page.locator('#support-modal-close').click();
  await expect(dialog).not.toHaveAttribute('open', '');
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
    const body = section && section.querySelector('.home-section-body');
    const grid = section && section.querySelector('.library-grid');
    const card = section && section.querySelector('.library-card');
    if (!section || !body || !grid || !card) {
      return { ok: false };
    }
    const b = body.getBoundingClientRect();
    const c = card.getBoundingClientRect();
    return {
      ok: true,
      cardW: c.width,
      cardH: c.height,
      bodyH: b.height,
      overflowY: getComputedStyle(card).overflowY,
    };
  });
  expect(metrics.ok).toBe(true);
  expect(metrics.cardW).toBeGreaterThan(190);
  expect(metrics.cardW).toBeLessThan(210);
  expect(Math.abs(metrics.cardH - metrics.bodyH)).toBeLessThanOrEqual(2);
  expect(metrics.overflowY).toBe('auto');
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
  await expect(page).toHaveURL(/\/libraries\/\d+\/tree/);
  await expect(page.locator('.topbar-title')).toContainText('TV');
  await expect(page.locator('.topbar-title')).toContainText('Sample Show');
  await expect(page.locator('.topbar-back[href^="/libraries/"]')).toBeVisible();
  await expect(page.locator('.show-hero-card, .folder-hero').first()).toBeVisible();
  await expect(page.locator('.folder-hero h1 .field-icon')).toHaveCount(0);
  const poster = page.locator('.media-hero-poster');
  await poster.hover();
  await poster.locator('.field-icon[data-role="edit-modal"]').click();
  await expect(page.locator('#edit-modal')).toBeVisible();
  await expect(page.locator('#edit-image-save')).toBeDisabled();
  await expect(page.locator('#edit-panel-text')).toBeHidden();
  await page.locator('[data-edit-tab="text"]').click();
  await expect(page.locator('#edit-panel-text')).toBeVisible();
  await expect(page.locator('#edit-text-title')).toBeEnabled();
  await page.locator('#edit-modal-close').click();
  await expect(page.locator('#edit-modal')).toBeHidden();

  const seasonCard = page.locator('#items a.file-card').filter({ hasText: /Season/i }).first();
  await expect(seasonCard).toBeVisible({ timeout: 30000 });
  await seasonCard.click();
  await expect(page).toHaveURL(/\/libraries\/\d+\/tree/);

  const episodeCard = page.locator('#items a.file-card[href^="/play/file/"]').first();
  await expect(episodeCard).toBeVisible();
  await expect(episodeCard).toContainText('Pilot');
  await expect(episodeCard.locator('.poster-progress')).toBeVisible();
  await expect(episodeCard).toHaveAttribute('href', /\/play\/file\/\d+/);
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

test.describe('phone home layout', () => {
  test.use({ viewport: { width: 390, height: 844 }, hasTouch: true, isMobile: true });

  test('home feed scrolls as one column without horizontal clipping', async ({ page }) => {
    await ensureAdmin(page);
    await ensureTVLibrary(page);
    await page.goto('/');
    await expect(page.locator('.home-section-libraries .library-card').first()).toBeVisible({
      timeout: 30000,
    });

    const metrics = await page.evaluate(() => {
      const page_ = document.querySelector('.home-page');
      const section = document.querySelector('.home-section-libraries');
      const card = section && section.querySelector('.library-card');
      const signout = document.querySelector('.topbar-signout');
      const style = page_ && getComputedStyle(page_);
      const pad = style
        ? parseFloat(style.paddingTop) + parseFloat(style.paddingBottom)
        : 0;
      const box = page_ ? page_.clientHeight - pad : 0;
      function band(selector) {
        const el = document.querySelector(selector);
        if (!el) return null;
        const cs = getComputedStyle(el);
        const length = (value) => (
          String(value).endsWith('%') ? (parseFloat(value) / 100) * box : parseFloat(value)
        );
        return {
          h: el.getBoundingClientRect().height,
          min: length(cs.minHeight),
          max: length(cs.maxHeight),
        };
      }
      return {
        overflowY: style ? style.overflowY : '',
        sectionScrollSlack: section ? section.scrollHeight - section.clientHeight : -1,
        cardWidth: card ? card.getBoundingClientRect().width : 0,
        signoutRight: signout ? signout.getBoundingClientRect().right : 0,
        innerWidth: window.innerWidth,
        scrollWidth: document.documentElement.scrollWidth,
        box,
        libs: band('.home-section-libraries'),
        cont: band('.home-section-continue'),
        recent: band('.home-section-recent'),
      };
    });

    expect(metrics.overflowY).toBe('auto');
    expect(Math.abs(metrics.sectionScrollSlack)).toBeLessThanOrEqual(1);
    const slack = 4;
    for (const [band, minRatio, maxRatio] of [
      [metrics.libs, 0.6, 0.9],
      [metrics.cont, 0.4, 0.8],
      [metrics.recent, 0.6, 0.9],
    ]) {
      expect(band).not.toBeNull();
      expect(Math.abs(band.min - metrics.box * minRatio)).toBeLessThanOrEqual(slack);
      expect(Math.abs(band.max - metrics.box * maxRatio)).toBeLessThanOrEqual(slack);
      expect(band.h).toBeGreaterThanOrEqual(band.min - slack);
      expect(band.h).toBeLessThanOrEqual(band.max + slack);
    }
    expect(metrics.cardWidth).toBeGreaterThanOrEqual(300);
    expect(metrics.signoutRight).toBeLessThanOrEqual(metrics.innerWidth + 0.5);
    expect(metrics.scrollWidth).toBeLessThanOrEqual(metrics.innerWidth);
  });
});

test.describe('landscape phone home layout', () => {
  test.use({ viewport: { width: 844, height: 390 }, hasTouch: true, isMobile: true });

  test('short landscape viewport keeps the single scrolling feed', async ({ page }) => {
    await ensureAdmin(page);
    await page.goto('/');
    await expect(page.locator('.home-section-libraries')).toBeVisible();

    const metrics = await page.evaluate(() => {
      const page_ = document.querySelector('.home-page');
      const style = page_ && getComputedStyle(page_);
      return {
        overflowY: style ? style.overflowY : '',
        innerWidth: window.innerWidth,
        scrollWidth: document.documentElement.scrollWidth,
      };
    });

    expect(metrics.overflowY).toBe('auto');
    expect(metrics.scrollWidth).toBeLessThanOrEqual(metrics.innerWidth);
  });
});
