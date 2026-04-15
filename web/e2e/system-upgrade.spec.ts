import { test, expect } from '@playwright/test';
import {
  mockConfiguredSystem,
  jsonError,
  SERVICE_STATUS_SYSTEMD,
  SYSTEM_VERSION_DEV,
} from './helpers/mock-api';

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

    await expect(page.getByRole('heading', { name: 'Version & Updates' })).toBeVisible({ timeout: 10_000 });
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
// System page — check for updates (auto-loads on mount)
// ---------------------------------------------------------------------------

test.describe('System page — check for updates', () => {
  test('shows update available when newer version exists', async ({ page }) => {
    await mockConfiguredSystem(page, {
      mode: 'systemd',
      systemService: SERVICE_STATUS_SYSTEMD,
    });
    await page.route('**/api/v1/system/updates', (route) =>
      route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({
          currentVersion: '0.0.5',
          latestVersion: '0.0.8',
          updateAvailable: true,
          releaseUrl: 'https://github.com/test/releases/v0.0.8',
          checkedAt: Date.now(),
        }),
      }),
    );
    await page.goto('/system');

    await expect(page.getByText('Version 0.0.8 is available')).toBeVisible({ timeout: 10_000 });
    await expect(page.getByRole('button', { name: /Upgrade to v0\.0\.8/ })).toBeVisible();
  });

  test('shows already up to date message', async ({ page }) => {
    await mockConfiguredSystem(page, {
      mode: 'systemd',
      systemService: SERVICE_STATUS_SYSTEMD,
    });
    await page.route('**/api/v1/system/updates', (route) =>
      route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({
          currentVersion: '0.0.8',
          latestVersion: '0.0.8',
          updateAvailable: false,
          checkedAt: Date.now(),
        }),
      }),
    );
    await page.goto('/system');

    await expect(page.getByText('You are running the latest version')).toBeVisible({ timeout: 10_000 });
  });

  test('shows offline message when no internet', async ({ page }) => {
    await mockConfiguredSystem(page, {
      mode: 'systemd',
      systemService: SERVICE_STATUS_SYSTEMD,
    });
    await page.route('**/api/v1/system/updates', (route) =>
      jsonError(route, 'unable to reach GitHub — check your internet connection', 503),
    );
    await page.route('**/api/v1/system/releases', (route) =>
      jsonError(route, 'unable to reach GitHub', 503),
    );
    await page.goto('/system');

    await expect(page.getByText('Unable to reach GitHub')).toBeVisible({ timeout: 10_000 });
  });

  test('hides upgrade button when not in systemd mode', async ({ page }) => {
    await mockConfiguredSystem(page, { mode: 'dev' });
    await page.route('**/api/v1/system/updates', (route) =>
      route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({
          currentVersion: '0.0.5',
          latestVersion: '0.0.8',
          updateAvailable: true,
          checkedAt: Date.now(),
        }),
      }),
    );
    await page.goto('/system');

    await expect(page.getByText('Version 0.0.8 is available')).toBeVisible({ timeout: 10_000 });
    await expect(page.getByText('Upgrades are only available when running as a systemd service')).toBeVisible();
    await expect(page.getByRole('button', { name: /Upgrade to/ })).not.toBeVisible();
  });
});

// ---------------------------------------------------------------------------
// System page — release list (auto-loads on mount)
// ---------------------------------------------------------------------------

test.describe('System page — release list', () => {
  test('shows all releases with current badge', async ({ page }) => {
    await mockConfiguredSystem(page, {
      mode: 'systemd',
      systemService: SERVICE_STATUS_SYSTEMD,
    });
    await page.route('**/api/v1/system/releases', (route) =>
      route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify([
          { version: '0.0.8', tagName: 'v0.0.8', name: 'v0.0.8', releaseUrl: '', publishedAt: '2026-04-10T00:00:00Z', current: false },
          { version: '0.0.5', tagName: 'v0.0.5', name: 'v0.0.5', releaseUrl: '', publishedAt: '2026-04-01T00:00:00Z', current: true },
          { version: '0.0.4', tagName: 'v0.0.4', name: 'v0.0.4', releaseUrl: '', publishedAt: '2026-03-15T00:00:00Z', current: false },
        ]),
      }),
    );
    await page.goto('/system');

    await expect(page.getByText('Available Releases')).toBeVisible({ timeout: 10_000 });
    await expect(page.locator('.current-badge')).toBeVisible();
    // Non-current releases should have Switch buttons
    const switchButtons = page.getByRole('button', { name: 'Switch' });
    await expect(switchButtons).toHaveCount(2);
  });

  test('shows offline message when releases fetch fails', async ({ page }) => {
    await mockConfiguredSystem(page, {
      mode: 'systemd',
      systemService: SERVICE_STATUS_SYSTEMD,
    });
    await page.route('**/api/v1/system/releases', (route) =>
      jsonError(route, 'unable to reach GitHub', 503),
    );
    await page.route('**/api/v1/system/updates', (route) =>
      jsonError(route, 'unable to reach GitHub', 503),
    );
    await page.goto('/system');

    await expect(page.getByText('Unable to reach GitHub')).toBeVisible({ timeout: 10_000 });
  });
});

