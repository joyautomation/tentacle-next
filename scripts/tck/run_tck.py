#!/usr/bin/env python3
"""
Eclipse Sparkplug TCK driver.

Drives the Sparkplug TCK (a HiveMQ extension) via its MQTT control protocol so
the tests can be run headlessly in CI. The TCK ships with a web console; this
script replaces that console for non-interactive use.

Protocol (reverse-engineered from tck/webconsole/pages/index.vue at
github.com/eclipse-sparkplug/sparkplug):

  Control:
    SPARKPLUG_TCK/RESULT_CONFIG  ->  "NEW_RESULT-LOG <filename>"
    SPARKPLUG_TCK/CONFIG         ->  "UTCwindow <seconds>"
    SPARKPLUG_TCK/TEST_CONTROL   ->  "NEW_TEST <profile> <testType> <space-joined params>"
    SPARKPLUG_TCK/TEST_CONTROL   ->  "END_TEST"
  Telemetry (subscribe):
    SPARKPLUG_TCK/RESULT
    SPARKPLUG_TCK/LOG
    SPARKPLUG_TCK/CONSOLE_PROMPT

A test is complete when a RESULT or LOG message contains "OVERALL: PASS",
"OVERALL: FAIL", or "OVERALL: NOT EXECUTED".

Profiles: "host", "edge", "broker"
"""

import argparse
import json
import os
import queue
import sys
import threading
import time
from pathlib import Path

import paho.mqtt.client as mqtt
import yaml

CONTROL_TOPIC = "SPARKPLUG_TCK/TEST_CONTROL"
RESULT_CFG_TOPIC = "SPARKPLUG_TCK/RESULT_CONFIG"
CONFIG_TOPIC = "SPARKPLUG_TCK/CONFIG"
RESULT_TOPIC = "SPARKPLUG_TCK/RESULT"
LOG_TOPIC = "SPARKPLUG_TCK/LOG"
PROMPT_TOPIC = "SPARKPLUG_TCK/CONSOLE_PROMPT"

PASS_MARK = "OVERALL: PASS"
FAIL_MARK = "OVERALL: FAIL"
SKIP_MARK = "OVERALL: NOT EXECUTED"


class TCKDriver:
    def __init__(self, broker_url: str, client_id: str = "tck-driver"):
        self.broker_url = broker_url
        self.client = mqtt.Client(client_id=client_id, clean_session=True)
        self.client.on_connect = self._on_connect
        self.client.on_message = self._on_message
        self.events: queue.Queue = queue.Queue()
        self._connected = threading.Event()
        self._transcript: list[str] = []

    def _on_connect(self, client, _u, _f, rc):
        if rc != 0:
            print(f"MQTT connect failed: rc={rc}", file=sys.stderr)
            return
        for t in (RESULT_TOPIC, LOG_TOPIC, PROMPT_TOPIC):
            client.subscribe(t, qos=1)
        self._connected.set()

    def _on_message(self, _c, _u, msg):
        try:
            payload = msg.payload.decode("utf-8", errors="replace")
        except Exception:
            payload = repr(msg.payload)
        line = f"[{msg.topic}] {payload}"
        self._transcript.append(line)
        self.events.put((msg.topic, payload))

    def connect(self):
        host, port = parse_mqtt_url(self.broker_url)
        self.client.connect(host, port, keepalive=30)
        self.client.loop_start()
        if not self._connected.wait(timeout=15):
            raise RuntimeError("timed out connecting to MQTT broker")

    def disconnect(self):
        self.client.loop_stop()
        try:
            self.client.disconnect()
        except Exception:
            pass

    def configure_results(self, log_filename: str, utc_window: int = 60):
        self.client.publish(RESULT_CFG_TOPIC, f"NEW_RESULT-LOG {log_filename}", qos=1).wait_for_publish()
        self.client.publish(CONFIG_TOPIC, f"UTCwindow {utc_window}", qos=1).wait_for_publish()

    def run_test(self, profile: str, test_type: str, params: list[str], timeout: float = 120.0) -> tuple[str, list[str]]:
        """Returns (verdict, transcript_lines). verdict ∈ {PASS, FAIL, SKIP, TIMEOUT}."""
        # Drain any leftover messages from a prior test
        with self.events.mutex:
            self.events.queue.clear()
        local_lines: list[str] = []

        params_str = " ".join(params)
        payload = f"NEW_TEST {profile} {test_type} {params_str}".strip()
        print(f">> {payload}")
        self.client.publish(CONTROL_TOPIC, payload, qos=1).wait_for_publish()

        deadline = time.monotonic() + timeout
        verdict = "TIMEOUT"
        while time.monotonic() < deadline:
            try:
                _topic, msg = self.events.get(timeout=1.0)
            except queue.Empty:
                continue
            local_lines.append(msg)
            if PASS_MARK in msg:
                verdict = "PASS"
                break
            if FAIL_MARK in msg:
                verdict = "FAIL"
                break
            if SKIP_MARK in msg:
                verdict = "SKIP"
                break

        # Always send END_TEST to release the TCK state machine
        try:
            self.client.publish(CONTROL_TOPIC, "END_TEST", qos=1).wait_for_publish()
        except Exception:
            pass

        return verdict, local_lines


