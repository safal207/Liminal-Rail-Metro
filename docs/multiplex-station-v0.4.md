# Multiplex Station v0.4

v0.4 tests whether Liminal Rail can carry multiple in-flight control decisions without relying on response order.

## Wire contract

Each request wraps the existing `lifetra.station.request.v0.1` payload:

```json
{
  "protocol": "lifetra.station.request-envelope.v0.2",
  "request_id": "request-123",
  "request": { "protocol": "lifetra.station.request.v0.1" }
}
```

The Rust station echoes the correlation identity independently of the Lifetra decision identity:

```json
{
  "protocol": "lifetra.station.response-envelope.v0.2",
  "request_id": "request-123",
  "decision": { "protocol": "lifetra.decision.v0.1" }
}
```

Response order is explicitly **not** part of the protocol. Go must bind each response to the pending request with the same `request_id`, then still validate the inner observation/action/receipt binding before exposing the decision.

## Concurrency model

A Rust station owns a bounded ingress queue and 1..64 worker threads. Worker completion may be out of order. All stdout writes pass through one writer thread, so NDJSON lines cannot interleave.

Go owns:

- a single reader loop per persistent Rust PID;
- a `pending[request_id]` correlation table;
- lifetime-unique request IDs per station process;
- late-response suppression for callers that already timed out;
- fail-closed handling for unknown or unbound response IDs;
- optional round-robin pooling across multiple persistent Rust PIDs.

## Benchmark matrix

The CI benchmark uses a release Rust binary and runs:

- pool sizes: `1`, `4` persistent Rust processes;
- in-flight concurrency: `1`, `4`, `16`, `64`;
- fixed request count per scenario;
- p50 / p95 / p99 / min / max request latency;
- observed requests per second;
- completed-without-error count;
- distinct persistent PIDs.

## Invariants

1. `request_id` is transport correlation only. It cannot replace `action_id`.
2. A correctly correlated response can still fail inner Lifetra/Metro binding validation.
3. Reusing a request ID within one station lifetime is rejected.
4. An unknown response ID is a protocol fault, not a best-effort guess.
5. Timeout of one pure control request does not silently map a later response onto another caller.
6. `ALLOW`, `BLOCK`, `REQUIRE_APPROVAL`, receipt provenance, and `UNKNOWN` semantics remain unchanged from v0.3.
7. Station pooling changes capacity, not authority.

## Claim ceiling

This benchmark measures a local Go ↔ Rust NDJSON **control plane** on one CI runner. It does not measure LLM inference, remote networking, browser/tool execution, distributed scheduling, or end-to-end agent task latency. Derived throughput is not a claim about AI agents per second.
