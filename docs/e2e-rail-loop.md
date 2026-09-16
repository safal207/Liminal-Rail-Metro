# E2E Rail Loop v0.2

This proof connects the Go Metro hot path to the Rust Lifetra control path and back again through JSON over stdin/stdout.

```text
Go Metro packet
  -> Go route
  -> bounded Go execution
  -> Metro receipt
  -> Lifetra observation
  -> Rust Lifetra station
  -> Lifetra decision
  -> Go validates decision binding
  -> ALLOW only
  -> next Metro packet
  -> next Go route
```

## Boundary

The Go process launches Lifetra as an external station process:

```bash
cargo run --quiet --example metro_station
```

The station receives `lifetra.station.request.v0.1` on stdin and must emit exactly one `lifetra.decision.v0.1` JSON value on stdout.

Go verifies that the returned decision is still bound to the request:

- `source_observation_id` matches the observation;
- `caused_by_action_id` matches the prior Metro action;
- `source_receipt_ref` matches the Metro receipt;
- only `ALLOW` may carry authority proof plus `next_action`;
- `BLOCK` and `REQUIRE_APPROVAL` must not carry an executable action;
- the next logical action cannot silently reuse the prior `action_id`.

This means the Rust process is a control authority, not an unchecked code generator.

## Run locally

Keep both repositories next to each other:

```text
workspace/
  Liminal-Rail-Metro/
  Lifetra/
```

Then from `Liminal-Rail-Metro`:

```bash
go test ./...
go run ./cmd/rail-loop-demo -lifetra-dir ../Lifetra
```

Or set:

```bash
export LIFETRA_REPO=/path/to/Lifetra
go run ./cmd/rail-loop-demo
```

A successful run emits JSON containing:

```json
{
  "proof": {
    "cross_runtime_loop": "PASS",
    "path": "Go -> Rust -> Go",
    "final_target": "qa-agent"
  }
}
```

## CI proof

The E2E workflow pins the Lifetra side to merge commit:

```text
80fc633e00c863aeb6505f008c43840ca6445579
```

That pin contains the proof-backed Metro control station merged from Lifetra PR #7. Pinning prevents a moving Lifetra branch from changing the meaning of the cross-runtime proof silently.

## Claim ceiling

v0.2 proves one local cross-process control loop. It does not yet claim:

- distributed exactly-once execution;
- network transport between independent hosts;
- production service discovery or auth;
- durable orchestration across process crashes;
- automatic retry after an `UNKNOWN` side effect;
- latency or throughput superiority over other agent runtimes.

The next performance milestone should measure the cost of this boundary before replacing stdin/stdout with a long-lived RPC transport.
