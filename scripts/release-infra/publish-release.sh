#!/usr/bin/env bash
# ---------------------------------------------------------------------------
# Upload goreleaser artifacts to DigitalOcean Spaces and create a Directus
# release entry. Invoked by .github/workflows/release.yml after goreleaser.
#
# Required env:
#   GITHUB_REF_NAME      git tag (e.g. v0.0.11) — set automatically by GHA
#   DO_SPACES_KEY        Spaces access key
#   DO_SPACES_SECRET     Spaces secret key
#   DO_SPACES_BUCKET     bucket name (e.g. joyautomation-releases)
#   DO_SPACES_REGION     bucket region (e.g. sfo3)
#   DIRECTUS_URL         e.g. https://directus.joyautomation.com
#   DIRECTUS_TOKEN       static admin token with write on tentacle_releases
#   GH_TOKEN             gh CLI token (provided by GHA as GITHUB_TOKEN)
#
# Optional:
#   DIST_DIR             path to goreleaser dist/ (default: dist)
#   PRODUCT              release product name (default: tentacle)
# ---------------------------------------------------------------------------
set -euo pipefail

: "${GITHUB_REF_NAME:?missing GITHUB_REF_NAME}"
: "${DO_SPACES_KEY:?missing DO_SPACES_KEY}"
: "${DO_SPACES_SECRET:?missing DO_SPACES_SECRET}"
: "${DO_SPACES_BUCKET:?missing DO_SPACES_BUCKET}"
: "${DO_SPACES_REGION:?missing DO_SPACES_REGION}"
: "${DIRECTUS_URL:?missing DIRECTUS_URL}"
: "${DIRECTUS_TOKEN:?missing DIRECTUS_TOKEN}"

DIST_DIR="${DIST_DIR:-dist}"
PRODUCT="${PRODUCT:-tentacle}"
TAG="$GITHUB_REF_NAME"
VERSION="${TAG#v}"
ENDPOINT="${DO_SPACES_REGION}.digitaloceanspaces.com"
CDN_HOST="${DO_SPACES_BUCKET}.${DO_SPACES_REGION}.cdn.digitaloceanspaces.com"
PREFIX="releases/${PRODUCT}/${TAG}"

S3CMD=(s3cmd
  --access_key="$DO_SPACES_KEY"
  --secret_key="$DO_SPACES_SECRET"
  --host="$ENDPOINT"
  --host-bucket="%(bucket)s.$ENDPOINT"
  --acl-public
  --no-progress)

echo "==> Uploading $PRODUCT $TAG artifacts to s3://$DO_SPACES_BUCKET/$PREFIX/"
declare -A ASSETS=()
shopt -s nullglob
for f in "$DIST_DIR"/${PRODUCT}_${VERSION}_linux_*.tar.gz "$DIST_DIR"/checksums.txt; do
  name=$(basename "$f")
  echo "    + $name"
  "${S3CMD[@]}" put "$f" "s3://$DO_SPACES_BUCKET/$PREFIX/$name"

  # Build asset key for the manifest: linux_amd64, linux_arm64, checksums
  if [[ "$name" == checksums.txt ]]; then
    key="checksums"
  else
    # Strip product prefix and version: tentacle_0.0.11_linux_amd64.tar.gz → linux_amd64
    key="${name#${PRODUCT}_${VERSION}_}"
    key="${key%.tar.gz}"
  fi
  ASSETS[$key]="https://${CDN_HOST}/${PREFIX}/${name}"
done

if [ ${#ASSETS[@]} -eq 0 ]; then
  echo "ERROR: no artifacts matched ${PRODUCT}_${VERSION}_linux_*.tar.gz in $DIST_DIR"
  ls -la "$DIST_DIR" || true
  exit 1
fi

# Build the assets JSON object.
ASSETS_JSON="{"
first=true
for k in "${!ASSETS[@]}"; do
  $first || ASSETS_JSON+=","
  ASSETS_JSON+="\"$k\":\"${ASSETS[$k]}\""
  first=false
done
ASSETS_JSON+="}"

echo "==> Fetching release notes from GitHub for $TAG"
NOTES=$(gh release view "$TAG" --json body --jq .body 2>/dev/null || echo "")
RELEASED_AT=$(gh release view "$TAG" --json publishedAt --jq .publishedAt 2>/dev/null || date -u +"%Y-%m-%dT%H:%M:%SZ")

echo "==> Posting to Directus $DIRECTUS_URL/items/tentacle_releases"
PAYLOAD=$(jq -n \
  --arg version "$VERSION" \
  --arg tag "$TAG" \
  --arg released_at "$RELEASED_AT" \
  --arg notes "$NOTES" \
  --argjson assets "$ASSETS_JSON" \
  '{version:$version, tag_name:$tag, released_at:$released_at, notes:$notes, assets:$assets, is_prerelease:false}')

# Upsert: try POST; if version unique-conflict, PATCH instead.
HTTP=$(curl -sS -o /tmp/directus-resp.json -w "%{http_code}" \
  -X POST "$DIRECTUS_URL/items/tentacle_releases" \
  -H "Authorization: Bearer $DIRECTUS_TOKEN" \
  -H "Content-Type: application/json" \
  -d "$PAYLOAD")

if [ "$HTTP" = "200" ] || [ "$HTTP" = "204" ]; then
  echo "    created"
elif [ "$HTTP" = "400" ] && grep -q "RECORD_NOT_UNIQUE" /tmp/directus-resp.json; then
  echo "    record exists, patching by version=$VERSION"
  ID=$(curl -sS -G "$DIRECTUS_URL/items/tentacle_releases" \
    -H "Authorization: Bearer $DIRECTUS_TOKEN" \
    --data-urlencode "filter[version][_eq]=$VERSION" \
    --data-urlencode "fields=id" | jq -r '.data[0].id')
  curl -sS -X PATCH "$DIRECTUS_URL/items/tentacle_releases/$ID" \
    -H "Authorization: Bearer $DIRECTUS_TOKEN" \
    -H "Content-Type: application/json" \
    -d "$PAYLOAD" >/dev/null
  echo "    patched id=$ID"
else
  echo "ERROR: Directus POST returned $HTTP:"
  cat /tmp/directus-resp.json
  exit 1
fi

echo ""
echo "----- Published $PRODUCT $TAG -----"
for k in "${!ASSETS[@]}"; do printf "  %-16s %s\n" "$k" "${ASSETS[$k]}"; done
