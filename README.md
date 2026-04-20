# Tentacle

A distributed industrial IoT gateway platform written in Go. Tentacle connects to industrial devices via multiple protocols and exposes a unified REST API with real-time streaming, all manageable through an embedded web UI.

## Features

- **Multi-protocol connectivity** — EtherNet/IP (CIP), Modbus TCP, OPC/UA, SNMP, PROFINET, MQTT/Sparkplug B
- **Flexible deployment** — Run everything as a single binary or distribute protocol modules across machines
- **Real-time streaming** — REST API with Server-Sent Events for live variable updates, logs, and status
- **Embedded web UI** — SvelteKit SPA built into the binary — no separate web server needed
- **Report by exception** — Deadband filtering reduces noise from high-frequency tag scans
- **Historical storage** — Optional PostgreSQL time-series backend
- **Service orchestration** — Manage and monitor protocol modules as supervised services
- **GitOps config sync** — Bidirectional sync between system config and a git repository
- **PLC engine** — Soft PLC with Starlark task runner and IEC 61131-3 support
- **CLI management** — `tentactl` CLI for kubectl-like config management (apply, diff, export)
- **Setup wizard** — Guided onboarding UI for initial configuration
- **NATS backbone** — All modules communicate over embedded NATS with JetStream KV for configuration

## Architecture

Tentacle is built as a Go monorepo with pluggable modules controlled by build tags. Each module implements a shared lifecycle interface and communicates over an embedded NATS bus.

```
┌─────────────────────────────────────────────────┐
│                  tentacle binary                │
│                                                 │
│  ┌──────────┐  ┌──────────┐  ┌──────────────┐   │
│  │ REST API │  │  Web UI  │  │ Orchestrator │   │
│  │  (chi)   │  │(embedded)│  │              │   │
│  └────┬─────┘  └──────────┘  └──────────────┘   │
│       │                                         │
│  ┌────┴──────────── NATS Bus ──────────────┐    │
│  │                                         │    │
│  ├── EtherNet/IP    ├── Modbus             │    │
│  ├── OPC/UA         ├── SNMP               │    │
│  ├── MQTT/Sparkplug ├── Gateway            │    │
│  ├── PROFINET       ├── Network            │    │
│  ├── History        ├── GitOps             │    │
│  ├── PLC            ├── nftables           │    │
│  └── ...                                   │    │
│                                            │    │
│  └─────────────────────────────────────────┘    │
└─────────────────────────────────────────────────┘
```

## Stable vs Experimental Modules

Modules are split into **stable** and **experimental** categories using Go build tags.

**Stable** — included in release builds:
- Gateway, EtherNet/IP scanner, SNMP, MQTT/Sparkplug B, Network, GitOps

**Experimental** — included only in dev builds (`-tags all`):
- History, NFTables, OPC UA, Modbus, PLC, PROFINET IO Device, PROFINET IO Controller, EtherNet/IP Server, Modbus Server

Release builds include two monolith binaries:
- **`tentacle`** — stable modules only
- **`tentacle-experimental`** — stable + all experimental modules

To build locally:

```bash
make build          # Dev: all modules (stable + experimental)
make build-release  # Release: stable modules only
```

Experimental modules are marked with a badge in the web UI. On stable builds, they appear as "Future" and are disabled.

## Installation

### Download a release

Download the latest binary directly:

```bash
# amd64
curl -LO "https://joyautomation.com/downloads/tentacle/latest/linux_amd64.tar.gz"
tar xzf linux_amd64.tar.gz

# arm64
curl -LO "https://joyautomation.com/downloads/tentacle/latest/linux_arm64.tar.gz"
tar xzf linux_arm64.tar.gz
```

To pin a specific version, replace `latest` with the tag (e.g.
`https://joyautomation.com/downloads/tentacle/v0.0.11/linux_amd64.tar.gz`).

