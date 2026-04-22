import { test, expect, type Route } from '@playwright/test';
import { mockConfiguredSystem } from './helpers/mock-api';
import type { PlcTest, PlcTestResult, TestListItem } from '../src/lib/types/plc';

/**
 * E2E for the PLC Tests UI in the workspace:
 *   create new test tab → save → run → pass dot appears.
 *
 * The Go backend is not running in CI, so every /plcs/plc/* route is
 * intercepted and answered from an in-memory store. The store mutates on
 * PUT/POST/DELETE so invalidateAll() → re-fetch produces consistent state.
 */

function json(route: Route, data: unknown, status = 200) {
	return route.fulfill({
		status,
		contentType: 'application/json',
		body: JSON.stringify(data)
	});
}

test.describe('PLC Tests UI', () => {
	test('create → save → run shows pass result', async ({ page }) => {
		await mockConfiguredSystem(page);

		// In-memory test store mutated by route handlers.
		const store = new Map<string, PlcTest>();

		// Minimal PLC routes so the workspace page load resolves.
		await page.route('**/api/v1/plcs/plc/config', (r) =>
			json(r, {
				name: 'plc',
				variables: {
					counter: { datatype: 'int', direction: 'internal', default: 0 }
				}
			})
		);
		await page.route('**/api/v1/plcs/plc/tasks', (r) => json(r, {}));
		await page.route('**/api/v1/plcs/plc/programs', (r) => json(r, []));
		await page.route('**/api/v1/plcs/plc/templates', (r) => json(r, []));

		// Tests list — derived live from `store` on each GET.
		await page.route('**/api/v1/plcs/plc/tests', (r) => {
			const method = r.request().method();
			if (method === 'GET') {
				const items: TestListItem[] = Array.from(store.values()).map((t) => ({
					name: t.name,
					description: t.description,
					updatedAt: t.updatedAt,
					updatedBy: t.updatedBy,
					lastResult: t.lastResult
				}));
				return json(r, items);
			}
			return r.fallback();
		});

		// Single-test GET / PUT / DELETE.
		await page.route('**/api/v1/plcs/plc/tests/*', (r) => {
			const url = new URL(r.request().url());
			const name = decodeURIComponent(url.pathname.split('/').pop() ?? '');
			const method = r.request().method();
			if (method === 'GET') {
				const t = store.get(name);
				if (!t) return json(r, { error: 'not found' }, 404);
				return json(r, t);
			}
			if (method === 'PUT') {
				const body = r.request().postDataJSON() as Partial<PlcTest>;
				const existing = store.get(name);
				const merged: PlcTest = {
					name: body.name ?? name,
					description: body.description,
					source: body.source ?? '',
					updatedAt: Date.now(),
					updatedBy: 'e2e',
					lastResult: existing?.lastResult
				};
				store.set(merged.name, merged);
				return json(r, merged);
			}
			if (method === 'DELETE') {
				store.delete(name);
				return json(r, { success: true });
			}
			return r.fallback();
		});

		// Run a single test — deterministic pass.
		await page.route('**/api/v1/plcs/plc/tests/*/run', (r) => {
			if (r.request().method() !== 'POST') return r.fallback();
			const url = new URL(r.request().url());
			const parts = url.pathname.split('/');
			const name = decodeURIComponent(parts[parts.length - 2] ?? '');
			const result: PlcTestResult = {
				name,
				status: 'pass',
				durationMs: 3,
				startedAt: Date.now(),
				logs: []
			};
			const t = store.get(name);
			if (t) store.set(name, { ...t, lastResult: result });
			return json(r, result);
		});

		// LSP WebSocket — not exercised here; let the connection fail silently.
		await page.route('**/api/v1/plcs/plc/lsp', (r) =>
			r.fulfill({ status: 404, body: '' })
		);

		await page.goto('/services/plc/workspace');

		// Open a new test tab from the Navigator.
		await page.getByRole('button', { name: 'New test', exact: true }).click();

		// TestEditor mounts; starter source contains `def test_example`.
		// The editor derives the tab label from the first def header.
		await expect(page.getByRole('button', { name: /Create/ })).toBeVisible();
		await expect(page.locator('.test-editor .test-name')).toHaveText('test_example');

		// Save → PUT → store now contains test_example.
		await page.getByRole('button', { name: /Create/ }).click();

		// After save the button label swaps to "Save" (no longer new).
		await expect(page.getByRole('button', { name: 'Save', exact: true })).toBeVisible();

		// Test now appears in Navigator list.
		await expect(
			page.locator('.navigator .item').filter({ hasText: 'test_example' })
		).toBeVisible();

		// Run it — POST returns pass → dot + result panel become visible.
		await page.getByRole('button', { name: /Run/, exact: false }).click();

		// Pass dot appears in the TestEditor header.
		await expect(page.locator('.test-editor .ed-header .status-dot.pass')).toBeVisible();

		// Results panel shows "Passed".
		await expect(page.locator('.results-panel .results-title')).toHaveText('Passed');
	});
});
