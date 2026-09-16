# Liminal Rail Metro

**High-speed execution and routing protocol for AI agents.**

> Agents think. Liminal Rail moves.

Liminal Rail Metro is an experimental open protocol and Go engine for moving bounded AI-agent actions across specialized agents and tools with explicit routing, stable action identity, and verifiable execution receipts.

The project starts from one narrow question:

> Can one agent hand off one bounded action to another agent through a router, without re-sending unnecessary context, while preserving enough identity and evidence to verify what actually executed?

## Engine

**Go is the primary runtime for Liminal Rail Metro.**

The engine is intentionally designed around Go's strengths for this problem: lightweight concurrency, networking, predictable deployment, small binaries, and a strong standard library.

Current layout:

```text
cmd/metro-demo/                 executable Metro proof
cmd/lifetra-bridge-demo/        typed bridge proof
cmd/rail-loop-demo/             real Go -> Rust -> Go loop
cmd/persistent-station-bench/   warm-path station benchmark
internal/metro/                 Go protocol engine
internal/lifetrabridge/         proof/control contracts
internal/lifetrastation/        one-shot + persistent station boundaries
protocol/                       JSON protocol schemas
examples/                       protocol journeys
docs/                           architecture and claim ceilings
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

A successful proof ends with:

```json
{
  "cross_runtime_loop": "PASS",
  "path": "Go -> Rust -> Go",
  "final_target": "qa-agent"
}
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

The station uses one NDJSON request and one NDJSON response per line. The Go client deliberately serializes requests in v0.3; request correlation and concurrent multiplexing are a later protocol bead.

Persistent-station CI pins Lifetra to merge commit `61b9d7ecdb59cea5a7b89675fdf5bd8c49bd6b55` and builds the Rust station as a release binary before measuring warm local-process round trips.

Run the benchmark against a prebuilt Lifetra station:

```bash
go run ./cmd/persistent-station-bench \
  -station-bin ../Lifetra/target/release/examples/metro_station_server \
  -iterations 200
```

The benchmark reports process startup separately from warm average, p50, p95, min/max and derived sequential decisions per second. It does **not** claim LLM, network, or distributed-agent speed.

## Core artifacts

- `protocol/metro.packet.v0.1.json` — bounded action envelope
- `protocol/metro.route.v0.1.json` — route decision envelope
- `protocol/metro.receipt.v0.1.json` — execution evidence envelope
- `protocol/lifetra.observation.v0.1.json` — Metro receipt -> Lifetra observation contract
- `protocol/lifetra.station.request.v0.1.json` — control-station request contract
- `protocol/lifetra.decision.v0.1.json` — Lifetra authority decision -> Metro packet contract
- `examples/research-code-qa.json` — minimal Metro journey
- `examples/lifetra-control-loop.json` — proof/control-loop bridge example
- `internal/metro/metro.go` — Go engine core
- `internal/lifetrabridge/bridge.go` — Go bridge adapter
- `internal/lifetrabridge/station.go` — station request types and validation
- `internal/lifetrastation/process.go` — one-shot external process adapter
- `internal/lifetrastation/persistent.go` — persistent NDJSON process adapter
- `internal/lifetrastation/persistent_test.go` — process reuse and fail-closed tests
- `cmd/rail-loop-demo/main.go` — executable Go -> Rust -> Go proof
- `cmd/persistent-station-bench/main.go` — persistent station benchmark
- `docs/e2e-rail-loop.md` — cross-runtime v0.2 proof
- `docs/persistent-station-v0.3.md` — persistent boundary and benchmark claim ceiling

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

## Non-goals

This repository does **not** yet claim:

- exactly-once execution across arbitrary distributed systems;
- cryptographic trust between independent organizations;
- production-grade scheduling, billing, auth, or service discovery;
- benchmark superiority over existing queues, buses, or agent frameworks;
- autonomous safety for high-risk actions;
- safe redispatch after an `UNKNOWN` external effect;
- durable orchestration across process crashes;
- production RPC between Metro and Lifetra;
- parallel persistent-station throughput.

The current milestone is to make the handoff, evidence, control, and cross-runtime boundary explicit, small, independently testable, and measurable without conflating process startup with warm decisions.

## Run the demos

Metro core:

```bash
go run ./cmd/metro-demo
```

Typed Lifetra bridge:

```bash
go run ./cmd/lifetra-bridge-demo
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

Run Go invariant tests:

```bash
go test ./...
```

## Status

`v0.3` — persistent Lifetra station proof in progress on top of the CI-verified v0.2 Go -> Rust -> Go loop.

Contributions should preserve the narrow claim ceiling: make the protocol more independently verifiable before making it more ambitious.

## License

MIT
