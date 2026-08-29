const { test, expect } = require('@playwright/test');
const { ensureAdmin } = require('./helpers');

test.describe.configure({ mode: 'serial' });

test('Metadata tab shows OMDb and TMDB secret fields', async ({ page }) => {
  await ensureAdmin(page);
  await page.goto('/settings/integrations');
  await expect(page.locator('#integrations-form')).toBeVisible();
  await page.locator('.settings-tab[data-tab="metadata"]').click();
  await expect(page.locator('#tab-metadata')).toBeVisible();
  await expect(page.locator('#secret-omdb')).toBeVisible();
  await expect(page.locator('#secret-tmdb')).toBeVisible();
  await expect(page.locator('#tab-metadata')).toContainText(/OMDb API key/i);
});

test('Test webhooks button is tab-exclusive', async ({ page }) => {
  await ensureAdmin(page);
  await page.goto('/settings/integrations');
  await expect(page.locator('#integrations-form')).toBeVisible();
  await expect(page.locator('#test-webhooks')).toBeVisible();
  await page.locator('.settings-tab[data-tab="metadata"]').click();
  await expect(page.locator('#test-webhooks')).toBeHidden();
  await page.locator('.settings-tab[data-tab="webhooks"]').click();
  await expect(page.locator('#test-webhooks')).toBeVisible();
});

test('Saving a metadata key keeps the integrations page', async ({ page }) => {
  await ensureAdmin(page);
  await page.goto('/settings/integrations');
  await page.locator('.settings-tab[data-tab="metadata"]').click();
  await page.fill('#secret-omdb', 'test');
  await page.locator('#tab-metadata button[type="submit"]').click();
  await expect(page).toHaveURL(/\/settings\/integrations/);
  await page.locator('.settings-tab[data-tab="metadata"]').click();
  const omdbLabel = page.locator('#tab-metadata label').filter({ hasText: 'OMDb' });
  await expect(omdbLabel).not.toContainText('not set');
});
