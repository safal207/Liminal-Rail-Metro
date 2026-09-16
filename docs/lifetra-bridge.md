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
Lifetra bead / trajectory / orientation / authority
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

## Contract 2: Lifetra decision -> Metro packet

`DecisionToPacket` converts only `ALLOW` decisions into dispatchable Metro packets.

`REQUIRE_APPROVAL` and `BLOCK` fail closed and cannot become Metro packets through this adapter.

An allowed decision must include an authority proof reference and a `next_action`. The new action receives a new stable `action_id`; the prior action remains linked as causal provenance through `caused_by_action_id` and `previous_receipt_ref`.

This intentionally rejects silent reuse of the previous `action_id`. Explicit retry or reconciliation authority is a future protocol rather than an accidental redispatch path.

## Why two engines stay separate

- **Metro / Go**: hot path, routing, concurrency, transport, adapters, receipts.
- **Lifetra / Rust**: beads, causal state, orientation delta, correction policy, decision authority, execution evidence semantics.

The bridge is JSON-compatible so either side can evolve independently behind the two wire contracts.

## Invariants

1. `UNKNOWN` remains `UNKNOWN` across the bridge.
2. Observation is evidence, not interpretation.
3. Authority is not execution proof.
4. Only `ALLOW` can produce a new Metro packet.
5. A new logical action receives a new `action_id`.
6. The source receipt and authority proof remain in packet provenance.
7. The bridge does not claim exactly-once execution or automatic safe recovery.

## Run

```bash
go test ./...
go run ./cmd/lifetra-bridge-demo
```

The demo performs one closed control-loop handoff:

```text
Metro receipt -> Lifetra observation -> ALLOW decision -> next Metro packet
```

## Claim ceiling

v0.1 is only a typed and testable interoperability boundary. It does not yet call the Lifetra Rust library over FFI or RPC, create beads automatically, calculate orientation deltas, perform reconciliation, or authorize redispatch after an `UNKNOWN` effect.
