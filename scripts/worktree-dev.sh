#!/usr/bin/env bash
set -euo pipefail

REPO_ROOT="/home/joyja/Development/joyautomation/kraken/tentacle-next"
WORKTREE_BASE="/home/joyja/Development/joyautomation/kraken"
IMAGE_ALIAS="tentacle-next-dev-golden"
CONTAINER_PREFIX="tentacle-next-dev"
TS_KEY_FILE="$HOME/.config/tentacle-next/tailscale-auth-key"

usage() {
  cat <<'EOF'
Usage: worktree-dev.sh <command> [args]

Commands:
  init-image            Create/refresh golden image from tentacle-next-dev-1
  create <name>         Create worktree + dev container for a module/feature
  destroy <name>        Remove worktree + dev container (and peer if any)
  finish <name>         Merge to main, destroy worktree + container, delete branch
  list                  Show all worktrees and container status
  sync <name|all>       Merge main into worktree branch(es)
  deploy <name>         Build and deploy to the worktree's container (and peer if any)
  shell <name> [edge]   Open a shell in the container (or peer with 'edge')
  logs <name> [svc]     Show logs (default: tentacle service)
  start <name>          Start a stopped container (and peer if any)
  stop <name>           Stop a running container (and peer if any)
  peer-create <name>    Create a paired '-edge' container off the same worktree
                        (for testing mantle ↔ edge interactions from one branch)
  peer-destroy <name>   Remove just the paired edge container
  peer-shell <name>     Open a shell in the edge container
  peer-logs <name> [s]  Show logs from the edge container

Examples:
  worktree-dev.sh create plc
  worktree-dev.sh finish plc       # merge feature/plc → main, cleanup
  worktree-dev.sh sync hmi         # merge main into feature/hmi
  worktree-dev.sh sync all         # merge main into all feature worktrees
  worktree-dev.sh deploy profinet
  worktree-dev.sh shell modbus
  worktree-dev.sh logs opcua tentacle-web-dev
  worktree-dev.sh peer-create mantle  # spin up tentacle-next-dev-mantle-edge
  worktree-dev.sh deploy mantle       # builds once, pushes to both containers
EOF
}

container_name() { echo "${CONTAINER_PREFIX}-${1}"; }
peer_container_name() { echo "${CONTAINER_PREFIX}-${1}-edge"; }
worktree_path() { echo "${WORKTREE_BASE}/tentacle-next-${1}"; }
branch_name() { echo "feature/${1}"; }

