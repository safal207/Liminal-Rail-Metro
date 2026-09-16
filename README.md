# Liminal Rail Metro

**High-speed execution and routing protocol for AI agents.**

> Agents think. Liminal Rail moves.

Liminal Rail Metro is an experimental open protocol and Go engine for moving bounded AI-agent actions across specialized agents and tools with explicit semantic choice, traffic control, stable action identity, and verifiable execution receipts.

The project starts from one narrow question:

> Can one agent hand off one bounded action to another agent quickly, without re-sending unnecessary context, while preserving enough identity and evidence to know what was chosen, what was authorized, and what actually executed?

## Current architecture

```text
Deep planner / bounded Metro Packet
              |
              v
+-----------------------------+
| Fast Decision Plane / Go    |
| bounded typed choices       |
| confidence + probability    |
+-------------+---------------+
              |
       AUTO_ROUTE only
              v
+-----------------------------+
| Adaptive Metro / Go         |
| backpressure + EWMA routing |
+-------------+---------------+
              |
              v
+-----------------------------+
| Lifetra Station / Rust      |
| trajectory + authority      |
+-------------+---------------+
              |
              v
        Execution Receipt
```

The layers deliberately have different jobs:

- **Fast Decision Plane** — chooses among already-bounded semantic options; it cannot invent authority.
- **Metro transport/traffic layer** — correlates concurrent requests, applies backpressure, and selects an available station.
- **Lifetra** — carries trajectory/proof semantics and authority decisions.
- **Receipt layer** — records what actually executed; a route or permission is not execution proof.

## Engine

**Go is the primary runtime for Liminal Rail Metro.**

Go owns the hot transport/decision path: lightweight concurrency, process/network boundaries, traffic admission, routing, validation, and small deployable binaries. Lifetra remains a separate Rust reflective/control layer.

Current layout:

```text
cmd/metro-demo/                  Metro protocol proof
cmd/lifetra-bridge-demo/         typed Lifetra bridge proof
cmd/rail-loop-demo/              real Go -> Rust -> Go loop
cmd/persistent-station-bench/    v0.3 warm-path benchmark
cmd/multiplex-station-bench/     v0.4 correlation/concurrency benchmark
cmd/adaptive-routing-bench/      v0.5 backpressure benchmark
cmd/fast-decision-demo/          v0.6 semantic-choice proof
cmd/fast-decision-bench/         v0.6 local gate benchmark
internal/metro/                  packet/route/receipt engine
internal/decisionplane/          bounded System-One-shaped provider/gate
internal/lifetrabridge/          proof/control contracts
internal/lifetrastation/         process, multiplex, pool and adaptive routing
protocol/                        JSON protocol schemas
docs/                            architecture and claim ceilings
```

## v0.1 — Protocol seed

The first version established three boundaries:

1. **Packet** — one bounded action.
2. **Route** — a target selected only from allowed targets.
3. **Receipt** — evidence bound back to the original action.

```text
Packet -> Route -> Execute -> Receipt
```

Every logical action receives an `action_id` before dispatch. Completion uncertainty is represented explicitly rather than being silently converted into success or a blind retry.

## v0.2 — Go -> Rust -> Go control loop

v0.2 connected Metro to the real Lifetra authority path:

```text
Go Metro
  -> packet / route / receipt
  -> Lifetra observation
  -> Rust Lifetra Station
  -> DecisionAuthority
  -> decision JSON
  -> Go binding validation
  -> ALLOW only
  -> next Metro packet
```

Go does not blindly trust the Rust output. Observation, prior action, receipt, and authority provenance are rebound before a new packet may exist. `BLOCK` and `REQUIRE_APPROVAL` cannot masquerade as dispatch authority.

The Lifetra station proof is pinned to merge commit:

```text
80fc633e00c863aeb6505f008c43840ca6445579
```

## v0.3 — Persistent Lifetra station

v0.3 removed one Rust process spawn per decision. One NDJSON station stays alive and handles sequential requests on the same PID.

A CI run over 200 warm local decisions reported:

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

These are local deterministic Go <-> Rust control-plane measurements, not LLM or distributed-agent throughput.

Persistent Lifetra is pinned to:

```text
61b9d7ecdb59cea5a7b89675fdf5bd8c49bd6b55
```

## v0.4 — Request-correlated multiplex rail

v0.4 lets many control requests be in flight while responses may finish out of order:

```text
req-101 ---+
req-102 ---+--> persistent Lifetra workers
req-103 ---+              |
                         responses
                            |
                            v
                     request_id map
                            |
                            v
                     original caller
```

`request_id` is transport correlation, not action identity. The inner `action_id`, observation, receipt, and authority bindings remain separately validated.

The Go race detector found and helped fix a real concurrent stderr-buffer race during development. Final v0.4 CI requires `go test -race ./internal/lifetrastation`.

