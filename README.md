# Liminal Rail Metro

**High-speed execution and routing protocol for AI agents.**

> Agents think. Liminal Rail moves.

Liminal Rail Metro is an experimental open protocol and Go engine for moving bounded AI-agent actions across specialized agents and tools with explicit routing, stable action identity, bounded admission, and verifiable execution receipts.

The project starts from one narrow question:

> Can one agent hand off one bounded action to another agent through a router, without re-sending unnecessary context, while preserving enough identity and evidence to verify what actually executed?

## Engine

**Go is the primary runtime for Liminal Rail Metro.**

The engine is intentionally designed around Go's strengths for this problem: lightweight concurrency, networking, predictable deployment, small binaries, and a strong standard library.

Current layout:

```text
cmd/metro-demo/                  executable Metro proof
cmd/lifetra-bridge-demo/         typed bridge proof
cmd/rail-loop-demo/              real Go -> Rust -> Go loop
cmd/persistent-station-bench/    warm-path station benchmark
cmd/multiplex-station-bench/     concurrent request-correlation benchmark
cmd/adaptive-routing-bench/      admission/backpressure benchmark
internal/metro/                  Go protocol engine
internal/lifetrabridge/          proof/control contracts
internal/lifetrastation/         process, multiplex, pool and adaptive routing boundaries
protocol/                        JSON protocol schemas
examples/                        protocol journeys
docs/                            architecture and claim ceilings
```

## v0.1 protocol seed

The first version intentionally stays small:

1. A **packet** describes one bounded action.
2. A **route decision** selects the next execution target from allowed targets.
3. The target executes the action.
4. A **receipt** records what executed and binds the result back to the original action.
5. An optional **Lifetra bridge** turns execution evidence into an observation and only an authorized Lifetra decision back into a new Metro packet.

```text
Research Agent
      |
      v
+-------------+
| Metro Gate  |
+-------------+
      |
      v
+-------------+
| Fast Router |
+-------------+
      |
      v
+-------------+
| Code Agent  |
+-------------+
      |
      v
Execution Receipt
      |
      v
+------------------+
| Lifetra Control  |
+------------------+
      |
      v
Next Metro Packet
```

## v0.2 E2E Rail Loop

v0.2 adds the first real cross-runtime control loop:

```text
Go Metro
  -> packet
  -> route
  -> bounded execution
  -> receipt
  -> observation
  -> Rust Lifetra Station
  -> DecisionAuthority
  -> decision JSON
  -> Go binding validation
  -> ALLOW only
  -> next Metro packet
  -> next Metro route
```

The Rust side runs as an external process over JSON stdin/stdout. Go does not blindly accept its output: the returned decision must remain bound to the same observation, prior action, and receipt. `BLOCK` and `REQUIRE_APPROVAL` are not dispatch authority and cannot carry an executable next action through this boundary.

Run it with a Lifetra checkout:

```bash
go run ./cmd/rail-loop-demo -lifetra-dir ../Lifetra
```

v0.2 CI pins the Lifetra station to merge commit `80fc633e00c863aeb6505f008c43840ca6445579` so the proof cannot silently change when Lifetra evolves.

## v0.3 Persistent Lifetra Station

v0.3 removes per-decision Rust process startup from the control path.

```text
Go Metro
   |
   | request #1
   v
+---------------------------+
| persistent Lifetra / Rust |
| same PID                  |
+---------------------------+
   |
   | decision #1
   v
Go validates binding
   |
   | request #2
   v
same Rust PID
```

The station uses one NDJSON request and one NDJSON response per line. The Go client deliberately serializes requests in v0.3.

Persistent-station CI pins Lifetra to merge commit `61b9d7ecdb59cea5a7b89675fdf5bd8c49bd6b55` and builds the Rust station as a release binary before measuring warm local-process round trips.

One CI run over 200 sequential warm decisions produced:

```text
process start       498 us
first response     1319 us
warm average       85.65 us
warm p50              83 us
warm p95             105 us
warm min              61 us
warm max             159 us
derived sequential rate ~11,675 decisions/s
same Rust PID      200/200 decisions
```

These numbers describe only the local deterministic Go -> NDJSON -> Lifetra Rust control decision -> NDJSON -> Go validation boundary. They are **not** LLM inference, network, distributed-agent, or end-to-end task throughput measurements.

## v0.4 Request-correlated multiplex rail

v0.4 allows multiple requests to be in flight at the same time without making response order part of the contract.

