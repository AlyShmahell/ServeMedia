const { test, expect } = require('@playwright/test');
const { ensureAdmin, ensureRegularUser, openSettingsTab } = require('./helpers');

test.describe.configure({ mode: 'serial' });

test('Engines tabs show MatchMedia secrets, YAML, and ffmpeg', async ({ page }) => {
  await ensureAdmin(page);
  await page.goto('/');
  await openSettingsTab(page, 'MatchMedia');
  await expect(page).toHaveURL(/\/$/);
  await expect(page.locator('#tab-matchmedia')).toBeVisible();
  await expect(page.locator('#secret-omdb')).toBeVisible();
  await expect(page.locator('#secret-tmdb')).toBeVisible();
  await expect(page.locator('#overlay-yaml')).toHaveValue(/providers:/);
  await expect(page.locator('#overlay-yaml')).toHaveValue(/browse_roots:/);
  await expect(page.locator('.CodeMirror')).toBeVisible({ timeout: 15000 });

  await openSettingsTab(page, 'FFmpeg');
  await expect(page.locator('#tab-ffmpeg')).toBeVisible();
  await expect(page.locator('#tab-ffmpeg')).toContainText(/H\.264 CRF/i);
  await expect(page.locator('#tab-ffmpeg')).toContainText(/Hardware acceleration/i);
  await expect(page.locator('.settings-nav-link.active')).toHaveText('FFmpeg');
});

test('Saving a MatchMedia key keeps the engines page', async ({ page }) => {
  await ensureAdmin(page);
  await page.goto('/');
  await openSettingsTab(page, 'MatchMedia');
  await page.fill('#secret-omdb', 'test');
  await page.locator('#engines-save').click();
  await expect(page).toHaveURL(/\/$/);
  const omdbLabel = page.locator('#tab-matchmedia label').filter({ hasText: 'OMDb' });
  await expect(omdbLabel).not.toContainText('not set');
});

test('Restore default stays on MatchMedia', async ({ page }) => {
  test.setTimeout(120000);
  await ensureAdmin(page);
  await page.goto('/');
  await openSettingsTab(page, 'MatchMedia');
  page.once('dialog', (dialog) => dialog.accept());
  await page.locator('#engines-restore').click();
  await expect(page).toHaveURL(/\/$/);
  await expect(page.locator('#tab-matchmedia')).toBeVisible();
  await expect(page.locator('#engines-restart')).toHaveCount(0);

  await expect(async () => {
    await page.goto('/');
    await openSettingsTab(page, 'MatchMedia');
    await expect(page.locator('#overlay-yaml')).toHaveValue(/omdb-stub/);
    await expect(page.locator('#overlay-yaml')).toHaveValue(/\/media/);
  }).toPass({ timeout: 30000 });
});

test('settings URLs return home and FFmpeg opens in the modal', async ({ page }) => {
  await ensureAdmin(page);
  await page.goto('/settings/server');
  await expect(page).toHaveURL(/\/$/);
  await page.goto('/settings');
  await expect(page).toHaveURL(/\/$/);
  await openSettingsTab(page, 'FFmpeg');
  await expect(page.locator('#tab-ffmpeg')).toBeVisible();
});

test('regular user cannot open Engines', async ({ page }) => {
  await ensureRegularUser(page);
  const res = await page.goto('/settings/engines');
  expect(res.status()).toBe(403);
});
