import { test, expect } from '@playwright/test';
import {
  mockFreshInstall,
  mockConfiguredSystem,
  MODULES_ALL,
  MODULES_STABLE,
  SERVICE_STATUSES_ALL_ACTIVE,
  MQTT_CONFIG_EXISTING,
  DESIRED_SERVICES_SPARKPLUG,
  GITOPS_CONFIG_EXISTING,
  jsonError,
} from './helpers/mock-api';

// ---------------------------------------------------------------------------
// First-boot redirect
// ---------------------------------------------------------------------------

test.describe('First-boot redirect', () => {
  test('redirects to /setup when no desired services exist', async ({ page }) => {
    await mockFreshInstall(page);
    await page.goto('/');
    await expect(page).toHaveURL('/setup');
    await expect(page.getByRole('heading', { name: 'Quickstart Setup' })).toBeVisible();
  });

  test('does not redirect when desired services exist', async ({ page }) => {
    await mockConfiguredSystem(page);
    await page.goto('/');
    await expect(page).not.toHaveURL('/setup');
  });

  test('does not redirect if setup_dismissed is set', async ({ page }) => {
    await mockFreshInstall(page);
    await page.goto('/');
    await page.evaluate(() => sessionStorage.setItem('setup_dismissed', 'true'));
    await page.goto('/');
    await expect(page).toHaveURL('/');
  });
});

// ---------------------------------------------------------------------------
// Sparkplug Gateway — full happy path
// ---------------------------------------------------------------------------

test.describe('Sparkplug Gateway wizard', () => {
  test('full flow: select sparkplug → protocols → MQTT → add-ons → review → apply', async ({
    page,
  }) => {
    // Set up with successful service statuses from the start
    await mockFreshInstall(page, {
      serviceStatuses: SERVICE_STATUSES_ALL_ACTIVE,
    });
    await page.goto('/setup');
    await expect(page.getByRole('heading', { name: 'Quickstart Setup' })).toBeVisible();

    // Step 1: Architecture — Next button only appears after selecting an archetype
    await page.getByText('Sparkplug Gateway').first().click();
    const nextBtn = page.getByRole('button', { name: 'Next' });
    await expect(nextBtn).toBeEnabled();
    await nextBtn.click();

    // Step 2: Protocols
    await expect(page.getByRole('heading', { name: 'Select Protocols' })).toBeVisible();
    await expect(nextBtn).toBeDisabled();
    await page.getByText('EtherNet/IP').first().click();
    await page.getByText('Modbus').first().click();
    await expect(nextBtn).toBeEnabled();
    await nextBtn.click();

    // Step 3: MQTT Config
    await expect(page.getByRole('heading', { name: 'MQTT Broker Settings' })).toBeVisible();
    await expect(page.locator('#mqtt-broker-url')).toHaveValue('tcp://localhost:1883');
    await page.locator('#mqtt-broker-url').fill('tcp://broker.test:1883');
    await page.locator('#mqtt-group-id').fill('TestGroup');
    await page.locator('#mqtt-edge-node').fill('TestNode');
    await nextBtn.click();

    // Step 4: Add-ons
    await expect(page.getByRole('heading', { name: 'Add-ons' })).toBeVisible();
    await nextBtn.click();

    // Step 5: Review
    await expect(page.getByRole('heading', { name: 'Review Configuration' })).toBeVisible();
    await expect(page.getByText('Sparkplug Gateway')).toBeVisible();
    await expect(page.getByText('tcp://broker.test:1883')).toBeVisible();
    await expect(page.getByText('TestGroup / TestNode')).toBeVisible();

    // Apply
    await page.getByRole('button', { name: 'Apply & Start' }).click();
    await expect(page.getByRole('heading', { name: 'Setup Complete' })).toBeVisible({ timeout: 15_000 });
    await expect(page.getByRole('button', { name: 'Continue' })).toBeVisible();
  });

  test('back button navigates to previous step', async ({ page }) => {
    await mockFreshInstall(page);
    await page.goto('/setup');

    await page.getByText('Sparkplug Gateway').first().click();
    await page.getByRole('button', { name: 'Next' }).click();
    await expect(page.getByRole('heading', { name: 'Select Protocols' })).toBeVisible();

    await page.getByRole('button', { name: 'Back' }).click();
    await expect(page.getByRole('heading', { name: 'Choose an Architecture' })).toBeVisible();
  });

  test('back button is hidden on first step', async ({ page }) => {
    await mockFreshInstall(page);
    await page.goto('/setup');
    await expect(page.getByRole('heading', { name: 'Quickstart Setup' })).toBeVisible();
    await expect(page.getByRole('button', { name: 'Back' })).not.toBeVisible();
  });

  test('continue button dismisses setup and navigates to dashboard', async ({ page }) => {
    await mockFreshInstall(page, {
      serviceStatuses: SERVICE_STATUSES_ALL_ACTIVE,
    });
    await page.goto('/setup');

    await page.getByText('Sparkplug Gateway').first().click();
    await page.getByRole('button', { name: 'Next' }).click();
    await page.getByText('EtherNet/IP').first().click();
    await page.getByRole('button', { name: 'Next' }).click();
    await page.getByRole('button', { name: 'Next' }).click();
    await page.getByRole('button', { name: 'Next' }).click();

    await page.getByRole('button', { name: 'Apply & Start' }).click();
    await expect(page.getByRole('heading', { name: 'Setup Complete' })).toBeVisible({ timeout: 15_000 });

    await page.getByRole('button', { name: 'Continue' }).click();
    await expect(page).toHaveURL('/');
  });
});

