import { test, expect, ADMIN_USER, ADMIN_PASSWORD, loginLocal, logout } from '../fixtures/test';

test.describe('06 - Domains Management, Manager & Team Assignment & Scoped RBAC', () => {
  test('full UI lifecycle on /admin/domains + real Domain Manager visibility on reports & cleanup', async ({
    page,
    healthGuard,
  }) => {
    await loginLocal(page, ADMIN_USER, ADMIN_PASSWORD);

    // 1. Seed a dedicated collaborator without any admin role + a dedicated team
    const mgrEmail = 'e2e.dommgr@example.com';
    const mgrPassword = 'DomainMgr#2026!';
    const mgrRes = await page.request.post('/admin/users', {
      data: { email: mgrEmail, name: 'E2E Domain Manager', password: mgrPassword },
    });
    expect(mgrRes.ok()).toBeTruthy();

    const teamRes = await page.request.post('/admin/teams', {
      data: { name: 'E2E Domain Team', timesheets_managed_manually: true },
    });
    expect(teamRes.ok()).toBeTruthy();

    // 2. Open /admin/domains -> create domain assigning manager + team via UI modal
    await page.goto('/admin/domains');
    await page.locator('div[x-data="domainsAdmin()"] > div:first-child button').click();
    const modal = page.locator('div[x-show="showModal"]');
    await expect(modal).toBeVisible();

    await modal.locator('input[x-model="form.name"]').fill('E2E Domain Alpha');
    await modal.locator('input[x-model="managerSearch"]').fill('e2e.dommgr');
    await modal.locator('button', { hasText: mgrEmail }).first().click();
    await modal.locator('input[x-model="teamSearch"]').fill('E2E Domain Team');
    await modal.locator('button', { hasText: 'E2E Domain Team' }).first().click();

    await Promise.all([
      page.waitForEvent('load'),
      modal.locator('div.flex.justify-end button').last().click(),
    ]);
    await page.waitForLoadState('networkidle');

    const alphaCard = page.locator('div[x-data="domainsAdmin()"] > div.bg-white', { hasText: 'E2E Domain Alpha' });
    await expect(alphaCard).toBeVisible();
    await expect(alphaCard).toContainText('E2E Domain Manager');
    await expect(alphaCard).toContainText('E2E Domain Team');

    // 3. Edit domain: rename to E2E Domain Beta, verify team & manager update works
    await alphaCard.locator('button[title="Edit"]').click();
    await expect(modal).toBeVisible();
    await modal.locator('input[x-model="form.name"]').fill('E2E Domain Beta');
    // Remove and re-add team chip to exercise both chip removal & search attachment
    await modal.locator('span.bg-blue-50', { hasText: 'E2E Domain Team' }).locator('button').click();
    await expect(modal.locator('span.bg-blue-50', { hasText: 'E2E Domain Team' })).toHaveCount(0);
    await modal.locator('input[x-model="teamSearch"]').fill('E2E Domain Team');
    await modal.locator('button', { hasText: 'E2E Domain Team' }).first().click();

    await Promise.all([
      page.waitForEvent('load'),
      modal.locator('div.flex.justify-end button').last().click(),
    ]);
    await page.waitForLoadState('networkidle');

    const betaCard = page.locator('div[x-data="domainsAdmin()"] > div.bg-white', { hasText: 'E2E Domain Beta' });
    await expect(betaCard).toBeVisible();
    await expect(betaCard).toContainText('E2E Domain Manager');
    await expect(betaCard).toContainText('E2E Domain Team');

    // 4. Verify Domain Manager gains scoped access to /admin/activity & /admin/projects-report, while /admin/domains stays 403
    await logout(page);
    await loginLocal(page, mgrEmail, mgrPassword);

    await page.goto('/admin/activity');
    await expect(page.locator('h1')).toBeVisible();
    await expect(page.locator('option[value^="domain:"]', { hasText: 'E2E Domain Beta' })).toBeAttached();

    const prResp = await page.goto('/admin/projects-report');
    expect(prResp?.status()).toBe(200);
    await expect(page.locator('h1')).toBeVisible();

    healthGuard.allowExpected4xx = true;
    expect((await page.goto('/admin/domains'))?.status()).toBe(403);
    expect((await page.goto('/admin/users'))?.status()).toBe(403);
    healthGuard.allowExpected4xx = false;

    // 5. Log back in as Admin -> delete domain -> confirm team survives & ex-manager loses access (403)
    await page.goto('/');
    await logout(page);
    await loginLocal(page, ADMIN_USER, ADMIN_PASSWORD);
    await page.goto('/admin/domains');

    const cardToDelete = page.locator('div[x-data="domainsAdmin()"] > div.bg-white', { hasText: 'E2E Domain Beta' });
    page.once('dialog', (d) => d.accept());
    await Promise.all([
      page.waitForEvent('load'),
      cardToDelete.locator('button[title="Delete"]').click(),
    ]);
    await page.waitForLoadState('networkidle');
    await expect(page.locator('div[x-data="domainsAdmin()"] > div.bg-white', { hasText: 'E2E Domain Beta' })).toHaveCount(0);

    await page.goto('/admin/teams');
    await expect(page.locator('body')).toContainText('E2E Domain Team');

    await logout(page);
    await loginLocal(page, mgrEmail, mgrPassword);
    healthGuard.allowExpected4xx = true;
    expect((await page.goto('/admin/activity'))?.status()).toBe(403);
  });
});
