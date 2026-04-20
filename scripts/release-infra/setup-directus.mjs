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

async function api(method, path, body, { allowMissing = false } = {}) {
	const res = await fetch(`${URL_BASE}${path}`, {
		method,
		headers,
		body: body ? JSON.stringify(body) : undefined
	});
	// Directus returns 403 (not 404) for nonexistent collections/fields to
	// prevent enumeration. Treat 403/404 alike when the caller asked for it.
	if (allowMissing && (res.status === 403 || res.status === 404)) return null;
	if (!res.ok) {
		const txt = await res.text();
		throw new Error(`${method} ${path} → ${res.status}: ${txt}`);
	}
	return res.json().catch(() => null);
}

async function ensureCollection() {
	const list = await api('GET', '/collections?limit=-1');
	const exists = list?.data?.some((c) => c.collection === COLLECTION);
	if (exists) {
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
	const existing = await api('GET', `/fields/${COLLECTION}/${field.field}`, undefined, {
		allowMissing: true
	});
	if (existing) {
		console.log(`  ✓ field ${field.field} exists`);
		return;
	}
	console.log(`  + adding field ${field.field}`);
	await api('POST', `/fields/${COLLECTION}`, field);
}

async function findPublicPolicy() {
	// Try a few lookup strategies — the "Public" policy may have a localized
	// or empty name in different Directus versions.
	const direct = await api(
		'GET',
		'/policies?filter[name][_eq]=Public&limit=1&fields=id,name'
	);
	if (direct?.data?.[0]) return direct.data[0];

	const role = await api(
		'GET',
		'/roles?filter[name][_eq]=Public&limit=1&fields=id'
	);
	const publicRoleId = role?.data?.[0]?.id;
	if (publicRoleId) {
		const access = await api(
			'GET',
			`/access?filter[role][_eq]=${publicRoleId}&fields=policy.id,policy.name`
		);
		const item = access?.data?.find((a) => a.policy);
		if (item?.policy) return item.policy;
	}

	const allPolicies = await api('GET', '/policies?limit=-1&fields=id,name');
	const fuzzy = allPolicies?.data?.find((p) =>
		(p.name ?? '').toLowerCase().includes('public')
	);
	return fuzzy ?? null;
}

async function ensurePublicRead() {
	const publicPolicy = await findPublicPolicy();
	if (!publicPolicy) {
		console.log('! could not find a "Public" policy in Directus.');
		console.log(
			'  Grant read access on tentacle_releases to the Public policy manually:'
		);
		console.log(
			'  Settings → Access Control → Public → Permissions → tentacle_releases → Read = All access'
		);
		return;
	}

	const existing = await api(
		'GET',
		`/permissions?filter[collection][_eq]=${COLLECTION}&filter[action][_eq]=read&filter[policy][_eq]=${publicPolicy.id}&limit=1`
	);
	if (existing?.data?.length) {
		console.log('✓ public read permission already configured');
		return;
	}

	console.log('+ granting public read permission via Public policy');
	await api('POST', '/permissions', {
		collection: COLLECTION,
		action: 'read',
		policy: publicPolicy.id,
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
