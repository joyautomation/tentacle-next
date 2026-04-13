/**
 * Shared mock API helpers and fixture data for Playwright e2e tests.
 *
 * Tests intercept /api/v1/* routes so the SvelteKit frontend runs against
 * deterministic data without needing the Go backend.
 */
import { type Page, type Route } from '@playwright/test';

// ---------------------------------------------------------------------------
// Fixture data
// ---------------------------------------------------------------------------

export const MODULES_ALL = [
  { moduleId: 'ethernetip', experimental: false },
  { moduleId: 'opcua', experimental: true },
  { moduleId: 'modbus', experimental: false },
  { moduleId: 'snmp', experimental: false },
  { moduleId: 'profinetcontroller', experimental: true },
];

export const MODULES_STABLE = [
  { moduleId: 'ethernetip', experimental: false },
  { moduleId: 'modbus', experimental: false },
  { moduleId: 'snmp', experimental: false },
];

export const SERVICES_RUNNING = [
  {
    serviceType: 'api',
    moduleId: 'api',
    startedAt: Date.now() - 60_000,
    version: '0.1.0',
    metadata: null,
    enabled: true,
  },
  {
    serviceType: 'orchestrator',
    moduleId: 'orchestrator',
    startedAt: Date.now() - 60_000,
    version: '0.1.0',
    metadata: { mode: 'monolith' },
    enabled: true,
  },
  {
    serviceType: 'ethernetip',
    moduleId: 'ethernetip',
    startedAt: Date.now() - 30_000,
    version: '0.1.0',
    metadata: null,
    enabled: true,
  },
  {
    serviceType: 'gateway',
    moduleId: 'gateway',
    startedAt: Date.now() - 30_000,
    version: '0.1.0',
    metadata: null,
    enabled: true,
  },
  {
    serviceType: 'mqtt',
    moduleId: 'mqtt',
    startedAt: Date.now() - 30_000,
    version: '0.1.0',
    metadata: { connected: true },
    enabled: true,
  },
];

export const DESIRED_SERVICES_SPARKPLUG = [
  { moduleId: 'ethernetip', version: 'latest', running: true },
  { moduleId: 'gateway', version: 'latest', running: true },
  { moduleId: 'mqtt', version: 'latest', running: true },
];

export const SERVICE_STATUSES_ALL_ACTIVE = [
  { moduleId: 'ethernetip', systemdState: 'active', reconcileState: 'ok' },
  { moduleId: 'gateway', systemdState: 'active', reconcileState: 'ok' },
  { moduleId: 'mqtt', systemdState: 'active', reconcileState: 'ok' },
  { moduleId: 'modbus', systemdState: 'active', reconcileState: 'ok' },
  { moduleId: 'snmp', systemdState: 'active', reconcileState: 'ok' },
  { moduleId: 'opcua', systemdState: 'active', reconcileState: 'ok' },
  { moduleId: 'network', systemdState: 'active', reconcileState: 'ok' },
  { moduleId: 'nftables', systemdState: 'active', reconcileState: 'ok' },
  { moduleId: 'gitops', systemdState: 'active', reconcileState: 'ok' },
  { moduleId: 'profinetcontroller', systemdState: 'active', reconcileState: 'ok' },
];

export const MQTT_CONFIG_EXISTING = [
  { moduleId: 'mqtt', envVar: 'MQTT_BROKER_URL', value: 'tcp://broker.local:1883' },
  { moduleId: 'mqtt', envVar: 'MQTT_CLIENT_ID', value: 'tentacle-mqtt' },
  { moduleId: 'mqtt', envVar: 'MQTT_GROUP_ID', value: 'PlantA' },
  { moduleId: 'mqtt', envVar: 'MQTT_EDGE_NODE', value: 'Line1' },
  { moduleId: 'mqtt', envVar: 'MQTT_USERNAME', value: '' },
  { moduleId: 'mqtt', envVar: 'MQTT_PASSWORD', value: '' },
];

export const GITOPS_CONFIG_EXISTING = [
  { moduleId: 'gitops', envVar: 'GITOPS_REPO_URL', value: 'git@github.com:org/config.git' },
  { moduleId: 'gitops', envVar: 'GITOPS_BRANCH', value: 'main' },
  { moduleId: 'gitops', envVar: 'GITOPS_PATH', value: 'config' },
  { moduleId: 'gitops', envVar: 'GITOPS_POLL_INTERVAL_S', value: '60' },
  { moduleId: 'gitops', envVar: 'GITOPS_AUTO_PUSH', value: 'true' },
  { moduleId: 'gitops', envVar: 'GITOPS_AUTO_PULL', value: 'true' },
];

