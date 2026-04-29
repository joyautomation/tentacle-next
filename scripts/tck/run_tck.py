#!/usr/bin/env python3
"""
Eclipse Sparkplug TCK driver.

Drives the TCK (a HiveMQ extension) over its MQTT control protocol so the
tests can run headlessly in CI.

Protocol (verified against eclipse-sparkplug/sparkplug master at the time of
writing — see scripts/tck/README.md):

  Control (publish):
    SPARKPLUG_TCK/RESULT_CONFIG  ->  "NEW_RESULT-LOG <filename>"
    SPARKPLUG_TCK/CONFIG         ->  "UTCwindow <ms>"
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
REPLY_TOPIC = "SPARKPLUG_TCK/CONSOLE_REPLY"

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
        # When set, every CONSOLE_PROMPT received triggers a CONSOLE_REPLY
        # publish with this payload. Used by tests that gate their state
        # machine on operator confirmation (e.g. EdgeSessionTerminationTest).
        self._auto_reply: str | None = None

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
            # Auto-respond to operator prompts when enabled. Tests like
            # EdgeSessionTerminationTest publish CONSOLE_PROMPT messages
            # asking a human to confirm host behavior; without a reply
            # they hang. We simply echo PASS so the per-assertion
            # setResultIfNotFail call uses the host-observed verdict.
            if msg.topic == PROMPT_TOPIC and self._auto_reply is not None:
                try:
                    self.client.publish(REPLY_TOPIC, self._auto_reply, qos=1)
                except Exception:
                    pass

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

    def configure_results(self, log_filename: str, utc_window_ms: int = 60_000):
        # The TCK's Utils.checkUTC compares now-vs-timestamp in MILLISECONDS
        # against this value (Date.getTime() - timestamp in ms <= UTCwindow).
        # Sending "60" gave a 60ms tolerance and failed every STATE BIRTH/WILL
        # parse on a busy CI runner. Default 60000ms matches the spec's
        # informal "fresh timestamp" expectation and Results.java's hardcoded
        # default (Results.Config.UTCwindow = 60000L).
        self._publish(RESULT_CFG_TOPIC, f"NEW_RESULT-LOG {log_filename}")
        self._publish(CONFIG_TOPIC, f"UTCwindow {utc_window_ms}")

    def run_test(
        self,
        profile: str,
        test_type: str,
        params: list[str],
        observe_seconds: float,
        result_timeout: float,
        impl_cmd: str | None = None,
        impl_log: str = "impl.log",
        partner_cmd: str | None = None,
        partner_log: str = "partner.log",
        fixture_cmd: str | None = None,
        fixture_log: str = "fixture.log",
        auto_reply: str | None = None,
        impl_first: bool = False,
        impl_first_settle: float = 5.0,
    ) -> dict:
        """
        Run one TCK test:
          1. publish NEW_TEST
          2. (optional) start impl-under-test, then a partner co-process
             (e.g. an edge node alongside a host impl) and a fixture
             (NATS data pump). Each is launched with a short stagger.
          3. wait observe_seconds while the impl interacts with the TCK
          4. stop fixture, partner, impl in reverse order
          5. publish END_TEST
          6. wait up to `result_timeout` for the multi-line RESULT summary
          7. parse per-assertion verdicts
        """
        # Drain any leftover RESULTs from a previous test
        while not self._results.empty():
            try: self._results.get_nowait()
            except queue.Empty: break

        self._auto_reply = auto_reply

        def launch(label: str, cmd: str, log_path: str):
            print(f">> launching {label}: {cmd}", flush=True)
            return subprocess.Popen(
                shlex.split(cmd),
                stdout=open(log_path, "wb"),
                stderr=subprocess.STDOUT,
                start_new_session=True,
            )

        # Some tests (e.g. EdgeSessionTerminationTest) call
        # Utils.checkHostApplicationIsOnline at construction and throw
        # IllegalStateException if the host isn't already STATE-online —
        # which it can't be if NEW_TEST starts the impl. For those tests
        # we launch the impl first, give it time to publish BIRTH, and
        # only then send NEW_TEST.
        impl_proc = None
        if impl_first and impl_cmd:
            impl_proc = launch("impl", impl_cmd, impl_log)
            time.sleep(impl_first_settle)

        params_str = " ".join(params)
        new_test = f"NEW_TEST {profile} {test_type} {params_str}".strip()
        print(f">> {new_test}", flush=True)
        self._publish(CONTROL_TOPIC, new_test)

        if impl_proc is None and impl_cmd:
            impl_proc = launch("impl", impl_cmd, impl_log)

        partner_proc = None
        if partner_cmd:
            # Stagger: impl finishes its first connect/NBIRTH (or host BIRTH)
            # before the partner starts publishing.
            time.sleep(2)
            partner_proc = launch("partner", partner_cmd, partner_log)

        fixture_proc = None
        if fixture_cmd:
            # Stagger again: partner edge node should be subscribed/birthed
            # before the fixture pumps DBIRTH-eligible NATS data.
            time.sleep(2)
            fixture_proc = launch("fixture", fixture_cmd, fixture_log)

        time.sleep(observe_seconds)

        for proc, label in (
            (fixture_proc, "fixture"),
            (partner_proc, "partner"),
            (impl_proc, "impl"),
        ):
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

        self._auto_reply = None

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
        "--partner-cmd",
        default=os.environ.get("TCK_PARTNER_CMD"),
        help="optional co-process launched alongside the impl (e.g. an edge "
             "node when running the host profile). Started 2s after the impl "
             "and stopped before it.",
    )
    ap.add_argument(
        "--partner-log",
        default=os.environ.get("TCK_PARTNER_LOG", "partner.log"),
        help="file to capture partner stdout+stderr",
    )
    ap.add_argument(
        "--fixture-cmd",
        default=os.environ.get("TCK_FIXTURE_CMD"),
        help="optional fixture process launched after the impl (and partner). "
             "For the edge profile, use cmd/tck-fixture which publishes synthetic "
             "gateway data to NATS so the bridge emits DBIRTH/DDATA. Without it, "
             "~20 device-related TCK assertions report NOT EXECUTED.",
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
        # Per-test overrides:
        #   no_partner: skip the global --partner-cmd for this test (e.g.
        #     EdgeSessionTerminationTest must run against the TCK's own
        #     edge utility — a real partner makes hasDevice() return true
        #     and disables the test's auto-kill scheduler).
        #   no_fixture: skip the global --fixture-cmd for this test.
        #   auto_reply: payload to publish on SPARKPLUG_TCK/CONSOLE_REPLY
        #     for every CONSOLE_PROMPT received during the test.
        partner_cmd = None if test.get("no_partner") else args.partner_cmd
        fixture_cmd = None if test.get("no_fixture") else args.fixture_cmd
        auto_reply = test.get("auto_reply")
        impl_first = bool(test.get("impl_first"))
        print(f"\n=== {args.profile}/{name} ===")
        r = driver.run_test(
            args.profile, name, params, observe, result_to,
            impl_cmd=args.impl_cmd, impl_log=args.impl_log,
            partner_cmd=partner_cmd, partner_log=args.partner_log,
            fixture_cmd=fixture_cmd, fixture_log=args.fixture_log,
            auto_reply=auto_reply,
            impl_first=impl_first,
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