cmd_init_image() {
  echo "==> Creating golden image from tentacle-next-dev-1..."

  # Stop services and clear runtime data for a clean image
  incus exec tentacle-next-dev-1 -- systemctl stop tentacle 2>/dev/null || true
  incus exec tentacle-next-dev-1 -- systemctl stop tentacle-web-dev 2>/dev/null || true
  incus exec tentacle-next-dev-1 -- bash -c 'rm -rf /var/lib/tentacle/*' 2>/dev/null || true

  # Clear Tailscale state so new containers get fresh identities
  incus exec tentacle-next-dev-1 -- tailscale down 2>/dev/null || true
  incus exec tentacle-next-dev-1 -- systemctl stop tailscaled 2>/dev/null || true
  incus exec tentacle-next-dev-1 -- bash -c 'rm -rf /var/lib/tailscale/*' 2>/dev/null || true

  # Must stop the container to publish
  incus stop tentacle-next-dev-1

  # Delete old image if it exists
  incus image delete "$IMAGE_ALIAS" 2>/dev/null || true

  # Publish as reusable image
  incus publish tentacle-next-dev-1 --alias "$IMAGE_ALIAS"

  # Restart the original container
  incus start tentacle-next-dev-1

  # Re-register Tailscale on the source container
  if [ -f "$TS_KEY_FILE" ]; then
    echo "==> Re-registering Tailscale on tentacle-next-dev-1..."
    sleep 3
    incus exec tentacle-next-dev-1 -- tailscale up --auth-key="$(cat "$TS_KEY_FILE")" --hostname=tentacle-next-dev-1
  fi

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

    # Register with Tailscale using a unique hostname
    if [ -f "$TS_KEY_FILE" ]; then
      echo "==> Registering Tailscale as $ct_name..."
      incus exec "$ct_name" -- bash -c 'rm -rf /var/lib/tailscale/*' 2>/dev/null || true
      incus exec "$ct_name" -- systemctl restart tailscaled
      sleep 2
      incus exec "$ct_name" -- tailscale up --auth-key="$(cat "$TS_KEY_FILE")" --hostname="$ct_name"
      echo "    Tailscale registered."
    else
      echo "    NOTE: No Tailscale key at $TS_KEY_FILE — skipping Tailscale setup."
    fi

    echo "    Container ready."
  fi

  # Get container IPs
  local ip ts_ip
  ip=$(incus list "$ct_name" -f csv -c 4 | grep -oP '[\d.]+(?=.*eth0)' | head -1) || true
  ts_ip=$(incus exec "$ct_name" -- tailscale ip -4 2>/dev/null) || true

  echo ""
  echo "==> Worktree '$name' is ready!"
  echo "    Source:      $wt_path"
  echo "    Branch:      $branch"
  echo "    Container:   $ct_name"
  echo "    LAN:         http://${ip:-<pending>}"
  echo "    Tailscale:   http://${ts_ip:-<not configured>}"
  echo "    Vite HMR:    http://${ip:-<pending>}:3012"
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
  local peer_name
  peer_name=$(peer_container_name "$name")
  local branch
  branch=$(branch_name "$name")

  echo "==> Destroying worktree '$name'..."

  # Stop and delete peer container first (it bind-mounts the same worktree)
  if incus info "$peer_name" &>/dev/null; then
    echo "    Deleting peer container $peer_name..."
    incus delete "$peer_name" --force
    echo "    Peer container deleted."
  fi

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

cmd_finish() {
  local name="$1"
  local wt_path
  wt_path=$(worktree_path "$name")
  local branch
  branch=$(branch_name "$name")

  if [ ! -d "$wt_path" ]; then
    echo "ERROR: Worktree not found at $wt_path"
    exit 1
  fi

  # Check for uncommitted changes in the worktree
  if ! git -C "$wt_path" diff --quiet || ! git -C "$wt_path" diff --cached --quiet; then
    echo "ERROR: Worktree '$name' has uncommitted changes. Commit or stash first."
    exit 1
  fi
  if [ -n "$(git -C "$wt_path" ls-files --others --exclude-standard)" ]; then
    echo "WARNING: Worktree '$name' has untracked files (they will be left behind)."
  fi

  # Merge feature branch into main
  echo "==> Merging '$branch' into main..."
  git -C "$REPO_ROOT" checkout main
  if ! git -C "$REPO_ROOT" merge "$branch" --no-ff -m "Merge $branch"; then
    echo ""
    echo "==> Merge conflict. Resolve in $REPO_ROOT, then re-run:"
    echo "    worktree-dev.sh destroy $name"
    echo "    git branch -d $branch"
    exit 1
  fi
  echo "    Merge successful."

  # Destroy worktree + container
  cmd_destroy "$name"

  # Delete the branch (it's merged)
  echo "==> Deleting branch '$branch'..."
  git -C "$REPO_ROOT" branch -d "$branch"

  echo ""
  echo "==> Finished '$name'. Feature merged to main, worktree and container removed."
  echo "    Push when ready: git -C $REPO_ROOT push"
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

  push_binary_to_container "$ct_name" "$wt_path/bin/tentacle"

  # If a paired edge container exists, push to it too — they share the worktree
  # so a single build serves both. This keeps mantle ↔ edge in lockstep when
  # iterating on protocol changes between the two sides.
  local peer_name
  peer_name=$(peer_container_name "$name")
  if incus info "$peer_name" &>/dev/null; then
    echo ""
    echo "==> Peer container $peer_name detected — deploying there too..."
    push_binary_to_container "$peer_name" "$wt_path/bin/tentacle"
  fi
}

# Helper used by cmd_deploy and cmd_peer_create. Stops the tentacle service,
# pushes the new binary, restarts the service, and reports status.
push_binary_to_container() {
  local ct="$1"
  local binary="$2"

  echo "==> [$ct] Stopping tentacle service..."
  incus exec "$ct" -- systemctl stop tentacle

  echo "==> [$ct] Pushing binary..."
  incus file push "$binary" "${ct}/usr/local/bin/tentacle"

  echo "==> [$ct] Starting tentacle service..."
  incus exec "$ct" -- systemctl start tentacle

  local status
  status=$(incus exec "$ct" -- systemctl is-active tentacle)
  if [ "$status" = "active" ]; then
    echo "==> [$ct] Deploy successful — tentacle is running."
  else
    echo "==> [$ct] ERROR: tentacle service status is '$status'"
    incus exec "$ct" -- journalctl -u tentacle -n 20 --no-pager
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

cmd_sync() {
  local target="$1"

  if [ "$target" = "all" ]; then
    # Find all feature worktrees by listing worktree paths that match our pattern.
    local failed=()
    local synced=()
    while IFS= read -r line; do
      local wt_path
      wt_path=$(echo "$line" | awk '{print $1}')
      # Skip the main worktree
      [[ "$wt_path" == "$REPO_ROOT" ]] && continue
      # Extract name from path: /path/tentacle-next-<name> → <name>
      local name="${wt_path##*/tentacle-next-}"
      [ -z "$name" ] && continue

      echo "==> Syncing '$name' (merging main)..."
      if git -C "$wt_path" merge main --no-edit 2>&1; then
        synced+=("$name")
        echo "    OK"
      else
        echo "    CONFLICT — aborting merge for '$name'"
        git -C "$wt_path" merge --abort 2>/dev/null || true
        failed+=("$name")
      fi
      echo ""
    done < <(git -C "$REPO_ROOT" worktree list)

    echo "=== Sync Summary ==="
    if [ ${#synced[@]} -gt 0 ]; then
      echo "  Synced: ${synced[*]}"
    fi
    if [ ${#failed[@]} -gt 0 ]; then
      echo "  Failed (conflicts): ${failed[*]}"
      echo "  Resolve manually: cd <worktree> && git merge main"
    fi
    if [ ${#synced[@]} -eq 0 ] && [ ${#failed[@]} -eq 0 ]; then
      echo "  No feature worktrees found."
    fi
  else
    local wt_path
    wt_path=$(worktree_path "$target")
    if [ ! -d "$wt_path" ]; then
      echo "ERROR: Worktree not found at $wt_path"
      exit 1
    fi

    echo "==> Syncing '$target' (merging main)..."
    if git -C "$wt_path" merge main --no-edit; then
      echo "==> Done. '$target' is up to date with main."
    else
      echo ""
      echo "==> Merge conflict detected. Resolve manually:"
      echo "    cd $wt_path"
      echo "    # fix conflicts, then: git merge --continue"
      echo ""
      echo "    Or abort: git -C $wt_path merge --abort"
      exit 1
    fi
  fi
}

cmd_start() {
  local ct_name peer_name
  ct_name=$(container_name "$1")
  peer_name=$(peer_container_name "$1")
  incus start "$ct_name"
  echo "==> $ct_name started."
  if incus info "$peer_name" &>/dev/null; then
    incus start "$peer_name"
    echo "==> $peer_name started."
  fi
}

cmd_stop() {
  local ct_name peer_name
  ct_name=$(container_name "$1")
  peer_name=$(peer_container_name "$1")
  if incus info "$peer_name" &>/dev/null; then
    incus stop "$peer_name"
    echo "==> $peer_name stopped."
  fi
  incus stop "$ct_name"
  echo "==> $ct_name stopped."
}

# A "peer" container shares the worktree with its primary and bind-mounts the
# same source. Used to spin up a second tentacle off one branch — e.g. mantle
# (gitserver host) and edge (gitops client) talking to each other from the
# same code. Bind-mount means one `deploy <name>` builds once and the peer
# picks up the same binary automatically (see cmd_deploy).
cmd_peer_create() {
  local name="$1"
  local wt_path
  wt_path=$(worktree_path "$name")
  local peer_name
  peer_name=$(peer_container_name "$name")

  if [ ! -d "$wt_path" ]; then
    echo "ERROR: Worktree not found at $wt_path. Run 'create $name' first."
    exit 1
  fi

  if incus info "$peer_name" &>/dev/null; then
    echo "==> Peer container $peer_name already exists."
    return
  fi

  echo "==> Creating peer edge container '$peer_name' off worktree '$name'..."

  if ! incus image show "$IMAGE_ALIAS" &>/dev/null; then
    echo "==> Golden image not found. Creating from tentacle-next-dev-1..."
    cmd_init_image
  fi

  echo "==> Launching $peer_name from golden image..."
  incus launch "$IMAGE_ALIAS" "$peer_name" \
    -c raw.idmap="both 1000 0" \
    -c security.nesting=true

  # Same bind-mount as the primary — both containers run the same code.
  incus config device add "$peer_name" tentacle-next-src disk \
    source="$wt_path" path=/root/tentacle-next shift=true

  incus config device add "$peer_name" eno1 nic network=ne1-net1
  incus config device add "$peer_name" eno2 nic network=ne1-net2

  echo "    Waiting for container networking..."
  sleep 5

  # Web deps live on the worktree filesystem, already installed by 'create'.
  # Clear stale runtime state from the golden image so this peer starts fresh.
  incus exec "$peer_name" -- bash -c 'rm -rf /var/lib/tentacle/*' 2>/dev/null || true

  incus exec "$peer_name" -- systemctl daemon-reload
  incus exec "$peer_name" -- systemctl enable tentacle tentacle-web-dev caddy
  incus exec "$peer_name" -- systemctl restart tentacle || true
  incus exec "$peer_name" -- systemctl restart tentacle-web-dev || true
  incus exec "$peer_name" -- systemctl restart caddy || true

  if [ -f "$TS_KEY_FILE" ]; then
    echo "==> Registering Tailscale as $peer_name..."
    incus exec "$peer_name" -- bash -c 'rm -rf /var/lib/tailscale/*' 2>/dev/null || true
    incus exec "$peer_name" -- systemctl restart tailscaled
    sleep 2
    incus exec "$peer_name" -- tailscale up --auth-key="$(cat "$TS_KEY_FILE")" --hostname="$peer_name"
    echo "    Tailscale registered."
  fi

  local ip ts_ip
  ip=$(incus list "$peer_name" -f csv -c 4 | grep -oP '[\d.]+(?=.*eth0)' | head -1) || true
  ts_ip=$(incus exec "$peer_name" -- tailscale ip -4 2>/dev/null) || true

  echo ""
  echo "==> Peer edge container '$peer_name' is ready (paired with worktree '$name')!"
  echo "    Source:    $wt_path (shared with $(container_name "$name"))"
  echo "    LAN:       http://${ip:-<pending>}"
  echo "    Tailscale: http://${ts_ip:-<not configured>}"
  echo ""
  echo "    Deploy:  scripts/worktree-dev.sh deploy $name   (pushes to both)"
  echo "    Shell:   scripts/worktree-dev.sh peer-shell $name"
  echo "    Logs:    scripts/worktree-dev.sh peer-logs $name"
}

cmd_peer_destroy() {
  local name="$1"
  local peer_name
  peer_name=$(peer_container_name "$name")

  if incus info "$peer_name" &>/dev/null; then
    incus delete "$peer_name" --force
    echo "==> Peer container $peer_name deleted."
  else
    echo "==> Peer container $peer_name not found."
  fi
}

cmd_peer_shell() {
  local peer_name
  peer_name=$(peer_container_name "$1")
  exec incus exec "$peer_name" -- bash
}

cmd_peer_logs() {
  local peer_name
  peer_name=$(peer_container_name "$1")
  local service="${2:-tentacle}"
  exec incus exec "$peer_name" -- journalctl -u "$service" -n 100 -f
}

# --- Main ---
case "${1:-}" in
  init-image)    cmd_init_image ;;
  create)        cmd_create "${2:?Usage: worktree-dev.sh create <name>}" ;;
  destroy)       cmd_destroy "${2:?Usage: worktree-dev.sh destroy <name>}" ;;
  finish)        cmd_finish "${2:?Usage: worktree-dev.sh finish <name>}" ;;
  list)          cmd_list ;;
  sync)          cmd_sync "${2:?Usage: worktree-dev.sh sync <name|all>}" ;;
  deploy)        cmd_deploy "${2:?Usage: worktree-dev.sh deploy <name>}" ;;
  shell)         cmd_shell "${2:?Usage: worktree-dev.sh shell <name>}" ;;
  logs)          cmd_logs "${2:?Usage: worktree-dev.sh logs <name>}" "${3:-}" ;;
  start)         cmd_start "${2:?Usage: worktree-dev.sh start <name>}" ;;
  stop)          cmd_stop "${2:?Usage: worktree-dev.sh stop <name>}" ;;
  peer-create)   cmd_peer_create "${2:?Usage: worktree-dev.sh peer-create <name>}" ;;
  peer-destroy)  cmd_peer_destroy "${2:?Usage: worktree-dev.sh peer-destroy <name>}" ;;
  peer-shell)    cmd_peer_shell "${2:?Usage: worktree-dev.sh peer-shell <name>}" ;;
  peer-logs)     cmd_peer_logs "${2:?Usage: worktree-dev.sh peer-logs <name>}" "${3:-}" ;;
  *)             usage ;;
esac