Browse all releases and notes at
[joyautomation.com/software/tentacle/releases](https://joyautomation.com/software/tentacle/releases).

The binary is fully self-contained — no runtime dependencies.

### Run

```bash
./tentacle
```

The web UI is available at `http://localhost:4000` by default.

### Install as a systemd service

**From the web UI:** Run tentacle, open the dashboard, and click "Install as Service" in the banner. Click "Activate" to switch to service mode.

**From the CLI:**

```bash
sudo tentacle service install
sudo systemctl start tentacle
```

**Manually:**

```bash
sudo cp tentacle /usr/local/bin/
sudo tee /etc/systemd/system/tentacle.service > /dev/null << 'EOF'
[Unit]
Description=Tentacle IoT Gateway
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
ExecStart=/usr/local/bin/tentacle
Restart=always
RestartSec=5

[Install]
WantedBy=multi-user.target
EOF

sudo systemctl daemon-reload
sudo systemctl enable --now tentacle
```

## Configuration

Tentacle uses environment variables for configuration, with NATS KV as persistent storage. Key variables:

| Variable | Default | Description |
|----------|---------|-------------|
| `API_PORT` | `4000` | REST API and web UI port |
| `TENTACLE_DATA_DIR` | `/var/lib/tentacle` or `~/.local/share/tentacle` | Data directory (auto-detected based on permissions) |
| `NATS_URL` | embedded | External NATS server URL (optional) |

Configuration can also be managed through:
- **Web UI** — Setup wizard and per-module settings pages
- **REST API** — `/api/v1/config` and `/api/v1/apply`
- **CLI** — `tentactl apply -f config.yaml`, `tentactl export`, `tentactl diff`
- **GitOps** — Bidirectional sync with a git repository

## Binaries

The monorepo produces 20 binaries for different deployment scenarios:

| Binary | Description |
|--------|-------------|
| `tentacle` | Stable modules (release monolith) |
| `tentacle-experimental` | All modules including experimental (release monolith) |
| `tentactl` | CLI for kubectl-like config management |
| `tentacle-core` | Gateway + API + Web UI |
| `tentacle-web` | API + Web UI only |
| `tentacle-gateway` | Gateway routing module |
| `tentacle-ethernetip` | EtherNet/IP (CIP) scanner |
| `tentacle-ethernetip-server` | EtherNet/IP server (experimental) |
| `tentacle-modbus` | Modbus TCP client (experimental) |
| `tentacle-modbus-server` | Modbus TCP server (experimental) |
| `tentacle-opcua` | OPC/UA client (experimental) |
| `tentacle-snmp` | SNMP client |
| `tentacle-sparkplug` | MQTT/Sparkplug B bridge |
| `tentacle-orchestrator` | Service lifecycle manager |
| `tentacle-history` | PostgreSQL time-series storage (experimental) |
| `tentacle-network` | Network interface management |
| `tentacle-nftables` | Firewall rules management (experimental) |
| `tentacle-profinet` | PROFINET IO Device (experimental) |
| `tentacle-profinet-controller` | PROFINET IO Controller (experimental) |
| `tentacle-plc` | Soft PLC engine (experimental) |

For most deployments, just use `tentacle` (the monolith). Individual binaries are for distributed setups where protocol modules run on separate machines connected via NATS.

## Building from source

### Prerequisites

- Go 1.25+
- Node.js 22+ (for web UI build)
- CMake and build-essential (for libplctag, only needed for EtherNet/IP)

### Build

```bash
# Build the monolith with all modules (dev)
make build

# Build the monolith with stable modules only (release)
make build-release

# Build the tentactl CLI
make build-cli

# Build all standalone binaries
make build-all
```

The web UI is built automatically as part of `make build`. The SvelteKit SPA compiles to static files that are embedded into the Go binary via `go:embed`.

### Build without web UI

```bash
# Build without the web tag to exclude the embedded UI
go build -tags all -o bin/tentacle ./cmd/tentacle
```

## REST API

Base URL: `/api/v1`

Key endpoints:

- `GET /services` — List running services
- `GET /variables` — List all scanned variables
- `GET /variables/stream` — SSE stream of variable updates
- `GET /gateways` — List gateway configurations
- `PUT /gateways/{id}/variables` — Configure variables to scan
- `POST /browse/{protocol}` — Discover devices and tags
- `GET /services/{type}/logs/stream` — Stream service logs (SSE)
- `POST /apply` — Apply YAML config (kubectl-style)
- `GET /export` — Export full system config as YAML

## License

See [LICENSE](LICENSE) for details.
