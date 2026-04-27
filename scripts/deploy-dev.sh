#!/usr/bin/env bash
set -euo pipefail

# Auto-detect container from worktree directory name.
# tentacle-next       → tentacle-next-dev-1   (main)
# tentacle-next-plc   → tentacle-next-dev-plc (worktree)
REPO_DIR=$(basename "$(git rev-parse --show-toplevel)")
if [[ "$REPO_DIR" =~ ^tentacle-next-(.+)$ ]]; then
  CONTAINER="tentacle-next-dev-${BASH_REMATCH[1]}"
else
  CONTAINER="tentacle-next-dev-1"
fi

BINARY="bin/tentacle"
REMOTE_PATH="/usr/local/bin/tentacle"

echo "==> Building web assets..."
(cd web && npm run build)

VERSION=$(git describe --tags --always --dirty 2>/dev/null || echo dev)

# Worktrees named tentacle-next-mantle (or anything matching *mantle*) get the
# mantle build tag layered on top of "all" so role_mantle.go fires and the
# binary self-identifies as mantle to the UI.
BUILD_TAGS="all"
if [[ "$REPO_DIR" == *mantle* ]]; then
  BUILD_TAGS="all,mantle"
fi

echo "==> Building tentacle (version: $VERSION, tags: $BUILD_TAGS)..."
CGO_ENABLED=1 CGO_LDFLAGS="-L/tmp/libplctag-check/build/bin_dist" \
  go build -tags "$BUILD_TAGS" -ldflags "-X github.com/joyautomation/tentacle/internal/version.Version=$VERSION" -o "$BINARY" ./cmd/tentacle

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
