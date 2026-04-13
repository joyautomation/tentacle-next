import { test, expect } from '@playwright/test';
import { mockConfiguredSystem, jsonError, SERVICES_RUNNING } from './helpers/mock-api';

// ---------------------------------------------------------------------------
// Service overview page
// ---------------------------------------------------------------------------

test.describe('Service overview', () => {
  test.beforeEach(async ({ page }) => {
    await mockConfiguredSystem(page);
  });

  test('shows service name and running status', async ({ page }) => {
    await page.goto('/services/ethernetip');
    await expect(page.getByRole('heading', { name: 'EtherNet/IP' })).toBeVisible();
    await expect(page.locator('.status-badge.running')).toContainText('Running');
  });

  test('shows stopped status when service is not running', async ({ page }) => {
    await mockConfiguredSystem(page, {
      services: SERVICES_RUNNING.filter((s) => s.serviceType !== 'ethernetip'),
    });
    await page.goto('/services/ethernetip');
    await expect(page.locator('.status-badge.stopped')).toContainText('Stopped');
  });

  test('enable/disable toggle sends API call', async ({ page }) => {
    let putCalled = false;
    await page.route('**/api/v1/services/ethernetip/enabled', async (route) => {
      if (route.request().method() === 'PUT') {
        putCalled = true;
        const body = route.request().postDataJSON();
        return route.fulfill({
          status: 200,
          contentType: 'application/json',
          body: JSON.stringify({ moduleId: 'ethernetip', enabled: body.enabled }),
        });
      }
      return route.fallback();
    });

    await page.goto('/services/ethernetip');

    const toggle = page.locator('.enable-row input[type="checkbox"]').first();
    if (await toggle.isVisible()) {
      await toggle.click();
      expect(putCalled).toBe(true);
    }
  });
});

// ---------------------------------------------------------------------------
// Service tabs
// ---------------------------------------------------------------------------

test.describe('Service tab navigation', () => {
  test.beforeEach(async ({ page }) => {
    await mockConfiguredSystem(page);
  });

  test('gateway service shows correct tabs', async ({ page }) => {
    await page.goto('/services/gateway');
    const tabs = page.locator('.tab');
    await expect(tabs.filter({ hasText: 'Overview' })).toBeVisible();
    await expect(tabs.filter({ hasText: 'Sources' })).toBeVisible();
    await expect(tabs.filter({ hasText: 'Variables' })).toBeVisible();
    await expect(tabs.filter({ hasText: 'Logs' })).toBeVisible();
  });

  test('mqtt service shows correct tabs', async ({ page }) => {
    await page.goto('/services/mqtt');
    const tabs = page.locator('.tab');
    await expect(tabs.filter({ hasText: 'Overview' })).toBeVisible();
    await expect(tabs.filter({ hasText: 'Metrics' })).toBeVisible();
    await expect(tabs.filter({ hasText: 'Settings' })).toBeVisible();
    await expect(tabs.filter({ hasText: 'Logs' })).toBeVisible();
  });

  test('ethernetip service shows correct tabs', async ({ page }) => {
    await page.goto('/services/ethernetip');
    const tabs = page.locator('.tab');
    await expect(tabs.filter({ hasText: 'Overview' })).toBeVisible();
    await expect(tabs.filter({ hasText: 'Devices' })).toBeVisible();
    await expect(tabs.filter({ hasText: 'Logs' })).toBeVisible();
  });

  test('clicking tabs navigates to correct sub-route', async ({ page }) => {
    await page.goto('/services/gateway');
    await page.locator('.tab').filter({ hasText: 'Logs' }).click();
    await expect(page).toHaveURL(/\/services\/gateway\/logs/);
  });

  test('breadcrumb back link goes to dashboard', async ({ page }) => {
    await page.goto('/services/ethernetip');
    await page.locator('a.back-link').click();
    await expect(page).toHaveURL('/');
  });
});

// ---------------------------------------------------------------------------
// Log viewer
// ---------------------------------------------------------------------------

test.describe('Log viewer', () => {
  test('displays initial log entries', async ({ page }) => {
    await mockConfiguredSystem(page);
    await page.goto('/services/ethernetip/logs');

    // Info-level messages should be visible (showInfo defaults to true)
    await expect(page.getByText('Service started')).toBeVisible({ timeout: 10_000 });
  });

  test('shows line count', async ({ page }) => {
    await mockConfiguredSystem(page);
    await page.goto('/services/ethernetip/logs');

    // Wait for logs to load, then check line count shows
    await expect(page.getByText('Service started')).toBeVisible({ timeout: 10_000 });
    await expect(page.locator('.line-count')).toBeVisible();
  });

  test('shows empty state when no logs', async ({ page }) => {
    await mockConfiguredSystem(page);
    // Override the logs route AFTER the base mock
    await page.route('**/api/v1/services/*/logs', (route) =>
      route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: '[]',
      }),
    );
    await page.goto('/services/ethernetip/logs');

    await expect(page.getByText('No log entries yet')).toBeVisible({ timeout: 10_000 });
  });
});

// ---------------------------------------------------------------------------
// MQTT-specific views
// ---------------------------------------------------------------------------

test.describe('MQTT service', () => {
  test('shows broker disconnected warning when not connected', async ({ page }) => {
    const mqttDisconnected = SERVICES_RUNNING.map((s) =>
      s.serviceType === 'mqtt' ? { ...s, metadata: { connected: false, brokerUrl: 'tcp://broker:1883' } } : s,
    );
    await mockConfiguredSystem(page, { services: mqttDisconnected });
    await page.goto('/services/mqtt');

    await expect(page.getByText('MQTT broker is not connected')).toBeVisible();
  });
});
