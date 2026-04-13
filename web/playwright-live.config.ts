/**
 * Playwright config for Tier 2 pre-release tests.
 *
 * Runs against a live tentacle binary (no vite dev server, no API mocking).
 * The binary URL is passed via PLAYWRIGHT_BASE_URL env var from the
 * pre-release-test.sh script.
 *
 * Only runs tests tagged with @live (tests that work against a real backend).
 */
import { defineConfig, devices } from '@playwright/test';

export default defineConfig({
  testDir: './e2e',
  testMatch: '**/live-*.spec.ts',
  fullyParallel: false, // serial — single binary
  retries: 1,
  workers: 1,
  reporter: 'list',
  use: {
    baseURL: process.env.PLAYWRIGHT_BASE_URL ?? 'http://localhost:4000',
    trace: 'on-first-retry',
    screenshot: 'only-on-failure',
  },
  projects: [
    {
      name: 'chromium',
      use: { ...devices['Desktop Chrome'] },
    },
  ],
  // No webServer — the binary is started externally
});
