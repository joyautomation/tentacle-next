# PrimaryHostTest: Monitor flags TCK-injected bad-timestamp STATE message

## Summary

`Monitor:host-topic-phid-birth-payload` always FAILs in `edge/PrimaryHostTest`, even when the implementation under test (the Edge Node) behaves correctly. The Monitor's global STATE handler validates every BIRTH (online=true) STATE payload it observes on the broker, but `PrimaryHostTest` deliberately publishes a STATE message with a hardcoded bad timestamp (16671135 ms — ~4.6 hours since epoch) to exercise the edge node's `wrongTimestamp` branch. The Monitor catches that injection and marks the assertion FAIL, with no consideration that the message originated from the test's own `HostApplication` utility rather than the implementation under test.

## Affected versions

Reproduced against `sparkplug-tck-3.0.0.jar` (Eclipse Sparkplug TCK 3.0.0) running as a HiveMQ Community Edition extension.

## How to reproduce

1. Build and install the TCK extension into HiveMQ.
2. Run a fully spec-compliant Sparkplug B Edge Node configured with a Primary Host Application.
3. Drive the TCK to run `edge/PrimaryHostTest`.
4. Observe `Monitor:host-topic-phid-birth-payload: FAIL` in the result summary.

Broker log shows the offending STATE message published by the TCK's own utility client:

```
Monitor: clientid Sparkplug_TCK_MantleHost *** STATE *** MantleHost {"online":true,"timestamp":16671135}
TCKTest log: Monitor: Test failed for assertion host-topic-phid-birth-payload: host id: MantleHost with timestamp=-1
```

The `Sparkplug_TCK_*` clientId belongs to the TCK utility, not the implementation. The `timestamp=-1` in the log is a parsing-error sentinel — the JSON timestamp `16671135` falls outside `Utils.checkUTC(ts, UTCwindow=60s)`, so the Monitor records `-1` for "no valid timestamp seen for this host".

## Root cause

`org.eclipse.sparkplug.tck.utility.HostApplication`:

```java
private byte[] getOldMessage(boolean online) {
    // ...
    StatePayload p = new StatePayload(online, 16671135L);  // hardcoded bad timestamp
    return new ObjectMapper().writeValueAsString(p).getBytes();
}

public void hostSendOldOnline()  { send(getOldMessage(true)); }
public void hostSendOldOffline() { send(getOldMessage(false)); }
```

`PrimaryHostTest.wrongTimestamp()` schedules a sequence that ultimately publishes via `hostSendOldOnline()`. The Monitor's STATE handler (`org.eclipse.sparkplug.tck.test.Monitor`) is subscribed broker-wide and runs `setResultIfNotFail("host-topic-phid-birth-payload", ts.isLong() && Utils.checkUTC(ts, UTCwindow))` on every STATE BIRTH payload, including the test's own intentionally-bad injection. Once it marks FAIL, no later valid STATE message can recover it.

## Why this can't be worked around by the implementation

The Edge Node under test never publishes the bad STATE message — the TCK does. The implementation handles `wrongTimestamp` correctly per the spec (it ignores the older STATE), and the rest of the test's per-test assertions PASS. Only the cross-cutting Monitor assertion fails, because the Monitor doesn't distinguish "STATE message from impl-under-test" from "STATE message from TCK utility client".

## Suggested fix (any of):

1. **Scope the Monitor check to the implementation's clientId.** `PrimaryHostTest` already passes a `hostApplicationId` and uses a known `Sparkplug_TCK_*` clientId for its own utility — the Monitor could ignore STATE messages with that prefix.
2. **Suspend the assertion during PrimaryHostTest's `wrongTimestamp` phase.** The test already manages a `Constants$TestStatus` state machine; expose a "monitor pause/resume" hook when transitioning into `HOST_WRONG_TIMESTAMP`.
3. **Don't reuse `host-topic-phid-birth-payload` in this test.** That assertion is for *the impl's host application's BIRTH payload* — not for arbitrary STATE traffic the TCK injects.

## Impact

This makes a 100% pass result on `edge/PrimaryHostTest` impossible for any conforming implementation. We're currently filtering this test out of CI with a documented `skip_reason`, but downstream CI matrices that depend on a clean TCK run can't treat the result as authoritative until this is fixed upstream.

Happy to put up a PR if there's interest in option (1) or (3).