// ---------------------------------------------------------------------------
// Sparkplug Gateway with GitOps add-on
// ---------------------------------------------------------------------------

test.describe('Sparkplug Gateway with GitOps', () => {
  test('selecting gitops add-on inserts gitops config step before review', async ({ page }) => {
    await mockFreshInstall(page);
    await page.goto('/setup');

    await page.getByText('Sparkplug Gateway').first().click();
    await page.getByRole('button', { name: 'Next' }).click();
    await page.getByText('EtherNet/IP').first().click();
    await page.getByRole('button', { name: 'Next' }).click();
    await page.getByRole('button', { name: 'Next' }).click();

    await expect(page.getByRole('heading', { name: 'Add-ons' })).toBeVisible();
    await page.getByText('GitOps').first().click();
    await page.getByRole('button', { name: 'Next' }).click();

    await expect(page.getByRole('heading', { name: 'Configure GitOps' })).toBeVisible();
    await expect(page.getByRole('button', { name: 'Next' })).toBeDisabled();

    await page.locator('#gitops-repo-url').fill('git@github.com:test/config.git');
    await expect(page.getByRole('button', { name: 'Next' })).toBeEnabled();
    await page.getByRole('button', { name: 'Next' }).click();

    await expect(page.getByRole('heading', { name: 'Review Configuration' })).toBeVisible();
    await expect(page.getByText('git@github.com:test/config.git')).toBeVisible();
  });

  test('SSH key section shows existing key and copy button', async ({ page }) => {
    await mockFreshInstall(page);
    await page.goto('/setup');

    await page.getByText('Sparkplug Gateway').first().click();
    await page.getByRole('button', { name: 'Next' }).click();
    await page.getByText('EtherNet/IP').first().click();
    await page.getByRole('button', { name: 'Next' }).click();
    await page.getByRole('button', { name: 'Next' }).click();
    await page.getByText('GitOps').first().click();
    await page.getByRole('button', { name: 'Next' }).click();

    await expect(page.getByText('ssh-ed25519 AAAA...test')).toBeVisible();
    await expect(page.getByRole('button', { name: 'Copy Public Key' })).toBeVisible();
  });

  test('test connection button shows success', async ({ page }) => {
    await mockFreshInstall(page);
    await page.goto('/setup');

    await page.getByText('Sparkplug Gateway').first().click();
    await page.getByRole('button', { name: 'Next' }).click();
    await page.getByText('EtherNet/IP').first().click();
    await page.getByRole('button', { name: 'Next' }).click();
    await page.getByRole('button', { name: 'Next' }).click();
    await page.getByText('GitOps').first().click();
    await page.getByRole('button', { name: 'Next' }).click();

    await page.locator('#gitops-repo-url').fill('git@github.com:test/config.git');
    await page.getByRole('button', { name: 'Test Connection' }).click();
    await expect(page.getByText('Connection successful')).toBeVisible();
  });

  test('test connection shows failure message', async ({ page }) => {
    await mockFreshInstall(page, {
      extraRoutes: {
        '**/api/v1/gitops/test-connection': (route) =>
          route.fulfill({
            status: 200,
            contentType: 'application/json',
            body: JSON.stringify({ success: false, error: 'Permission denied (publickey)' }),
          }),
      },
    });
    await page.goto('/setup');

    await page.getByText('Sparkplug Gateway').first().click();
    await page.getByRole('button', { name: 'Next' }).click();
    await page.getByText('EtherNet/IP').first().click();
    await page.getByRole('button', { name: 'Next' }).click();
    await page.getByRole('button', { name: 'Next' }).click();
    await page.getByText('GitOps').first().click();
    await page.getByRole('button', { name: 'Next' }).click();

    await page.locator('#gitops-repo-url').fill('git@github.com:test/config.git');
    await page.getByRole('button', { name: 'Test Connection' }).click();
    await expect(page.getByText('Permission denied (publickey)')).toBeVisible();
  });
});

