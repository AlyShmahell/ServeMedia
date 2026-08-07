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

module.exports = { ensureAdmin, ensureRegularUser, loginAs };