The benchmark showed the expected queueing trade-off: throughput grew with concurrency, but tail latency rose near saturation. With four persistent Rust PIDs and concurrency 64, one CI run observed about `27.6k` local control decisions/s with p50 around `1.77 ms`; this remains a local control-plane result only.

Multiplex Lifetra is pinned to:

```text
b8f1ff5ba4de78d3ab618c0886c5b528d40be15e
```

## v0.5 — Adaptive backpressure and load-aware routing

v0.5 adds a separate `AdaptivePool`; the v0.4 round-robin pool remains available as a control path.

```text
caller
  |
  v
Adaptive admission
  |
  +-- all stations at cap --> ErrBackpressure (no dispatch)
  |
  v
score stations
(in_flight + 1) * EWMA service latency
  |
  v
selected persistent Lifetra station
```

Important semantics:

- each station has a hard `MaxInFlightPerStation` cap;
- backpressure occurs **before dispatch**, so a rejected request ID can be retried later;
- once admitted, the request ID is lifetime-unique and an execution error is not automatically rerouted;
- every station receives a cold sample before measured latency affects routing;
- snapshots expose in-flight count, completed/errors, EWMA latency, and estimated delay;
- a real two-process test makes one station deliberately `+5 ms` slower and verifies that subsequent work moves to the faster station.

One CI run used four Rust PIDs, 16 workers/PID, 64 callers, and 1,024 completed requests per scenario:

| Cap / PID | Total capacity | Backpressure | Service p50 | Service p95 | E2E p95 | Throughput |
| ---: | ---: | ---: | ---: | ---: | ---: | ---: |
| 2 | 8 | 6,260 | 439 us | 1.134 ms | 18.715 ms | 15.3k/s |
| 8 | 32 | 1,122 | 1.277 ms | 2.737 ms | 9.884 ms | 22.6k/s |
| 16 | 64 | 0 | 2.264 ms | 5.470 ms | 5.471 ms | 23.4k/s |

All scenarios completed `1024/1024`; observed per-station in-flight counts never exceeded the configured cap.

The result is deliberately not described as a universal speedup:

> Backpressure bounds internal queue exposure; choosing the bound is a latency/throughput policy decision, not free performance.

## v0.6 — Bounded Fast Decision Plane

v0.6 introduces a provider-agnostic **System-One-shaped** semantic decision layer before physical Metro traffic routing.

```text
Metro Packet
    |
    v
metro.decision.request.v0.1
    |
    | packet_hash
    | state_hash
    | choices_hash
    v
Provider
 typed probabilities + confidence
    |
    v
Metro validation + policy
  |          |             |
  v          v             v
AUTO       SYSTEM-2      APPROVAL
  |
  v
Metro Route
```

The provider does **not** receive permission to invent actions or targets. It can only select from choices already bounded by the Metro packet.

Before a route can exist, Metro binds the decision to:

```text
request_id
+ action_id
+ packet_hash
+ state_hash
+ choices_hash
```

Metro recomputes packet/state/choice digests when consuming the decision. Post-binding mutation fails closed. Decision state is deep-copied at request creation so caller-side map mutation cannot silently change the state under its digest.

The result must also contain a complete probability distribution over exactly the supplied choices. Unknown choices, disallowed targets, duplicate/incomplete distributions, invalid probability mass, stale packet/state/choices, or a non-max selected choice fail closed.

Default experimental gate policy:

```text
auto-route confidence >= 0.98
selected probability   >= 0.98
minimum top margin     >= 0.15
side_effect=true       -> REQUIRE_APPROVAL
```

A deterministic `StaticProvider` exists only as a proof/fallback provider. It is not an AI model.

### v0.6 proof

The CI proof gives the provider the same semantic preference in two separately bound packets:

```text
research-agent  0.005
code-agent      0.990
qa-agent        0.005
confidence      0.990
```

For the non-side-effecting packet:

```text
AUTO_ROUTE -> code-agent
top margin = 0.985
```

For a separately hashed packet with `side_effect=true`:

```text
REQUIRE_APPROVAL
route = none
```

The two decisions have different `packet_hash` values. Confidence cannot override the effect policy boundary.

The same runtime head passes:

```text
gofmt                                        PASS
go test ./...                                PASS
go test -race ./internal/decisionplane \
  ./internal/lifetrastation                  PASS
bounded AUTO_ROUTE / APPROVAL proof          PASS
50,000 local decision-plane iterations       PASS
v0.2 Go -> Rust -> Go regression             PASS
v0.3 persistent station regression           PASS
v0.4 multiplex regression                    PASS
v0.5 adaptive backpressure regression        PASS
```

One GitHub Actions run over 50,000 local iterations reported:

```text
completed       50,000 / 50,000
average         5.631 us
p50             4.899 us
p95             9.387 us
p99            14.567 us
min             4.659 us
max           289.253 us
```

