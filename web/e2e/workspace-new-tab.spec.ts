import { test, expect, type Route } from '@playwright/test';
import { mockConfiguredSystem } from './helpers/mock-api';

/**
 * E2E regression: typing inside a brand-new "unsaved" function tab must
 * not steal focus back to the previously-selected program tab. Prior bug:
 * ProgramEditor's $effect calls setTabLabel on every keystroke, which
 * mutates state.tabs; the selection-driven $effect in +page.svelte read
 * state.tabs via workspaceTabs.open() and re-activated the old selection.
 */

function json(route: Route, data: unknown, status = 200) {
	return route.fulfill({
		status,
		contentType: 'application/json',
		body: JSON.stringify(data)
	});
}

test('typing in a new function tab does not activate another tab', async ({ page }) => {
	await mockConfiguredSystem(page);

	const mainProgram = {
		name: 'main',
		language: 'starlark',
		source: 'def main():\n    pass\n',
		updatedAt: Date.now()
	};

	await page.route('**/api/v1/plcs/plc/config', (r) =>
		json(r, {
			plcId: 'plc',
			variables: {},
			devices: {},
			tasks: {},
			updatedAt: Date.now()
		})
	);
	await page.route('**/api/v1/plcs/plc/tasks', (r) => json(r, {}));
	await page.route('**/api/v1/plcs/plc/programs', (r) =>
		json(r, [{ name: 'main', language: 'starlark', tags: [] }])
	);
	await page.route('**/api/v1/plcs/plc/programs/main', (r) => json(r, mainProgram));
	await page.route('**/api/v1/plcs/plc/tests', (r) => json(r, []));
	await page.route('**/api/v1/plcs/plc/templates', (r) => json(r, []));
	await page.route('**/api/v1/services/plc/logs', (r) => json(r, []));
	await page.route('**/api/v1/plcs/plc/lsp', (r) => r.fulfill({ status: 404, body: '' }));
	await page.route('**/api/v1/plcs/plc/programs/main/try', (r) =>
		json(r, { session: null })
	);

	await page.goto('/services/plc/workspace');

	// Open `main` by clicking it in the Navigator.
	await page.locator('.navigator .item').filter({ hasText: 'main' }).click();
	const tabStrip = page.locator('.editor-tabs .tab-strip');
	await expect(tabStrip.getByText('main', { exact: true })).toBeVisible();

	// Click the "+" on FUNCTIONS to create a new unsaved function tab.
	await page
		.locator('.navigator .section')
		.filter({ hasText: 'Functions' })
		.getByRole('button', { name: /New function/i })
		.click();

	// The new tab should now be active — it carries a pencil icon and the
	// derived name "new_function" from the placeholder body.
	await expect(tabStrip.getByText('new_function', { exact: true })).toBeVisible();

	// Type a character in the new tab's editor. Use the visible CodeMirror
	// instance (the one whose editor-host is not .hidden).
	const activeEditor = page.locator('.editor-host:not(.hidden) .cm-content');
	await activeEditor.click();
	await page.keyboard.press('End');
	await page.keyboard.type('x');

	// After typing, the new tab must STILL be active. Previously the
	// selection-driven effect would reopen/activate `main` on each tabs
	// mutation, making `main` the active tab after the first keystroke.
	const activeTab = tabStrip.locator('.tab.active, [aria-selected="true"]');
	await expect(activeTab).toContainText('new_function');
});
