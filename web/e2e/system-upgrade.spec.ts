import { test, expect } from '@playwright/test';
import {
  mockConfiguredSystem,
  jsonError,
  SERVICE_STATUS_SYSTEMD,
  SYSTEM_VERSION_DEV,
} from './helpers/mock-api';

/** Wrap a release array into the ReleasesResponse shape the API returns. */
function releasesResponse(releases: unknown[]) {
  return { releases, lastChecked: Date.now() };
}

// ---------------------------------------------------------------------------
// System page — version display
// ---------------------------------------------------------------------------

test.describe('System page — version display', () => {
  test('shows current version and mode', async ({ page }) => {
    await mockConfiguredSystem(page, {
      mode: 'systemd',
      systemService: SERVICE_STATUS_SYSTEMD,
    });
    await page.goto('/system');

    await expect(page.getByRole('heading', { name: 'Updates' })).toBeVisible({ timeout: 10_000 });
    await expect(page.getByRole('main').getByText('0.0.5')).toBeVisible();
    await expect(page.getByRole('main').getByText('systemd').first()).toBeVisible();
  });

  test('shows version in sidebar footer', async ({ page }) => {
    await mockConfiguredSystem(page);
    await page.goto('/');

    // Open sidebar
    await page.getByLabel('Open navigation').click();

    await expect(page.locator('.version-label')).toContainText('0.0.5', { timeout: 10_000 });
  });
});

// ---------------------------------------------------------------------------
// System page — releases (auto-loads on mount)
// ---------------------------------------------------------------------------

test.describe('System page — releases', () => {
  test('shows releases with current and update badges', async ({ page }) => {
    await mockConfiguredSystem(page, {
      mode: 'systemd',
      systemService: SERVICE_STATUS_SYSTEMD,
    });
    await page.route('**/api/v1/system/releases', (route) =>
      route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify(releasesResponse([
          { version: '0.0.8', tagName: 'v0.0.8', name: 'v0.0.8', releaseUrl: '', publishedAt: '2026-04-10T00:00:00Z', current: false },
          { version: '0.0.7', tagName: 'v0.0.7', name: 'v0.0.7', releaseUrl: '', publishedAt: '2026-04-05T00:00:00Z', current: false },
          { version: '0.0.5', tagName: 'v0.0.5', name: 'v0.0.5', releaseUrl: '', publishedAt: '2026-04-01T00:00:00Z', current: true },
          { version: '0.0.4', tagName: 'v0.0.4', name: 'v0.0.4', releaseUrl: '', publishedAt: '2026-03-15T00:00:00Z', current: false },
        ])),
      }),
    );
    await page.goto('/system');

    await expect(page.getByRole('heading', { name: 'Releases' })).toBeVisible({ timeout: 10_000 });
    // Current release has "current" badge
    await expect(page.locator('.current-badge')).toBeVisible();
    // Newer releases have "update" badges
    await expect(page.locator('.update-badge')).toHaveCount(2);
    // Non-current releases should have Switch buttons
    const switchButtons = page.getByRole('button', { name: 'Switch' });
    await expect(switchButtons).toHaveCount(3);
    // Shows "checked" timestamp
    await expect(page.getByText(/checked/)).toBeVisible();
  });

  test('shows offline message when no internet', async ({ page }) => {
    await mockConfiguredSystem(page, {
      mode: 'systemd',
      systemService: SERVICE_STATUS_SYSTEMD,
    });
    await page.route('**/api/v1/system/releases', (route) =>
      jsonError(route, 'unable to reach GitHub', 503),
    );
    await page.goto('/system');

    await expect(page.getByText('Unable to reach GitHub')).toBeVisible({ timeout: 10_000 });
  });

  test('hides switch buttons when not in systemd mode', async ({ page }) => {
    await mockConfiguredSystem(page, { mode: 'dev' });
    await page.route('**/api/v1/system/releases', (route) =>
      route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify(releasesResponse([
          { version: '0.0.8', tagName: 'v0.0.8', name: 'v0.0.8', releaseUrl: '', publishedAt: '2026-04-10T00:00:00Z', current: false },
          { version: '0.0.5', tagName: 'v0.0.5', name: 'v0.0.5', releaseUrl: '', publishedAt: '2026-04-01T00:00:00Z', current: true },
        ])),
      }),
    );
    await page.goto('/system');

    // Releases still show but Switch buttons are hidden
    await expect(page.locator('.update-badge')).toBeVisible({ timeout: 10_000 });
    await expect(page.getByText('Upgrades are only available when running as a systemd service')).toBeVisible();
    await expect(page.getByRole('button', { name: 'Switch' })).not.toBeVisible();
  });
});

// ---------------------------------------------------------------------------
// System page — upgrade flow
// ---------------------------------------------------------------------------

