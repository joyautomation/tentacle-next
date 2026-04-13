import { test, expect } from '@playwright/test';
import { mockConfiguredSystem, mockFreshInstall } from './helpers/mock-api';

// ---------------------------------------------------------------------------
// Dashboard rendering
// ---------------------------------------------------------------------------

test.describe('Dashboard', () => {
  test('shows system topology when API is connected', async ({ page }) => {
    await mockConfiguredSystem(page);
    await page.goto('/');

    // Mode badge should show
    await expect(page.locator('.mode-badge')).toContainText('dev');

    // Should NOT show disconnected banner
    await expect(page.locator('.disconnected-banner')).not.toBeVisible();
  });

  test('shows disconnected banner when API is unreachable', async ({ page }) => {
    // Mock all API calls to fail
    await page.route('**/api/v1/**', (route) =>
      route.fulfill({ status: 502, contentType: 'application/json', body: '{"error":"unreachable"}' }),
    );
    await page.goto('/');

    await expect(page.getByText('API service unreachable')).toBeVisible({ timeout: 10_000 });
    await expect(page.getByText('Start tentacle to view system status')).toBeVisible();
  });
});

// ---------------------------------------------------------------------------
// Navigation sidebar
// ---------------------------------------------------------------------------

test.describe('Navigation sidebar', () => {
  test('shows running services in sidebar', async ({ page }) => {
    await mockConfiguredSystem(page);
    await page.goto('/');

    await page.getByRole('button', { name: 'Open navigation' }).click();

    await expect(page.locator('nav').getByText('EtherNet/IP')).toBeVisible();
    await expect(page.locator('nav').getByText('Gateway')).toBeVisible();
    await expect(page.locator('nav').getByText('MQTT')).toBeVisible();
  });

  test('clicking a service navigates to its overview', async ({ page }) => {
    await mockConfiguredSystem(page);
    await page.goto('/');

    await page.getByRole('button', { name: 'Open navigation' }).click();
    await page.locator('nav').getByText('EtherNet/IP').click();

    await expect(page).toHaveURL(/\/services\/ethernetip/);
  });

  test('home link navigates to dashboard', async ({ page }) => {
    await mockConfiguredSystem(page);
    await page.goto('/services/ethernetip');

    await page.locator('a.logo').click();
    await expect(page).toHaveURL('/');
  });
});

// ---------------------------------------------------------------------------
// Theme switching
// ---------------------------------------------------------------------------

test.describe('Theme', () => {
  test('theme switch toggles between themes', async ({ page }) => {
    await mockConfiguredSystem(page);
    await page.goto('/');

    // Click the "Light" theme button (title="Light")
    await page.locator('button[title="Light"]').click();
    await expect(page.locator('body')).toHaveClass('themeLight');

    // Switch to dark
    await page.locator('button[title="Dark"]').click();
    await expect(page.locator('body')).toHaveClass('themeDark');

    // Switch back to system
    await page.locator('button[title="System"]').click();
    await expect(page.locator('body')).toHaveClass('themeSystem');
  });
});