export const SERVICE_STATUS_DEV_CAN_INSTALL = {
  mode: 'dev',
  systemdAvailable: true,
  unitExists: false,
  unitEnabled: false,
  unitActive: false,
  binaryInstalled: false,
  canInstall: true,
};

export const SERVICE_STATUS_DEV_CANNOT_INSTALL = {
  mode: 'dev',
  systemdAvailable: true,
  unitExists: false,
  unitEnabled: false,
  unitActive: false,
  binaryInstalled: false,
  canInstall: false,
  reason: 'not running as root',
};

export const SERVICE_STATUS_SYSTEMD = {
  mode: 'systemd',
  systemdAvailable: true,
  unitExists: true,
  unitEnabled: true,
  unitActive: true,
  binaryInstalled: true,
  canInstall: false,
};

// ---------------------------------------------------------------------------
// Route mocking helpers
// ---------------------------------------------------------------------------

type MockOverrides = {
  mode?: string;
  services?: unknown[];
  desiredServices?: unknown[];
  serviceStatuses?: unknown[];
  modules?: unknown[];
  mqttConfig?: unknown[];
  gitopsConfig?: unknown[];
  systemService?: unknown;
  /** Extra route handlers keyed by glob pattern */
  extraRoutes?: Record<string, (route: Route) => Promise<void> | void>;
};

/**
 * Set up API route interception for a fresh (no services) state.
 * Used for first-boot / setup wizard tests.
 */
export async function mockFreshInstall(page: Page, overrides: MockOverrides = {}) {
  const opts = {
    mode: 'dev',
    services: [],
    desiredServices: [],
    serviceStatuses: [],
    modules: MODULES_ALL,
    mqttConfig: [],
    gitopsConfig: [],
    systemService: SERVICE_STATUS_DEV_CAN_INSTALL,
    ...overrides,
  };

  await setupRoutes(page, opts);
}

/**
 * Set up API route interception for a configured system with running services.
 */
export async function mockConfiguredSystem(page: Page, overrides: MockOverrides = {}) {
  const opts = {
    mode: 'dev',
    services: SERVICES_RUNNING,
    desiredServices: DESIRED_SERVICES_SPARKPLUG,
    serviceStatuses: SERVICE_STATUSES_ALL_ACTIVE,
    modules: MODULES_ALL,
    mqttConfig: MQTT_CONFIG_EXISTING,
    gitopsConfig: [],
    systemService: SERVICE_STATUS_DEV_CAN_INSTALL,
    ...overrides,
  };

  await setupRoutes(page, opts);
}