// ---------------------------------------------------------------------------
// System page — upgrade flow
// ---------------------------------------------------------------------------

test.describe('System page — upgrade flow', () => {
  test('shows confirmation modal before upgrading', async ({ page }) => {
    await mockConfiguredSystem(page, {
      mode: 'systemd',
      systemService: SERVICE_STATUS_SYSTEMD,
    });
    await page.route('**/api/v1/system/updates', (route) =>
      route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({
          currentVersion: '0.0.5',
          latestVersion: '0.0.8',
          updateAvailable: true,
          checkedAt: Date.now(),
        }),
      }),
    );
    await page.goto('/system');

    await page.getByRole('button', { name: /Upgrade to v0\.0\.8/ }).click({ timeout: 10_000 });

    await expect(page.getByText('Confirm Version Change')).toBeVisible();
    await expect(page.getByRole('button', { name: 'Cancel' })).toBeVisible();
    await expect(page.getByRole('button', { name: /Switch to v0\.0\.8/ })).toBeVisible();
  });

  test('cancel closes confirmation modal', async ({ page }) => {
    await mockConfiguredSystem(page, {
      mode: 'systemd',
      systemService: SERVICE_STATUS_SYSTEMD,
    });
    await page.route('**/api/v1/system/updates', (route) =>
      route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({
          currentVersion: '0.0.5',
          latestVersion: '0.0.8',
          updateAvailable: true,
          checkedAt: Date.now(),
        }),
      }),
    );
    await page.goto('/system');

    await page.getByRole('button', { name: /Upgrade to v0\.0\.8/ }).click({ timeout: 10_000 });
    await page.getByRole('button', { name: 'Cancel' }).click();

    await expect(page.getByText('Confirm Version Change')).not.toBeVisible();
  });

  test('shows upgrading state after confirming', async ({ page }) => {
    await mockConfiguredSystem(page, {
      mode: 'systemd',
      systemService: SERVICE_STATUS_SYSTEMD,
    });
    await page.route('**/api/v1/system/updates', (route) =>
      route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({
          currentVersion: '0.0.5',
          latestVersion: '0.0.8',
          updateAvailable: true,
          checkedAt: Date.now(),
        }),
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

    await page.getByRole('button', { name: /Upgrade to v0\.0\.8/ }).click({ timeout: 10_000 });
    await page.getByRole('button', { name: /Switch to v0\.0\.8/ }).click();

    await expect(page.getByText('Downloading new version')).toBeVisible({ timeout: 5_000 });
  });

  test('shows failure state on upgrade error', async ({ page }) => {
    await mockConfiguredSystem(page, {
      mode: 'systemd',
      systemService: SERVICE_STATUS_SYSTEMD,
    });
    await page.route('**/api/v1/system/updates', (route) =>
      route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({
          currentVersion: '0.0.5',
          latestVersion: '0.0.8',
          updateAvailable: true,
          checkedAt: Date.now(),
        }),
      }),
    );
    await page.route('**/api/v1/system/upgrade', (route) =>
      jsonError(route, 'upgrade requires running as a systemd service', 400),
    );
    await page.goto('/system');

    await page.getByRole('button', { name: /Upgrade to v0\.0\.8/ }).click({ timeout: 10_000 });
    await page.getByRole('button', { name: /Switch to v0\.0\.8/ }).click();

    await expect(page.getByText('Upgrade failed')).toBeVisible({ timeout: 5_000 });
    await expect(page.getByRole('button', { name: 'Dismiss' })).toBeVisible();
  });
});
