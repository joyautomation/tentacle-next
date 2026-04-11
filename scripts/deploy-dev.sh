#!/usr/bin/env bash
set -euo pipefail

CONTAINER="tentacle-next-dev-1"
BINARY="bin/tentacle"
REMOTE_PATH="/usr/local/bin/tentacle"

echo "==> Building web assets..."
(cd web && npm run build)

echo "==> Building tentacle..."
CGO_ENABLED=1 CGO_LDFLAGS="-L/tmp/libplctag-check/build/bin_dist" \
  go build -tags all -o "$BINARY" ./cmd/tentacle

echo "==> Stopping tentacle service..."
incus exec "$CONTAINER" -- systemctl stop tentacle

echo "==> Pushing binary..."
incus file push "$BINARY" "$CONTAINER$REMOTE_PATH"

echo "==> Starting tentacle service..."
incus exec "$CONTAINER" -- systemctl start tentacle

echo "==> Verifying..."
STATUS=$(incus exec "$CONTAINER" -- systemctl is-active tentacle)
if [ "$STATUS" = "active" ]; then
  echo "==> Deploy successful. tentacle is running."
else
  echo "==> ERROR: tentacle service status is '$STATUS'"
  exit 1
fi
