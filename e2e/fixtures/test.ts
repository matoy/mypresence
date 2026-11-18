import * as fs from 'node:fs';
import { test as base, expect, Page, APIRequestContext, Locator } from '@playwright/test';

export const ADMIN_USER = process.env.ADMIN_USER || 'admin';
export const ADMIN_PASSWORD = process.env.ADMIN_PASSWORD || 'AdminRecette#2026!';
export const MOCK_URL = (process.env.MOCK_URL || 'http://127.0.0.1:19080').replace(/\/+$/, '');

// Valid 1x1 transparent PNG
export const TINY_PNG = Buffer.from(
  'iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mP8z8BQDwAEhQGAhKmMIQAAAABJRU5ErkJggg==',
  'base64',
);

export function firstWeekdaysOf(year: number, month: number, count: number): string[] {
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

export async function expectXlsxDownload(page: Page, trigger: Locator): Promise<string> {
  const [download] = await Promise.all([
    page.waitForEvent('download'),
    trigger.click(),
  ]);
  const name = download.suggestedFilename();
  expect(name).toMatch(/\.xlsx$/i);
  const savedPath = await download.path();
  expect(savedPath, 'Expected Playwright to save downloaded .xlsx file').toBeTruthy();
  expect(fs.statSync(savedPath!).size).toBeGreaterThan(100);
  return name;
}

type HealthGuard = {
  allowExpected4xx: boolean;
};

export const test = base.extend<{ healthGuard: HealthGuard }>({
  healthGuard: [
    async ({ page }, use) => {
      const guard: HealthGuard = { allowExpected4xx: false };
      const errors: string[] = [];

      const onPageError = (err: Error) => {
        errors.push(`pageerror: ${err.message}\n${err.stack || ''}`);
      };
      const onConsole = (msg: { type(): string; text(): string }) => {
        if (msg.type() === 'error') {
          const txt = msg.text();
          if (guard.allowExpected4xx && txt.includes('Failed to load resource')) {
            return;
          }
          errors.push(`console.error: ${txt}`);
        }
      };
      const onResponse = (resp: { url(): string; status(): number }) => {
        const st = resp.status();
        const u = resp.url();
        if (st >= 500) {
          errors.push(`HTTP ${st} on ${u}`);
        } else if (st >= 400 && (u.includes('/static/') || u.includes('/floorplan-img/'))) {
          errors.push(`Static asset failed (${st}) on ${u}`);
        }
      };

      page.on('pageerror', onPageError);
      page.on('console', onConsole);
      page.on('response', onResponse);

      await use(guard);

      page.off('pageerror', onPageError);
      page.off('console', onConsole);
      page.off('response', onResponse);

      expect(errors, `Unhandled browser/server errors detected:\n${errors.join('\n')}`).toEqual([]);
    },
    { auto: true },
  ],
});

export { expect };

export async function loginLocal(page: Page, username: string, password: string): Promise<void> {
  await page.goto('/login?local=1');
  await page.locator('#username').fill(username);
  await page.locator('#password').fill(password);
  await Promise.all([
    page.waitForURL((u) => !u.pathname.startsWith('/login')),
    page.locator('form[action="/login"] button[type="submit"]').click(),
  ]);
  await expect(page.locator('nav')).toBeVisible();
}

export async function logout(page: Page): Promise<void> {
  if ((await page.locator('nav').count()) === 0) {
    await page.goto('/');
    if (page.url().includes('/login')) return;
  }
  const userMenuToggle = page.locator('nav div[x-data*="userMenuOpen"] > button').first();
  if (await userMenuToggle.isVisible()) {
    await userMenuToggle.click();
    const logoutBtn = page.locator('nav div[x-data*="userMenuOpen"] form[action="/logout"] button[type="submit"]').first();
    await expect(logoutBtn).toBeVisible();
    await Promise.all([
      page.waitForURL(/\/login/),
      logoutBtn.click(),
    ]);
    return;
  }
  const mobileToggle = page.locator('nav button.md\\:hidden').first();
  if (await mobileToggle.isVisible()) {
    await mobileToggle.click();
    const mLogout = page.locator('nav div[x-show="mobileMenu"] form[action="/logout"] button[type="submit"]').first();
    await expect(mLogout).toBeVisible();
    await Promise.all([
      page.waitForURL(/\/login/),
      mLogout.click(),
    ]);
  }
}

export async function setMockPersona(
  request: APIRequestContext,
  persona: string | { email: string; display_name: string; groups: string[] },
): Promise<void> {
  const res =
    typeof persona === 'string'
      ? await request.post(`${MOCK_URL}/__mock/persona?persona=${encodeURIComponent(persona)}`)
      : await request.post(`${MOCK_URL}/__mock/persona`, { data: persona });
  expect(res.ok(), `Failed to set mock persona: ${res.status()}`).toBeTruthy();
}

export async function getMockState(request: APIRequestContext): Promise<{
  o365_events: Array<Record<string, unknown>>;
  jira_requests: Array<Record<string, unknown>>;
  emails: Array<{ to: string[]; subject: string; body: string; reset_links: string[] }>;
  saml_logins: Array<{ email: string; groups: string[] }>;
}> {
  const res = await request.get(`${MOCK_URL}/__mock/state`);
  expect(res.ok()).toBeTruthy();
  return res.json();
}
