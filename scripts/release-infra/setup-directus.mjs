#!/usr/bin/env node
// ---------------------------------------------------------------------------
// One-time setup: create the `tentacle_releases` collection in Directus
// via the REST API. Idempotent — safe to re-run.
//
// Usage:
//   DIRECTUS_URL=https://directus.joyautomation.com \
//   DIRECTUS_TOKEN=<static admin token> \
//   node setup-directus.mjs
// ---------------------------------------------------------------------------

const URL_BASE = process.env.DIRECTUS_URL;
const TOKEN = process.env.DIRECTUS_TOKEN;
if (!URL_BASE || !TOKEN) {
	console.error('need DIRECTUS_URL and DIRECTUS_TOKEN env vars');
	process.exit(1);
}

const COLLECTION = 'tentacle_releases';

const headers = {
	Authorization: `Bearer ${TOKEN}`,
	'Content-Type': 'application/json'
};

async function api(method, path, body) {
	const res = await fetch(`${URL_BASE}${path}`, {
		method,
		headers,
		body: body ? JSON.stringify(body) : undefined
	});
	if (!res.ok && res.status !== 404) {
		const txt = await res.text();
		throw new Error(`${method} ${path} → ${res.status}: ${txt}`);
	}
	return res.status === 404 ? null : res.json().catch(() => null);
}

async function ensureCollection() {
	const existing = await api('GET', `/collections/${COLLECTION}`);
	if (existing) {
		console.log(`✓ collection ${COLLECTION} already exists`);
		return;
	}
	console.log(`+ creating collection ${COLLECTION}`);
	await api('POST', '/collections', {
		collection: COLLECTION,
		meta: {
			icon: 'inventory_2',
			note: 'Tentacle release artifacts published from CI',
			sort_field: 'released_at',
			archive_field: null,
			singleton: false
		},
		schema: { name: COLLECTION }
	});
}

const FIELDS = [
	{
		field: 'id',
		type: 'integer',
		meta: { hidden: true, interface: 'input', readonly: true },
		schema: { is_primary_key: true, has_auto_increment: true }
	},
	{
		field: 'version',
		type: 'string',
		meta: { interface: 'input', required: true, note: 'Semver, no leading v (e.g. 0.0.11)' },
		schema: { is_unique: true, is_nullable: false }
	},
	{
		field: 'tag_name',
		type: 'string',
		meta: { interface: 'input', required: true, note: 'Git tag (e.g. v0.0.11)' },
		schema: { is_nullable: false }
	},
	{
		field: 'released_at',
		type: 'timestamp',
		meta: { interface: 'datetime', required: true, display: 'datetime' },
		schema: { is_nullable: false }
	},
	{
		field: 'notes',
		type: 'text',
		meta: {
			interface: 'input-rich-text-md',
			note: 'Release notes (markdown). Usually pulled from goreleaser changelog.'
		}
	},
	{
		field: 'assets',
		type: 'json',
		meta: {
			interface: 'input-code',
			options: { language: 'json' },
			note: 'Map of asset name → public URL (e.g. {"linux_amd64": "https://...tar.gz", "checksums": "..."})'
		}
	},
	{
		field: 'is_prerelease',
		type: 'boolean',
		meta: { interface: 'boolean', special: ['cast-boolean'] },
		schema: { default_value: false }
	}
];

async function ensureField(field) {
	const existing = await api('GET', `/fields/${COLLECTION}/${field.field}`);
	if (existing) {
		console.log(`  ✓ field ${field.field} exists`);
		return;
	}
	console.log(`  + adding field ${field.field}`);
	await api('POST', `/fields/${COLLECTION}`, field);
}

async function ensurePublicRead() {
	// Allow public role to read this collection (so the website can list
	// releases without auth, and the manifest endpoint works for tentacles).
	const policies = await api('GET', `/permissions?filter[collection][_eq]=${COLLECTION}&filter[action][_eq]=read`);
	const items = policies?.data ?? [];
	const hasPublic = items.some((p) => p.policy === null || p.role === null);
	if (hasPublic) {
		console.log('✓ public read permission already configured');
		return;
	}
	console.log('+ granting public read permission');
	await api('POST', '/permissions', {
		collection: COLLECTION,
		action: 'read',
		role: null,
		fields: ['*']
	});
}

(async () => {
	await ensureCollection();
	for (const f of FIELDS) await ensureField(f);
	await ensurePublicRead();
	console.log('\nDone.');
})().catch((err) => {
	console.error(err);
	process.exit(1);
});
