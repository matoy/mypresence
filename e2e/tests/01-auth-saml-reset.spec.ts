import {
  test,
  expect,
  ADMIN_USER,
  ADMIN_PASSWORD,
  loginLocal,
  logout,
  setMockPersona,
  getMockState,
} from '../fixtures/test';

test.describe('01 - Auth, Password Reset via SMTP & SAML SSO RBAC', () => {
  test('unauthenticated requests redirect to /login and API returns 401', async ({ page, request }) => {
    await page.goto('/');
    await expect(page).toHaveURL(/\/login/);
    await expect(page.locator('#username')).toBeVisible();

    const apiResp = await request.get('/api/presences', { maxRedirects: 0 });
    expect([303, 401]).toContain(apiResp.status());

    const badBearer = await request.get('/api/presences', {
      headers: { Authorization: 'Bearer invalid-token' },
      maxRedirects: 0,
    });
    expect(badBearer.status()).toBe(401);
  });

  test('admin creates collaborator & password reset via mock SMTP works end-to-end', async ({ page, request }) => {
    await loginLocal(page, ADMIN_USER, ADMIN_PASSWORD);

    const createRes = await page.request.post('/admin/users', {
      data: {
        email: 'e2e.alice@example.com',
        name: 'Alice E2E',
        password: 'InitialPassword#1',
      },
    });
    expect(createRes.ok()).toBeTruthy();

    await logout(page);

    // Trigger Forgot Password
    await page.goto('/forgot-password');
    await page.locator('#email').fill('e2e.alice@example.com');
    await page.locator('form[action="/forgot-password"] button[type="submit"]').click();
    await expect(page.locator('a[href="/login"]')).toBeVisible();

    // Poll mock SMTP capture state until the email arrives
    let resetUrl = '';
    await expect
      .poll(
        async () => {
          const state = await getMockState(request);
          const matching = state.emails.filter((e) =>
            (e.to || []).some((addr) => addr.includes('e2e.alice@example.com')),
          );
          if (matching.length === 0) return '';
          const links = matching[matching.length - 1].reset_links || [];
          resetUrl = links.find((l) => l.includes('/reset-password?token=')) || '';
          return resetUrl;
        },
        { timeout: 10_000, message: 'Expected SMTP reset-password link to be captured' },
      )
      .toContain('/reset-password?token=');

    const parsed = new URL(resetUrl);
    await page.goto(`${parsed.pathname}${parsed.search}`);
    await page.locator('input[name="password"]').fill('AliceE2ENew#2026!');
    await page.locator('input[name="confirm"]').fill('AliceE2ENew#2026!');
    await page.locator('form[action="/reset-password"] button[type="submit"]').click();
    await expect(page.locator('form[action="/reset-password"]')).toHaveCount(0);

    // Log in cleanly with new password
    await loginLocal(page, 'e2e.alice@example.com', 'AliceE2ENew#2026!');
    await expect(page.locator('nav')).toContainText('Alice E2E');
  });

  test('SAML SSO provisions Entra Global Admin and Basic Collab roles accurately', async ({
    page,
    request,
    healthGuard,
  }) => {
    // 1. SAML Global Admin persona
    await setMockPersona(request, 'admin');
    await page.goto('/login');
    const ssoBtn = page.locator('a[href="/saml/login"]');
    await expect(ssoBtn).toBeVisible();

    await Promise.all([
      page.waitForURL((u) => u.pathname === '/'),
      ssoBtn.click(),
    ]);
    await expect(page.locator('nav')).toContainText('SAML Admin');
    await expect(page.locator('nav')).toContainText('Global');

    await page.goto('/admin/users');
    await expect(page.locator('h1')).toBeVisible();

    await logout(page);

    // 2. SAML Basic Collaborator persona -> denied /admin/users (403)
    await setMockPersona(request, 'collab');
    await page.goto('/login');
    await Promise.all([
      page.waitForURL((u) => u.pathname === '/'),
      page.locator('a[href="/saml/login"]').click(),
    ]);
    await expect(page.locator('nav')).toContainText('SAML Collab');
    await expect(page.locator('nav')).not.toContainText('Global');

    healthGuard.allowExpected4xx = true;
    const forbiddenResp = await page.goto('/admin/users');
    expect(forbiddenResp?.status()).toBe(403);
  });
});
