#!/usr/bin/env bash
set -euo pipefail

REPO_ROOT="/home/joyja/Development/joyautomation/kraken/tentacle-next"
WORKTREE_BASE="/home/joyja/Development/joyautomation/kraken"
IMAGE_ALIAS="tentacle-next-dev-golden"
CONTAINER_PREFIX="tentacle-next-dev"

usage() {
  cat <<'EOF'
Usage: worktree-dev.sh <command> [args]

Commands:
  init-image          Create/refresh golden image from tentacle-next-dev-1
  create <name>       Create worktree + dev container for a module/feature
  destroy <name>      Remove worktree + dev container
  list                Show all worktrees and container status
  deploy <name>       Build and deploy to a specific worktree's container
  shell <name>        Open a shell in the container
  logs <name> [svc]   Show logs (default: tentacle service)
  start <name>        Start a stopped container
  stop <name>         Stop a running container

Examples:
  worktree-dev.sh create plc
  worktree-dev.sh deploy profinet
  worktree-dev.sh shell modbus
  worktree-dev.sh logs opcua tentacle-web-dev
EOF
}

container_name() { echo "${CONTAINER_PREFIX}-${1}"; }
worktree_path() { echo "${WORKTREE_BASE}/tentacle-next-${1}"; }
branch_name() { echo "feature/${1}"; }

cmd_init_image() {
  echo "==> Creating golden image from tentacle-next-dev-1..."

  # Stop services and clear runtime data for a clean image
  incus exec tentacle-next-dev-1 -- systemctl stop tentacle 2>/dev/null || true
  incus exec tentacle-next-dev-1 -- systemctl stop tentacle-web-dev 2>/dev/null || true
  incus exec tentacle-next-dev-1 -- bash -c 'rm -rf /var/lib/tentacle/*' 2>/dev/null || true

  # Must stop the container to publish
  incus stop tentacle-next-dev-1

  # Delete old image if it exists
  incus image delete "$IMAGE_ALIAS" 2>/dev/null || true

  # Publish as reusable image
  incus publish tentacle-next-dev-1 --alias "$IMAGE_ALIAS"

  # Restart the original container
  incus start tentacle-next-dev-1

  echo "==> Golden image '$IMAGE_ALIAS' created."
}

cmd_create() {
  local name="$1"
  local wt_path
  wt_path=$(worktree_path "$name")
  local ct_name
  ct_name=$(container_name "$name")
  local branch
  branch=$(branch_name "$name")

  echo "==> Creating worktree '$name'..."
  echo "    Branch:    $branch"
  echo "    Path:      $wt_path"
  echo "    Container: $ct_name"
  echo ""

  # Ensure golden image exists
  if ! incus image show "$IMAGE_ALIAS" &>/dev/null; then
    echo "==> Golden image not found. Creating from tentacle-next-dev-1..."
    cmd_init_image
  fi

  # Create git worktree
  if [ -d "$wt_path" ]; then
    echo "    Worktree already exists at $wt_path, skipping."
  else
    # Create branch from main; if branch already exists, just check it out
    if git -C "$REPO_ROOT" show-ref --verify --quiet "refs/heads/$branch" 2>/dev/null; then
      git -C "$REPO_ROOT" worktree add "$wt_path" "$branch"
    else
      git -C "$REPO_ROOT" worktree add -b "$branch" "$wt_path" main
    fi
    echo "    Worktree created."
  fi

  # Create container
  if incus info "$ct_name" &>/dev/null; then
    echo "    Container $ct_name already exists, skipping."
  else
    echo "==> Launching container $ct_name from golden image..."
    incus launch "$IMAGE_ALIAS" "$ct_name" \
      -c raw.idmap="both 1000 0" \
      -c security.nesting=true

    # Bind-mount the worktree source into the container
    incus config device add "$ct_name" tentacle-next-src disk \
      source="$wt_path" path=/root/tentacle-next shift=true

    # Attach physical network interfaces (same as main dev container)
    incus config device add "$ct_name" eno1 nic network=ne1-net1
    incus config device add "$ct_name" eno2 nic network=ne1-net2

    echo "    Waiting for container networking..."
    sleep 5

    # Install web dependencies for this worktree
    echo "==> Installing web dependencies..."
    incus exec "$ct_name" -- bash -c 'cd /root/tentacle-next/web && npm install' 2>&1 | tail -3

    # Clear any stale data from the golden image
    incus exec "$ct_name" -- bash -c 'rm -rf /var/lib/tentacle/*' 2>/dev/null || true

    # Enable and start services
    incus exec "$ct_name" -- systemctl daemon-reload
    incus exec "$ct_name" -- systemctl enable tentacle tentacle-web-dev caddy
    incus exec "$ct_name" -- systemctl restart tentacle || true
    incus exec "$ct_name" -- systemctl restart tentacle-web-dev || true
    incus exec "$ct_name" -- systemctl restart caddy || true

    echo "    Container ready."
  fi

  # Get container IP
  local ip
  ip=$(incus list "$ct_name" -f csv -c 4 | grep -oP '[\d.]+(?=.*eth0)' | head -1) || true

  echo ""
  echo "==> Worktree '$name' is ready!"
  echo "    Source:     $wt_path"
  echo "    Branch:     $branch"
  echo "    Container:  $ct_name"
  echo "    Web UI:     http://${ip:-<pending>}"
  echo "    Vite HMR:   http://${ip:-<pending>}:3012"
  echo ""
  echo "    Deploy:  scripts/worktree-dev.sh deploy $name"
  echo "    Shell:   scripts/worktree-dev.sh shell $name"
  echo "    Logs:    scripts/worktree-dev.sh logs $name"
}

