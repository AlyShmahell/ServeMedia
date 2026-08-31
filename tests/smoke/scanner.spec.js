const { test, expect } = require('@playwright/test');
const { openLibrary, localScanLibrary } = require('./helpers');

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
  // Matchora 0.0.4 grouping emits extra sibling-folder titles. A local rescan
  // restores disk titles (Dual Show, Season Pack Show, …) without deleting extras.
  await localScanLibrary(page, 'Anime');
  await openLibrary(page, 'Anime');
  await expect(page.locator('#items .card').first()).toBeVisible({ timeout: 90000 });
}

function libraryCards(page) {
  return page.locator('#items a.card');
}

test.describe.configure({ mode: 'serial' });

test('anime library: stray film, pack show, dual show, season pack', async ({ page }) => {
  test.setTimeout(240000);
  await ensureAdmin(page);
  await ensureAnimeLibrary(page);

  const cards = libraryCards(page);
  await expect(cards.first()).toBeVisible();
  await expect(cards).not.toHaveCount(0);

  // Zero Season 2 movie dupes.
  const seasonDupes = cards.filter({ has: page.locator('.t', { hasText: /^Season 2$/ }) });
  await expect(seasonDupes).toHaveCount(0);

  // Show With Films: root film + Movies/ pack stay on the show as season 0.
  const showWithFilms = cards.filter({ has: page.locator('.t', { hasText: /^Show With Films/ }) });
  await expect(showWithFilms).toHaveCount(1);
  await expect(showWithFilms.first()).toHaveAttribute('href', /\/shows\/\d+/);
  await expect(cards.filter({ has: page.locator('.t', { hasText: /Legend Film Title/i }) })).toHaveCount(0);
  await expect(cards.filter({ has: page.locator('.t', { hasText: /Pack Film Title/i }) })).toHaveCount(0);
  await showWithFilms.first().click();
  await expect(page).toHaveURL(/\/shows\/\d+/);
  const specials = page.locator('a.season-card').filter({ hasText: /Specials|Season 0/i });
  await expect(specials).toHaveCount(1);
  await specials.click();
  await expect(page.locator('.episode-card')).toHaveCount(2);
  await page.goto('/');
  await page.locator('.library-card-title', { hasText: 'Anime' }).first().click();
  await expect(page.locator('#items .card').first()).toBeVisible({ timeout: 30000 });

  // Flat multi-ep packs are single shows, not N movies.
  for (const title of ['Flat Dash Show', 'Nested Cour Show', 'Extras Sibling Show']) {
    const card = cards.filter({ has: page.locator('.t', { hasText: new RegExp(`^${title}`) }) });
    await expect(card).toHaveCount(1);
    await expect(card.first()).toHaveAttribute('href', /\/shows\/\d+/);
  }

  // Franchise pack expands to nested shows; parent is not a card.
  await expect(cards.filter({ has: page.locator('.t', { hasText: /^Franchise Pack Show$/ }) })).toHaveCount(0);
  const mainSeries = cards.filter({ has: page.locator('.t', { hasText: /^Main Series Show/ }) });
  await expect(mainSeries).toHaveCount(1);
  await expect(mainSeries.first()).toHaveAttribute('href', /\/shows\/\d+/);
  const spinoff = cards.filter({ has: page.locator('.t', { hasText: /^Spinoff Series Show/ }) });
  await expect(spinoff).toHaveCount(1);
  await expect(spinoff.first()).toHaveAttribute('href', /\/shows\/\d+/);

  // Extras sibling: root eps only (NCOP skipped).
  const extrasSibling = cards.filter({ has: page.locator('.t', { hasText: /^Extras Sibling Show/ }) });
  await extrasSibling.first().click();
  await expect(page).toHaveURL(/\/shows\/\d+/);
  await page.locator('a.season-card').first().click();
  await expect(page.locator('.episode-card')).toHaveCount(2);

  await page.goto('/');
  await page.locator('.library-card-title', { hasText: 'Anime' }).first().click();
  await expect(page.locator('#items .card').first()).toBeVisible({ timeout: 30000 });

  // Film gate: stray tvshow.nfo still a movie.
  const film = libraryCards(page).filter({ has: page.locator('.t', { hasText: /Stray NFO Film/i }) });
  await expect(film).toHaveCount(1);
  await expect(film.first()).toHaveAttribute('href', /\/movies\/\d+/);

  // Pack Show (not Season Pack Show).
  const pack = libraryCards(page).filter({ has: page.locator('.t', { hasText: /^Pack Show/ }) });
  await expect(pack).toHaveCount(1);
  await pack.first().click();
  await expect(page).toHaveURL(/\/shows\/\d+/);
  await expect(page.locator('a.season-card').first()).toBeVisible();
  await page.locator('a.season-card').first().click();
  await expect(page.locator('.episode-card')).toHaveCount(2);

  // Dual Show: S1 stays at 2 (Alpha only); S2 has leading SxxEyy.
  await page.goto('/');
  await page.locator('.library-card-title', { hasText: 'Anime' }).first().click();
  await expect(page.locator('#items .card').first()).toBeVisible({ timeout: 30000 });
  const dual = libraryCards(page).filter({ has: page.locator('.t', { hasText: /^Dual Show/ }) });
  await expect(dual).toHaveCount(1);
  await dual.first().click();
  await expect(page).toHaveURL(/\/shows\/\d+/);
  const dualSeasons = page.locator('a.season-card');
  await expect(dualSeasons).toHaveCount(2);
  const showURL = page.url();

  const s1 = page.locator('a.season-card[href$="/seasons/1"]');
  await expect(s1).toHaveCount(1);
  await expect(s1).toContainText(/2 episode/);
  await s1.click();
  await expect(page.locator('.episode-card')).toHaveCount(2);

  await page.goto(showURL);
  const s2 = page.locator('a.season-card[href$="/seasons/2"]');
  await expect(s2).toHaveCount(1);
  await s2.click();
  await expect(page.locator('.episode-card')).toHaveCount(2);

  // Season Pack Show: one show, three episodes, not Season 2 movies.
  await page.goto('/');
  await page.locator('.library-card-title', { hasText: 'Anime' }).first().click();
  await expect(page.locator('#items .card').first()).toBeVisible({ timeout: 30000 });
  const seasonPack = libraryCards(page).filter({ has: page.locator('.t', { hasText: /^Season Pack Show/ }) });
  await expect(seasonPack).toHaveCount(1);
  await seasonPack.first().click();
  await expect(page).toHaveURL(/\/shows\/\d+/);
  await page.locator('a.season-card').first().click();
  await expect(page.locator('.episode-card')).toHaveCount(3);

  // Complex Show: irregular cour dirs + OVA → multiple seasons (not S04-only).
  await page.goto('/');
  await page.locator('.library-card-title', { hasText: 'Anime' }).first().click();
  await expect(page.locator('#items .card').first()).toBeVisible({ timeout: 30000 });
  const complex = libraryCards(page).filter({ has: page.locator('.t', { hasText: /Complex Show/i }) });
  await expect(complex).toHaveCount(1);
  await expect(complex.first()).toHaveAttribute('href', /\/shows\/\d+/);
  await complex.first().click();
  await expect(page).toHaveURL(/\/shows\/\d+/);
  await expect(page.locator('a.season-card')).toHaveCount(4);
});
