#!/usr/bin/env bash
# ---------------------------------------------------------------------------
# Deploy latest GitHub release to an incus container for manual testing
#
# Creates (or recreates) a container called "tentacle-dist", downloads the
# latest release binary, installs it as a systemd service, and prints the
# URL for manual testing.
#
# Usage:
#   ./scripts/deploy-dist.sh [--keep]
#
# Flags:
#   --keep   Don't delete an existing container — reuse it (re-downloads binary)
# ---------------------------------------------------------------------------
set -euo pipefail

CONTAINER="tentacle-dist"
IMAGE="images:ubuntu/24.04"
REMOTE_BIN="/usr/local/bin/tentacle"
REPO="joyautomation/tentacle-next"
TENTACLE_PORT=4000
KEEP=false

for arg in "$@"; do
  case "$arg" in
    --keep) KEEP=true ;;
  esac
done

# ---------------------------------------------------------------------------
# Determine latest release and asset URL
# ---------------------------------------------------------------------------
echo "==> Fetching latest release info..."
VERSION=$(gh release list --repo "$REPO" --limit 1 --json tagName --jq '.[0].tagName')
if [ -z "$VERSION" ]; then
  echo "    ERROR: Could not determine latest release"
  exit 1
fi
echo "    Latest release: $VERSION"

# Strip leading 'v' for the archive name
VERSION_NUM="${VERSION#v}"
ARCH=$(uname -m)
case "$ARCH" in
  x86_64)  ARCH="amd64" ;;
  aarch64) ARCH="arm64" ;;
  *)
    echo "    ERROR: Unsupported architecture: $ARCH"
    exit 1
    ;;
esac

ASSET="tentacle_${VERSION_NUM}_linux_${ARCH}.tar.gz"
echo "    Asset: $ASSET"

# Download to a temp directory
TMPDIR=$(mktemp -d)
trap "rm -rf $TMPDIR" EXIT

echo "==> Downloading release asset..."
gh release download "$VERSION" \
  --repo "$REPO" \
  --pattern "$ASSET" \
  --dir "$TMPDIR"

echo "==> Extracting binary..."
tar -xzf "$TMPDIR/$ASSET" -C "$TMPDIR"

if [ ! -f "$TMPDIR/tentacle" ]; then
  echo "    ERROR: 'tentacle' binary not found in archive"
  ls -la "$TMPDIR"
  exit 1
fi

# ---------------------------------------------------------------------------
# Container setup
# ---------------------------------------------------------------------------
EXISTING=$(incus list "$CONTAINER" -f csv -c n 2>/dev/null || true)
if [ -n "$EXISTING" ]; then
  if [ "$KEEP" = true ]; then
    echo "==> Reusing existing container '$CONTAINER'..."
    # Stop service if running
    incus exec "$CONTAINER" -- systemctl stop tentacle 2>/dev/null || true
  else
    echo "==> Deleting existing container '$CONTAINER'..."
    incus delete "$CONTAINER" --force 2>/dev/null || true
    EXISTING=""
  fi
fi

if [ -z "$EXISTING" ] || [ "$KEEP" = false ]; then
  echo "==> Launching fresh container: $IMAGE..."
  incus launch "$IMAGE" "$CONTAINER"

  echo "==> Waiting for container networking..."
  for i in $(seq 1 30); do
    if incus exec "$CONTAINER" -- ping -c1 -W1 1.1.1.1 &>/dev/null; then
      break
    fi
    if [ "$i" -eq 30 ]; then
      echo "    ERROR: Container networking not ready after 30s"
      exit 1
    fi
    sleep 1
  done
fi

# ---------------------------------------------------------------------------
# Install binary and service
# ---------------------------------------------------------------------------
echo "==> Pushing binary into container..."
incus file push "$TMPDIR/tentacle" "$CONTAINER$REMOTE_BIN"
incus exec "$CONTAINER" -- chmod +x "$REMOTE_BIN"

echo "==> Installing systemd service..."
incus exec "$CONTAINER" -- "$REMOTE_BIN" service install

echo "==> Starting service..."
incus exec "$CONTAINER" -- systemctl start tentacle

echo "==> Waiting for API to be ready..."
CONTAINER_IP=$(incus list "$CONTAINER" -f csv -c4 | head -1 | awk '{print $1}')
if [ -z "$CONTAINER_IP" ]; then
  echo "    ERROR: Could not determine container IP"
  exit 1
fi

for i in $(seq 1 30); do
  if curl -sf "http://${CONTAINER_IP}:${TENTACLE_PORT}/api/v1/mode" >/dev/null 2>&1; then
    break
  fi
  if [ "$i" -eq 30 ]; then
    echo "    ERROR: API not ready after 30s. Service logs:"
    incus exec "$CONTAINER" -- journalctl -u tentacle --no-pager -n 30
    exit 1
  fi
  sleep 1
done

# ---------------------------------------------------------------------------
# Done
# ---------------------------------------------------------------------------
echo ""
echo "========================================="
echo "  tentacle $VERSION running in '$CONTAINER'"
echo "  URL: http://${CONTAINER_IP}:${TENTACLE_PORT}"
echo ""
echo "  Useful commands:"
echo "    incus exec $CONTAINER -- journalctl -u tentacle -f"
echo "    incus exec $CONTAINER -- systemctl restart tentacle"
echo "    incus delete $CONTAINER --force"
echo "========================================="
