# Release infrastructure setup

One-time setup to host tentacle release artifacts on DigitalOcean Spaces and
publish release metadata to Directus, so that:

- The tentacle self-upgrader can fetch updates from a stable URL
- Install scripts (`scripts/deploy-dist.sh`, README curl commands) work without
  GitHub auth
- The tentacle-next GitHub repo can be made private without breaking deployed
  instances

## 1. Generate Spaces credentials

The DigitalOcean web console is the only way to create Spaces access keys:

1. Go to https://cloud.digitalocean.com/account/api/spaces
2. Click **Generate New Key**, name it `tentacle-releases-ci`
3. Save the access key + secret key — DO will only show the secret once

## 2. Create the Space + CDN

```bash
chmod +x setup-spaces.sh
SPACES_KEY=...  SPACES_SECRET=...  ./setup-spaces.sh
```

Defaults: region `sfo3`, bucket `joyautomation-releases`. Override with
`SPACES_REGION=` and `BUCKET=` env vars.

The script creates the Space, applies a public-read policy on the
`releases/*` prefix, and enables the CDN.

## 3. Create the Directus collection

Generate a static Directus admin token (Settings → Access Tokens → create one
on the admin user) and run:

```bash
DIRECTUS_URL=https://directus.joyautomation.com \
DIRECTUS_TOKEN=<token> \
node setup-directus.mjs
```

Creates the `tentacle_releases` collection with fields: `version`, `tag_name`,
`released_at`, `notes` (markdown), `assets` (json), `is_prerelease`. Grants
public read so the website + self-upgrader can fetch without auth.

## 4. Add GitHub Actions secrets

On the tentacle-next repo (Settings → Secrets and variables → Actions):

| Secret              | Value                              |
| ------------------- | ---------------------------------- |
| `DO_SPACES_KEY`     | Spaces access key from step 1      |
| `DO_SPACES_SECRET`  | Spaces secret key from step 1      |
| `DO_SPACES_BUCKET`  | `joyautomation-releases`           |
| `DO_SPACES_REGION`  | `sfo3`                             |
| `DIRECTUS_URL`      | `https://directus.joyautomation.com` |
| `DIRECTUS_TOKEN`    | Static admin token from step 3     |

After all four steps, the next `git tag v0.0.X && git push --tags` will:

1. Build via goreleaser (existing behavior — still publishes to GitHub Releases)
2. Upload tarballs to `s3://joyautomation-releases/releases/tentacle/vX.Y.Z/`
3. POST a `tentacle_releases` row to Directus with the asset URLs and notes

The joyautomation.com website renders the list at `/software/tentacle/releases`
and exposes a JSON manifest at `/api/releases/tentacle/latest` for the
self-upgrader.
