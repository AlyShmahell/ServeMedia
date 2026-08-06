const { test, expect } = require('@playwright/test');
const { ensureAdmin } = require('./helpers');

const STUB = process.env.WEBHOOK_STUB_URL || 'http://webhook-stub:9090';

async function clearStubEvents(request) {
  await request.delete(`${STUB}/events`);
}

async function waitForStubEvents(request, minCount, timeoutMs = 15000) {
  const deadline = Date.now() + timeoutMs;
  while (Date.now() < deadline) {
    const res = await request.get(`${STUB}/events`);
    expect(res.ok()).toBeTruthy();
    const events = await res.json();
    if (events.length >= minCount) return events;
    await new Promise((r) => setTimeout(r, 500));
  }
  throw new Error(`expected at least ${minCount} webhook event(s)`);
}

test.describe.configure({ mode: 'serial' });

test('integrations page shows webhook settings', async ({ page }) => {
  await ensureAdmin(page);
  await page.goto('/settings/integrations');
  await expect(page.getByRole('heading', { name: 'Integrations' })).toBeVisible();
  await expect(page.getByRole('button', { name: 'Webhooks' })).toBeVisible();
  await expect(page.locator('#webhook-api-key')).not.toHaveValue('');
});

test('test webhook delivers Medora webhook payload', async ({ page, request }) => {
  await ensureAdmin(page);
  await clearStubEvents(request);

  await page.goto('/settings/integrations');
  await page.locator('input[name="webhook_enabled"]').check();
  await page.fill('input[name="dest_0_name"]', 'Smoke');
  await page.fill('input[name="dest_0_url"]', `${STUB}/hook`);
  await page.locator('input[name="dest_0_enabled"]').check();
  await page.locator('input[name="dest_0_types"][value="Generic"]').check();
  await page.getByRole('button', { name: 'Save' }).click();
  await expect(page).toHaveURL(/\/settings\/integrations$/);

  await page.locator('#test-webhooks').click();
  await expect(page.locator('#webhook-test-result')).toContainText(/Sent to 1/, { timeout: 15000 });

  const events = await waitForStubEvents(request, 1);
  const evt = events[events.length - 1];
  expect(evt.body.ServerName).toBe('Medora');
  expect(evt.body.NotificationType).toBe('Generic');
  expect(evt.body.ClientName).toBe('Medora');
  const keyHeader = evt.headers['X-Medora-Webhook-Key'] || evt.headers['x-medora-webhook-key'];
  expect(keyHeader).toBeTruthy();
});

test('user created fires UserCreated webhook', async ({ page, request }) => {
  await ensureAdmin(page);
  await clearStubEvents(request);

  await page.goto('/settings/integrations');
  await page.locator('input[name="webhook_enabled"]').check();
  await page.fill('input[name="dest_0_name"]', 'Smoke');
  await page.fill('input[name="dest_0_url"]', `${STUB}/hook`);
  await page.locator('input[name="dest_0_enabled"]').check();
  await page.locator('input[name="dest_0_types"][value="UserCreated"]').check();
  await page.getByRole('button', { name: 'Save' }).click();
  await expect(page).toHaveURL(/\/settings\/integrations$/);

  await page.goto('/settings/users');
  await page.locator('#add-user-open').click();
  await page.fill('#add-user-username', `webhookuser${Date.now()}`);
  await page.fill('#add-user-password', 'userpass1');
  await page.fill('#add-user-confirm', 'userpass1');
  await page.locator('#add-user-submit').click();
  await expect(page).toHaveURL(/\/settings\/users$/);

  const events = await waitForStubEvents(request, 1, 20000);
  const created = events.find((e) => e.body && e.body.NotificationType === 'UserCreated');
  expect(created).toBeTruthy();
  expect(created.body.ServerName).toBe('Medora');
  expect(created.body.Username).toMatch(/^webhookuser/);
});
