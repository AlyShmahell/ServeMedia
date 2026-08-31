const { expect } = require('@playwright/test');

async function libraryCard(page, name) {
  return page.locator('.library-card').filter({
    has: page.locator('.library-card-title', { hasText: name }),
  }).first();
}

async function waitForLibraryIdle(page, name) {
  await page.goto('/');
  const card = await libraryCard(page, name);
  await expect(card).toBeVisible({ timeout: 30000 });
  await expect(card).not.toHaveClass(/is-scanning/, { timeout: 180000 });
  return card;
}

async function openLibrary(page, name) {
  const card = await waitForLibraryIdle(page, name);
  await card.locator('.library-card-title').click();
  await expect(page).toHaveURL(/\/libraries\/\d+/);
}

async function localScanLibrary(page, name) {
  const card = await waitForLibraryIdle(page, name);
  await card.locator('.library-action-scan').click();
  await expect(page.locator('#scan-modal')).toBeVisible();
  await page.selectOption('#scan-mode', 'local');
  await page.locator('#scan-modal-form button[type="submit"]').click();
  await expect(card).not.toHaveClass(/is-scanning/, { timeout: 180000 });
}

async function ensureAdmin(page) {
  await page.goto('/');
  const url = page.url();
  if (url.includes('/register')) {
    await page.fill('input[name="username"]', 'admin');
    await page.fill('input[name="password"]', 'adminpass');
    await page.fill('input[name="confirm"]', 'adminpass');
    await page.click('button[type="submit"]');
    await page.waitForURL(/\/$/);
    return;
  }
  if (url.includes('/login')) {
    await page.fill('input[name="username"]', 'admin');
    await page.fill('input[name="password"]', 'adminpass');
    await page.click('button[type="submit"]');
    await page.waitForURL(/\/$/);
  }
}

async function loginAs(page, username, password) {
  await page.goto('/login');
  await page.fill('input[name="username"]', username);
  await page.fill('input[name="password"]', password);
  await page.click('button[type="submit"]');
  await page.waitForURL(/\/$/);
}

async function ensureRegularUser(page, username = 'trackeruser', password = 'userpass1') {
  await page.goto('/login');
  await page.fill('input[name="username"]', username);
  await page.fill('input[name="password"]', password);
  await page.click('button[type="submit"]');
  try {
    await page.waitForURL(/\/$/, { timeout: 5000 });
    return;
  } catch (_) {
    // user may not exist yet
  }

  await ensureAdmin(page);
  await page.goto('/settings/users');
  const row = page.locator('tbody tr', { hasText: username });
  if ((await row.count()) === 0) {
    await page.locator('#add-user-open').click();
    await page.fill('#add-user-username', username);
    await page.fill('#add-user-password', password);
    await page.fill('#add-user-confirm', password);
    await page.locator('#add-user-submit').click();
    await page.waitForURL(/\/settings\/users$/, { timeout: 15000 });
  }

  await page.locator('form.logout button').click();
  await page.waitForURL(/\/login$/);
  await loginAs(page, username, password);
}

module.exports = {
  ensureAdmin,
  ensureRegularUser,
  loginAs,
  libraryCard,
  waitForLibraryIdle,
  openLibrary,
  localScanLibrary,
};
