const { test, expect } = require('@playwright/test');
const { ensureAdmin } = require('./helpers');

test.describe.configure({ mode: 'serial' });

test('Plugins tab lists bundled providers plugin', async ({ page }) => {
  await ensureAdmin(page);
  await page.goto('/settings/integrations');
  await expect(page.locator('#integrations-form')).toBeVisible();
  await page.locator('.settings-tab[data-tab="plugins"]').click();
  await expect(page.locator('#tab-plugins')).toBeVisible();
  await expect(page.locator('.plugin-card[data-plugin="providers"]')).toBeVisible();
  await expect(page.locator('.plugin-settings[data-plugin="providers"]')).toBeVisible();
  await expect(page.locator('.plugin-card[data-plugin="providers"] h3')).toContainText('Metadata providers');
});

test('Test webhooks button is tab-exclusive', async ({ page }) => {
  await ensureAdmin(page);
  await page.goto('/settings/integrations');
  await expect(page.locator('#integrations-form')).toBeVisible();
  await expect(page.locator('#test-webhooks')).toBeVisible();
  await page.locator('.settings-tab[data-tab="plugins"]').click();
  await expect(page.locator('#test-webhooks')).toBeHidden();
  await page.locator('.settings-tab[data-tab="webhooks"]').click();
  await expect(page.locator('#test-webhooks')).toBeVisible();
});

test('Install plugin modal opens from green plus', async ({ page }) => {
  await ensureAdmin(page);
  await page.goto('/settings/integrations');
  await expect(page.locator('#integrations-form')).toBeVisible();
  await page.locator('.settings-tab[data-tab="plugins"]').click();
  await page.click('#add-plugin-open');
  await expect(page.locator('#plugin-install-dialog')).toBeVisible();
  await expect(page.locator('#plugin-install-form input[name="archive"]')).toBeVisible();
});
