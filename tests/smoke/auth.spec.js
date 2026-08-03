const { test, expect } = require('@playwright/test');

test('bootstrap admin register and home', async ({ page }) => {
  const res = await page.goto('/register');
  if (res && res.status() === 404) {
    // Another smoke file may have registered already (fresh volume, alphabetical order).
    await page.goto('/login');
    await page.fill('input[name="username"]', 'admin');
    await page.fill('input[name="password"]', 'adminpass');
    await page.click('button[type="submit"]');
  } else {
    await expect(page.getByRole('heading', { name: 'Medora' })).toBeVisible();
    await page.fill('input[name="username"]', 'admin');
    await page.fill('input[name="password"]', 'adminpass');
    await page.fill('input[name="confirm"]', 'adminpass');
    await page.click('button[type="submit"]');
  }
  await expect(page).toHaveURL(/\/$/);
  await expect(page.getByRole('heading', { name: 'Home' })).toBeVisible();
});

test('register is unreachable after bootstrap', async ({ page }) => {
  const res = await page.goto('/register');
  expect(res.status()).toBe(404);
});