// ---------------------------------------------------------------------------
// NAT Gateway — full happy path
// ---------------------------------------------------------------------------

test.describe('NAT Gateway wizard', () => {
  test('full flow: select NAT → add-ons → review → apply', async ({ page }) => {
    await mockFreshInstall(page, {
      serviceStatuses: SERVICE_STATUSES_ALL_ACTIVE,
    });
    await page.goto('/setup');

    await page.getByText('NAT').first().click();
    await page.getByRole('button', { name: 'Next' }).click();

    await expect(page.getByRole('heading', { name: 'Add-ons' })).toBeVisible();
    await page.getByRole('button', { name: 'Next' }).click();

    await expect(page.getByRole('heading', { name: 'Review Configuration' })).toBeVisible();
    await expect(page.getByText('Network Manager')).toBeVisible();
    await expect(page.getByText('Firewall (nftables)')).toBeVisible();

    await page.getByRole('button', { name: 'Apply & Start' }).click();
    await expect(page.getByRole('heading', { name: 'Setup Complete' })).toBeVisible({ timeout: 15_000 });
  });

  test('NAT with GitOps add-on shows gitops config step', async ({ page }) => {
    await mockFreshInstall(page);
    await page.goto('/setup');

    await page.getByText('NAT').first().click();
    await page.getByRole('button', { name: 'Next' }).click();

    await page.getByText('GitOps').first().click();
    await page.getByRole('button', { name: 'Next' }).click();

    await expect(page.getByRole('heading', { name: 'Configure GitOps' })).toBeVisible();
  });
});

// ---------------------------------------------------------------------------
// Validation edge cases
// ---------------------------------------------------------------------------

test.describe('Setup wizard validation', () => {
  test('MQTT step: clearing required fields disables Next', async ({ page }) => {
    await mockFreshInstall(page);
    await page.goto('/setup');

    await page.getByText('Sparkplug Gateway').first().click();
    await page.getByRole('button', { name: 'Next' }).click();
    await page.getByText('EtherNet/IP').first().click();
    await page.getByRole('button', { name: 'Next' }).click();

    const nextBtn = page.getByRole('button', { name: 'Next' });
    await expect(nextBtn).toBeEnabled();

    await page.locator('#mqtt-broker-url').fill('');
    await expect(nextBtn).toBeDisabled();

    await page.locator('#mqtt-broker-url').fill('tcp://test:1883');
    await page.locator('#mqtt-group-id').fill('');
    await expect(nextBtn).toBeDisabled();

    await page.locator('#mqtt-group-id').fill('Group');
    await page.locator('#mqtt-edge-node').fill('');
    await expect(nextBtn).toBeDisabled();
  });

  test('protocol step requires at least one protocol selected', async ({ page }) => {
    await mockFreshInstall(page);
    await page.goto('/setup');

    await page.getByText('Sparkplug Gateway').first().click();
    await page.getByRole('button', { name: 'Next' }).click();

    const nextBtn = page.getByRole('button', { name: 'Next' });
    await expect(nextBtn).toBeDisabled();

    await page.getByText('EtherNet/IP').first().click();
    await expect(nextBtn).toBeEnabled();

    await page.getByText('EtherNet/IP').first().click();
    await expect(nextBtn).toBeDisabled();
  });

  test('switching archetype resets to step 0', async ({ page }) => {
    await mockFreshInstall(page);
    await page.goto('/setup');

    await page.getByText('Sparkplug Gateway').first().click();
    await page.getByRole('button', { name: 'Next' }).click();
    await expect(page.getByRole('heading', { name: 'Select Protocols' })).toBeVisible();

    await page.getByRole('button', { name: 'Back' }).click();
    await page.getByText('NAT').first().click();

    await expect(page.getByRole('heading', { name: 'Choose an Architecture' })).toBeVisible();
  });
});

// ---------------------------------------------------------------------------
// Experimental / unavailable protocols
// ---------------------------------------------------------------------------