```text
req-101 ---+
req-102 ---+--> persistent Lifetra workers
req-103 ---+              |
                         responses may finish out of order
                              |
                              v
                       request_id correlation
                              |
                              v
                       original Go caller
```

`request_id` is transport correlation, not execution identity. The inner `action_id`, observation, receipt and Lifetra authority bindings are still validated after correlation.

Key boundaries:

- one stdout reader dispatches responses through a pending-request table;
- duplicate request IDs are rejected for the lifetime of both the Go client and Rust station;
- an unknown or unbound response ID fails the station closed;
- late responses for caller-abandoned control requests cannot be rebound to another request;
- a pool can spread traffic across multiple persistent Rust PIDs;
- `go test -race ./internal/lifetrastation` is a required CI gate.

Lifetra multiplex support is pinned to merge commit `b8f1ff5ba4de78d3ab618c0886c5b528d40be15e`.

The v0.4 matrix demonstrated that increasing concurrency raises throughput until queue pressure dominates tail latency. It motivated the next bead: explicit admission instead of letting internal queues grow without a Metro-level bound.

## v0.5 Adaptive backpressure and load-aware routing

v0.5 adds a separate `AdaptivePool`; the v0.4 round-robin pool stays unchanged as a control path.

```text
caller
  |
  v
Adaptive admission
  |
  +-- all stations at cap --> ErrBackpressure (no dispatch)
  |
  v
score available stations
(in_flight + 1) * EWMA service latency
  |
  v
selected persistent Lifetra station
  |
  v
request-correlated decision
```

Important semantics:

- each station has a hard `MaxInFlightPerStation` cap;
- backpressure happens **before dispatch**, so the rejected attempt does not consume `request_id` and may be retried later;
- once admitted, `request_id` becomes lifetime-unique and an execution error is **not** automatically rerouted or retried;
- every station receives one cold exploration sample before measured latency affects routing;
- snapshots expose in-flight count, completed/error counts, EWMA service latency and estimated delay;
- an integration test uses two real child processes (one deliberately +5 ms slower) and verifies that after cold exploration subsequent sequential work is routed to the faster station;
- unit tests and the Go race detector pass on the same v0.5 head.

### Bounded-load proof

One CI run used four persistent Rust PIDs, 16 Rust workers per PID, 64 concurrent Go callers and 1,024 completed requests per scenario:

| Per-station cap | Total capacity | Backpressure events | Service p50 | Service p95 | E2E p95 | Throughput |
| ---: | ---: | ---: | ---: | ---: | ---: | ---: |
| 2 | 8 | 6,260 | 439 us | 1.134 ms | 18.715 ms | 15.3k/s |
| 8 | 32 | 1,122 | 1.277 ms | 2.737 ms | 9.884 ms | 22.6k/s |
| 16 | 64 | 0 | 2.264 ms | 5.470 ms | 5.471 ms | 23.4k/s |

Every scenario completed `1024/1024` without an execution error. Observed per-station in-flight counts never exceeded the configured cap: `2`, `8`, and `16` respectively.

The important result is **not** that a smaller cap is universally faster. A tight cap protects the internal station service time but transfers waiting to callers. In this workload, cap `2` kept admitted service p50 low while producing a much larger end-to-end tail through repeated backpressure. Cap `16` admitted the full 64-caller workload with no backpressure, but allowed higher internal service latency.

So v0.5 establishes a measurable control knob:

> Backpressure bounds internal queue exposure; choosing the right bound is a latency/throughput policy decision, not a free speedup.

The benchmark intentionally reports service latency separately from end-to-end latency so queueing cannot be hidden by moving it outside the station.

## Core artifacts

- `protocol/metro.packet.v0.1.json` — bounded action envelope
- `protocol/metro.route.v0.1.json` — route decision envelope
- `protocol/metro.receipt.v0.1.json` — execution evidence envelope
- `protocol/lifetra.observation.v0.1.json` — Metro receipt -> Lifetra observation contract
- `protocol/lifetra.station.request.v0.1.json` — control-station request contract
- `protocol/lifetra.decision.v0.1.json` — Lifetra authority decision -> Metro packet contract
- `protocol/lifetra.station.request-envelope.v0.2.json` — request correlation envelope
- `protocol/lifetra.station.response-envelope.v0.2.json` — correlated decision envelope
- `protocol/lifetra.station.error.v0.2.json` — correlated station error envelope
- `internal/metro/metro.go` — Go engine core
- `internal/lifetrabridge/bridge.go` — Go bridge adapter
- `internal/lifetrastation/process.go` — one-shot external process adapter
- `internal/lifetrastation/persistent.go` — persistent sequential NDJSON adapter
- `internal/lifetrastation/multiplex.go` — concurrent request-correlated process and pool
- `internal/lifetrastation/adaptive.go` — bounded admission and EWMA-informed routing
- `cmd/persistent-station-bench/main.go` — v0.3 benchmark
- `cmd/multiplex-station-bench/main.go` — v0.4 multiplex benchmark
- `cmd/adaptive-routing-bench/main.go` — v0.5 admission/backpressure benchmark

