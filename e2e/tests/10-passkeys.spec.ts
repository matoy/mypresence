import { test, expect, ADMIN_USER, ADMIN_PASSWORD, loginLocal, logout } from '../fixtures/test';

test.describe('10 - WebAuthn Passkeys Ceremony (Virtual Authenticator via CDP)', () => {
  test('registers a passkey on /settings/passkeys, logs in passwordless via #passkey-btn, then deletes it', async ({
    browser,
    baseURL,
  }) => {
    // WebAuthn RP ID cannot be a raw IP literal (127.0.0.1); run on http://localhost:<port>
    const parsed = new URL(baseURL || 'http://127.0.0.1:18080');
    parsed.hostname = 'localhost';
    const localhostURL = parsed.toString().replace(/\/+$/, '');

    const context = await browser.newContext({ baseURL: localhostURL });
    const page = await context.newPage();
    const errors: string[] = [];
    page.on('pageerror', (e) => errors.push(`pageerror: ${e.message}`));
    page.on('response', (r) => {
      if (r.status() >= 500) errors.push(`HTTP ${r.status()} on ${r.url()}`);
    });

    try {
      await page.goto('/login?local=1');
      const passkeyBtn = page.locator('#passkey-btn');
      if ((await passkeyBtn.count()) === 0) {
        if (process.env.EXPECT_PASSKEYS === '1') {
          throw new Error('ENABLE_PASSKEYS=true expected #passkey-btn on /login');
        }
        test.skip(true, 'Passkeys feature disabled on target server');
        return;
      }

      // Attach virtual CTAP2 authenticator supporting resident keys & user verification
      const cdp = await context.newCDPSession(page);
      await cdp.send('WebAuthn.enable');
      await cdp.send('WebAuthn.addVirtualAuthenticator', {
        options: {
          protocol: 'ctap2',
          transport: 'internal',
          hasResidentKey: true,
          hasUserVerification: true,
          isUserVerified: true,
        },
      });

      // 1. Login as Admin on localhost & register passkey
      await loginLocal(page, ADMIN_USER, ADMIN_PASSWORD);
      await page.goto('/settings/passkeys');
      await page.locator('#pk-name').fill('E2E Virtual Key');
      await page.locator('#pk-register-btn').click();
      await expect(page.locator('#pk-register-success')).toBeVisible();
      await page.waitForEvent('load'); // auto reload after 1.2s
      await expect(page.locator('body')).toContainText('E2E Virtual Key');

      // 2. Logout & authenticate passwordless via Passkey button
      await logout(page);
      await page.goto('/login');
      await Promise.all([
        page.waitForURL((u) => u.pathname === '/'),
        page.locator('#passkey-btn').click(),
      ]);
      await expect(page.locator('nav')).toContainText('Global');

      // 3. Delete the passkey via UI form (CSRF + confirm)
      await page.goto('/settings/passkeys');
      const keyItem = page.locator('li', { hasText: 'E2E Virtual Key' });
      await expect(keyItem).toBeVisible();
      page.once('dialog', (d) => d.accept());
      await Promise.all([
        page.waitForEvent('load'),
        keyItem.locator('button[type="submit"]').click(),
      ]);
      await expect(page.locator('li', { hasText: 'E2E Virtual Key' })).toHaveCount(0);
      expect(errors).toEqual([]);
    } finally {
      await context.close();
    }
  });
});
