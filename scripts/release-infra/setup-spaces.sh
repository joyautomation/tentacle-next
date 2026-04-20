#!/usr/bin/env bash
# ---------------------------------------------------------------------------
# One-time setup: create the DigitalOcean Space + CDN that hosts tentacle
# release artifacts (tarballs + checksums.txt) for download by the install
# scripts and the tentacle self-upgrader.
#
# Prerequisites:
#   - s3cmd installed (apt: s3cmd, brew: s3cmd)
#   - doctl authenticated (doctl auth init)
#   - Spaces access key + secret generated at:
#       https://cloud.digitalocean.com/account/api/spaces
#
# Usage:
#   SPACES_KEY=...  SPACES_SECRET=...  ./setup-spaces.sh
#
# Optional env:
#   SPACES_REGION (default sfo3)
#   BUCKET        (default joyautomation-releases)
# ---------------------------------------------------------------------------
set -euo pipefail

: "${SPACES_KEY:?need SPACES_KEY env var}"
: "${SPACES_SECRET:?need SPACES_SECRET env var}"
SPACES_REGION="${SPACES_REGION:-sfo3}"
BUCKET="${BUCKET:-joyautomation-releases}"
ENDPOINT="${SPACES_REGION}.digitaloceanspaces.com"

command -v s3cmd >/dev/null || { echo "ERROR: install s3cmd"; exit 1; }
command -v doctl >/dev/null || { echo "ERROR: install doctl"; exit 1; }

S3CMD=(s3cmd
  --access_key="$SPACES_KEY"
  --secret_key="$SPACES_SECRET"
  --host="$ENDPOINT"
  --host-bucket="%(bucket)s.$ENDPOINT")

echo "==> Creating Space '$BUCKET' in $SPACES_REGION..."
"${S3CMD[@]}" mb "s3://$BUCKET" 2>&1 | grep -v "BucketAlreadyOwnedByYou" || true

POLICY_FILE=$(mktemp)
trap 'rm -f "$POLICY_FILE"' EXIT
cat > "$POLICY_FILE" <<EOF
{
  "Version": "2012-10-17",
  "Statement": [{
    "Sid": "PublicReadReleases",
    "Effect": "Allow",
    "Principal": {"AWS": ["*"]},
    "Action": ["s3:GetObject"],
    "Resource": ["arn:aws:s3:::$BUCKET/releases/*"]
  }]
}
EOF
echo "==> Applying public-read policy to releases/* prefix..."
"${S3CMD[@]}" setpolicy "$POLICY_FILE" "s3://$BUCKET"

echo "==> Enabling CDN..."
ORIGIN="$BUCKET.$ENDPOINT"
if ! doctl compute cdn list --format Origin --no-header | grep -qx "$ORIGIN"; then
  doctl compute cdn create "$ORIGIN" --ttl 3600
else
  echo "    CDN already exists for $ORIGIN"
fi

echo ""
echo "----- Done -----"
echo "Bucket:       s3://$BUCKET"
echo "S3 endpoint:  https://$BUCKET.$ENDPOINT/"
echo "CDN endpoint: https://$BUCKET.$SPACES_REGION.cdn.digitaloceanspaces.com/"
echo ""
echo "Add these as GitHub Actions secrets on tentacle-next:"
echo "  DO_SPACES_KEY     = <your access key>"
echo "  DO_SPACES_SECRET  = <your secret key>"
echo "  DO_SPACES_BUCKET  = $BUCKET"
echo "  DO_SPACES_REGION  = $SPACES_REGION"