## Lifetra bridge

Metro stays on the hot path; Lifetra stays on the reflective/control path.

```text
Metro Receipt
    -> Lifetra Observation
    -> bead / trajectory / authority
    -> Lifetra Decision
    -> next Metro Packet
```

The bridge preserves `UNKNOWN`, rejects `BLOCK` and `REQUIRE_APPROVAL` as dispatch authority, carries receipt/authority provenance forward, and rejects silent reuse of the prior action identity for a new logical action.

> Metro moves. Lifetra remembers why.

## Design principles

### 1. Stable action identity
Every action receives an `action_id` before dispatch. Retries and recovery must refer to the same logical action rather than silently creating a new one.

### 2. Minimal context movement
Packets should carry only the context required for the next bounded action. Large histories should be referenced, not copied by default.

### 3. Routing is separate from reasoning
A fast decision layer may choose among already-valid actions or destinations. Deep planning remains the job of a larger reasoning model when needed.

### 4. Execution must produce evidence
A route decision is not proof of execution. A receipt binds the action, selected target, inputs, and result evidence.

### 5. Fail closed on uncertainty
If execution identity or completion is uncertain, the system should surface `UNKNOWN` rather than claim success or blindly redispatch a side effect.

### 6. Authority is separate from execution
A Lifetra `ALLOW` decision can authorize creation of a new Metro packet, but permission is not evidence that the packet was dispatched or that its external effect completed.

### 7. Cross-runtime output is untrusted until rebound
A station decision must be checked against the observation, action identity, and receipt that caused it before Go can create the next packet.

### 8. Persistence must not weaken the proof boundary
Keeping Rust alive is a transport optimization only. Each returned decision is validated exactly as if the station had been started for one request.

### 9. Backpressure must happen before ambiguous execution
A saturated control plane may reject an action before dispatch. Once dispatch may have occurred, timeout or failure cannot be treated as permission to send the logical action elsewhere.

## Non-goals

This repository does **not** yet claim:

- exactly-once execution across arbitrary distributed systems;
- cryptographic trust between independent organizations;
- production-grade scheduling, billing, auth, service discovery or autoscaling;
- benchmark superiority over existing queues, buses, RPC systems or agent frameworks;
- autonomous safety for high-risk actions;
- safe redispatch after an `UNKNOWN` external effect;
- durable orchestration across Metro process crashes;
- production RPC between Metro and Lifetra;
- that one admission cap is optimal across workloads;
- that local control-plane throughput equals AI-agent or LLM throughput.

## Run the demos and proofs

Metro core:

```bash
go run ./cmd/metro-demo
```

Real Go -> Rust -> Go loop:

```bash
go run ./cmd/rail-loop-demo -lifetra-dir ../Lifetra
```

Persistent station benchmark:

```bash
go run ./cmd/persistent-station-bench \
  -station-bin ../Lifetra/target/release/examples/metro_station_server \
  -iterations 200
```

Multiplex benchmark:

```bash
go run ./cmd/multiplex-station-bench \
  -station-bin ../Lifetra/target/release/examples/metro_station_mux
```

Adaptive admission benchmark:

```bash
go run ./cmd/adaptive-routing-bench \
  -station-bin ../Lifetra/target/release/examples/metro_station_mux \
  -pool 4 -concurrency 64 -limits 2,8,16
```

Run Go invariants and race checks:

```bash
go test ./...
go test -race ./internal/lifetrastation
```

## Status

`v0.5` — CI-verified request-correlated multiplex transport with race-checked bounded admission, explicit backpressure, EWMA-informed station routing, real slow/fast station avoidance, and separate service versus end-to-end latency evidence.

Contributions should preserve the narrow claim ceiling: make the protocol more independently verifiable before making it more ambitious.

## License

MIT
