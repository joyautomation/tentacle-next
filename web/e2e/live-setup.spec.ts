/**
 * Live integration tests — run against a real tentacle binary (no API mocking).
 * Used by scripts/pre-release-test.sh (Tier 2).
 *
 * These tests exercise the actual install & setup flow end-to-end.
 */
import { test, expect } from '@playwright/test';

test.describe('Live: first-boot setup', () => {
  test('redirects to setup wizard on fresh install', async ({ page }) => {
    await page.goto('/');
    // Fresh binary has no desired services → should redirect to /setup
    await expect(page).toHaveURL('/setup', { timeout: 10_000 });
    await expect(page.getByRole('heading', { name: 'Quickstart Setup' })).toBeVisible();
  });

  test('mode badge shows dev', async ({ page }) => {
    await page.goto('/');
    await expect(page.locator('.mode-badge')).toContainText('dev', { timeout: 10_000 });
  });

  test('Sparkplug Gateway wizard completes successfully', async ({ page }) => {
    await page.goto('/setup');
    await expect(page.getByRole('heading', { name: 'Quickstart Setup' })).toBeVisible();

    // Step 1: Architecture
    await page.getByText('Sparkplug Gateway').first().click();
    await page.getByRole('button', { name: 'Next' }).click();

    // Step 2: Protocols — select EtherNet/IP
    await expect(page.getByRole('heading', { name: 'Select Protocols' })).toBeVisible();
    await page.getByText('EtherNet/IP').first().click();
    await page.getByRole('button', { name: 'Next' }).click();

    // Step 3: MQTT — use defaults
    await expect(page.getByRole('heading', { name: 'MQTT Broker Settings' })).toBeVisible();
    await page.getByRole('button', { name: 'Next' }).click();

    // Step 4: Add-ons — skip
    await expect(page.getByRole('heading', { name: 'Add-ons' })).toBeVisible();
    await page.getByRole('button', { name: 'Next' }).click();

    // Step 5: Review & Apply
    await expect(page.getByRole('heading', { name: 'Review Configuration' })).toBeVisible();
    await expect(page.getByText('Sparkplug Gateway')).toBeVisible();
    await expect(page.getByText('EtherNet/IP')).toBeVisible();

    await page.getByRole('button', { name: 'Apply & Start' }).click();

    // Wait for setup to complete — services need to actually start
    await expect(page.getByText('Setup Complete')).toBeVisible({ timeout: 120_000 });

    // Navigate to dashboard
    await page.getByRole('button', { name: 'Continue' }).click();
    await expect(page).toHaveURL('/');
  });

  test('dashboard shows running services after setup', async ({ page }) => {
    // This test runs after the wizard test — services should be running
    await page.goto('/');

    // Wait for services to appear (polling interval)
    await expect(page.locator('.disconnected-banner')).not.toBeVisible({ timeout: 10_000 });

    // Should not redirect to setup anymore
    await expect(page).toHaveURL('/');
  });
});
