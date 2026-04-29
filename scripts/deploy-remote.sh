#!/usr/bin/env bash
set -euo pipefail

# Cross-compile tentacle for linux/arm64 inside a debian:bullseye container
# (matches the CI release env, so the binary links against glibc old enough to
# run on devices like the CompuLab IOT-GATE-iMX8) and deploy it to a remote
# gateway over SSH+SCP.
#
# Usage:
#   scripts/deploy-remote.sh user@host [--no-web] [--no-rebuild-image]
#
# Requirements:
#   - docker available without sudo (rootless or `docker` group membership)
#   - ssh key auth as the given user on the remote
#   - on the remote: passwordless sudo for `systemctl` and `install`
#     (or be root). The script uses `sudo -n` and fails fast if a password
#     prompt would be needed.
#   - /etc/systemd/system/tentacle.service already configured on the remote.

if [[ $# -lt 1 ]]; then
  echo "usage: $0 user@host [--no-web] [--no-rebuild-image]" >&2
  exit 2
fi

REMOTE="$1"
shift || true

BUILD_WEB=1
REBUILD_IMAGE_IF_MISSING=1
for arg in "$@"; do
  case "$arg" in
    --no-web) BUILD_WEB=0 ;;
    --no-rebuild-image) REBUILD_IMAGE_IF_MISSING=0 ;;
    *) echo "unknown arg: $arg" >&2; exit 2 ;;
  esac
done

REPO_ROOT=$(git rev-parse --show-toplevel)
REPO_DIR=$(basename "$REPO_ROOT")
BINARY_REL="bin/tentacle-arm64"
BINARY_ABS="$REPO_ROOT/$BINARY_REL"
REMOTE_PATH="/usr/local/bin/tentacle"
IMAGE_TAG="tentacle-cross-arm64:bullseye"

if ! command -v docker >/dev/null; then
  echo "ERROR: docker not found in PATH" >&2
  exit 1
fi

# Build the cross-compile image if missing.
if ! docker image inspect "$IMAGE_TAG" >/dev/null 2>&1; then
  if [[ "$REBUILD_IMAGE_IF_MISSING" != "1" ]]; then
    echo "ERROR: $IMAGE_TAG missing and --no-rebuild-image set" >&2
    exit 1
  fi
  echo "==> Building $IMAGE_TAG (one-time, ~5-10 min)..."
  docker build -t "$IMAGE_TAG" -f "$REPO_ROOT/scripts/cross-arm64.Dockerfile" "$REPO_ROOT/scripts"
fi

if [[ "$BUILD_WEB" == "1" ]]; then
  echo "==> Building web assets..."
  (cd "$REPO_ROOT/web" && npm run build)
else
  echo "==> Skipping web build (--no-web)"
fi

VERSION=$(git describe --tags --always --dirty 2>/dev/null || echo dev)

# Same tag policy as deploy-dev.sh: mantle worktrees layer the mantle tag on
# top of "all" so role_mantle.go fires.
BUILD_TAGS="all"
if [[ "$REPO_DIR" == *mantle* ]]; then
  BUILD_TAGS="all,mantle"
fi

echo "==> Cross-compiling tentacle for linux/arm64 (version: $VERSION, tags: $BUILD_TAGS)..."
mkdir -p "$REPO_ROOT/bin"

# Persistent module + build cache so subsequent deploys are fast.
GO_CACHE_VOL="tentacle-cross-arm64-gocache"
GO_MOD_VOL="tentacle-cross-arm64-gomodcache"

docker run --rm \
  -v "$REPO_ROOT:/workspace" \
  -v "$GO_CACHE_VOL:/root/.cache/go-build" \
  -v "$GO_MOD_VOL:/root/go/pkg/mod" \
  -w /workspace \
  -e GOOS=linux -e GOARCH=arm64 \
  -e CGO_ENABLED=1 \
  -e CC=aarch64-linux-gnu-gcc \
  -e CGO_LDFLAGS="-L/opt/libplctag-arm64/lib" \
  -e C_INCLUDE_PATH=/opt/libplctag-arm64/include \
  "$IMAGE_TAG" \
  go build -tags "$BUILD_TAGS" \
    -ldflags "-X github.com/joyautomation/tentacle/internal/version.Version=$VERSION" \
    -o "$BINARY_REL" ./cmd/tentacle

echo "==> Verifying binary architecture..."
file "$BINARY_ABS" | grep -q "aarch64\|ARM aarch64" \
  || { echo "ERROR: built binary is not aarch64"; file "$BINARY_ABS"; exit 1; }

echo "==> Stopping tentacle on $REMOTE..."
ssh "$REMOTE" 'sudo -n systemctl stop tentacle'

echo "==> Pushing binary to $REMOTE..."
TMP_REMOTE="/tmp/tentacle.new.$$"
scp "$BINARY_ABS" "$REMOTE:$TMP_REMOTE"
ssh "$REMOTE" "sudo -n install -m 0755 -o root -g root '$TMP_REMOTE' '$REMOTE_PATH' && rm -f '$TMP_REMOTE'"

echo "==> Starting tentacle on $REMOTE..."
ssh "$REMOTE" 'sudo -n systemctl start tentacle'

echo "==> Verifying..."
STATUS=$(ssh "$REMOTE" 'systemctl is-active tentacle' || true)
if [[ "$STATUS" == "active" ]]; then
  echo "==> Deploy successful. tentacle is running on $REMOTE."
else
  echo "==> ERROR: tentacle service status on $REMOTE is '$STATUS'"
  ssh "$REMOTE" 'sudo -n journalctl -u tentacle -n 30 --no-pager' || true
  exit 1
fi
