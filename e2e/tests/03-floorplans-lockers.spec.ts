import { test, expect, ADMIN_USER, ADMIN_PASSWORD, loginLocal, logout } from '../fixtures/test';

// Valid 1x1 transparent PNG
const TINY_PNG = Buffer.from(
  'iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mP8z8BQDwAEhQGAhKmMIQAAAABJRU5ErkJggg==',
  'base64',
);

test.describe('03 - Sites, Floorplans, Seats, Lockers Pagination & Auto-Release Toast', () => {
  test('full physical workspace lifecycle: site -> floorplan -> seat -> locker claim/code/release -> site-change auto-release', async ({
    page,
  }) => {
    await loginLocal(page, ADMIN_USER, ADMIN_PASSWORD);

    // 1. Create Sites & look up Alice's user ID via API
    const parisRes = await page.request.post('/api/admin/sites', {
      data: { name: 'E2E Paris HQ', country_code: 'FR', seats: 80, workstations: 60 },
    });
    expect(parisRes.ok()).toBeTruthy();
    const parisSite = (await parisRes.json()) as { id: number };

    const lyonRes = await page.request.post('/api/admin/sites', {
      data: { name: 'E2E Lyon Hub', country_code: 'FR', seats: 30, workstations: 25 },
    });
    expect(lyonRes.ok()).toBeTruthy();
    const lyonSite = (await lyonRes.json()) as { id: number };

    const usersResp = await page.request.get('/api/users');
    expect(usersResp.ok()).toBeTruthy();
    const users = (await usersResp.json()) as Array<{ id: number; email: string }>;
    const alice = users.find((u) => u.email === 'e2e.alice@example.com');
    expect(alice).toBeTruthy();

    const assignSiteRes = await page.request.put(`/api/admin/users/${alice!.id}/site`, {
      data: { site_id: parisSite.id },
    });
    expect(assignSiteRes.ok()).toBeTruthy();

    // 2. Open /admin/floorplans -> create floorplan -> upload PNG -> place seat & locker range
    await page.goto('/admin/floorplans');
    await page.locator('button:has-text("+")').first().click();
    await page.locator('input[x-model="newName"]').fill('E2E Paris F1');
    await page.locator('select[x-model\\.number="newSiteID"]').selectOption(String(parisSite.id));
    await Promise.all([
      page.waitForResponse((r) => r.url().includes('/admin/floorplans') && r.status() === 200),
      page.locator('div[x-show="showCreate"] button[type="submit"]').click(),
    ]);
    await page.waitForLoadState('networkidle');

    // Select the floorplan & upload PNG
    await page.locator('li', { hasText: 'E2E Paris F1' }).first().click();
    const fileInput = page.locator('input[type="file"][accept="image/*"]');
    await Promise.all([
      page.waitForResponse((r) => r.url().includes('/image') && r.status() === 200),
      fileInput.setInputFiles({ name: 'floor.png', mimeType: 'image/png', buffer: TINY_PNG }),
    ]);

    const planImg = page.locator('img[x-ref="planImg"]');
    await expect(planImg).toBeVisible();
    await expect.poll(() => planImg.evaluate((el: HTMLImageElement) => el.naturalWidth)).toBeGreaterThan(0);

    // Add a seat by clicking the canvas center
    const imgBox = (await planImg.boundingBox())!;
    await page.mouse.click(imgBox.x + imgBox.width * 0.3, imgBox.y + imgBox.height * 0.4);
    await page.locator('input[x-model="seatModal.label"]').fill('E01');
    await Promise.all([
      page.waitForResponse((r) => r.url().includes('/seats') && r.status() === 200),
      page.locator('input[x-model="seatModal.label"]').locator('xpath=ancestor::form//button[@type="submit"]').click(),
    ]);

    // Switch editor mode to Locker Ranges and place a range pin
    await page.locator('button[x-model], button:has-text("🔐")').first().click();
    await page.mouse.click(imgBox.x + imgBox.width * 0.65, imgBox.y + imgBox.height * 0.35);
    await page.locator('input[x-model="rangeModal.label"]').fill('Zone A (01-55)');
    await page.locator('input[x-model="rangeModal.zone"]').fill('Zone A');
    await Promise.all([
      page.waitForResponse((r) => r.url().includes('/locker-ranges') && r.status() === 200),
      page.locator('input[x-model="rangeModal.label"]').locator('xpath=ancestor::form//button[@type="submit"]').click(),
    ]);

    // 3. Open /admin/lockers -> test UI Bulk Create (55 lockers) & Pagination Next/Prev
    await page.goto('/admin/lockers');
    await page.locator('div[x-data*="lockersAdmin"] button.bg-amber-50').first().click();
    const bulkDialog = page.locator('input[x-model="bulkModal.prefix"]').locator('xpath=ancestor::div[@role="dialog"]');
    await expect(bulkDialog).toBeVisible();
    await bulkDialog.locator('select[x-model\\.number="bulkModal.site_id"]').selectOption(String(parisSite.id));
    await bulkDialog.locator('input[x-model="bulkModal.prefix"]').fill('K-');
    await bulkDialog.locator('input[x-model\\.number="bulkModal.start_num"]').fill('1');
    await bulkDialog.locator('input[x-model\\.number="bulkModal.end_num"]').fill('55');
    await bulkDialog.locator('input[x-model="bulkModal.zone"]').fill('Zone A');
    await Promise.all([
      page.waitForResponse((r) => r.url().includes('/lockers/bulk') && r.status() === 200),
      bulkDialog.locator('button[type="submit"]').click(),
    ]);

    // Navigate to Page 2 using pagination button
    const nextBtn = page.locator('div[x-show="totalPages > 1"] button').nth(1);
    await expect(nextBtn).toBeEnabled();
    await Promise.all([
      page.waitForResponse((r) => r.url().includes('/api/admin/lockers') && r.url().includes('page=2')),
      nextBtn.click(),
    ]);
    await expect(page.locator('tbody tr')).toHaveCount(5);

    // 4. Switch to Alice -> self-service Locker Claim, Reveal, Edit Code, Escape & Floorplan Seat Reservation
    await logout(page);
    await loginLocal(page, 'e2e.alice@example.com', 'AliceE2ENew#2026!');

    // Declare an On-Site presence in Dec 2026 via API so Alice can reserve a desk on Dec 2026
    await page.evaluate(async (uid) => {
      await fetch('/api/presences', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ user_id: uid, dates: ['2026-12-01'], status_id: 1, half: 'full' }),
      });
    }, alice!.id);

    await page.goto('/?year=2026&month=12');
    const claimBtn = page.locator('div[x-show="claimableSites.length > 0"] button');
    await expect(claimBtn).toBeVisible();
    await claimBtn.click();

    const claimDialog = page.locator('input[x-model="claimModal.code"]').locator('xpath=ancestor::div[@role="dialog"]');
    await expect(claimDialog).toBeVisible();
    await claimDialog.locator('button', { hasText: '🔐 K-01' }).first().click();
    await claimDialog.locator('input[x-model="claimModal.code"]').fill('4829');
    await Promise.all([
      page.waitForResponse((r) => r.url().includes('/api/lockers/') && r.url().includes('/claim')),
      claimDialog.locator('button:has-text("🔐"), button.bg-amber-600').last().click(),
    ]);
    await expect(page.locator('text=🔐 #K-01')).toBeVisible();

    // Reveal hidden code with 👁️ button, verify 4829 shown, press Esc on edit modal
    await page.locator('button:has-text("👁️")').first().click();
    await expect(page.locator('span.font-mono:has-text("4829")')).toBeVisible();

    await page.locator('button:has-text("✏️")').first().click();
    const codeDialog = page.locator('input[x-model="codeModal.code"]').locator('xpath=ancestor::div[@role="dialog"]');
    await expect(codeDialog).toBeVisible();
    await page.keyboard.press('Escape');
    await expect(codeDialog).toBeHidden();

    // Open /floorplan for 2026-12-01 -> see locker range badge & reserve seat E01
    await page.goto('/floorplan?date=2026-12-01');
    await expect(page.locator('button:has-text("Zone A (01-55)")')).toBeVisible();
    await page.locator('button:has-text("E01")').first().click();
    const reserveDialog = page.locator('div[x-show="reserveModal.show"]');
    await expect(reserveDialog).toBeVisible();
    await Promise.all([
      page.waitForResponse((r) => r.url().includes('/api/reservations') && r.status() === 200),
      reserveDialog.locator('button.bg-primary').click(),
    ]);

    // Check calendar shows 🖥️ desktop icon for Dec 1
    await page.goto('/?year=2026&month=12');
    await expect(page.locator('td.calendar-cell[data-date="2026-12-01"]')).toContainText('🖥️');

    // 5. Log back in as Admin -> move Alice's site to Lyon -> verify 🔓 auto-release notification toast & preserved PIN!
    await logout(page);
    await loginLocal(page, ADMIN_USER, ADMIN_PASSWORD);

    const moveRes = await page.request.put(`/api/admin/users/${alice!.id}/site`, {
      data: { site_id: lyonSite.id },
    });
    expect(moveRes.ok()).toBeTruthy();

    await page.reload();
    const toastContainer = page.locator('#notifications-toast-container');
    await expect(toastContainer).toBeVisible();
    await expect(toastContainer).toContainText('🔓');
    await expect(toastContainer).toContainText('K-01');
    const k01Toast = toastContainer.locator('div.pointer-events-auto', { hasText: 'K-01' }).first();
    await Promise.all([
      page.waitForResponse((r) => r.url().includes('/ack') && r.status() === 200),
      k01Toast.locator('button[type="button"]').click(),
    ]);

    // Verify locker K-01 is now free in /admin/lockers and code 4829 is preserved
    const lkCheck = await page.request.get(`/api/admin/lockers?site_id=${parisSite.id}&q=K-01&page=1&limit=10`);
    const lkData = (await lkCheck.json()) as { items: Array<{ user_id: number; code: string }> };
    expect(lkData.items[0].user_id).toBe(0);
    expect(lkData.items[0].code).toBe('4829');
  });
});