cmd_destroy() {
  local name="$1"
  local wt_path
  wt_path=$(worktree_path "$name")
  local ct_name
  ct_name=$(container_name "$name")
  local branch
  branch=$(branch_name "$name")

  echo "==> Destroying worktree '$name'..."

  # Stop and delete container
  if incus info "$ct_name" &>/dev/null; then
    echo "    Deleting container $ct_name..."
    incus delete "$ct_name" --force
    echo "    Container deleted."
  else
    echo "    Container $ct_name not found (already removed?)."
  fi

  # Remove worktree
  if [ -d "$wt_path" ]; then
    git -C "$REPO_ROOT" worktree remove "$wt_path" --force
    echo "    Worktree removed."
  else
    echo "    Worktree not found at $wt_path."
  fi

  echo ""
  echo "==> Worktree '$name' destroyed."
  echo "    Branch '$branch' was kept. Delete manually: git branch -D $branch"
}

cmd_list() {
  echo "=== Git Worktrees ==="
  git -C "$REPO_ROOT" worktree list
  echo ""
  echo "=== Dev Containers ==="
  incus list "${CONTAINER_PREFIX}-" -f table -c ns4t
}

cmd_deploy() {
  local name="$1"
  local wt_path
  wt_path=$(worktree_path "$name")
  local ct_name
  ct_name=$(container_name "$name")

  if [ ! -d "$wt_path" ]; then
    echo "ERROR: Worktree not found at $wt_path"
    exit 1
  fi

  if ! incus info "$ct_name" &>/dev/null; then
    echo "ERROR: Container $ct_name not found"
    exit 1
  fi

  echo "==> Deploying to $ct_name from $wt_path..."

  echo "==> Building web assets..."
  (cd "$wt_path/web" && npm run build)

  echo "==> Building tentacle..."
  (cd "$wt_path" && \
    CGO_ENABLED=1 \
    CGO_LDFLAGS="-L/tmp/libplctag-check/build/bin_dist" \
    go build -tags all -o bin/tentacle ./cmd/tentacle)

  echo "==> Stopping tentacle service..."
  incus exec "$ct_name" -- systemctl stop tentacle

  echo "==> Pushing binary..."
  incus file push "$wt_path/bin/tentacle" "${ct_name}/usr/local/bin/tentacle"

  echo "==> Starting tentacle service..."
  incus exec "$ct_name" -- systemctl start tentacle

  echo "==> Verifying..."
  local status
  status=$(incus exec "$ct_name" -- systemctl is-active tentacle)
  if [ "$status" = "active" ]; then
    echo "==> Deploy successful. tentacle is running on $ct_name."
  else
    echo "==> ERROR: tentacle service status is '$status'"
    incus exec "$ct_name" -- journalctl -u tentacle -n 20 --no-pager
    exit 1
  fi
}

cmd_shell() {
  local ct_name
  ct_name=$(container_name "$1")
  exec incus exec "$ct_name" -- bash
}

cmd_logs() {
  local ct_name
  ct_name=$(container_name "$1")
  local service="${2:-tentacle}"
  exec incus exec "$ct_name" -- journalctl -u "$service" -n 100 -f
}

cmd_start() {
  local ct_name
  ct_name=$(container_name "$1")
  incus start "$ct_name"
  echo "==> $ct_name started."
}

cmd_stop() {
  local ct_name
  ct_name=$(container_name "$1")
  incus stop "$ct_name"
  echo "==> $ct_name stopped."
}

# --- Main ---
case "${1:-}" in
  init-image) cmd_init_image ;;
  create)     cmd_create "${2:?Usage: worktree-dev.sh create <name>}" ;;
  destroy)    cmd_destroy "${2:?Usage: worktree-dev.sh destroy <name>}" ;;
  list)       cmd_list ;;
  deploy)     cmd_deploy "${2:?Usage: worktree-dev.sh deploy <name>}" ;;
  shell)      cmd_shell "${2:?Usage: worktree-dev.sh shell <name>}" ;;
  logs)       cmd_logs "${2:?Usage: worktree-dev.sh logs <name>}" "${3:-}" ;;
  start)      cmd_start "${2:?Usage: worktree-dev.sh start <name>}" ;;
  stop)       cmd_stop "${2:?Usage: worktree-dev.sh stop <name>}" ;;
  *)          usage ;;
esac
