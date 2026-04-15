.PHONY: build build-release build-cli test vet fmt clean web-build web-install

# Stable module tags for release builds (excludes experimental modules).
# Dev builds use "all" which includes everything.
STABLE_TAGS = stable,api,orchestrator,gateway,ethernetip,snmp,mqtt,network,gitops

# Version injection via ldflags.
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS = -X github.com/joyautomation/tentacle/internal/version.Version=$(VERSION)

# Build SvelteKit SPA into internal/web/static/
web-install:
	cd web && NODE_ENV=development npm install

web-build: web-install
	cd web && npm run build

# Default: build monolith with all modules (dev)
build: web-build
	go build -tags all,web -ldflags "$(LDFLAGS)" -o bin/tentacle ./cmd/tentacle

# Release: build monolith with stable modules only
build-release: web-build
	go build -tags $(STABLE_TAGS),web -ldflags "$(LDFLAGS)" -o bin/tentacle ./cmd/tentacle

# Build tentactl CLI (no build tags, lightweight)
build-cli:
	go build -o bin/tentactl ./cmd/tentactl

# Build all standalone binaries
build-all: build build-cli
	go build -tags gateway,api,web -o bin/tentacle-core ./cmd/tentacle-core
	go build -tags ethernetip -o bin/tentacle-ethernetip ./cmd/tentacle-ethernetip
	go build -tags opcua -o bin/tentacle-opcua ./cmd/tentacle-opcua
	go build -tags snmp -o bin/tentacle-snmp ./cmd/tentacle-snmp
	go build -tags modbus -o bin/tentacle-modbus ./cmd/tentacle-modbus
	go build -tags mqtt -o bin/tentacle-sparkplug ./cmd/tentacle-sparkplug
	go build -tags gateway -o bin/tentacle-gateway ./cmd/tentacle-gateway
	go build -tags orchestrator -o bin/tentacle-orchestrator ./cmd/tentacle-orchestrator
	go build -tags ethernetipserver -o bin/tentacle-ethernetip-server ./cmd/tentacle-ethernetip-server
	go build -tags modbusserver -o bin/tentacle-modbus-server ./cmd/tentacle-modbus-server
	go build -tags history -o bin/tentacle-history ./cmd/tentacle-history
	go build -tags network -o bin/tentacle-network ./cmd/tentacle-network
	go build -tags nftables -o bin/tentacle-nftables ./cmd/tentacle-nftables
	go build -tags web,api -o bin/tentacle-web ./cmd/tentacle-web
	go build -tags profinet -o bin/tentacle-profinet ./cmd/tentacle-profinet
	go build -tags profinetcontroller -o bin/tentacle-profinet-controller ./cmd/tentacle-profinet-controller
	go build -tags telemetry -o bin/tentacle-telemetry ./cmd/tentacle-telemetry

test:
	go test ./...

vet:
	go vet ./...

fmt:
	gofmt -w .

clean:
	rm -rf bin/
