const { test, expect } = require('@playwright/test');
const { ensureAdmin, ensureRegularUser } = require('./helpers');

test.describe.configure({ mode: 'serial' });

test('Engines tabs show MatchMedia secrets, YAML, and ffmpeg', async ({ page }) => {
  await ensureAdmin(page);
  await page.goto('/settings/engines');
  await expect(page.getByRole('tab', { name: 'MatchMedia' })).toBeVisible();
  await expect(page.getByRole('tab', { name: 'ffmpeg' })).toBeVisible();
  await expect(page.locator('#tab-matchmedia')).toBeVisible();
  await expect(page.locator('#secret-omdb')).toBeVisible();
  await expect(page.locator('#secret-tmdb')).toBeVisible();
  await expect(page.locator('#engines-status-text')).toBeVisible();
  await expect(page.locator('#engines-status-text')).toHaveText(/^(On|Off)$/);
  await expect(page.locator('#overlay-yaml')).toHaveValue(/providers:/);
  await expect(page.locator('#overlay-yaml')).toHaveValue(/browse_roots:/);
  await expect(page.locator('.CodeMirror')).toBeVisible();

  await page.getByRole('tab', { name: 'ffmpeg' }).click();
  await expect(page.locator('#tab-ffmpeg')).toBeVisible();
  await expect(page.locator('#tab-ffmpeg')).toContainText(/H\.264 CRF/i);
  await expect(page.locator('#tab-ffmpeg')).toContainText(/Hardware acceleration/i);
});

test('Saving a MatchMedia key keeps the engines page', async ({ page }) => {
  await ensureAdmin(page);
  await page.goto('/settings/engines');
  await page.fill('#secret-omdb', 'test');
  await page.locator('#engines-save').click();
  await expect(page).toHaveURL(/\/settings\/engines/);
  const omdbLabel = page.locator('#tab-matchmedia label').filter({ hasText: 'OMDb' });
  await expect(omdbLabel).not.toContainText('not set');
});

test('Restore default and Restart stay on Engines', async ({ page }) => {
  test.setTimeout(120000);
  await ensureAdmin(page);
  await page.goto('/settings/engines');
  page.once('dialog', (dialog) => dialog.accept());
  await page.locator('#engines-restore').click();
  await expect(page).toHaveURL(/\/settings\/engines/);
  await expect(page.locator('#engines-status-text')).toBeVisible();
  await expect(page.locator('#engines-restart')).toBeVisible();

  page.once('dialog', (dialog) => dialog.accept());
  await page.locator('#engines-restart').click();
	await expect(page.locator('#engines-status-text')).toContainText(/^On$/, { timeout: 60000 });

  await expect(async () => {
    await page.goto('/settings/engines');
    await expect(page.locator('#overlay-yaml')).toHaveValue(/omdb-stub/);
    await expect(page.locator('#overlay-yaml')).toHaveValue(/\/media/);
  }).toPass({ timeout: 30000 });
});

test('/settings/server redirects to Engines ffmpeg tab', async ({ page }) => {
  await ensureAdmin(page);
  await page.goto('/settings/server');
  await expect(page).toHaveURL(/\/settings\/engines\?tab=ffmpeg/);
  await expect(page.locator('#tab-ffmpeg')).toBeVisible();
});

test('regular user cannot open Engines', async ({ page }) => {
  await ensureRegularUser(page);
  const res = await page.goto('/settings/engines');
  expect(res.status()).toBe(403);
});