async function setupRoutes(page: Page, opts: Required<MockOverrides>) {
  // Suppress SSE endpoints — return empty event stream that stays open
  await page.route('**/api/v1/**/stream**', (route) =>
    route.fulfill({
      status: 200,
      contentType: 'text/event-stream',
      headers: {
        'Cache-Control': 'no-cache',
        Connection: 'keep-alive',
      },
      body: '',
    }),
  );

  // System
  await page.route('**/api/v1/mode', (route) =>
    json(route, { mode: opts.mode }),
  );

  // Services
  await page.route('**/api/v1/services', (route) => {
    if (route.request().method() === 'GET') return json(route, opts.services);
    return route.continue();
  });

  // Enable/disable service
  await page.route('**/api/v1/services/*/enabled', (route) => {
    if (route.request().method() === 'PUT') {
      return json(route, { moduleId: 'test', enabled: true });
    }
    return route.continue();
  });

  // Orchestrator
  await page.route('**/api/v1/orchestrator/desired-services', (route) => {
    if (route.request().method() === 'GET') return json(route, opts.desiredServices);
    return route.continue();
  });

  await page.route('**/api/v1/orchestrator/desired-services/*', (route) => {
    if (route.request().method() === 'PUT') {
      return json(route, { moduleId: 'test', version: 'latest', running: true });
    }
    if (route.request().method() === 'DELETE') {
      return json(route, { success: true });
    }
    return route.continue();
  });

  await page.route('**/api/v1/orchestrator/service-statuses', (route) =>
    json(route, opts.serviceStatuses),
  );

  await page.route('**/api/v1/orchestrator/modules', (route) =>
    json(route, opts.modules),
  );

  await page.route('**/api/v1/orchestrator/modules/*/versions', (route) =>
    json(route, { versions: [{ version: '0.1.0', digest: 'sha256:abc' }] }),
  );

  await page.route('**/api/v1/orchestrator/internet', (route) =>
    json(route, { online: true }),
  );

  // Config — register generic routes FIRST so specific routes have higher LIFO priority
  await page.route('**/api/v1/config/*/schema', (route) =>
    json(route, []),
  );

  await page.route('**/api/v1/config/*', (route) => {
    const method = route.request().method();
    if (method === 'GET') return json(route, []);
    if (method === 'PUT') return json(route, { moduleId: 'test', envVar: 'test', value: 'test' });
    return route.continue();
  });

  // Specific config routes (registered AFTER generic, so checked FIRST in LIFO)
  await page.route('**/api/v1/config/mqtt', (route) => {
    if (route.request().method() === 'GET') return json(route, opts.mqttConfig);
    return route.continue();
  });

  await page.route('**/api/v1/config/mqtt/*', (route) => {
    if (route.request().method() === 'PUT') {
      return json(route, { moduleId: 'mqtt', envVar: 'test', value: 'test' });
    }
    return route.continue();
  });

  await page.route('**/api/v1/config/gitops', (route) => {
    if (route.request().method() === 'GET') return json(route, opts.gitopsConfig);
    return route.continue();
  });

  await page.route('**/api/v1/config/gitops/*', (route) => {
    if (route.request().method() === 'PUT') {
      return json(route, { moduleId: 'gitops', envVar: 'test', value: 'test' });
    }
    return route.continue();
  });

  // System service
  await page.route('**/api/v1/system/service', (route) =>
    json(route, opts.systemService),
  );

  await page.route('**/api/v1/system/service/install', (route) =>
    json(route, { success: true, message: 'Service installed' }),
  );

  await page.route('**/api/v1/system/service/activate', (route) =>
    json(route, { success: true }),
  );

  // GitOps
  await page.route('**/api/v1/gitops/git-check', (route) =>
    json(route, { installed: true }),
  );

  await page.route('**/api/v1/gitops/ssh-key', (route) =>
    json(route, { exists: true, publicKey: 'ssh-ed25519 AAAA...test key@tentacle', path: '/data/.ssh/id_ed25519' }),
  );

  await page.route('**/api/v1/gitops/ssh-key/generate', (route) =>
    json(route, { exists: true, publicKey: 'ssh-ed25519 AAAA...new-key key@tentacle', path: '/data/.ssh/id_ed25519' }),
  );

  await page.route('**/api/v1/gitops/test-connection', (route) =>
    json(route, { success: true }),
  );

  await page.route('**/api/v1/gitops/git-install', (route) =>
    json(route, { success: true }),
  );

  // Logs
  await page.route('**/api/v1/services/*/logs', (route) => {
    if (route.request().url().includes('stream')) return route.continue();
    return json(route, [
      { timestamp: Date.now(), level: 'info', message: 'Service started', serviceType: 'test', moduleId: 'test', logger: null },
      { timestamp: Date.now(), level: 'debug', message: 'Debug message', serviceType: 'test', moduleId: 'test', logger: 'main' },
    ]);
  });

  // MQTT store-forward
  await page.route('**/api/v1/mqtt/store-forward', (route) =>
    json(route, { queued: 0, disk_usage: 0, config: {} }),
  );

  // Hostname
  await page.route('**/api/v1/system/hostname', (route) =>
    json(route, { hostname: 'tentacle-test', path: '/etc/hostname' }),
  );

  // Extra routes
  for (const [pattern, handler] of Object.entries(opts.extraRoutes ?? {})) {
    await page.route(pattern, handler);
  }
}

function json(route: Route, data: unknown) {
  return route.fulfill({
    status: 200,
    contentType: 'application/json',
    body: JSON.stringify(data),
  });
}

export function jsonError(route: Route, error: string, status = 500) {
  return route.fulfill({
    status,
    contentType: 'application/json',
    body: JSON.stringify({ error }),
  });
}
