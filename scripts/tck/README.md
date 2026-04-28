# Sparkplug TCK in CI

Drives the [Eclipse Sparkplug TCK](https://github.com/eclipse-sparkplug/sparkplug/tree/master/tck)
against our Sparkplug B implementations:

- **Edge Node profile** — `cmd/tentacle-sparkplug` (the `mqtt`-tagged module on `main`)
- **Host Application profile** — `cmd/tentacle-sparkplug-host` (the `sparkplughost`-tagged module on `feature/mantle`)

## How it works

The TCK is a HiveMQ extension. Its web console talks to the extension via
control topics on the broker — we replace the console with `run_tck.py`,
which:

1. Connects to the broker and configures result logging
2. Publishes `NEW_TEST <profile> <name> <params>` on `SPARKPLUG_TCK/TEST_CONTROL`
3. Spawns the impl-under-test as a subprocess (`--impl-cmd`) so the TCK observes
   a fresh connect → NBIRTH sequence — running the impl beforehand makes the
   test report all assertions as `NOT EXECUTED`
4. After `observe_seconds`, stops the impl (which sends NDEATH via its will)
5. Publishes `END_TEST`, which causes the TCK to emit a multi-line summary on
   `SPARKPLUG_TCK/RESULT` listing each assertion as `PASS`/`FAIL`/`NOT EXECUTED`
6. Derives an overall verdict (any FAIL → FAIL; else any PASS → PASS;
   else NOT_EXECUTED) and exits non-zero on FAIL/TIMEOUT

CI flow (`.github/workflows/sparkplug-tck.yml`):

1. **build-tck-extension** — builds the extension zip from source (Eclipse
   doesn't publish binaries to GitHub releases) and caches it by `TCK_REF`.
2. **edge-node** — boots HiveMQ (with the TCK extension mounted), NATS, and
   the sparkplug node binary; runs `run_tck.py --profile edge`.
3. **host-application** — same as above with the host binary; auto-skips on
   refs that don't carry `cmd/tentacle-sparkplug-host`.

## Running locally

You need Docker, Java 17, Go, and Python 3.11+ with `paho-mqtt` and `pyyaml`.

```bash
# 1. Build the TCK extension (cached after first run). Requires JDK 11.
scripts/tck/build-tck-extension.sh ./tck-ext

# 2. Start broker + NATS
docker run -d --name hivemq-tck \
  -p 1883:1883 -p 8080:8080 \
  -v "$PWD/tck-ext/sparkplug-tck:/opt/hivemq/extensions/sparkplug-tck:ro" \
  hivemq/hivemq-ce:latest
docker run -d --name nats -p 4222:4222 nats:2 -js

# 3. Build the impl
go build -tags mqtt -o tentacle-sparkplug ./cmd/tentacle-sparkplug
# (or: go build -tags sparkplughost -o tentacle-sparkplug-host ./cmd/tentacle-sparkplug-host)

# 4. Drive the TCK — the driver starts the impl itself
NATS_URL=nats://localhost:4222 \
MQTT_BROKER_URL=tcp://localhost:1883 \
MQTT_PRIMARY_HOST_ID=MantleHost \
python scripts/tck/run_tck.py \
  --profile edge \
  --impl-cmd "$PWD/tentacle-sparkplug"
```

For richer interactive debugging, point a browser at the TCK web console
(`yarn start` in the upstream repo's `tck/webconsole`) — same broker, same
extension, same control topics. The driver and console can coexist; just don't
run a test from both at once.

## Adding more tests

Edit `tests.yaml`. Each entry compiles to a `NEW_TEST <profile> <name>
<space-joined params>` publish. Parameter ordering was extracted from the TCK
web console source — if Eclipse changes it, update the templates here.

The TCK currently exposes these test types:

- **edge**: `SessionEstablishmentTest`, `SessionTerminationTest`, `SendDataTest`,
  `SendComplexDataTest`, `PrimaryHostTest`, `ReceiveCommandTest`,
  `MultipleBrokerTest`
- **host**: `SessionEstablishmentTest`, `SessionTerminationTest`, `SendCommandTest`,
  `ReceiveDataTest`, `EdgeSessionTerminationTest`, `MessageOrderingTest`,
  `MultipleBrokerTest`

Start with `SessionEstablishmentTest` (already enabled) and add others as the
implementation gets each one green. Several host tests need a co-operating
edge node on the same broker — `cmd/sparkplug-smoke` works as that partner.

## Pinning the TCK

`TCK_REF` (env var on the workflow + script) defaults to `master`. Once
Eclipse Sparkplug starts cutting tags, pin to a tag for reproducibility.