test.describe('Protocol availability', () => {
  test('unavailable protocols show Future badge and are not selectable', async ({ page }) => {
    await mockFreshInstall(page, { modules: MODULES_STABLE });
    await page.goto('/setup');

    await page.getByText('Sparkplug Gateway').first().click();
    await page.getByRole('button', { name: 'Next' }).click();

    await expect(page.getByText('Future').first()).toBeVisible();

    await page.getByText('EtherNet/IP').first().click();
    await expect(page.getByRole('button', { name: 'Next' })).toBeEnabled();
  });

  test('experimental protocols show Experimental badge but are selectable', async ({ page }) => {
    await mockFreshInstall(page, { modules: MODULES_ALL });
    await page.goto('/setup');

    await page.getByText('Sparkplug Gateway').first().click();
    await page.getByRole('button', { name: 'Next' }).click();

    await expect(page.getByText('Experimental').first()).toBeVisible();
  });
});

// ---------------------------------------------------------------------------
// Re-running wizard with existing config
// ---------------------------------------------------------------------------

test.describe('Wizard with existing configuration', () => {
  test('shows notice about existing services', async ({ page }) => {
    await mockFreshInstall(page, {
      desiredServices: DESIRED_SERVICES_SPARKPLUG,
      mqttConfig: MQTT_CONFIG_EXISTING,
    });
    await page.goto('/setup');

    await expect(
      page.getByText('Services are already configured'),
    ).toBeVisible();
  });

  test('pre-populates MQTT config from existing values', async ({ page }) => {
    await mockFreshInstall(page, {
      desiredServices: DESIRED_SERVICES_SPARKPLUG,
      mqttConfig: MQTT_CONFIG_EXISTING,
    });
    await page.goto('/setup');

    // ethernetip is pre-selected from desired services, so Next is already enabled
    await page.getByText('Sparkplug Gateway').first().click();
    await page.getByRole('button', { name: 'Next' }).click();
    await expect(page.getByRole('button', { name: 'Next' })).toBeEnabled();
    await page.getByRole('button', { name: 'Next' }).click();

    await expect(page.locator('#mqtt-broker-url')).toHaveValue('tcp://broker.local:1883');
    await expect(page.locator('#mqtt-group-id')).toHaveValue('PlantA');
    await expect(page.locator('#mqtt-edge-node')).toHaveValue('Line1');
  });

  test('pre-populates selected protocols from desired services', async ({ page }) => {
    await mockFreshInstall(page, {
      desiredServices: DESIRED_SERVICES_SPARKPLUG,
    });
    await page.goto('/setup');

    await page.getByText('Sparkplug Gateway').first().click();
    await page.getByRole('button', { name: 'Next' }).click();

    await expect(page.getByRole('button', { name: 'Next' })).toBeEnabled();
  });
});

// ---------------------------------------------------------------------------
// Apply errors
// ---------------------------------------------------------------------------

test.describe('Apply configuration errors', () => {
  test('shows error when MQTT config write fails', async ({ page }) => {
    await mockFreshInstall(page, {
      extraRoutes: {
        '**/api/v1/config/mqtt/**': (route) => {
          if (route.request().method() === 'PUT') {
            return jsonError(route, 'KV bucket unavailable');
          }
          return route.fallback();
        },
      },
    });
    await page.goto('/setup');

    await page.getByText('Sparkplug Gateway').first().click();
    await page.getByRole('button', { name: 'Next' }).click();
    await page.getByText('EtherNet/IP').first().click();
    await page.getByRole('button', { name: 'Next' }).click();
    await page.getByRole('button', { name: 'Next' }).click();
    await page.getByRole('button', { name: 'Next' }).click();

    await page.getByRole('button', { name: 'Apply & Start' }).click();
    await expect(page.getByText('KV bucket unavailable')).toBeVisible({ timeout: 10_000 });
  });

  test('shows error when module enable fails', async ({ page }) => {
    await mockFreshInstall(page, {
      extraRoutes: {
        '**/api/v1/orchestrator/desired-services/**': (route) => {
          if (route.request().method() === 'PUT') {
            return jsonError(route, 'Binary not found');
          }
          return route.fallback();
        },
      },
    });
    await page.goto('/setup');

    await page.getByText('Sparkplug Gateway').first().click();
    await page.getByRole('button', { name: 'Next' }).click();
    await page.getByText('EtherNet/IP').first().click();
    await page.getByRole('button', { name: 'Next' }).click();
    await page.getByRole('button', { name: 'Next' }).click();
    await page.getByRole('button', { name: 'Next' }).click();

    await page.getByRole('button', { name: 'Apply & Start' }).click();
    await expect(page.getByText('Binary not found')).toBeVisible({ timeout: 10_000 });
  });

  test('service errored state shows error', async ({ page }) => {
    await mockFreshInstall(page, {
      serviceStatuses: [
        { moduleId: 'ethernetip', systemdState: 'failed', reconcileState: 'error' },
      ],
    });
    await page.goto('/setup');

    await page.getByText('Sparkplug Gateway').first().click();
    await page.getByRole('button', { name: 'Next' }).click();
    await page.getByText('EtherNet/IP').first().click();
    await page.getByRole('button', { name: 'Next' }).click();
    await page.getByRole('button', { name: 'Next' }).click();
    await page.getByRole('button', { name: 'Next' }).click();

    await page.getByRole('button', { name: 'Apply & Start' }).click();
    await expect(page.getByText('ethernetip failed to start')).toBeVisible({ timeout: 15_000 });
  });
});

