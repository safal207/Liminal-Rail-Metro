# Lifetra bridge v0.1

The bridge connects Liminal Rail Metro's fast execution path to Lifetra's proof-carrying trajectory and authority model without embedding the Rust engine into every Metro hop.

## Boundary

```text
Metro Packet
    |
    v
Route -> Executor
    |
    v
Metro Receipt
    |
    v
lifetra.observation.v0.1
    |
    v
lifetra.station.request.v0.1
    |
    v
Lifetra Rust station
TrajectoryBead -> OrientationDelta -> CorrectionPolicy -> DecisionAuthority
    |
    v
lifetra.decision.v0.1
    |
    v
new Metro Packet
```

Metro remains responsible for routing and execution transport. Lifetra remains responsible for evidence-aware trajectory reasoning, correction proposals, and authority decisions.

## Contract 1: Metro receipt -> Lifetra observation

`ReceiptToObservation` turns a verified Metro receipt into a small proof-carrying observation.

The bridge preserves:

- the original `action_id`;
- the receipt status exactly, including `UNKNOWN`;
- a SHA-256 hash of the full receipt;
- a proof reference to the source receipt;
- an optional reference to the previous Lifetra bead.

The bridge does not infer success from missing evidence and does not turn an observation into a trajectory commit by itself.

## Contract 2: observation -> Lifetra station request

`protocol/lifetra.station.request.v0.1.json` defines the JSON request consumed by the companion Rust station.

It carries the Metro observation plus the minimum control context Lifetra needs to interpret the event:

- intended orientation;
- observed movement for externally confirmed outcomes;
- correction-policy bounds;
- safety/authority bounds;
- runtime approval context;
- a proposed *new* action that can be emitted only if Lifetra returns `ALLOW`.

`examples/lifetra-station-request.json` is the first concrete journey fixture.

The request deliberately does not ask Metro to compute Lifetra's orientation delta or authority verdict. Those remain Rust-side Lifetra semantics.

## Contract 3: Lifetra decision -> Metro packet

`DecisionToPacket` converts only `ALLOW` decisions into dispatchable Metro packets.

`REQUIRE_APPROVAL` and `BLOCK` fail closed and cannot become Metro packets through this adapter.

An allowed decision must include an authority proof reference and a `next_action`. The new action receives a new stable `action_id`; the prior action remains linked as causal provenance through `caused_by_action_id` and `previous_receipt_ref`.

This intentionally rejects silent reuse of the previous `action_id`. Explicit retry or reconciliation authority is a separate protocol rather than an accidental redispatch path.

## Real Lifetra station

The companion Lifetra work is implemented in `safal207/Lifetra` PR #7 (`feat/metro-station-v0.1`). The Rust example consumes `lifetra.station.request.v0.1` from stdin and emits `lifetra.decision.v0.1` on stdout using Lifetra's existing primitives:

```text
TrajectoryBead
    -> BeadCommit
    -> ProvenOrientation
    -> OrientationDelta
    -> CorrectionPolicy
    -> DecisionAuthority
```

Important behavior:

- Metro `UNKNOWN` creates unresolved bead uncertainty and emits `BLOCK`;
- `REJECTED` emits `BLOCK` in station v0.1;
- confirmed `SUCCEEDED` or `FAILED` outcomes can enter the proof-backed orientation path when observed movement is supplied;
- corrections outside the automatic envelope emit `REQUIRE_APPROVAL` rather than an executable action;
- only `ALLOW` returns a new action proposal.

## Why two engines stay separate

- **Metro / Go**: hot path, routing, concurrency, transport, adapters, receipts.
- **Lifetra / Rust**: beads, causal state, orientation delta, correction policy, decision authority, execution evidence semantics.

The bridge is JSON-compatible so either side can evolve independently behind the wire contracts.

## Invariants

1. `UNKNOWN` remains `UNKNOWN` across the bridge.
2. Observation is evidence, not interpretation.
3. Authority is not execution proof.
4. Only `ALLOW` can produce a new Metro packet.
5. A new logical action receives a new `action_id`.
6. The source receipt and authority proof remain in packet provenance.
7. The bridge does not claim exactly-once execution or automatic safe recovery.

## Run Metro side

```bash
go test ./...
go run ./cmd/lifetra-bridge-demo
```

The Go demo performs the typed handoff:

```text
Metro receipt -> Lifetra observation -> ALLOW decision -> next Metro packet
```

## Run the companion Rust station

From the Lifetra branch in PR #7:

```bash
cargo run --quiet --example metro_station < request.json
```

A full end-to-end journey is therefore:

```text
Go Metro receipt
  -> Go observation adapter
  -> lifetra.station.request.v0.1 JSON
  -> Rust Lifetra station
  -> lifetra.decision.v0.1 JSON
  -> Go DecisionToPacket
  -> next Metro route
```

## Claim ceiling

v0.1 is a typed, testable interoperability boundary plus an experimental external Lifetra station. It does not yet provide a long-running RPC service, automatic semantic inference of orientation from arbitrary tool output, exactly-once execution, or safe automatic redispatch after an `UNKNOWN` effect.
