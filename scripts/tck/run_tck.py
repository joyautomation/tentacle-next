#!/usr/bin/env python3
"""
Eclipse Sparkplug TCK driver.

Drives the TCK (a HiveMQ extension) over its MQTT control protocol so the
tests can run headlessly in CI.

Protocol (verified against eclipse-sparkplug/sparkplug master at the time of
writing — see scripts/tck/README.md):

  Control (publish):
    SPARKPLUG_TCK/RESULT_CONFIG  ->  "NEW_RESULT-LOG <filename>"
    SPARKPLUG_TCK/CONFIG         ->  "UTCwindow <seconds>"
    SPARKPLUG_TCK/TEST_CONTROL   ->  "NEW_TEST <profile> <name> <space-joined params>"
    SPARKPLUG_TCK/TEST_CONTROL   ->  "END_TEST"
  Telemetry (subscribe):
    SPARKPLUG_TCK/RESULT          (multi-line summary, emitted on END_TEST)
    SPARKPLUG_TCK/LOG             (per-assertion narration during the test)
    SPARKPLUG_TCK/CONSOLE_PROMPT  (interactive prompts — we ignore)

The TCK emits its summary on RESULT only after we send END_TEST. The summary
lists each assertion individually, e.g.:

    Summary Test Results for Edge SessionEstablishment
    message-flow-edge-node-birth-publish-connect: PASS;
    message-flow-edge-node-birth-publish-nbirth-payload: NOT EXECUTED;
    ...

There is no "OVERALL: PASS/FAIL" line — verdict is derived from per-assertion
verdicts: any FAIL → FAIL; else any PASS → PASS; else NOT EXECUTED.

Profiles: "host", "edge", "broker".
"""

import argparse
import json
import os
import re
import queue
import shlex
import signal
import subprocess
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

PUBLISH_ACK_TIMEOUT = 10.0  # seconds; never block forever on a publish

# Each line of the TCK summary is `[<class-prefix>:]<assertion-id>: <verdict>[ ...];`
# where <class-prefix> is e.g. `Monitor:` for assertions raised by the
# upstream Monitor harness. The class prefix counts as part of the name
# (it disambiguates monitor- from impl-raised assertions of the same id),
# so we match optionally and keep it in the name.
# We explicitly skip the `OVERALL: FAIL/PASS;` trailer — we derive overall
# verdict ourselves.
ASSERTION_LINE = re.compile(
    r"^\s*((?:Monitor:)?[a-zA-Z0-9_\-]+)\s*:\s+(PASS|FAIL|NOT EXECUTED)\b"
)


def parse_mqtt_url(url: str) -> tuple[str, int]:
    s = url
    for prefix in ("tcp://", "mqtt://", "ssl://"):
        if s.startswith(prefix):
            s = s[len(prefix):]
            break
    if ":" in s:
        host, port = s.split(":", 1)
        return host, int(port)
    return s, 1883


def parse_summary(payload: str) -> tuple[dict[str, str], str]:
    """Return ({assertion: verdict}, derived overall verdict)."""
    results: dict[str, str] = {}
    for line in payload.splitlines():
        m = ASSERTION_LINE.match(line)
        if not m:
            continue
        name = m.group(1)
        if name == "OVERALL":
            continue
        results[name] = m.group(2).strip()
    if not results:
        return {}, "NO_RESULTS"
    if any(v == "FAIL" for v in results.values()):
        return results, "FAIL"
    if any(v == "PASS" for v in results.values()):
        return results, "PASS"
    return results, "NOT_EXECUTED"


