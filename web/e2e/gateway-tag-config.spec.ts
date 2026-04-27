import { test, expect, type Route } from '@playwright/test';
import { mockConfiguredSystem } from './helpers/mock-api';

// Saved gateway config: the UDT template was previously saved with members
// `value` and `units`, but `min` and `max` were unchecked at save time so
// the persisted member list dropped them. The browse cache (below) still
// knows about all four members.
const SAVED_GATEWAY_CONFIG = {
  gatewayId: 'gateway',
  devices: [
    {
      deviceId: 'plc1',
      protocol: 'ethernetip',
      host: '10.0.0.5',
      slot: 0,
    },
  ],
  variables: [],
  udtTemplates: [
    {
      name: 'Analog',
      version: '1.0',
      members: [
        { name: 'value', datatype: 'number', templateRef: null },
        { name: 'units', datatype: 'string', templateRef: null },
      ],
    },
  ],
  udtVariables: [
    {
      id: 'flow1',
      deviceId: 'plc1',
      tag: 'flow1',
      templateName: 'Analog',
      memberTags: { value: 'flow1.value', units: 'flow1.units' },
      memberCipTypes: { value: 'REAL', units: 'STRING' },
    },
  ],
  availableProtocols: ['ethernetip'],
  updatedAt: new Date().toISOString(),
};

// Fresh browse of the device — the PLC's UDT still defines all four members.
const BROWSE_CACHE = {
  deviceId: 'plc1',
  protocol: 'ethernetip',
  items: [],
  udts: [
    {
      name: 'Analog',
      members: [
        { name: 'value', datatype: 'REAL', cipType: 'REAL', udtType: '', isArray: false },
        { name: 'units', datatype: 'STRING', cipType: 'STRING', udtType: '', isArray: false },
        { name: 'min', datatype: 'REAL', cipType: 'REAL', udtType: '', isArray: false },
        { name: 'max', datatype: 'REAL', cipType: 'REAL', udtType: '', isArray: false },
      ],
    },
  ],
  structTags: { flow1: 'Analog' },
  cachedAt: new Date().toISOString(),
};

test.describe('Gateway tag config — UDT template members', () => {
  test('shows all browse-cache members even when saved template dropped some', async ({ page }) => {
    await mockConfiguredSystem(page, {
      extraRoutes: {
        '**/api/v1/gateways/gateway': (route: Route) => {
          if (route.request().method() === 'GET') {
            return route.fulfill({
              status: 200,
              contentType: 'application/json',
              body: JSON.stringify(SAVED_GATEWAY_CONFIG),
            });
          }
          return route.continue();
        },
        '**/api/v1/gateways/gateway/browse-cache/plc1': (route: Route) =>
          route.fulfill({
            status: 200,
            contentType: 'application/json',
            body: JSON.stringify(BROWSE_CACHE),
          }),
        '**/api/v1/gateways/browse-states': (route: Route) =>
          route.fulfill({ status: 200, contentType: 'application/json', body: '[]' }),
      },
    });

    await page.goto('/services/gateway/tag-config');

    // Template auto-selects on load. Switch to the Template Defaults tab.
    await page.getByRole('button', { name: 'Template Defaults', exact: true }).click();

    // All four members should be visible — including the ones whose MQTT
    // toggle was previously unchecked.
    const table = page.locator('.tpl-table');
    await expect(table.getByText('value', { exact: true })).toBeVisible();
    await expect(table.getByText('units', { exact: true })).toBeVisible();
    await expect(table.getByText('min', { exact: true })).toBeVisible();
    await expect(table.getByText('max', { exact: true })).toBeVisible();

    // The unchecked members must show their MQTT toggle in the off state so
    // the user can re-enable them.
    const minRow = table.locator('tr', { has: page.getByText('min', { exact: true }) });
    const maxRow = table.locator('tr', { has: page.getByText('max', { exact: true }) });
    await expect(minRow.locator('input[type="checkbox"]')).not.toBeChecked();
    await expect(maxRow.locator('input[type="checkbox"]')).not.toBeChecked();

    // Sanity: included members are still on.
    const valueRow = table.locator('tr', { has: page.getByText('value', { exact: true }) });
    await expect(valueRow.locator('input[type="checkbox"]')).toBeChecked();
  });

  test('toggling a member when no template is saved yet pops up the save bar', async ({ page }) => {
    // Empty saved config — only browse-cache UDTs exist. Replicates the case
    // where the user is preparing template defaults before publishing any
    // instances. Toggling a member off should still mark dirty so the user
    // can save (and then publish an instance to actually persist).
    const EMPTY_GATEWAY_CONFIG = {
      gatewayId: 'gateway',
      devices: [{ deviceId: 'plc1', protocol: 'ethernetip', host: '10.0.0.5', slot: 0 }],
      variables: [],
      udtTemplates: [],
      udtVariables: [],
      availableProtocols: ['ethernetip'],
      updatedAt: new Date().toISOString(),
    };

    await mockConfiguredSystem(page, {
      extraRoutes: {
        '**/api/v1/gateways/gateway': (route: Route) => {
          if (route.request().method() === 'GET') {
            return route.fulfill({
              status: 200,
              contentType: 'application/json',
              body: JSON.stringify(EMPTY_GATEWAY_CONFIG),
            });
          }
          return route.continue();
        },
        '**/api/v1/gateways/gateway/browse-cache/plc1': (route: Route) =>
          route.fulfill({
            status: 200,
            contentType: 'application/json',
            body: JSON.stringify(BROWSE_CACHE),
          }),
        '**/api/v1/gateways/browse-states': (route: Route) =>
          route.fulfill({ status: 200, contentType: 'application/json', body: '[]' }),
      },
    });

    await page.goto('/services/gateway/tag-config');
    await page.getByRole('button', { name: 'Template Defaults', exact: true }).click();

    // No save bar yet — clean state.
    await expect(page.locator('.save-bar')).toHaveCount(0);

    // Toggle a member off.
    const table = page.locator('.tpl-table');
    const valueRow = table.locator('tr', { has: page.getByText('value', { exact: true }) });
    await valueRow.locator('label.toggle-switch').click();

    // Save bar should fly in.
    await expect(page.locator('.save-bar')).toBeVisible();
    await expect(page.getByRole('button', { name: /Save Changes/ })).toBeVisible();

    // Toggling back to original state should clear dirty.
    await valueRow.locator('label.toggle-switch').click();
    await expect(page.locator('.save-bar')).toHaveCount(0);
  });
});
