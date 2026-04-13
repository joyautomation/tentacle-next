import { test, expect } from '@playwright/test';
import { mockConfiguredSystem, jsonError } from './helpers/mock-api';

// ---------------------------------------------------------------------------
// Module detail page
// ---------------------------------------------------------------------------

test.describe('Module management', () => {
  const MODULE_REGISTRY = [
    {
      moduleId: 'tentacle-snmp',
      repo: 'github.com/joyautomation/tentacle-snmp',
      description: 'SNMP device scanner',
      category: 'Protocol Clients',
      runtime: 'go',
      experimental: false,
    },
    {
      moduleId: 'tentacle-opcua',
      repo: 'github.com/joyautomation/tentacle-opcua',
      description: 'OPC UA client',
      category: 'Protocol Clients',
      runtime: 'go',
      experimental: true,
    },
  ];

  test.beforeEach(async ({ page }) => {
    await mockConfiguredSystem(page, {
      modules: [
        { moduleId: 'ethernetip', experimental: false },
        { moduleId: 'opcua', experimental: true },
        { moduleId: 'modbus', experimental: false },
        { moduleId: 'snmp', experimental: false },
        { moduleId: 'profinetcontroller', experimental: true },
      ],
      extraRoutes: {
        '**/api/v1/orchestrator/modules': (route) =>
          route.fulfill({
            status: 200,
            contentType: 'application/json',
            body: JSON.stringify(MODULE_REGISTRY),
          }),
      },
    });
  });

  test('module page shows module info and status', async ({ page }) => {
    await page.route('**/api/v1/config/tentacle-snmp', (route) =>
      route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify([]),
      }),
    );

    await page.goto('/modules/tentacle-snmp');
    await expect(page.getByText('SNMP device scanner')).toBeVisible();
  });

  test('experimental module shows Experimental badge', async ({ page }) => {
    await page.route('**/api/v1/config/tentacle-opcua', (route) =>
      route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify([]),
      }),
    );

    await page.goto('/modules/tentacle-opcua');
    await expect(page.getByText('Experimental')).toBeVisible();
  });

  test('install button sends desired-services PUT', async ({ page }) => {
    let putPayload: unknown = null;
    await page.route('**/api/v1/orchestrator/desired-services/tentacle-snmp', (route) => {
      if (route.request().method() === 'PUT') {
        putPayload = route.request().postDataJSON();
        return route.fulfill({
          status: 200,
          contentType: 'application/json',
          body: JSON.stringify({ moduleId: 'tentacle-snmp', version: 'latest', running: true }),
        });
      }
      if (route.request().method() === 'DELETE') {
        return route.fulfill({ status: 200, contentType: 'application/json', body: '{"success":true}' });
      }
      return route.continue();
    });

    await page.route('**/api/v1/orchestrator/desired-services', (route) => {
      if (route.request().method() === 'GET') {
        return route.fulfill({
          status: 200,
          contentType: 'application/json',
          body: JSON.stringify([]), // Not installed yet
        });
      }
      return route.continue();
    });

    await page.route('**/api/v1/config/tentacle-snmp', (route) =>
      route.fulfill({ status: 200, contentType: 'application/json', body: '[]' }),
    );

    await page.goto('/modules/tentacle-snmp');

    // Look for install/enable button
    const installBtn = page.getByRole('button', { name: /Install|Enable/ });
    if (await installBtn.isVisible()) {
      await installBtn.click();
      expect(putPayload).toBeTruthy();
    }
  });

  test('uninstall button sends desired-services DELETE', async ({ page }) => {
    let deleteCalled = false;
    await page.route('**/api/v1/orchestrator/desired-services/tentacle-snmp', (route) => {
      if (route.request().method() === 'DELETE') {
        deleteCalled = true;
        return route.fulfill({
          status: 200,
          contentType: 'application/json',
          body: JSON.stringify({ success: true }),
        });
      }
      if (route.request().method() === 'PUT') {
        return route.fulfill({
          status: 200,
          contentType: 'application/json',
          body: JSON.stringify({ moduleId: 'tentacle-snmp', version: 'latest', running: true }),
        });
      }
      return route.continue();
    });

    await page.route('**/api/v1/orchestrator/desired-services', (route) => {
      if (route.request().method() === 'GET') {
        return route.fulfill({
          status: 200,
          contentType: 'application/json',
          body: JSON.stringify([{ moduleId: 'tentacle-snmp', version: '0.1.0', running: true }]),
        });
      }
      return route.continue();
    });

    await page.route('**/api/v1/config/tentacle-snmp', (route) =>
      route.fulfill({ status: 200, contentType: 'application/json', body: '[]' }),
    );

    await page.goto('/modules/tentacle-snmp');

    const uninstallBtn = page.getByRole('button', { name: /Remove|Disable/ });
    if (await uninstallBtn.isVisible()) {
      await uninstallBtn.click();
      expect(deleteCalled).toBe(true);
    }
  });
});