test.describe('System page — upgrade flow', () => {
  test('shows confirmation modal with disabled button until version typed', async ({ page }) => {
    await mockConfiguredSystem(page, {
      mode: 'systemd',
      systemService: SERVICE_STATUS_SYSTEMD,
    });
    await page.route('**/api/v1/system/releases', (route) =>
      route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify(releasesResponse([
          { version: '0.0.8', tagName: 'v0.0.8', name: 'v0.0.8', releaseUrl: '', publishedAt: '2026-04-10T00:00:00Z', current: false },
          { version: '0.0.5', tagName: 'v0.0.5', name: 'v0.0.5', releaseUrl: '', publishedAt: '2026-04-01T00:00:00Z', current: true },
        ])),
      }),
    );
    await page.goto('/system');

    await page.getByRole('button', { name: 'Switch' }).first().click({ timeout: 10_000 });

    await expect(page.getByText('Confirm Version Change')).toBeVisible();
    await expect(page.getByRole('button', { name: 'Cancel' })).toBeVisible();
    // Button is disabled until version is typed
    const switchBtn = page.getByRole('button', { name: /Switch to v0\.0\.8/ });
    await expect(switchBtn).toBeDisabled();
    // Type the version
    await page.getByPlaceholder('0.0.8').fill('0.0.8');
    await expect(switchBtn).toBeEnabled();
  });

  test('shows warning for versions without self-update feature', async ({ page }) => {
    await mockConfiguredSystem(page, {
      mode: 'systemd',
      systemService: SERVICE_STATUS_SYSTEMD,
    });
    await page.route('**/api/v1/system/releases', (route) =>
      route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify(releasesResponse([
          { version: '0.0.9', tagName: 'v0.0.9', name: 'v0.0.9', releaseUrl: '', publishedAt: '2026-04-12T00:00:00Z', current: true },
          { version: '0.0.8', tagName: 'v0.0.8', name: 'v0.0.8', releaseUrl: '', publishedAt: '2026-04-10T00:00:00Z', current: false },
        ])),
      }),
    );
    await page.goto('/system');

    // Switch to v0.0.8 — should show warning
    await page.getByRole('button', { name: 'Switch' }).first().click({ timeout: 10_000 });
    await expect(page.getByText('does not include the self-update feature')).toBeVisible();
  });

  test('cancel closes confirmation modal', async ({ page }) => {
    await mockConfiguredSystem(page, {
      mode: 'systemd',
      systemService: SERVICE_STATUS_SYSTEMD,
    });
    await page.route('**/api/v1/system/releases', (route) =>
      route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify(releasesResponse([
          { version: '0.0.8', tagName: 'v0.0.8', name: 'v0.0.8', releaseUrl: '', publishedAt: '2026-04-10T00:00:00Z', current: false },
          { version: '0.0.5', tagName: 'v0.0.5', name: 'v0.0.5', releaseUrl: '', publishedAt: '2026-04-01T00:00:00Z', current: true },
        ])),
      }),
    );
    await page.goto('/system');

    await page.getByRole('button', { name: 'Switch' }).first().click({ timeout: 10_000 });
    await page.getByRole('button', { name: 'Cancel' }).click();

    await expect(page.getByText('Confirm Version Change')).not.toBeVisible();
  });

  test('shows upgrading state after confirming', async ({ page }) => {
    await mockConfiguredSystem(page, {
      mode: 'systemd',
      systemService: SERVICE_STATUS_SYSTEMD,
    });
    await page.route('**/api/v1/system/releases', (route) =>
      route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify(releasesResponse([
          { version: '0.0.8', tagName: 'v0.0.8', name: 'v0.0.8', releaseUrl: '', publishedAt: '2026-04-10T00:00:00Z', current: false },
          { version: '0.0.5', tagName: 'v0.0.5', name: 'v0.0.5', releaseUrl: '', publishedAt: '2026-04-01T00:00:00Z', current: true },
        ])),
      }),
    );
    await page.route('**/api/v1/system/upgrade', (route) => {
      if (route.request().method() === 'POST') {
        return route.fulfill({
          status: 202,
          contentType: 'application/json',
          body: JSON.stringify({ status: 'upgrading', version: '0.0.8' }),
        });
      }
      return route.continue();
    });
    await page.route('**/api/v1/system/upgrade/status', (route) =>
      route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({ state: 'downloading', version: '0.0.8' }),
      }),
    );
    await page.goto('/system');

    await page.getByRole('button', { name: 'Switch' }).first().click({ timeout: 10_000 });
    await page.getByPlaceholder('0.0.8').fill('0.0.8');
    await page.getByRole('button', { name: /Switch to v0\.0\.8/ }).click();

    await expect(page.getByText('Downloading new version')).toBeVisible({ timeout: 5_000 });
  });

  test('shows failure state on upgrade error', async ({ page }) => {
    await mockConfiguredSystem(page, {
      mode: 'systemd',
      systemService: SERVICE_STATUS_SYSTEMD,
    });
    await page.route('**/api/v1/system/releases', (route) =>
      route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify(releasesResponse([
          { version: '0.0.8', tagName: 'v0.0.8', name: 'v0.0.8', releaseUrl: '', publishedAt: '2026-04-10T00:00:00Z', current: false },
          { version: '0.0.5', tagName: 'v0.0.5', name: 'v0.0.5', releaseUrl: '', publishedAt: '2026-04-01T00:00:00Z', current: true },
        ])),
      }),
    );
    await page.route('**/api/v1/system/upgrade', (route) =>
      jsonError(route, 'upgrade requires running as a systemd service', 400),
    );
    await page.goto('/system');

    await page.getByRole('button', { name: 'Switch' }).first().click({ timeout: 10_000 });
    await page.getByPlaceholder('0.0.8').fill('0.0.8');
    await page.getByRole('button', { name: /Switch to v0\.0\.8/ }).click();

    await expect(page.getByText('Upgrade failed')).toBeVisible({ timeout: 5_000 });
    await expect(page.getByRole('button', { name: 'Dismiss' })).toBeVisible();
  });
});