def parse_mqtt_url(url: str) -> tuple[str, int]:
    # Accept tcp://host:port or just host:port or host
    s = url
    for prefix in ("tcp://", "mqtt://", "ssl://"):
        if s.startswith(prefix):
            s = s[len(prefix):]
            break
    if ":" in s:
        host, port = s.split(":", 1)
        return host, int(port)
    return s, 1883


def load_test_plan(path: Path, profile: str) -> list[dict]:
    data = yaml.safe_load(path.read_text())
    plan = data.get(profile)
    if not plan:
        raise SystemExit(f"no tests defined for profile '{profile}' in {path}")
    return plan


def render_params(template_params: list[str], context: dict) -> list[str]:
    out = []
    for p in template_params:
        rendered = p.format(**context)
        out.append(rendered)
    return out


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--profile", required=True, choices=["host", "edge", "broker"])
    ap.add_argument("--broker", default=os.environ.get("MQTT_BROKER_URL", "tcp://localhost:1883"))
    ap.add_argument("--plan", default=str(Path(__file__).parent / "tests.yaml"))
    ap.add_argument("--host-id", default=os.environ.get("TCK_HOST_ID", "MantleHost"))
    ap.add_argument("--group-id", default=os.environ.get("TCK_GROUP_ID", "TentacleGroup"))
    ap.add_argument("--edge-node-id", default=os.environ.get("TCK_EDGE_NODE_ID", "EdgeNode1"))
    ap.add_argument("--device-id", default=os.environ.get("TCK_DEVICE_ID", "Device1"))
    ap.add_argument("--client-id", default=os.environ.get("TCK_CLIENT_ID", "tentacle-mqtt"))
    ap.add_argument("--results-file", default="SparkplugTCKResults.log")
    ap.add_argument("--report", default=os.environ.get("TCK_REPORT", "tck-report.json"))
    ap.add_argument("--only", help="comma-separated subset of test names to run")
    args = ap.parse_args()

    plan = load_test_plan(Path(args.plan), args.profile)
    if args.only:
        wanted = {n.strip() for n in args.only.split(",") if n.strip()}
        plan = [t for t in plan if t["name"] in wanted]
        if not plan:
            raise SystemExit(f"--only filtered out every test: {sorted(wanted)}")

    ctx = {
        "host_id": args.host_id,
        "group_id": args.group_id,
        "edge_node_id": args.edge_node_id,
        "device_id": args.device_id,
        "client_id": args.client_id,
        "broker_uri": args.broker,
    }

    driver = TCKDriver(args.broker)
    driver.connect()
    driver.configure_results(args.results_file)

    results = []
    for test in plan:
        name = test["name"]
        params = render_params(test.get("params", []), ctx)
        timeout = float(test.get("timeout", 120))
        print(f"\n=== {args.profile}/{name} ===")
        verdict, lines = driver.run_test(args.profile, name, params, timeout=timeout)
        print(f"   {verdict}")
        results.append({"name": name, "verdict": verdict, "lines": lines})
        # Brief settle time between tests so the impl can re-establish session.
        time.sleep(2)

    driver.disconnect()

    Path(args.report).write_text(json.dumps({
        "profile": args.profile,
        "broker": args.broker,
        "context": ctx,
        "results": results,
    }, indent=2))

    passed = sum(1 for r in results if r["verdict"] == "PASS")
    failed = sum(1 for r in results if r["verdict"] in ("FAIL", "TIMEOUT"))
    skipped = sum(1 for r in results if r["verdict"] == "SKIP")
    print(f"\nSummary: {passed} pass, {failed} fail, {skipped} skip")
    print(f"Report: {args.report}")
    sys.exit(0 if failed == 0 else 1)


if __name__ == "__main__":
    main()
