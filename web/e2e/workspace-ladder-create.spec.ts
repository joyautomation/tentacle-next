import { test, expect, type Route } from '@playwright/test';
import { mockConfiguredSystem } from './helpers/mock-api';

/**
 * E2E regression: creating a new ladder (LD) function from the Navigator's
 * "+ Ladder Diagram" menu and clicking Create must add the program to the
 * Functions section. Earlier the user saw no error but no entry appeared.
 */

function json(route: Route, data: unknown, status = 200) {
	return route.fulfill({
		status,
		contentType: 'application/json',
		body: JSON.stringify(data)
	});
}

test('creating a ladder program adds it to the Functions section', async ({ page }) => {
	await mockConfiguredSystem(page);

	// Mutable program list so PUT can append, GET reflects changes.
	type ProgramListItem = { name: string; language: string; tags?: string[]; updatedAt?: number };
	const programs: ProgramListItem[] = [];
	const programBodies: Record<string, unknown> = {};

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
	await page.route('**/api/v1/plcs/plc/programs', (r) => {
		if (r.request().method() === 'GET') return json(r, programs);
		return r.continue();
	});
	await page.route('**/api/v1/plcs/plc/programs/*', async (r) => {
		const url = r.request().url();
		const name = decodeURIComponent(url.split('/').pop()!.split('?')[0]);
		const method = r.request().method();
		if (method === 'PUT') {
			const body = JSON.parse(r.request().postData() ?? '{}');
			const item = {
				name: body.name ?? name,
				language: body.language,
				tags: body.tags ?? [],
				updatedAt: Date.now()
			};
			programs.push(item);
			programBodies[item.name] = { ...body, updatedAt: item.updatedAt };
			return json(r, programBodies[item.name]);
		}
		if (method === 'GET') {
			const stored = programBodies[name];
			if (stored) return json(r, stored);
			return json(r, { error: 'not found' }, 404);
		}
		return r.continue();
	});
	await page.route('**/api/v1/plcs/plc/tests', (r) => json(r, []));
	await page.route('**/api/v1/plcs/plc/templates', (r) => json(r, []));
	await page.route('**/api/v1/services/plc/logs', (r) => json(r, []));
	await page.route('**/api/v1/plcs/plc/lsp', (r) => r.fulfill({ status: 404, body: '' }));

	await page.goto('/services/plc/workspace');

	// Open "+ New function" menu, then click "Ladder Diagram".
	await page
		.locator('.navigator .section')
		.filter({ hasText: 'Functions' })
		.getByRole('button', { name: /New function/i })
		.click();
	await page.getByRole('menuitem', { name: /Ladder Diagram/i }).click();

	// The new tab should be active. Type a program name in the diagram-name input.
	const nameInput = page.locator('.lad-name input[type="text"]');
	await expect(nameInput).toBeVisible();
	await nameInput.fill('ladtest');

	// Click Create.
	const createBtn = page.getByRole('button', { name: /^Create$/ });
	await expect(createBtn).toBeEnabled();
	await createBtn.click();

	// After create, "ladtest" should appear in the Functions list.
	const navigator = page.locator('.navigator');
	await expect(navigator.locator('.item').filter({ hasText: 'ladtest' })).toBeVisible({
		timeout: 5_000
	});
});
