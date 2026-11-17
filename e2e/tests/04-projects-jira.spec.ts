import { test, expect, ADMIN_USER, ADMIN_PASSWORD, loginLocal, logout, getMockState } from '../fixtures/test';

test.describe('04 - Manual Teams Mode, Jira Ticket Selection & Reports', () => {
  test('collaborator logs manual Jira ticket activity on /projects and admin views reports & logs', async ({
    page,
    request,
  }) => {
    await loginLocal(page, ADMIN_USER, ADMIN_PASSWORD);

    const usersResp = await page.request.get('/api/users');
    const users = (await usersResp.json()) as Array<{ id: number; email: string }>;
    const alice = users.find((u) => u.email === 'e2e.alice@example.com');
    expect(alice).toBeTruthy();

    // Admin creates Team in "Timesheets managed manually" mode linked to Jira project PRJ
    const teamRes = await page.request.post('/admin/teams', {
      data: {
        name: 'E2E Engineering Team',
        jira_space_key: 'PRJ',
        timesheets_managed_manually: true,
        require_activity_comment: false,
      },
    });
    expect(teamRes.ok()).toBeTruthy();
    const team = (await teamRes.json()) as { id: number };

    const addMemRes = await page.request.post(`/admin/teams/${team.id}/members`, {
      data: { user_id: alice!.id },
    });
    expect(addMemRes.ok()).toBeTruthy();

    await logout(page);
    await loginLocal(page, 'e2e.alice@example.com', 'AliceE2ENew#2026!');

    // Open /projects for 2026-12 (where Alice has 2026-12-01 billable presence)
    await page.goto('/projects?year=2026&month=12');
    const dayCard = page.locator('div[x-data*="projectsManualApp"] .space-y-4 > div', { hasText: '2026-12-01' });
    await expect(dayCard).toBeVisible();

    // Expand day card if collapsed and click + Add activity
    const addBtn = dayCard.locator('button', { hasText: '+' }).first();
    if (!(await addBtn.isVisible())) {
      await dayCard.locator('span:has-text("2026-12-01")').click();
    }
    await expect(addBtn).toBeVisible();
    await addBtn.click();

    // Select PRJ-101 from Jira mock suggestions
    const jiraInput = dayCard.locator('input[x-model="act._jiraSearch"]');
    await jiraInput.focus();
    const jiraOption = dayCard.locator('text=PRJ-101').first();
    await expect(jiraOption).toBeVisible();
    await jiraOption.dispatchEvent('mousedown');

    // Preset 100% & click Save
    await dayCard.locator('button:has-text("100")').click();
    await Promise.all([
      page.waitForResponse((r) => r.url().includes('/api/project-activities/day') && r.status() === 200),
      dayCard.locator('button.bg-primary').last().click(),
    ]);
    await expect(dayCard).toContainText('100% / 100%');

    // Verify mock Jira received the search request
    await expect
      .poll(async () => (await getMockState(request)).jira_requests.length)
      .toBeGreaterThan(0);

    // Log back as Admin -> inspect /admin/activity, /admin/projects-report, /admin/teams/{id}/logs, /admin/users/{id}/logs
    await logout(page);
    await loginLocal(page, ADMIN_USER, ADMIN_PASSWORD);

    await page.goto('/admin/activity?year=2026&month=12');
    await expect(page.locator('h1')).toBeVisible();

    await page.goto('/admin/projects-report?year=2026&month=12');
    await expect(page.locator('h1')).toBeVisible();

    await page.goto(`/admin/teams/${team.id}/logs`);
    await expect(page.locator('h1')).toBeVisible();

    await page.goto(`/admin/users/${alice!.id}/logs`);
    await expect(page.locator('h1')).toBeVisible();
  });
});
