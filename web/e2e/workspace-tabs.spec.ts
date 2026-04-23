import { test, expect, type Route } from '@playwright/test';
import { mockConfiguredSystem } from './helpers/mock-api';

/**
 * E2E regression for workspace tab close: selecting an item in the
 * Navigator opens a tab, and clicking the tab's X must actually close
 * it. A previous bug reopened the closed tab because a selection-driven
 * $effect read state.tabs via workspaceTabs.open() — mutating state.tabs
 * in close() re-ran the effect, which re-opened the tab.
 */

function json(route: Route, data: unknown, status = 200) {
	return route.fulfill({
		status,
		contentType: 'application/json',
		body: JSON.stringify(data)
	});
}

test('closing a task tab does not immediately reopen it', async ({ page }) => {
	await mockConfiguredSystem(page);

	const task = {
		name: 'Main',
		description: 'Main Task',
		scanRateMs: 1000,
		programRef: 'main',
		enabled: true
	};
	await page.route('**/api/v1/plcs/plc/config', (r) =>
		json(r, {
			plcId: 'plc',
			variables: {},
			devices: {},
			tasks: { Main: task },
			updatedAt: Date.now()
		})
	);
	await page.route('**/api/v1/plcs/plc/tasks', (r) => json(r, { Main: task }));
	await page.route('**/api/v1/plcs/plc/programs', (r) => json(r, []));
	await page.route('**/api/v1/plcs/plc/tests', (r) => json(r, []));
	await page.route('**/api/v1/plcs/plc/templates', (r) => json(r, []));
	await page.route('**/api/v1/services/plc/logs', (r) => json(r, []));
	await page.route('**/api/v1/plcs/plc/lsp', (r) => r.fulfill({ status: 404, body: '' }));

	await page.goto('/services/plc/workspace');

	// Click the Main task in the Navigator to open it as a tab.
	await page.locator('.navigator .item').filter({ hasText: 'Main' }).click();

	const tabStrip = page.locator('.editor-tabs .tab-strip');
	await expect(tabStrip.getByText('Main', { exact: true })).toBeVisible();

	// Click the X on the task tab.
	await tabStrip
		.locator('.tab', { hasText: 'Main' })
		.getByRole('button', { name: /Close Main/ })
		.click();

	// Tab should disappear and stay gone — the selection-driven reopen
	// effect would have resurrected it before the fix.
	await expect(tabStrip.getByText('Main', { exact: true })).toHaveCount(0);
	await page.waitForTimeout(200);
	await expect(tabStrip.getByText('Main', { exact: true })).toHaveCount(0);
});
