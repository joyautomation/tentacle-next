#!/usr/bin/env bash
# ---------------------------------------------------------------------------
# Tier 2: Pre-release integration test
#
# Spins up a fresh incus container, installs the tentacle binary, runs
# the setup wizard via Playwright, then tears down. Run locally before
# cutting a release.
#
# Usage:
#   ./scripts/pre-release-test.sh [--keep]
#
# Flags:
#   --keep   Don't delete the container on exit (useful for debugging)
# ---------------------------------------------------------------------------
set -euo pipefail

CONTAINER="tentacle-release-test"
IMAGE="ubuntu:24.04"
BINARY="bin/tentacle"
REMOTE_BIN="/usr/local/bin/tentacle"
TENTACLE_PORT=4000
HOST_PORT=4100          # avoid colliding with a dev instance on 4000
KEEP=false

for arg in "$@"; do
  case "$arg" in
    --keep) KEEP=true ;;
  esac
done

# ---------------------------------------------------------------------------
# Cleanup
# ---------------------------------------------------------------------------
cleanup() {
  echo ""
  echo "==> Cleaning up..."
  if [ "$KEEP" = true ]; then
    echo "    --keep flag set. Container '$CONTAINER' left running."
    echo "    To remove: incus delete $CONTAINER --force"
  else
    incus delete "$CONTAINER" --force 2>/dev/null || true
    echo "    Container removed."
  fi
}
trap cleanup EXIT

# ---------------------------------------------------------------------------
# Build
# ---------------------------------------------------------------------------
echo "==> Building web assets..."
(cd web && npm run build)

echo "==> Building tentacle binary (all modules)..."
CGO_ENABLED=1 go build -tags all,web -o "$BINARY" ./cmd/tentacle

# ---------------------------------------------------------------------------
# Container setup
# ---------------------------------------------------------------------------
echo "==> Deleting any leftover test container..."
incus delete "$CONTAINER" --force 2>/dev/null || true

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

echo "==> Pushing binary into container..."
incus file push "$BINARY" "$CONTAINER$REMOTE_BIN"
incus exec "$CONTAINER" -- chmod +x "$REMOTE_BIN"

# ---------------------------------------------------------------------------
# Start tentacle in foreground (background process inside container)
# ---------------------------------------------------------------------------
echo "==> Starting tentacle in dev mode..."
incus exec "$CONTAINER" -- bash -c "
  API_PORT=$TENTACLE_PORT nohup $REMOTE_BIN > /tmp/tentacle.log 2>&1 &
  echo \$! > /tmp/tentacle.pid
"

echo "==> Waiting for tentacle API to be ready..."
CONTAINER_IP=$(incus list "$CONTAINER" -f csv -c4 | head -1 | awk '{print $1}')
if [ -z "$CONTAINER_IP" ]; then
  echo "    ERROR: Could not determine container IP"
  exit 1
fi

for i in $(seq 1 30); do
  if curl -sf "http://${CONTAINER_IP}:${TENTACLE_PORT}/api/v1/mode" >/dev/null 2>&1; then
    echo "    API responding at http://${CONTAINER_IP}:${TENTACLE_PORT}"
    break
  fi
  if [ "$i" -eq 30 ]; then
    echo "    ERROR: API not ready after 30s. Dumping logs:"
    incus exec "$CONTAINER" -- cat /tmp/tentacle.log
    exit 1
  fi
  sleep 1
done

# ---------------------------------------------------------------------------
# Run Playwright tests against the live binary
# ---------------------------------------------------------------------------
echo ""
echo "==> Running Playwright e2e tests against live binary..."
echo "    Target: http://${CONTAINER_IP}:${TENTACLE_PORT}"
echo ""

cd web
PLAYWRIGHT_BASE_URL="http://${CONTAINER_IP}:${TENTACLE_PORT}" \
  npx playwright test \
    --config playwright-live.config.ts \
    --reporter=list \
  || TEST_EXIT=$?

cd ..

# ---------------------------------------------------------------------------
# Service install test
# ---------------------------------------------------------------------------
echo ""
echo "==> Testing systemd service install..."
incus exec "$CONTAINER" -- bash -c "
  # Stop foreground instance
  kill \$(cat /tmp/tentacle.pid) 2>/dev/null || true
  sleep 2

  # Install as service
  $REMOTE_BIN service install
"

echo "==> Starting systemd service..."
incus exec "$CONTAINER" -- systemctl start tentacle

echo "==> Verifying service is active..."
sleep 3
SERVICE_STATUS=$(incus exec "$CONTAINER" -- systemctl is-active tentacle)
if [ "$SERVICE_STATUS" = "active" ]; then
  echo "    systemd service is active."
else
  echo "    ERROR: Service status is '$SERVICE_STATUS'"
  incus exec "$CONTAINER" -- journalctl -u tentacle --no-pager -n 50
  exit 1
fi

echo "==> Verifying API responds under systemd..."
for i in $(seq 1 15); do
  MODE=$(curl -sf "http://${CONTAINER_IP}:${TENTACLE_PORT}/api/v1/mode" 2>/dev/null | grep -o '"mode":"[^"]*"' || true)
  if echo "$MODE" | grep -q "systemd"; then
    echo "    API reports mode: systemd"
    break
  fi
  if [ "$i" -eq 15 ]; then
    echo "    ERROR: API did not report systemd mode"
    exit 1
  fi
  sleep 1
done

# ---------------------------------------------------------------------------
# Results
# ---------------------------------------------------------------------------
echo ""
echo "========================================="
if [ "${TEST_EXIT:-0}" -eq 0 ]; then
  echo "  All pre-release tests PASSED"
else
  echo "  Playwright tests FAILED (exit code: $TEST_EXIT)"
  echo "  Systemd install test passed."
fi
echo "========================================="

exit "${TEST_EXIT:-0}"
