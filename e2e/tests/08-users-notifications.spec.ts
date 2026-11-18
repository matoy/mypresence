import {
  test,
  expect,
  ADMIN_USER,
  ADMIN_PASSWORD,
  loginLocal,
  logout,
  setMockPersona,
} from '../fixtures/test';

test.describe('08 - Users Lifecycle (Preprovision, Edit, Roles, Disable+AutoRelease, Delete) & Notifications UI', () => {
  test('full users admin UI workflow including pending SSO preprovision, batch import, disable auto-release & delete', async ({
    page,
    request,
  }) => {
    await loginLocal(page, ADMIN_USER, ADMIN_PASSWORD);
    await page.goto('/admin/users');

    // Wait for anti-autofill search input to mount
    const searchInput = page.locator('input[name="filter_search_query"]');
    await expect(searchInput).toBeVisible();

    // 1. Create local user Bob via UI modal
    await page.locator('div[x-data="usersRolesAdmin()"] > div:has(h1) button').nth(1).click();
    const createModal = page.locator('div[x-show="showCreateModal"]');
    await expect(createModal).toBeVisible();
    await createModal.locator('input[x-model="createName"]').fill('Bob E2E');
    await createModal.locator('input[x-model="createEmail"]').fill('e2e.bob@example.com');
    await createModal.locator('input[x-model="createPassword"]').fill('BobInitPass#2026!');
    await Promise.all([
      page.waitForResponse((r) => r.url().includes('/admin/users') && r.request().method() === 'POST' && r.status() === 200),
      createModal.locator('button[type="submit"]').click(),
    ]);
    await expect(createModal).toBeHidden();

    // 2. Search Bob, edit name inline, change password via admin inline form, toggle role & assign Paris site
    await searchInput.fill('e2e.bob@example.com');
    const bobRow = page.locator('tbody tr', { hasText: 'e2e.bob@example.com' });
    await expect(bobRow).toBeVisible();

    // Inline Edit name -> "Bob Edited"
    await bobRow.locator('td').last().locator('button').nth(0).click();
    await bobRow.locator('input[x-model="user._editName"]').fill('Bob Edited');
    await Promise.all([
      page.waitForResponse((r) => r.url().includes('/admin/users/') && r.request().method() === 'PUT' && r.status() === 200),
      bobRow.locator('div[x-show="user._editing"] button').first().click(),
    ]);
    await expect(bobRow).toContainText('Bob Edited');

    // Admin inline password change
    await bobRow.locator('td').last().locator('button').nth(1).click();
    await bobRow.locator('input[x-model="user._newPw"]').fill('BobAdminUpdated#2026!');
    page.once('dialog', (d) => d.accept());
    await Promise.all([
      page.waitForResponse((r) => r.url().includes('/password') && r.request().method() === 'PUT' && r.status() === 200),
      bobRow.locator('form button[type="submit"]').click(),
    ]);

    // Toggle "Team manager" role pill on & off
    const teamMgrPill = bobRow.locator('button', { hasText: 'Team manager' });
    await Promise.all([
      page.waitForResponse((r) => r.url().includes('/roles') && r.status() === 200),
      teamMgrPill.click(),
    ]);
    await expect(teamMgrPill).toHaveClass(/bg-indigo-100/);

    // Assign Paris HQ site via inline site dropdown
    const siteCell = bobRow.locator('td').nth(2);
    await siteCell.locator('button').first().click();
    const parisOpt = siteCell.locator('div', { hasText: 'E2E Paris HQ' }).last();
    await expect(parisOpt).toBeVisible();
    await Promise.all([
      page.waitForResponse((r) => r.url().includes('/site') && r.status() === 200),
      parisOpt.click(),
    ]);
    await expect(siteCell).toContainText('E2E Paris HQ');

    // Verify Admin's own row has NO Delete button
    await searchInput.fill(ADMIN_USER);
    const adminRow = page
      .locator('tbody tr')
      .filter({ has: page.locator('span[x-text="user.email"]', { hasText: new RegExp(`^${ADMIN_USER}$`) }) })
      .first();
    await expect(adminRow).toBeVisible();
    await expect(adminRow.locator('button.bg-red-100')).toBeHidden();
    await searchInput.fill('');

    // 3. Single + Batch SSO Pre-provisioning
    await page.locator('div[x-data="usersRolesAdmin()"] > div:has(h1) button').nth(0).click();
    const prevModal = page.locator('div[x-show="showPreprovisionModal"]');
    await expect(prevModal).toBeVisible();
    await prevModal.locator('input[x-model="prevEmail"]').fill('e2e.samlpending@example.com');
    await prevModal.locator('input[x-model="prevName"]').fill('Pending E2E SSO');
    await Promise.all([
      page.waitForResponse((r) => r.url().includes('/api/admin/users/preprovision') && r.status() === 200),
      prevModal.locator('div[x-show*="single"] button[type="submit"]').click(),
    ]);
    await expect(prevModal).toBeHidden();

    // Filter Pending federated users -> confirm Pending E2E SSO shows pending badge
    await page.locator('select[x-model="filterActive"]').selectOption('pending');
    const pendingRow = page.locator('tbody tr', { hasText: 'e2e.samlpending@example.com' });
    await expect(pendingRow).toBeVisible();
    await expect(pendingRow).toContainText('⏳');
    await page.locator('select[x-model="filterActive"]').selectOption('all');

    // Batch Pre-provision (1 new + 1 duplicate skipped)
    await page.locator('div[x-data="usersRolesAdmin()"] > div:has(h1) button').nth(0).click();
    await prevModal.locator('button', { hasText: '📋' }).click();
    await prevModal.locator('textarea[x-model="batchText"]').fill(
      'e2e.batchone@example.com,Batch E2E User\ne2e.alice@example.com,Alice Duplicate',
    );
    await Promise.all([
      page.waitForResponse((r) => r.url().includes('/api/admin/users/batch-preprovision') && r.status() === 200),
      prevModal.locator('div[x-show*="batch"] button[type="submit"]').click(),
    ]);
    await expect(prevModal).toContainText('1');
    await prevModal.locator('div[x-show*="batch"] button[type="button"]').first().click();

    // Verify Pending SSO user becomes Active after first SAML login
    await logout(page);
    await setMockPersona(request, {
      email: 'e2e.samlpending@example.com',
      display_name: 'Pending E2E SSO',
      groups: [],
    });
    await page.goto('/login');
    await Promise.all([
      page.waitForURL((u) => u.pathname === '/'),
      page.locator('a[href="/saml/login"]').click(),
    ]);
    await logout(page);

    // Log in as Bob using admin-updated password & claim locker K-02 on Paris HQ
    await loginLocal(page, 'e2e.bob@example.com', 'BobAdminUpdated#2026!');
    await page.goto('/');
    const claimBtn = page.locator('div[x-show="claimableSites.length > 0"] button');
    await expect(claimBtn).toBeVisible();
    await claimBtn.click();
    const claimDialog = page.locator('input[x-model="claimModal.code"]').locator('xpath=ancestor::div[@role="dialog"]');
    await claimDialog.locator('button', { hasText: '🔐 K-02' }).first().click();
    await claimDialog.locator('input[x-model="claimModal.code"]').fill('5150');
    await Promise.all([
      page.waitForResponse((r) => r.url().includes('/claim') && r.status() === 200),
      claimDialog.locator('button:has-text("🔐"), button.bg-amber-600').last().click(),
    ]);
    await expect(page.locator('text=🔐 #K-02')).toBeVisible();
    await logout(page);

    // 4. Admin confirms Pending user is now Active -> disables Bob (auto-releases K-02 + notification) -> enables & deletes Bob
    await loginLocal(page, ADMIN_USER, ADMIN_PASSWORD);
    // Toast banner for K-02 locker auto-release only fires AFTER disable:
    await page.goto('/admin/users');
    await expect(page.locator('input[name="filter_search_query"]')).toBeVisible();
    await page.locator('select[x-model="filterActive"]').selectOption('active');
    await expect(page.locator('tbody tr', { hasText: 'e2e.samlpending@example.com' })).toBeVisible();
    await page.locator('select[x-model="filterActive"]').selectOption('all');

    await page.locator('input[name="filter_search_query"]').fill('e2e.bob@example.com');
    const bobRow2 = page.locator('tbody tr', { hasText: 'e2e.bob@example.com' });
    await Promise.all([
      page.waitForResponse((r) => r.url().includes('/disabled') && r.status() === 200),
      bobRow2.locator('td').last().locator('button').nth(2).click(), // Disable
    ]);

    // Check auto-release toast on home page for Admin and acknowledge it
    await page.goto('/');
    const toast = page.locator('#notifications-toast-container');
    await expect(toast).toBeVisible();
    await expect(toast).toContainText('🔓');
    await expect(toast).toContainText('K-02');
    await Promise.all([
      page.waitForResponse((r) => r.url().includes('/ack') && r.status() === 200),
      toast.locator('div.pointer-events-auto', { hasText: 'K-02' }).first().locator('button[type="button"]').click(),
    ]);

    // Verify disabled Bob login is blocked
    await logout(page);
    await page.goto('/login?local=1');
    await page.locator('#username').fill('e2e.bob@example.com');
    await page.locator('#password').fill('BobAdminUpdated#2026!');
    await page.locator('form[action="/login"] button[type="submit"]').click();
    await expect(page).toHaveURL(/\/login/);

    // Re-enable & delete Bob cleanly via UI
    await loginLocal(page, ADMIN_USER, ADMIN_PASSWORD);
    await page.goto('/admin/users');
    await expect(page.locator('input[name="filter_search_query"]')).toBeVisible();
    await page.locator('input[name="filter_search_query"]').fill('e2e.bob@example.com');
    const bobRow3 = page.locator('tbody tr', { hasText: 'e2e.bob@example.com' });
    await Promise.all([
      page.waitForResponse((r) => r.url().includes('/disabled') && r.status() === 200),
      bobRow3.locator('td').last().locator('button').nth(2).click(), // Enable
    ]);
    page.once('dialog', (d) => d.accept());
    await Promise.all([
      page.waitForResponse((r) => r.url().includes('/admin/users/') && r.request().method() === 'DELETE' && r.status() === 200),
      bobRow3.locator('td').last().locator('button').nth(3).click(), // Delete
    ]);
    await expect(page.locator('tbody tr', { hasText: 'e2e.bob@example.com' })).toHaveCount(0);
  });

  test('notifications admin compose in user/team/site/all modes, recipient toast ack & history delete', async ({
    page,
  }) => {
    await loginLocal(page, ADMIN_USER, ADMIN_PASSWORD);
    await page.goto('/admin/notifications');

    const modeTabs = page.locator('div.grid.grid-cols-4 > button');
    const sendBtn = page.locator('main form button[type="submit"]');
    const confirmModal = page.locator('div[x-show="showConfirmModal"]');
    const directTitle = 'E2E Direct Notification';

    // 1. Send to Individual User (Alice) with optional link
    await modeTabs.nth(0).click();
    const userDrop = page.locator('div[x-show="recipientMode === \'user\'"] > div.relative');
    await userDrop.locator('> button').click();
    await userDrop.locator('input[x-model="userSearch"]').fill('e2e.alice@example.com');
    await userDrop.locator('button', { hasText: 'e2e.alice@example.com' }).first().click();
    await userDrop.locator('div.pt-1 button').click();

    await page.locator('select[x-model="notifType"]').selectOption('info');
    await page.locator('input[x-model="title"]').fill(directTitle);
    await page.locator('input[x-model="link"]').fill('/floorplan');
    await page.locator('textarea[x-model="message"]').fill('Please check the updated seating plan.');
    await sendBtn.click();
    await expect(confirmModal).toBeVisible();
    await Promise.all([
      page.waitForEvent('load'),
      confirmModal.locator('button.bg-primary').click(),
    ]);
    await page.waitForLoadState('networkidle');

    // 2. Send in Team mode (E2E Engineering Team)
    const teamTitle = 'E2E Team Broadcast';
    await page.locator('div.grid.grid-cols-4 > button').nth(1).click();
    const teamDrop = page.locator('div[x-show="recipientMode === \'team\'"] > div.relative');
    await teamDrop.locator('> button').click();
    await teamDrop.locator('input[x-model="teamSearch"]').fill('E2E Engineering Team');
    await teamDrop.locator('button', { hasText: 'E2E Engineering Team' }).first().click();
    await teamDrop.locator('div.pt-1 button').click();
    await page.locator('input[x-model="title"]').fill(teamTitle);
    await page.locator('textarea[x-model="message"]').fill('Weekly engineering sync reminder.');
    await sendBtn.click();
    await Promise.all([page.waitForEvent('load'), confirmModal.locator('button.bg-primary').click()]);
    await page.waitForLoadState('networkidle');

    // 3. Send in Site mode (E2E Lyon Hub)
    const siteTitle = 'E2E Site Alert';
    await page.locator('div.grid.grid-cols-4 > button').nth(2).click();
    const siteDrop = page.locator('div[x-show="recipientMode === \'site\'"] > div.relative');
    await siteDrop.locator('> button').click();
    await siteDrop.locator('input[x-model="siteSearch"]').fill('E2E Lyon Hub');
    await siteDrop.locator('button', { hasText: 'E2E Lyon Hub' }).first().click();
    await siteDrop.locator('div.pt-1 button').click();
    await page.locator('input[x-model="title"]').fill(siteTitle);
    await page.locator('textarea[x-model="message"]').fill('Lyon fire drill tomorrow.');
    await sendBtn.click();
    await Promise.all([page.waitForEvent('load'), confirmModal.locator('button.bg-primary').click()]);
    await page.waitForLoadState('networkidle');

    // 4. Send in All mode
    const allTitle = 'E2E Global Maintenance';
    await page.locator('div.grid.grid-cols-4 > button').nth(3).click();
    await page.locator('select[x-model="notifType"]').selectOption('warning');
    await page.locator('input[x-model="title"]').fill(allTitle);
    await page.locator('textarea[x-model="message"]').fill('Planned Sunday upgrade.');
    await sendBtn.click();
    await Promise.all([page.waitForEvent('load'), confirmModal.locator('button.bg-primary').click()]);
    await page.waitForLoadState('networkidle');

    // 5. Log in as Alice -> verify floating toast, link, acknowledge it & verify persistence
    await logout(page);
    await loginLocal(page, 'e2e.alice@example.com', 'AliceE2ENew#2026!');
    await page.goto('/');

    const toastContainer = page.locator('#notifications-toast-container');
    await expect(toastContainer).toBeVisible();
    await expect(toastContainer).toContainText(directTitle);
    await expect(toastContainer).toContainText(allTitle);

    const directCard = toastContainer.locator('div.pointer-events-auto', { hasText: directTitle }).first();
    await expect(directCard.locator('a[href="/floorplan"]')).toBeVisible();
    await Promise.all([
      page.waitForResponse((r) => r.url().includes('/ack') && r.status() === 200),
      directCard.locator('button[type="button"]').click(),
    ]);
    await expect(toastContainer.locator('div.pointer-events-auto', { hasText: directTitle })).toBeHidden();

    // Reload and verify direct notification stays acknowledged
    await page.reload();
    await expect(page.locator('#notifications-toast-container')).not.toContainText(directTitle);

    // 6. Admin verifies acknowledged badge in history search & deletes the notification via delete modal
    await logout(page);
    await loginLocal(page, ADMIN_USER, ADMIN_PASSWORD);
    await page.goto('/admin/notifications');

    await page.locator('input[x-model="searchQuery"]').fill(directTitle);
    const histRow = page.locator('tbody tr', { hasText: directTitle }).first();
    await expect(histRow).toBeVisible();
    await expect(histRow.locator('span.bg-green-100')).toBeVisible(); // Acknowledged badge

    await histRow.locator('button').click();
    const deleteModal = page.locator('div[x-show="showDeleteModal"]');
    await expect(deleteModal).toBeVisible();
    await Promise.all([
      page.waitForResponse((r) => r.url().includes('/delete') && r.status() === 200),
      deleteModal.locator('button.bg-red-600').click(),
    ]);
    await expect(page.locator('tbody tr', { hasText: directTitle })).toHaveCount(0);
  });
});