class TCKDriver:
    def __init__(self, broker_url: str, client_id: str = "tck-driver"):
        self.broker_url = broker_url
        # paho 2.x preferred constructor; falls back to legacy on older paho.
        try:
            self.client = mqtt.Client(
                callback_api_version=mqtt.CallbackAPIVersion.VERSION2,
                client_id=client_id,
                clean_session=True,
            )
            self._api_v2 = True
        except (AttributeError, TypeError):
            self.client = mqtt.Client(client_id=client_id, clean_session=True)
            self._api_v2 = False
        self.client.on_connect = self._on_connect_v2 if self._api_v2 else self._on_connect_v1
        self.client.on_message = self._on_message
        self._connected = threading.Event()
        self._results: queue.Queue = queue.Queue()
        self._log_lines: list[str] = []

    def _on_connect_v1(self, client, _u, _f, rc):
        if rc == 0:
            for t in (RESULT_TOPIC, LOG_TOPIC, PROMPT_TOPIC):
                client.subscribe(t, qos=1)
            self._connected.set()
        else:
            print(f"connect rc={rc}", file=sys.stderr)

    def _on_connect_v2(self, client, _u, _f, rc, _props=None):
        return self._on_connect_v1(client, _u, _f, rc)

    def _on_message(self, _c, _u, msg):
        try:
            payload = msg.payload.decode("utf-8", errors="replace")
        except Exception:
            payload = repr(msg.payload)
        if msg.topic == RESULT_TOPIC:
            self._results.put(payload)
        else:
            # Log + prompt — keep a tail for debugging
            self._log_lines.append(f"[{msg.topic}] {payload}")
            if len(self._log_lines) > 1000:
                self._log_lines = self._log_lines[-1000:]

    def connect(self):
        host, port = parse_mqtt_url(self.broker_url)
        self.client.connect(host, port, keepalive=30)
        self.client.loop_start()
        if not self._connected.wait(timeout=15):
            raise RuntimeError(f"timed out connecting to {self.broker_url}")

    def disconnect(self):
        try:
            self.client.loop_stop()
        finally:
            try:
                self.client.disconnect()
            except Exception:
                pass

    def _publish(self, topic: str, payload: str):
        info = self.client.publish(topic, payload, qos=1)
        info.wait_for_publish(timeout=PUBLISH_ACK_TIMEOUT)
        if not info.is_published():
            raise RuntimeError(f"publish to {topic} not acknowledged within {PUBLISH_ACK_TIMEOUT}s")

    def configure_results(self, log_filename: str, utc_window: int = 60):
        self._publish(RESULT_CFG_TOPIC, f"NEW_RESULT-LOG {log_filename}")
        self._publish(CONFIG_TOPIC, f"UTCwindow {utc_window}")

    def run_test(
        self,
        profile: str,
        test_type: str,
        params: list[str],
        observe_seconds: float,
        result_timeout: float,
        impl_cmd: str | None = None,
        impl_log: str = "impl.log",
        fixture_cmd: str | None = None,
        fixture_log: str = "fixture.log",
    ) -> dict:
        """
        Run one TCK test:
          1. publish NEW_TEST
          2. (optional) start impl-under-test as a subprocess so its first
             connect+NBIRTH is observed by the TCK
          3. wait observe_seconds while the impl interacts with the TCK
          4. stop the impl (NDEATH from its will message)
          5. publish END_TEST
          6. wait up to `result_timeout` for the multi-line RESULT summary
          7. parse per-assertion verdicts
        """
        # Drain any leftover RESULTs from a previous test
        while not self._results.empty():
            try: self._results.get_nowait()
            except queue.Empty: break

        params_str = " ".join(params)
        new_test = f"NEW_TEST {profile} {test_type} {params_str}".strip()
        print(f">> {new_test}", flush=True)
        self._publish(CONTROL_TOPIC, new_test)

        impl_proc = None
        if impl_cmd:
            print(f">> launching impl: {impl_cmd}", flush=True)
            impl_proc = subprocess.Popen(
                shlex.split(impl_cmd),
                stdout=open(impl_log, "wb"),
                stderr=subprocess.STDOUT,
                start_new_session=True,
            )

        fixture_proc = None
        if fixture_cmd:
            # Slight delay so the impl finishes its connect+NBIRTH before the
            # fixture starts driving DBIRTH-eligible variables.
            time.sleep(2)
            print(f">> launching fixture: {fixture_cmd}", flush=True)
            fixture_proc = subprocess.Popen(
                shlex.split(fixture_cmd),
                stdout=open(fixture_log, "wb"),
                stderr=subprocess.STDOUT,
                start_new_session=True,
            )

        time.sleep(observe_seconds)

        for proc, label in ((fixture_proc, "fixture"), (impl_proc, "impl")):
            if proc is None:
                continue
            print(f">> stopping {label}", flush=True)
            try:
                os.killpg(proc.pid, signal.SIGTERM)
            except ProcessLookupError:
                pass
            try:
                proc.wait(timeout=10)
            except subprocess.TimeoutExpired:
                try:
                    os.killpg(proc.pid, signal.SIGKILL)
                except ProcessLookupError:
                    pass

        print(">> END_TEST", flush=True)
        self._publish(CONTROL_TOPIC, "END_TEST")

        try:
            summary_payload = self._results.get(timeout=result_timeout)
        except queue.Empty:
            return {
                "name": test_type,
                "verdict": "TIMEOUT",
                "assertions": {},
                "summary": "",
            }

        assertions, overall = parse_summary(summary_payload)
        return {
            "name": test_type,
            "verdict": overall,
            "assertions": assertions,
            "summary": summary_payload,
        }