// ---------------------------------------------------------------------------
// GitOps edge cases
// ---------------------------------------------------------------------------

test.describe('GitOps config edge cases', () => {
  test('shows git not installed warning and install button', async ({ page }) => {
    await mockFreshInstall(page, {
      extraRoutes: {
        '**/api/v1/gitops/git-check': (route) =>
          route.fulfill({
            status: 200,
            contentType: 'application/json',
            body: JSON.stringify({ installed: false }),
          }),
      },
    });
    await page.goto('/setup');

    await page.getByText('Sparkplug Gateway').first().click();
    await page.getByRole('button', { name: 'Next' }).click();
    await page.getByText('EtherNet/IP').first().click();
    await page.getByRole('button', { name: 'Next' }).click();
    await page.getByRole('button', { name: 'Next' }).click();
    await page.getByText('GitOps').first().click();
    await page.getByRole('button', { name: 'Next' }).click();

    await expect(page.getByText('Git is not installed')).toBeVisible();
    await expect(page.getByRole('button', { name: 'Install Git' })).toBeVisible();
  });

  test('shows generate key button when no SSH key exists', async ({ page }) => {
    await mockFreshInstall(page, {
      extraRoutes: {
        '**/api/v1/gitops/ssh-key': (route) =>
          route.fulfill({
            status: 200,
            contentType: 'application/json',
            body: JSON.stringify({ exists: false, publicKey: '', path: '' }),
          }),
      },
    });
    await page.goto('/setup');

    await page.getByText('Sparkplug Gateway').first().click();
    await page.getByRole('button', { name: 'Next' }).click();
    await page.getByText('EtherNet/IP').first().click();
    await page.getByRole('button', { name: 'Next' }).click();
    await page.getByRole('button', { name: 'Next' }).click();
    await page.getByText('GitOps').first().click();
    await page.getByRole('button', { name: 'Next' }).click();

    await expect(page.getByRole('button', { name: 'Generate SSH Key' })).toBeVisible();
  });

  test('pre-populates gitops config from existing values', async ({ page }) => {
    await mockFreshInstall(page, {
      desiredServices: [{ moduleId: 'gitops', version: 'latest', running: true }],
      gitopsConfig: GITOPS_CONFIG_EXISTING,
    });
    await page.goto('/setup');

    await page.getByText('Sparkplug Gateway').first().click();
    await page.getByRole('button', { name: 'Next' }).click();
    await page.getByText('EtherNet/IP').first().click();
    await page.getByRole('button', { name: 'Next' }).click();
    await page.getByRole('button', { name: 'Next' }).click();
    // gitops should be pre-selected from desired services
    await page.getByRole('button', { name: 'Next' }).click();

    await expect(page.locator('#gitops-repo-url')).toHaveValue('git@github.com:org/config.git');
    await expect(page.locator('#gitops-branch')).toHaveValue('main');
  });
});

// ---------------------------------------------------------------------------
// Wizard stepper navigation
// ---------------------------------------------------------------------------

test.describe('Wizard stepper', () => {
  test('completed steps are clickable for navigation', async ({ page }) => {
    await mockFreshInstall(page);
    await page.goto('/setup');

    await page.getByText('Sparkplug Gateway').first().click();
    await page.getByRole('button', { name: 'Next' }).click();
    await page.getByText('EtherNet/IP').first().click();
    await page.getByRole('button', { name: 'Next' }).click();

    await page.locator('.wizard-stepper .step.completed').first().click();
    await expect(page.getByRole('heading', { name: 'Choose an Architecture' })).toBeVisible();
  });
});
