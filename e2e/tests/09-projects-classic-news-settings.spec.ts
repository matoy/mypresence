import {
  test,
  expect,
  ADMIN_USER,
  ADMIN_PASSWORD,
  TINY_PNG,
  firstWeekdaysOf,
  expectXlsxDownload,
  loginLocal,
  logout,
} from '../fixtures/test';

test.describe('09 - Classic Projects, Decertification, Excel Exports, Reminders, News, PAT UI, Settings & Impersonate', () => {
  test('classic project flow, admin decertification, Excel exports & team reminders trigger', async ({
    page,
    healthGuard,
  }) => {
    await loginLocal(page, ADMIN_USER, ADMIN_PASSWORD);

    // 1. Seed classic user & non-manual team with presence reminder threshold
    const classicEmail = 'e2e.classic@example.com';
    const classicPass = 'ClassicE2E#2026!';
    const uRes = await page.request.post('/admin/users', {
      data: { email: classicEmail, name: 'Classic E2E', password: classicPass },
    });
    expect(uRes.ok()).toBeTruthy();
    const classicUser = (await uRes.json()) as { id: number };

    const tRes = await page.request.post('/admin/teams', {
      data: {
        name: 'E2E Classic Team',
        timesheets_managed_manually: false,
        remind_presence: true,
        presence_reminder_days: 5,
        remind_activity: false,
        activity_reminder_days: 0,
      },
    });
    expect(tRes.ok()).toBeTruthy();
    const classicTeam = (await tRes.json()) as { id: number };
    expect(
      (await page.request.post(`/admin/teams/${classicTeam.id}/members`, { data: { user_id: classicUser.id } })).ok(),
    ).toBeTruthy();

    // Trigger team reminders from /admin/teams UI button (bell icon -> confirm + alert dialogs)
    await page.goto('/admin/teams');
    const teamCard = page.locator('div.bg-white.rounded-xl', { hasText: 'E2E Classic Team' });
    await expect(teamCard).toBeVisible();
    const reminderDialogs: string[] = [];
    const onReminderDialog = async (d: { message(): string; accept(): Promise<void> }) => {
      reminderDialogs.push(d.message());
      await d.accept();
    };
    page.on('dialog', onReminderDialog);
    await Promise.all([
      page.waitForResponse((r) => r.url().includes(`/trigger-reminders`) && r.status() === 200),
      teamCard.locator('button', { hasText: '🔔' }).first().click(),
    ]);
    await expect.poll(() => reminderDialogs.length).toBeGreaterThanOrEqual(2);
    page.off('dialog', onReminderDialog);

    // 2. Create Classic Project & assign Classic E2E member via /admin/projects UI
    await page.goto('/admin/projects');
    await page.locator('div[x-data*="adminProjectsApp"] > div:first-child button').click();
    const projModal = page.locator('div[x-show="showModal"]');
    await expect(projModal).toBeVisible();
    await projModal.locator('input[x-model="form.name"]').fill('E2E Classic Project');
    await projModal.locator('input[x-model="form.code"]').fill('E2E-CL');
    await projModal.locator('input[x-model="teamSearch"]').fill('E2E Classic Team');
    await projModal.locator('div[x-show="showTeamList"] div', { hasText: 'E2E Classic Team' }).first().click();

    await Promise.all([
      page.waitForResponse((r) => r.url().includes('/admin/projects') && r.request().method() === 'POST' && r.status() === 200),
      page.waitForEvent('load'),
      projModal.locator('div.flex.justify-end button').last().click(),
    ]);
    await page.waitForLoadState('networkidle');

    // Edit newly created project to assign Classic E2E user
    const projRow = page.locator('tbody tr', { hasText: 'E2E-CL' });
    await expect(projRow).toBeVisible();
    await projRow.locator('button').click();
    await expect(projModal).toBeVisible();
    await projModal.locator('input[x-model="memberSearch"]').fill('e2e.classic');
    await projModal.locator('label', { hasText: classicEmail }).locator('input[type="checkbox"]').check();
    await Promise.all([
      page.waitForResponse((r) => r.url().includes('/members') && r.request().method() === 'PUT' && r.status() === 200),
      page.waitForEvent('load'),
      projModal.locator('div.flex.justify-end button').last().click(),
    ]);
    await page.waitForLoadState('networkidle');

    // 3. Collaborator logs in, fills 2027-03 presences, uses Classic Projects UI (favorite, cap error, valid entry, certify both)
    await logout(page);
    await loginLocal(page, classicEmail, classicPass);

    const marWeekdays = firstWeekdaysOf(2027, 3, 31);
    const fillRes = await page.evaluate(
      async ({ uid, dates }) => {
        const res = await fetch('/api/presences', {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({ user_id: uid, dates, status_id: 1, half: 'full' }),
        });
        return res.status;
      },
      { uid: classicUser.id, dates: marWeekdays },
    );
    expect(fillRes).toBe(200);

    await page.goto('/?year=2027&month=3');
    const calCertBtn = page.locator('div[x-show="declarable > 0 && declared >= declarable && !certified"] button').first();
    await calCertBtn.click();
    await Promise.all([
      page.waitForEvent('load'),
      page.locator('div[x-show="confirmShow"] button').last().click(),
    ]);
    await page.waitForLoadState('networkidle');

    // Open Classic Projects page
    await page.goto('/projects?year=2027&month=3');
    const clRow = page.locator('tbody tr', { hasText: 'E2E-CL' });
    await expect(clRow).toBeVisible();

    // Toggle favorite star (2nd button in actions cell)
    await Promise.all([
      page.waitForResponse((r) => r.url().includes('/api/project-favorite/') && r.status() === 200),
      clRow.locator('td').last().locator('button').nth(1).click(),
    ]);

    // Try exceeding billable cap (click Save button -> toastError / 4xx)
    healthGuard.allowExpected4xx = true;
    await clRow.locator('input[type="number"]').fill(String(marWeekdays.length + 5));
    await Promise.all([
      page.waitForResponse((r) => r.url().includes('/api/project-time') && r.status() >= 400),
      clRow.locator('td').last().locator('button').nth(0).click(),
    ]);
    healthGuard.allowExpected4xx = false;

    // Enter exact billable days & click Save -> certify project month
    await clRow.locator('input[type="number"]').fill(String(marWeekdays.length));
    await Promise.all([
      page.waitForResponse((r) => r.url().includes('/api/project-time') && r.status() === 200),
      clRow.locator('td').last().locator('button').nth(0).click(),
    ]);

    const projCertBtn = page.locator('div[x-show*="totalDeclared"][x-show*="!certified"] button').first();
    await expect(projCertBtn).toBeVisible();
    await projCertBtn.click();
    await Promise.all([
      page.waitForEvent('load'),
      page.locator('div[x-show="confirmShow"] button').last().click(),
    ]);
    await page.waitForLoadState('networkidle');
    await expect(projCertBtn).toBeHidden();

    // 4. Admin decertifies BOTH project & presence from /admin/activity UI modals & checks Excel exports
    await logout(page);
    await loginLocal(page, ADMIN_USER, ADMIN_PASSWORD);
    await page.goto(`/admin/activity?year=2027&month=3&team=${classicTeam.id}`);

    // Excel export on /admin/activity
    await expectXlsxDownload(page, page.locator('button[onclick*="exportTable"]').first());

    // Open Decertify Project modal & confirm
    const decertProjBtn = page.locator('button[onclick*="openDecertifyProjectModal"]').first();
    await expect(decertProjBtn).toBeVisible();
    await decertProjBtn.click();
    await expect(page.locator('#decertify-project-modal')).toBeVisible();
    await Promise.all([
      page.waitForEvent('load'),
      page.locator('#decertify-project-modal button').last().click(),
    ]);
    await page.waitForLoadState('networkidle');

    // Open Decertify Presence modal & confirm
    const decertPresBtn = page.locator('button[onclick*="openDecertifyModal"]').first();
    await expect(decertPresBtn).toBeVisible();
    await decertPresBtn.click();
    await expect(page.locator('#decertify-modal')).toBeVisible();
    await Promise.all([
      page.waitForEvent('load'),
      page.locator('#decertify-modal button').last().click(),
    ]);
    await page.waitForLoadState('networkidle');

    // Excel export on /admin/projects-report & /admin/sites-report
    await page.goto('/admin/projects-report');
    await expectXlsxDownload(page, page.locator('button[onclick="exportProjectsReport()"]'));

    await page.goto('/admin/sites-report');
    await expectXlsxDownload(page, page.locator('button[onclick*="sites-summary-table"]'));
  });

  test('news banners, PAT tokens UI, general settings logo+live env, personal password & impersonation', async ({
    page,
    request,
  }) => {
    await loginLocal(page, ADMIN_USER, ADMIN_PASSWORD);
    const originalAppName = (await page.locator('nav a[href="/"]').first().innerText()).trim();

    // 1. News banner lifecycle on /admin/news -> visible on / -> edit -> delete
    await page.goto('/admin/news');
    await page.locator('div[x-data*="newsAdmin"] > div:first-child button').click();
    const newsModal = page.locator('div[x-show="showModal"]');
    await expect(newsModal).toBeVisible();
    await newsModal.locator('input[x-model="form.title"]').fill('E2E Maintenance');
    await newsModal.locator('textarea[x-model="form.content"]').fill('Scheduled E2E Banner [Portal](https://example.com)');
    await Promise.all([
      page.waitForEvent('load'),
      newsModal.locator('button.bg-primary').click(),
    ]);
    await page.waitForLoadState('networkidle');

    await page.goto('/');
    const banner = page.locator('.news-banner-container');
    await expect(banner).toBeVisible();
    await expect(banner).toContainText('Scheduled E2E Banner');

    await page.goto('/admin/news');
    const newsRow = page.locator('tbody tr', { hasText: 'E2E Maintenance' });
    await newsRow.locator('button').first().click(); // Edit
    await newsModal.locator('textarea[x-model="form.content"]').fill('Updated E2E Banner Notice');
    await Promise.all([
      page.waitForEvent('load'),
      newsModal.locator('button.bg-primary').click(),
    ]);
    await page.waitForLoadState('networkidle');

    const newsRow2 = page.locator('tbody tr', { hasText: 'E2E Maintenance' });
    page.once('dialog', (d) => d.accept());
    await Promise.all([
      page.waitForEvent('load'),
      newsRow2.locator('button').last().click(), // Delete
    ]);
    await page.waitForLoadState('networkidle');
    await expect(page.locator('tbody tr', { hasText: 'E2E Maintenance' })).toHaveCount(0);

    // 2. PAT UI lifecycle on /settings/tokens
    await page.goto('/settings/tokens');
    await page.locator('div[x-data*="patApp"] > div:first-child button').click();
    const patCreateModal = page.locator('div[x-show="showCreate"]');
    await expect(patCreateModal).toBeVisible();
    await patCreateModal.locator('input[x-model="newDesc"]').fill('E2E Browser Token');
    await patCreateModal.locator('button[type="submit"]').click();

    const patReveal = page.locator('div[x-show*="newTokenValue"]');
    await expect(patReveal).toBeVisible();
    const rawPat = (await patReveal.locator('code').innerText()).trim();
    expect(rawPat.length).toBeGreaterThan(10);

    const apiCall = await request.get('/api/tokens', {
      headers: { Authorization: `Bearer ${rawPat}` },
    });
    expect(apiCall.status()).toBe(200);

    await Promise.all([
      page.waitForEvent('load'),
      patReveal.locator('button').last().click(), // Close -> reload
    ]);
    await page.waitForLoadState('networkidle');

    const patListItem = page.locator('ul li', { hasText: 'E2E Browser Token' }).first();
    await expect(patListItem).toBeVisible();
    page.once('dialog', (d) => d.accept());
    await Promise.all([
      page.waitForResponse((r) => r.url().includes('/api/tokens/') && r.request().method() === 'DELETE' && r.status() === 200),
      patListItem.locator('button').click(),
    ]);
    await expect(page.locator('ul li', { hasText: 'E2E Browser Token' })).toHaveCount(0);

    // 3. General settings: upload PNG logo + live APP_NAME override + restore + delete logo
    await page.goto('/admin/settings');
    await Promise.all([
      page.waitForEvent('load'),
      page.locator('input[type="file"][name="logo"]').setInputFiles({
        name: 'e2e-logo.png',
        mimeType: 'image/png',
        buffer: TINY_PNG,
      }),
    ]);
    await page.waitForLoadState('networkidle');
    const logoImg = page.locator('img[src="/data/logo.png"]').first();
    await expect(logoImg).toBeVisible();
    await expect.poll(() => logoImg.evaluate((el: HTMLImageElement) => el.naturalWidth)).toBeGreaterThan(0);

    // Delete uploaded logo
    const logoCard = page.locator('div[x-data*="confirmDelete"]');
    await logoCard.locator('div[x-show="!confirmDelete"] button').click();
    await Promise.all([
      page.waitForEvent('load'),
      logoCard.locator('div[x-show="confirmDelete"] button').first().click(),
    ]);
    await page.waitForLoadState('networkidle');

    // Edit APP_NAME live & verify in navbar, then restore originalAppName
    const appNameRow = page.locator('tbody tr', { hasText: 'APP_NAME' }).first();
    await appNameRow.locator('button').click();
    await appNameRow.locator('input[x-model="editValue"]').fill('Presence E2E Live');
    await Promise.all([
      page.waitForResponse((r) => r.url().includes('/admin/settings/env') && r.status() === 200),
      appNameRow.locator('button.bg-primary').click(),
    ]);
    await page.reload();
    await expect(page.locator('nav a[href="/"]').first()).toContainText('Presence E2E Live');

    const appNameRowRestore = page.locator('tbody tr', { hasText: 'APP_NAME' }).first();
    await appNameRowRestore.locator('button').click();
    await appNameRowRestore.locator('input[x-model="editValue"]').fill(originalAppName);
    await Promise.all([
      page.waitForResponse((r) => r.url().includes('/admin/settings/env') && r.status() === 200),
      appNameRowRestore.locator('button.bg-primary').click(),
    ]);

    // 4. Impersonate Classic E2E user -> banner shown -> exit impersonation -> Admin restored
    await page.goto('/impersonate');
    await page.locator('input[x-model="search"]').fill('e2e.classic@example.com');
    const impRow = page.locator('tbody tr', { hasText: 'e2e.classic@example.com' });
    await expect(impRow).toBeVisible();
    await Promise.all([
      page.waitForURL((u) => u.pathname === '/'),
      impRow.locator('button[type="submit"]').click(),
    ]);
    const exitForm = page.locator('form[action="/impersonate-exit"]');
    await expect(exitForm).toBeVisible();
    await expect(page.locator('nav')).toContainText('Classic E2E');
    await Promise.all([
      page.waitForEvent('load'),
      exitForm.locator('button[type="submit"]').click(),
    ]);
    await page.waitForLoadState('networkidle');
    await expect(page.locator('nav')).toContainText('Global');
    await logout(page);

    // 5. Personal password change (/settings/change-password with CSRF) for Classic E2E
    await loginLocal(page, 'e2e.classic@example.com', 'ClassicE2E#2026!');
    await page.goto('/settings/change-password');
    await page.locator('input[name="current_password"]').fill('ClassicE2E#2026!');
    await page.locator('input[name="new_password"]').fill('ClassicSelfUpdated#2026!');
    await page.locator('input[name="confirm_password"]').fill('ClassicSelfUpdated#2026!');
    await Promise.all([
      page.waitForEvent('load'),
      page.locator('form[action="/settings/change-password"] button[type="submit"]').click(),
    ]);
    await expect(page.locator('div.bg-green-50')).toBeVisible();
    await logout(page);
    await loginLocal(page, 'e2e.classic@example.com', 'ClassicSelfUpdated#2026!');
  });
});