def render_params(template_params: list[str], context: dict) -> list[str]:
    return [p.format(**context) for p in template_params]


def load_test_plan(path: Path, profile: str) -> list[dict]:
    data = yaml.safe_load(path.read_text())
    plan = data.get(profile)
    if not plan:
        raise SystemExit(f"no tests defined for profile '{profile}' in {path}")
    kept = []
    for t in plan:
        if t.get("skip"):
            print(f">> skipping {profile}/{t['name']}: {t.get('skip_reason', 'no reason given')}")
            continue
        kept.append(t)
    return kept


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
    ap.add_argument(
        "--impl-cmd",
        default=os.environ.get("TCK_IMPL_CMD"),
        help="command to launch the impl-under-test for each test (e.g. './tentacle-sparkplug'). "
             "Started after NEW_TEST and stopped before END_TEST so the TCK observes a fresh "
             "connect/NBIRTH sequence.",
    )
    ap.add_argument(
        "--impl-log",
        default=os.environ.get("TCK_IMPL_LOG", "impl.log"),
        help="file to capture impl stdout+stderr",
    )
    ap.add_argument(
        "--fixture-cmd",
        default=os.environ.get("TCK_FIXTURE_CMD"),
        help="optional fixture process launched after the impl. For the edge profile, "
             "use cmd/tck-fixture which publishes synthetic gateway data to NATS so the "
             "bridge emits DBIRTH/DDATA. Without it, ~20 device-related TCK assertions "
             "report NOT EXECUTED.",
    )
    ap.add_argument(
        "--fixture-log",
        default=os.environ.get("TCK_FIXTURE_LOG", "fixture.log"),
        help="file to capture fixture stdout+stderr",
    )
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
        observe = float(test.get("observe_seconds", 30))
        result_to = float(test.get("result_timeout", 30))
        print(f"\n=== {args.profile}/{name} ===")
        r = driver.run_test(
            args.profile, name, params, observe, result_to,
            impl_cmd=args.impl_cmd, impl_log=args.impl_log,
            fixture_cmd=args.fixture_cmd, fixture_log=args.fixture_log,
        )
        v = r["verdict"]
        a = r["assertions"]
        if a:
            pas = sum(1 for x in a.values() if x == "PASS")
            fai = sum(1 for x in a.values() if x == "FAIL")
            ne = sum(1 for x in a.values() if x == "NOT EXECUTED")
            print(f"   {v}  (assertions: {pas} pass, {fai} fail, {ne} not-executed)")
        else:
            print(f"   {v}")
        results.append(r)
        time.sleep(2)

    driver.disconnect()

    Path(args.report).write_text(json.dumps({
        "profile": args.profile,
        "broker": args.broker,
        "context": ctx,
        "results": results,
        "log_tail": driver._log_lines[-200:],
    }, indent=2))

    failures = sum(1 for r in results if r["verdict"] in ("FAIL", "TIMEOUT", "NO_RESULTS"))
    passes = sum(1 for r in results if r["verdict"] == "PASS")
    print(f"\nSummary: {passes} pass, {failures} fail, {len(results) - passes - failures} other")
    print(f"Report: {args.report}")
    sys.exit(0 if failures == 0 else 1)


if __name__ == "__main__":
    main()
