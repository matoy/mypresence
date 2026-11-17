import { test, expect, loginLocal, getMockState } from '../fixtures/test';

function firstWeekdaysOf(year: number, month: number, count: number): string[] {
  const res: string[] = [];
  for (let d = 1; d <= 31 && res.length < count; d++) {
    const dt = new Date(Date.UTC(year, month - 1, d));
    if (dt.getUTCMonth() + 1 !== month) break;
    const dow = dt.getUTCDay();
    if (dow !== 0 && dow !== 6) {
      res.push(dt.toISOString().slice(0, 10));
    }
  }
  return res;
}

test.describe('02 - Calendar Drag-Paint, Context Menu, Dark Mode, Locale & O365 Sync', () => {
  const testYear = 2026;
  const testMonth = 11;

  test('Alice toggles theme/locale, drag-paints cells, splits AM/PM, syncs O365 & certifies month', async ({
    page,
    request,
  }) => {
    await loginLocal(page, 'e2e.alice@example.com', 'AliceE2ENew#2026!');
    await page.goto(`/?year=${testYear}&month=${testMonth}`);

    // Toggle dark mode via localStorage helper/menu & confirm persistence on reload
    await page.locator('nav button:has-text("Alice E2E")').click();
    const langSelect = page.locator('nav div[x-data*="userMenuOpen"] form[action="/set-lang"] select[name="lang"]').first();
    await langSelect.selectOption('en');
    await page.waitForLoadState('networkidle');
    await expect(page.locator('html')).toHaveAttribute('lang', 'en');

    await page.locator('nav button:has-text("Alice E2E")').click();
    const darkToggle = page.locator('nav div[x-data*="userMenuOpen"] div[x-data*="darkModeToggle"] button').first();
    for (let i = 0; i < 3; i++) {
      if (await page.evaluate(() => localStorage.getItem('darkMode') === 'dark')) break;
      await darkToggle.click();
    }
    const themeClassAfterClick = await page.locator('html').evaluate((el) => el.className);
    expect(themeClassAfterClick).toContain('dark');
    await page.reload();
    expect(await page.locator('html').evaluate((el) => el.classList.contains('dark'))).toBeTruthy();

    // Drag-paint first 2 weekdays using real mouse drag
    const weekdays = firstWeekdaysOf(testYear, testMonth, 3);
    const cell1 = page.locator(`td.calendar-cell[data-date="${weekdays[0]}"]`).first();
    const cell2 = page.locator(`td.calendar-cell[data-date="${weekdays[1]}"]`).first();
    const box1 = (await cell1.boundingBox())!;
    const box2 = (await cell2.boundingBox())!;

    await page.mouse.move(box1.x + box1.width / 2, box1.y + box1.height / 2);
    await page.mouse.down();
    await page.mouse.move(box2.x + box2.width / 2, box2.y + box2.height / 2, { steps: 5 });
    await page.mouse.up();

    // Picker opens — pick "Remote work" (2nd button, showAs="workingElsewhere") so O365 sync emits an event
    const picker = page.locator('div[x-show="showPicker"]');
    await expect(picker).toBeVisible();
    await Promise.all([
      page.waitForEvent('load'),
      picker.locator('button').nth(1).click(),
    ]);
    await page.waitForLoadState('networkidle');
    await expect(page.locator(`td.calendar-cell[data-date="${weekdays[0]}"]`).first()).toBeVisible();

    // Right-click 3rd weekday -> open context menu -> choose Morning (AM) -> pick "On site" (1st button)
    const cell3 = page.locator(`td.calendar-cell[data-date="${weekdays[2]}"]`).first();
    await cell3.click({ button: 'right' });
    const ctxMenu = page.locator('div[x-show="showContextMenu"]');
    await expect(ctxMenu).toBeVisible();
    await ctxMenu.locator('button').first().click(); // Morning (AM)
    await expect(picker).toBeVisible();
    await Promise.all([
      page.waitForEvent('load'),
      picker.locator('button').first().click(),
    ]);
    await page.waitForLoadState('networkidle');
    await expect(page.locator(`td.calendar-cell[data-date="${weekdays[2]}"]`).first()).toBeVisible();

    // Connect & Synchronize Office 365 Outlook through real OAuth2 redirect loop
    const o365Btn = page.locator('div[x-data*="syncNow"] button').first();
    await expect(o365Btn).toBeVisible();
    await Promise.all([
      page.waitForResponse((r) => r.url().includes('/api/o365/sync') && r.status() === 200),
      o365Btn.click(),
    ]);
    await expect(page.locator('div[x-data*="syncNow"]')).toContainText('2');
    await page.reload();
    await expect(page.locator('div[x-data*="syncNow"]')).toContainText('o365.user@office.example.com');

    await expect
      .poll(async () => (await getMockState(request)).o365_events.length, {
        timeout: 10_000,
        message: 'Expected O365 mock Graph to receive synced calendar event(s)',
      })
      .toBeGreaterThan(0);

    // Fill remaining weekdays via page context fetch & test full UI Certification modal flow
    const remainingWeekdays = firstWeekdaysOf(testYear, testMonth, 31).slice(3);
    const aliceUid = await cell1.getAttribute('data-user-id');
    const stResp = await page.evaluate(
      async ({ uid, dates }) => {
        const res = await fetch('/api/presences', {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({ user_id: Number(uid), dates, status_id: 2, half: 'full' }),
        });
        return res.status;
      },
      { uid: aliceUid, dates: remainingWeekdays },
    );
    expect(stResp).toBe(200);

    await page.reload();
    const certBtn = page.locator('div[x-show="declarable > 0 && declared >= declarable && !certified"] button').first();
    await expect(certBtn).toBeVisible();
    await certBtn.click();
    const certConfirmModal = page.locator('div[x-show="confirmShow"]');
    await expect(certConfirmModal).toBeVisible();
    await Promise.all([
      page.waitForEvent('load'),
      certConfirmModal.locator('button').last().click(),
    ]);
    await page.waitForLoadState('networkidle');
    await expect(certBtn).toBeHidden();
  });
});
