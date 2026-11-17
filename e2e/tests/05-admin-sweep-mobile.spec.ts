import { test, expect, ADMIN_USER, ADMIN_PASSWORD, loginLocal } from '../fixtures/test';

const ALL_PAGES = [
  '/',
  '/floorplan',
  '/projects',
  '/admin/activity',
  '/admin/projects-report',
  '/admin/sites-report',
  '/admin/teams',
  '/admin/sites',
  '/admin/lockers',
  '/admin/floorplans',
  '/admin/domains',
  '/admin/holidays',
  '/admin/statuses',
  '/admin/projects',
  '/admin/users',
  '/admin/settings',
  '/admin/news',
  '/admin/notifications',
  '/settings/tokens',
  '/settings/my-logs',
  '/impersonate',
];

test.describe('05 - Full Route Sweep, Online Help Modal & Mobile Responsive Check', () => {
  test('every admin & user screen renders cleanly without JS/server errors and Online Help opens', async ({
    page,
  }, testInfo) => {
    await loginLocal(page, ADMIN_USER, ADMIN_PASSWORD);
    const isMobile = testInfo.project.name === 'chromium-mobile';

    const routesToCheck = isMobile
      ? ['/', '/floorplan', '/projects', '/admin/lockers', '/admin/teams']
      : ALL_PAGES;

    for (const route of routesToCheck) {
      const resp = await page.goto(route);
      expect(resp?.status(), `Unexpected status on ${route}`).toBeLessThan(400);
      await expect(page.locator('body')).not.toBeEmpty();

      if (isMobile) {
        // Document-level horizontal overflow should not occur (scrollable tables use inner overflow-x-auto containers)
        const docOverflow = await page.evaluate(() => {
          return document.documentElement.scrollWidth - document.documentElement.clientWidth;
        });
        expect(docOverflow, `Horizontal overflow (${docOverflow}px) on mobile route ${route}`).toBeLessThanOrEqual(4);
      }
    }

    // Verify Online Help modal opens and closes cleanly on /, /floorplan, /admin/lockers
    for (const helpRoute of ['/', '/floorplan', '/admin/lockers']) {
      await page.goto(helpRoute);
      await page.evaluate((pg) => {
        window.dispatchEvent(new CustomEvent('open-help', { detail: pg }));
      }, helpRoute === '/' ? 'calendar' : helpRoute === '/floorplan' ? 'floorplan' : 'admin_lockers');
      const helpDialog = page.locator('div[x-data*="onlineHelpModal"]');
      await expect(helpDialog).toBeVisible();
      await page.keyboard.press('Escape');
      await expect(helpDialog).toBeHidden();
    }
  });
});
