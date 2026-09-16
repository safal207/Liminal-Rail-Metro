# Persistent Lifetra Station v0.3

v0.3 removes per-decision Rust process startup from the Liminal Rail control path.

The goal is deliberately narrow:

> Can one Go Metro process reuse one long-lived Lifetra Rust process for many sequential, proof-bound control decisions while preserving the v0.2 fail-closed boundary?

## Data path

```text
Go Metro
   |
   | NDJSON request #1
   v
+--------------------------+
| Persistent Lifetra Rust  |
| station (same PID)       |
+--------------------------+
   |
   | NDJSON decision #1
   v
Go validates binding
   |
   | NDJSON request #2
   v
same Rust PID
```

Each request remains `lifetra.station.request.v0.1`. Each valid response remains `lifetra.decision.v0.1`.

Per-request input errors use `lifetra.station.error.v0.1`. An error line does not by itself terminate the station, so a later valid request can still be evaluated by the same process.

## What stays unchanged

Persistence changes transport lifetime, not Lifetra's decision semantics.

The Rust station still evaluates the existing chain:

```text
TrajectoryBead
  -> ProvenOrientation
  -> OrientationDelta
  -> CorrectionPolicy
  -> DecisionAuthority
```

The Go side still treats every response as untrusted until these bindings are checked:

- `source_observation_id` matches the sent observation;
- `caused_by_action_id` matches the prior Metro action;
- `source_receipt_ref` matches the observed Metro receipt;
- only `ALLOW` can contain dispatch authority and a next action;
- `ALLOW` must still pass `DecisionToPacket`;
- a new logical action cannot silently reuse the prior `action_id`.

## Failure boundary

v0.3 is sequential by design. `PersistentProcess` serializes callers instead of multiplexing multiple in-flight requests over one stdout stream.

A request timeout is fail-closed:

1. the process is marked closed;
2. stdin is closed;
3. the child is killed;
4. the child is reaped with `Wait()`;
5. that process instance is never reused.

No timeout is interpreted as an execution result.

## Benchmark

`cmd/persistent-station-bench` measures two different costs instead of blending them:

- **process start / first response** — includes startup before the first successful decision;
- **warm station round trip** — repeated sequential `Evaluate` calls through the same Rust PID after warmup.

Reported warm metrics:

- average latency;
- p50 latency;
- p95 latency;
- min / max latency;
- derived sequential decisions per second;
- first-response / warm-average ratio.

The benchmark prepares each request before its timer starts, so the warm latency is focused on the local Go -> NDJSON -> Rust decision -> NDJSON -> Go validation round trip.

## Claim ceiling

v0.3 does **not** measure:

- LLM inference latency;
- network transport between machines or regions;
- parallel station throughput;
- database-backed Lifetra recovery paths;
- queue or broker performance;
- arbitrary agent-framework speed;
- end-to-end user task completion time.

The bounded claim is only:

> On one runner, a single Go process can reuse one Lifetra Rust process across multiple sequential control decisions, and the warm local-process round-trip can be measured independently from process startup.

## Next bead

Only after this proof is stable should Metro test multiple concurrent callers. That will require an explicit correlation / multiplexing protocol rather than assuming line order remains safe under concurrency.
