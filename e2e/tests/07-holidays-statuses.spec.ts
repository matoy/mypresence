import { test, expect, ADMIN_USER, ADMIN_PASSWORD, loginLocal, logout } from '../fixtures/test';

test.describe('07 - Holidays & Statuses UI Management + Real Calendar Impact', () => {
  test('holidays CRUD, duplicate, filters & calendar blocking/imputation behavior', async ({ page }) => {
    await loginLocal(page, ADMIN_USER, ADMIN_PASSWORD);
    await page.goto('/admin/holidays');

    const root = page.locator('div[x-data*="holidayAdmin"]');
    const modal = page.locator('div[x-show="showCreateModal"]');

    // 1. Create a global holiday (non-imputable)
    await root.locator('> div:first-child button').click();
    await expect(modal).toBeVisible();
    await modal.locator('input[x-model="newDate"]').fill('2027-01-04');
    await modal.locator('input[x-model="newName"]').fill('E2E Global Holiday');
    await Promise.all([
      page.waitForEvent('load'),
      modal.locator('div.flex.justify-end button').last().click(),
    ]);
    await page.waitForLoadState('networkidle');

    // 2. Create a country-targeted holiday for FR via pill click (non-imputable)
    await root.locator('> div:first-child button').click();
    await expect(modal).toBeVisible();
    await modal.locator('input[x-model="newDate"]').fill('2027-01-05');
    await modal.locator('input[x-model="newName"]').fill('E2E France Holiday');
    await modal.locator('button', { hasText: 'FR' }).first().click();
    await expect(modal.locator('input[x-model="newCountryCode"]')).toHaveValue(/FR/);
    await Promise.all([
      page.waitForEvent('load'),
      modal.locator('div.flex.justify-end button').last().click(),
    ]);
    await page.waitForLoadState('networkidle');

    // 3. Test filters (text + country + year)
    await page.locator('input[x-model="filterText"]').fill('France Holiday');
    await page.locator('select[x-model="filterCountry"]').selectOption('FR');
    await page.locator('input[x-model="filterYear"]').fill('2027');
    await expect(page.locator('tbody tr', { hasText: 'E2E France Holiday' })).toBeVisible();
    await expect(page.locator('tbody tr', { hasText: 'E2E Global Holiday' })).toBeHidden();
    await page.locator('div.bg-white button').filter({ has: page.locator('text=/reset|réinitialiser/i') }).click().catch(async () => {
      await page.locator('select[x-model="filterCountry"]').selectOption('all');
      await page.locator('input[x-model="filterText"]').fill('');
      await page.locator('input[x-model="filterYear"]').fill('');
    });
    await page.locator('input[x-model="filterText"]').fill('');
    await page.locator('select[x-model="filterCountry"]').selectOption('all');
    await page.locator('input[x-model="filterYear"]').fill('');
    await expect(page.locator('tbody tr', { hasText: 'E2E Global Holiday' })).toBeVisible();

    // 4. Verify Alice (attached to FR site in test 03) sees 2027-01-05 blocked when allow_imputed=false
    await logout(page);
    await loginLocal(page, 'e2e.alice@example.com', 'AliceE2ENew#2026!');
    await page.goto('/?year=2027&month=1');
    const frCell = page.locator('td.calendar-cell[data-date="2027-01-05"]').first();
    await expect(frCell).toHaveAttribute('data-holiday', 'true');
    await expect(frCell).not.toHaveAttribute('data-holiday-allow-imputed', 'true');
    const frBox = (await frCell.boundingBox())!;
    await page.mouse.click(frBox.x + frBox.width / 2, frBox.y + frBox.height / 2);
    await expect(page.locator('div[x-show="showPicker"]')).toBeHidden();

    // 5. Admin edits E2E France Holiday inline -> sets allow_imputed=true & duplicates for 2028
    await logout(page);
    await loginLocal(page, ADMIN_USER, ADMIN_PASSWORD);
    await page.goto('/admin/holidays');
    const frRow = page.locator('tbody tr', { hasText: 'E2E France Holiday' });
    await frRow.locator('td').last().locator('button').nth(0).click(); // Edit
    const editingFrRow = page.locator('tbody tr').filter({ has: page.locator('input[x-model="editName"]') });
    await editingFrRow.locator('input[x-model="editName"]').fill('E2E France Imputable');
    await editingFrRow.locator('input[x-model="editAllowImputed"]').check();
    await Promise.all([
      page.waitForEvent('load'),
      editingFrRow.locator('td').last().locator('button').nth(0).click(), // Save
    ]);
    await page.waitForLoadState('networkidle');

    // Duplicate -> prefills 2028-01-05
    const impRow = page.locator('tbody tr', { hasText: 'E2E France Imputable' });
    await impRow.locator('td').last().locator('button').nth(1).click(); // Duplicate
    await expect(modal).toBeVisible();
    await expect(modal.locator('input[x-model="newDate"]')).toHaveValue('2028-01-05');
    await modal.locator('input[x-model="newName"]').fill('E2E France Duplicated 2028');
    await Promise.all([
      page.waitForEvent('load'),
      modal.locator('div.flex.justify-end button').last().click(),
    ]);
    await page.waitForLoadState('networkidle');
    await expect(page.locator('tbody tr', { hasText: 'E2E France Duplicated 2028' })).toBeVisible();

    // 6. Verify Alice can now select 2027-01-05 on calendar (allow_imputed=true)
    await logout(page);
    await loginLocal(page, 'e2e.alice@example.com', 'AliceE2ENew#2026!');
    await page.goto('/?year=2027&month=1');
    const impCell = page.locator('td.calendar-cell[data-date="2027-01-05"]').first();
    await expect(impCell).toHaveAttribute('data-holiday-allow-imputed', 'true');
    const impBox = (await impCell.boundingBox())!;
    await page.mouse.click(impBox.x + impBox.width / 2, impBox.y + impBox.height / 2);
    await expect(page.locator('div[x-show="showPicker"]')).toBeVisible();

    // 7. Clean up test holidays via UI delete button
    await logout(page);
    await loginLocal(page, ADMIN_USER, ADMIN_PASSWORD);
    await page.goto('/admin/holidays');
    for (const hName of ['E2E Global Holiday', 'E2E France Imputable', 'E2E France Duplicated 2028']) {
      const r = page.locator('tbody tr', { hasText: hName });
      page.once('dialog', (d) => d.accept());
      await Promise.all([
        page.waitForEvent('load'),
        r.locator('td').last().locator('button').nth(2).click(), // Delete
      ]);
      await page.waitForLoadState('networkidle');
      await expect(page.locator('tbody tr', { hasText: hName })).toHaveCount(0);
    }
  });

  test('statuses CRUD, filters, disable/enable impact on calendar & 409 delete-in-use guard', async ({
    page,
    healthGuard,
  }) => {
    await loginLocal(page, ADMIN_USER, ADMIN_PASSWORD);
    await page.goto('/admin/statuses');

    const root = page.locator('div[x-data*="statusAdmin"]');
    const modal = page.locator('div[x-show="showCreateModal"]');

    // 1. Create custom active status "E2E Mission" and an unused status "E2E Temp Unused"
    await root.locator('> div:first-child button').click();
    await expect(modal).toBeVisible();
    await modal.locator('input[x-model="newName"]').fill('E2E Mission');
    await modal.locator('input[x-model="newColor"]').fill('#10b981');
    await modal.locator('input[x-model\\.number="newOrder"]').fill('80');
    await modal.locator('input[x-model="newBillable"]').check();
    await modal.locator('select[x-model="newShowAs"]').selectOption('workingElsewhere');
    await Promise.all([
      page.waitForEvent('load'),
      modal.locator('div.flex.justify-end button').last().click(),
    ]);
    await page.waitForLoadState('networkidle');

    // 2. Test UI filters & inline edit
    await page.locator('input[x-model="filterText"]').fill('E2E Mission');
    await page.locator('select[x-model="filterBillable"]').selectOption('1');
    await page.locator('select[x-model="filterShowAs"]').selectOption('workingElsewhere');
    const mRow = page.locator('tbody tr', { hasText: 'E2E Mission' });
    await expect(mRow).toBeVisible();

    await mRow.locator('td').last().locator('button').nth(0).click(); // Edit
    const editingRow = page.locator('tbody tr').filter({ has: page.locator('input[x-model="editName"]') });
    await editingRow.locator('input[x-model="editName"]').fill('E2E Mission Plus');
    await editingRow.locator('input[x-model\\.number="editOrder"]').fill('85');
    await Promise.all([
      page.waitForEvent('load'),
      editingRow.locator('td').last().locator('button').nth(0).click(), // Save
    ]);
    await page.waitForLoadState('networkidle');
    await expect(page.locator('tbody tr', { hasText: 'E2E Mission Plus' })).toBeVisible();

    // 3. Apply E2E Mission Plus on calendar (2027-02-01) via picker UI
    await page.goto('/?year=2027&month=2');
    const targetCell = page.locator('td.calendar-cell[data-date="2027-02-01"]').first();
    const box = (await targetCell.boundingBox())!;
    await page.mouse.click(box.x + box.width / 2, box.y + box.height / 2);
    const picker = page.locator('div[x-show="showPicker"]');
    await expect(picker).toBeVisible();
    const statusBtn = picker.locator('button', { hasText: 'E2E Mission Plus' });
    await expect(statusBtn).toBeVisible();
    await Promise.all([page.waitForEvent('load'), statusBtn.click()]);
    await page.waitForLoadState('networkidle');

    // 4. Disable status -> verify hidden from calendar picker -> re-enable
    await page.goto('/admin/statuses');
    const mpRow = page.locator('tbody tr', { hasText: 'E2E Mission Plus' });
    await Promise.all([
      page.waitForEvent('load'),
      mpRow.locator('td').last().locator('button').nth(1).click(), // Disable
    ]);
    await page.waitForLoadState('networkidle');

    await page.goto('/?year=2027&month=2');
    const cellAgain = page.locator('td.calendar-cell[data-date="2027-02-01"]').first();
    const box2 = (await cellAgain.boundingBox())!;
    await page.mouse.click(box2.x + box2.width / 2, box2.y + box2.height / 2);
    await expect(picker).toBeVisible();
    await expect(picker.locator('button', { hasText: 'E2E Mission Plus' })).toHaveCount(0);

    // 5. Try deleting in-use status -> expect 409 alert & row remains; then delete unused status
    await page.goto('/admin/statuses');
    const rowInUse = page.locator('tbody tr', { hasText: 'E2E Mission Plus' });
    // Re-enable first
    await Promise.all([
      page.waitForEvent('load'),
      rowInUse.locator('td').last().locator('button').nth(1).click(),
    ]);
    await page.waitForLoadState('networkidle');

    healthGuard.allowExpected4xx = true;
    const dialogs: string[] = [];
    const onDialog = async (d: { message(): string; accept(): Promise<void> }) => {
      dialogs.push(d.message());
      await d.accept();
    };
    page.on('dialog', onDialog);
    await Promise.all([
      page.waitForResponse((r) => r.url().includes('/admin/statuses/') && r.request().method() === 'DELETE' && r.status() === 409),
      page.locator('tbody tr', { hasText: 'E2E Mission Plus' }).locator('td').last().locator('button').nth(2).click(),
    ]);
    await expect.poll(() => dialogs.length).toBeGreaterThanOrEqual(2); // confirm + 409 alert
    page.off('dialog', onDialog);
    healthGuard.allowExpected4xx = false;
    await expect(page.locator('tbody tr', { hasText: 'E2E Mission Plus' })).toBeVisible();

    // Create unused status & delete cleanly
    await root.locator('> div:first-child button').click();
    await modal.locator('input[x-model="newName"]').fill('E2E Status Unused');
    await Promise.all([
      page.waitForEvent('load'),
      modal.locator('div.flex.justify-end button').last().click(),
    ]);
    await page.waitForLoadState('networkidle');
    const unusedRow = page.locator('tbody tr', { hasText: 'E2E Status Unused' });
    await expect(unusedRow).toBeVisible();
    page.once('dialog', (d) => d.accept());
    await Promise.all([
      page.waitForEvent('load'),
      unusedRow.locator('td').last().locator('button').nth(2).click(),
    ]);
    await page.waitForLoadState('networkidle');
    await expect(page.locator('tbody tr', { hasText: 'E2E Status Unused' })).toHaveCount(0);
  });
});
