import { test, expect } from '@playwright/test';
import {
  mockConfiguredSystem,
  jsonError,
  SERVICE_STATUS_DEV_CAN_INSTALL,
  SERVICE_STATUS_DEV_CANNOT_INSTALL,
  SERVICE_STATUS_SYSTEMD,
} from './helpers/mock-api';

// ---------------------------------------------------------------------------
// Service install banner
// ---------------------------------------------------------------------------

test.describe('Service install banner', () => {
  test('shows install button when in dev mode and can install', async ({ page }) => {
    await mockConfiguredSystem(page, {
      systemService: SERVICE_STATUS_DEV_CAN_INSTALL,
    });
    await page.goto('/');

    await expect(page.getByText('Running in standalone mode')).toBeVisible({ timeout: 10_000 });
    await expect(page.getByText('Install as a system service')).toBeVisible();
    await expect(page.getByRole('button', { name: 'Install as Service' })).toBeVisible();
  });

  test('shows CLI instruction when cannot install', async ({ page }) => {
    await mockConfiguredSystem(page, {
      systemService: SERVICE_STATUS_DEV_CANNOT_INSTALL,
    });
    await page.goto('/');

    await expect(page.getByText('Running in standalone mode')).toBeVisible({ timeout: 10_000 });
    await expect(page.getByText('sudo tentacle service install')).toBeVisible();
    await expect(page.getByRole('button', { name: 'Install as Service' })).not.toBeVisible();
  });

  test('does not show banner when already running as systemd service', async ({ page }) => {
    await mockConfiguredSystem(page, {
      mode: 'systemd',
      systemService: SERVICE_STATUS_SYSTEMD,
    });
    await page.goto('/');

    // Wait for page to load and stabilize
    await expect(page.locator('.mode-badge')).toContainText('systemd', { timeout: 10_000 });
    await expect(page.getByText('Running in standalone mode')).not.toBeVisible();
  });

  test('install flow: idle → installing → installed → activate', async ({ page }) => {
    await mockConfiguredSystem(page, {
      systemService: SERVICE_STATUS_DEV_CAN_INSTALL,
    });
    await page.goto('/');

    const installBtn = page.getByRole('button', { name: 'Install as Service' });
    await expect(installBtn).toBeVisible({ timeout: 10_000 });
    await installBtn.click();

    // Should transition to installed state
    await expect(page.getByText('Service installed and enabled')).toBeVisible({ timeout: 5_000 });
    await expect(page.getByRole('button', { name: 'Activate Service' })).toBeVisible();
  });

  test('install failure shows error with retry', async ({ page }) => {
    await mockConfiguredSystem(page, {
      systemService: SERVICE_STATUS_DEV_CAN_INSTALL,
    });
    // Override install endpoint to fail
    await page.route('**/api/v1/system/service/install', (route) =>
      jsonError(route, 'Permission denied: not running as root'),
    );
    await page.goto('/');

    await page.getByRole('button', { name: 'Install as Service' }).click({ timeout: 10_000 });

    await expect(page.getByText('Service installation failed')).toBeVisible();
    await expect(page.getByText('Permission denied: not running as root')).toBeVisible();
    await expect(page.getByRole('button', { name: 'Retry' })).toBeVisible();
  });

  test('retry resets to idle state', async ({ page }) => {
    await mockConfiguredSystem(page, {
      systemService: SERVICE_STATUS_DEV_CAN_INSTALL,
    });
    await page.route('**/api/v1/system/service/install', (route) =>
      jsonError(route, 'Permission denied'),
    );
    await page.goto('/');

    await page.getByRole('button', { name: 'Install as Service' }).click({ timeout: 10_000 });
    await expect(page.getByText('Service installation failed')).toBeVisible();

    await page.getByRole('button', { name: 'Retry' }).click();

    await expect(page.getByText('Running in standalone mode')).toBeVisible();
    await expect(page.getByRole('button', { name: 'Install as Service' })).toBeVisible();
  });

  test('activate sends activate API call and shows activating state', async ({ page }) => {
    let activateCalled = false;

    // Set up a system that already has the service installed but not active
    await mockConfiguredSystem(page, {
      systemService: {
        ...SERVICE_STATUS_DEV_CAN_INSTALL,
        unitExists: true,
        unitEnabled: true,
        unitActive: false,
      },
    });

    // Override the activate endpoint
    await page.route('**/api/v1/system/service/activate', (route) => {
      activateCalled = true;
      return route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({ success: true }),
      });
    });

    await page.goto('/');

    // Wait for the banner to appear (depends on services poll + system/service fetch)
    await expect(page.getByText('Service installed and enabled')).toBeVisible({ timeout: 15_000 });

    const activateBtn = page.getByRole('button', { name: 'Activate Service' });
    await expect(activateBtn).toBeVisible();
    await activateBtn.click();

    // Should show activating state
    await expect(page.getByText('Restarting as system service')).toBeVisible();
    expect(activateCalled).toBe(true);
  });
});