That measured path is only:

```text
StaticProvider
  -> normalize probabilities
  -> packet/state/choices re-hash + validation
  -> policy gate
  -> route construction
```

It is **not** a TypeSafe Jev benchmark, remote-model benchmark, LLM benchmark, Lifetra execution benchmark, or end-to-end agent throughput result.

The provider interface is intentionally suitable for a future fast typed decision adapter, but v0.6 makes **no TypeSafe/Jev API compatibility claim**. See [`docs/fast-decision-plane-v0.6.md`](docs/fast-decision-plane-v0.6.md).

## Core artifacts

- `protocol/metro.packet.v0.1.json` — bounded action envelope
- `protocol/metro.route.v0.1.json` — route decision envelope
- `protocol/metro.receipt.v0.1.json` — execution evidence envelope
- `protocol/metro.decision.request.v0.1.json` — bounded semantic decision request
- `protocol/metro.decision.result.v0.1.json` — typed probabilistic decision result
- `protocol/lifetra.observation.v0.1.json` — Metro receipt -> Lifetra observation
- `protocol/lifetra.station.request.v0.1.json` — control-station request
- `protocol/lifetra.decision.v0.1.json` — Lifetra authority decision -> Metro packet
- `protocol/lifetra.station.request-envelope.v0.2.json` — request correlation envelope
- `protocol/lifetra.station.response-envelope.v0.2.json` — correlated Lifetra response
- `protocol/lifetra.station.error.v0.2.json` — correlated station error
- `internal/metro/metro.go` — Go packet/route/receipt core
- `internal/decisionplane/decision.go` — provider contract, provenance validation, policy gate
- `internal/decisionplane/static_provider.go` — deterministic proof/fallback provider
- `internal/lifetrabridge/` — Lifetra proof/control bridge
- `internal/lifetrastation/multiplex.go` — concurrent process transport
- `internal/lifetrastation/adaptive.go` — admission + EWMA routing
- `cmd/fast-decision-demo/main.go` — AUTO_ROUTE / APPROVAL proof
- `cmd/fast-decision-bench/main.go` — local Go decision-plane benchmark
- `docs/fast-decision-plane-v0.6.md` — v0.6 proof and claim ceiling

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

1. **Stable action identity.** Create `action_id` before dispatch; recovery must not silently create a new logical action.
2. **Minimal context movement.** Reference large histories/artifacts instead of copying them by default.
3. **Routing is separate from reasoning.** Deep planning and fast bounded choice are different jobs.
4. **Execution must produce evidence.** A route is not proof that an effect occurred.
5. **Fail closed on uncertainty.** `UNKNOWN` is not success, failure, or permission to redispatch.
6. **Authority is separate from execution.** Permission does not imply dispatch or completion.
7. **Cross-runtime output is untrusted until rebound.** Returned data must match the evidence/identity that caused it.
8. **Persistence must not weaken proof.** Reusing a process is an optimization, not a relaxation of validation.
9. **Backpressure must happen before ambiguous execution.** A saturated request may be refused pre-dispatch; a possibly executed request may not be casually sent elsewhere.
10. **Semantic choice is bounded, not sovereign.** A fast provider may rank existing choices; it cannot expand allowed actions/targets.
11. **Decision provenance binds the whole decision context.** Packet, semantic state, and choice set are hashed and revalidated before route creation.

## Non-goals

This repository does **not** yet claim:

- exactly-once execution across arbitrary distributed systems;
- cryptographic trust between independent organizations;
- production-grade scheduling, billing, auth, discovery, or autoscaling;
- benchmark superiority over queues, buses, RPC systems, or agent frameworks;
- autonomous safety for high-risk actions;
- safe redispatch after an `UNKNOWN` external effect;
- durable orchestration across Metro process crashes;
- production RPC between Metro and Lifetra;
- that one admission cap is optimal across workloads;
- that local control-plane throughput equals AI-agent or LLM throughput;
- TypeSafe Jev API compatibility, Jev latency/accuracy, or any remote-model result;
- that the default `0.98` confidence threshold is universally calibrated.

## Run the demos and proofs

Metro core:

```bash
go run ./cmd/metro-demo
```

Fast decision proof:

```bash
go run ./cmd/fast-decision-demo
```

Local fast-decision overhead benchmark:

```bash
go run ./cmd/fast-decision-bench -iterations 50000
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
go test -race ./internal/decisionplane ./internal/lifetrastation
```

## Status

`v0.6` — CI-verified bounded fast decision plane with packet/state/choice provenance binding, complete probabilistic-choice validation, System-2 escalation, side-effect approval gating, race-checked transport regressions, and measured local Go gate overhead.

Contributions should preserve the narrow claim ceiling: make each boundary independently verifiable before making the system more ambitious.

## License

MIT
