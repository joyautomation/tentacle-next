# Tentacle

A distributed industrial IoT gateway platform written in Go. Tentacle connects to industrial devices via multiple protocols and exposes a unified REST API with real-time streaming, all manageable through an embedded web UI.

## Features

- **Multi-protocol connectivity** — EtherNet/IP (CIP), Modbus TCP, OPC/UA, SNMP, MQTT/Sparkplug B
- **Flexible deployment** — Run everything as a single binary or distribute protocol modules across machines
- **Real-time streaming** — REST API with Server-Sent Events for live variable updates, logs, and status
- **Embedded web UI** — SvelteKit SPA built into the binary — no separate web server needed
- **Report by exception** — Deadband filtering reduces noise from high-frequency tag scans
- **Historical storage** — Optional PostgreSQL time-series backend
- **Service orchestration** — Manage and monitor protocol modules as supervised services
- **NATS backbone** — All modules communicate over embedded NATS with JetStream KV for configuration

## Architecture

Tentacle is built as a Go monorepo with pluggable modules controlled by build tags. Each module implements a shared lifecycle interface and communicates over an embedded NATS bus.

```
┌─────────────────────────────────────────────────┐
│                  tentacle binary                 │
│                                                  │
│  ┌──────────┐  ┌──────────┐  ┌──────────────┐   │
│  │ REST API │  │  Web UI  │  │ Orchestrator │   │
│  │  (chi)   │  │(embedded)│  │              │   │
│  └────┬─────┘  └──────────┘  └──────────────┘   │
│       │                                          │
│  ┌────┴──────────── NATS Bus ──────────────┐     │
│  │                                         │     │
│  ├── EtherNet/IP    ├── Modbus             │     │
│  ├── OPC/UA         ├── SNMP               │     │
│  ├── MQTT/Sparkplug ├── Gateway            │     │
│  ├── History        ├── Network            │     │
│  └── nftables       └── ...                │     │
│                                            │     │
│  └─────────────────────────────────────────┘     │
└─────────────────────────────────────────────────┘
```

## Installation

### Download a release

Download the latest binary from [GitHub Releases](https://github.com/joyautomation/tentacle-next/releases):

```bash
# Replace VERSION with the desired release (e.g. 0.0.2)
VERSION=0.0.2

# For amd64
curl -LO "https://github.com/joyautomation/tentacle-next/releases/download/v${VERSION}/tentacle_${VERSION}_linux_amd64.tar.gz"
tar xzf "tentacle_${VERSION}_linux_amd64.tar.gz"

# For arm64
curl -LO "https://github.com/joyautomation/tentacle-next/releases/download/v${VERSION}/tentacle_${VERSION}_linux_arm64.tar.gz"
tar xzf "tentacle_${VERSION}_linux_arm64.tar.gz"
```

Or browse all releases at [github.com/joyautomation/tentacle-next/releases](https://github.com/joyautomation/tentacle-next/releases).

The binary is fully self-contained — no runtime dependencies.

### Run

```bash
./tentacle
```

The web UI is available at `http://localhost:4000` by default.

### Install as a systemd service

```bash
sudo cp tentacle /usr/local/bin/

sudo tee /etc/systemd/system/tentacle.service > /dev/null << 'EOF'
[Unit]
Description=Tentacle IoT Gateway
After=network.target

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
| `NATS_URL` | embedded | External NATS server URL (optional) |

Configuration can also be managed through the web UI or REST API at `/api/v1/config`.

## Binaries

The monorepo produces 15 binaries for different deployment scenarios:

| Binary | Description |
|--------|-------------|
| `tentacle` | All modules (monolithic) |
| `tentacle-core` | Gateway + API + Web UI |
| `tentacle-web` | API + Web UI only |
| `tentacle-gateway` | Gateway routing module |
| `tentacle-ethernetip` | EtherNet/IP (CIP) scanner |
| `tentacle-ethernetip-server` | EtherNet/IP server |
| `tentacle-modbus` | Modbus TCP client |
| `tentacle-modbus-server` | Modbus TCP server |
| `tentacle-opcua` | OPC/UA client |
| `tentacle-snmp` | SNMP client |
| `tentacle-sparkplug` | MQTT/Sparkplug B bridge |
| `tentacle-orchestrator` | Service lifecycle manager |
| `tentacle-history` | PostgreSQL time-series storage |
| `tentacle-network` | Network interface management |
| `tentacle-nftables` | Firewall rules management |

For most deployments, just use `tentacle` (the monolith). Individual binaries are for distributed setups where protocol modules run on separate machines connected via NATS.

## Building from source

### Prerequisites

- Go 1.25+
- Node.js 22+ (for web UI build)
- CMake and build-essential (for libplctag, only needed for EtherNet/IP)

### Build

```bash
# Build the monolith (includes web UI)
make build

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

## License

See [LICENSE](LICENSE) for details.
