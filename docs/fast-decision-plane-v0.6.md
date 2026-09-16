# Fast Decision Plane v0.6

v0.6 adds a provider-agnostic System-One-shaped semantic decision layer before Metro traffic routing.

It does **not** give the semantic provider dispatch authority. The provider may only choose from a bounded set already admitted by the Metro packet.

```text
bounded Metro Packet
        |
        v
metro.decision.request.v0.1
        |
        | packet_hash
        | state_hash
        | choices_hash
        v
System-One-shaped Provider
 typed probability distribution
        |
        v
Metro validation + policy gate
   |           |             |
   v           v             v
AUTO_ROUTE   SYSTEM-2      APPROVAL
   |
   v
metro.route.v0.1
   |
   v
AdaptivePool / execution / receipt
```

## Provenance binding

A provider result is not accepted merely because it names the same `action_id`.

Before a route can exist, Metro requires the result to remain bound to:

- `request_id` — this decision attempt;
- `action_id` — the logical Metro action;
- `packet_hash` — the complete Metro packet, including action inputs and constraints;
- `state_hash` — the semantic state supplied to the provider;
- `choices_hash` — the exact bounded set of allowed choices.

Metro recomputes the packet, state, and choice digests when it consumes the result. A mutation after binding fails closed. `NewRequest` also deep-copies JSON-compatible state so caller-side map mutation cannot silently change the state under a stored digest.

## Provider contract

A provider implements:

```go
type Provider interface {
    Decide(context.Context, Request) (Decision, error)
}
```

The result must contain a complete probability distribution over exactly the supplied choices. Metro rejects:

- invented choice IDs;
- targets outside `packet.allowed_targets`;
- incomplete or duplicate probability entries;
- probabilities outside `[0,1]`;
- distributions that do not sum to one;
- a selected choice that is not a maximum-probability choice;
- packet/state/choice digest mismatches.

`StaticProvider` is a deterministic proof and fallback implementation. It is not an AI model.

## Policy gate

The default proof policy uses:

```text
auto-route confidence >= 0.98
selected probability   >= 0.98
minimum top margin     >= 0.15
side-effecting packet  -> REQUIRE_APPROVAL
```

These thresholds are an experimental policy, not a universal calibration claim.

Possible dispositions are:

- `AUTO_ROUTE` — a Metro route may be created;
- `ESCALATE_SYSTEM2` — no route is emitted; deeper reasoning is required;
- `REQUIRE_APPROVAL` — no route is emitted; policy requires explicit approval.

A provider's confidence never overrides the side-effect gate.

## CI proof

The final v0.6 proof uses the deterministic provider to choose `code-agent` from:

```text
research-agent  0.005
code-agent      0.990
qa-agent        0.005
```

For a non-side-effecting packet the result is:

```text
AUTO_ROUTE -> code-agent
confidence = 0.99
top margin = 0.985
```

For a separately bound packet with the same semantic preference but `side_effect=true`, the result is:

```text
REQUIRE_APPROVAL
route = none
```

The two decisions have different `packet_hash` values, proving the approval path is not created by mutating a packet after the decision was made.

The same runtime head passes:

```text
gofmt                                      PASS
go test ./...                              PASS
go test -race ./internal/decisionplane \
  ./internal/lifetrastation                PASS
bounded AUTO_ROUTE / APPROVAL proof        PASS
50,000 local decision-plane iterations     PASS
v0.2 Go -> Rust -> Go regression           PASS
v0.3 persistent-station regression         PASS
v0.4 multiplex regression                  PASS
v0.5 adaptive-backpressure regression      PASS
```

## Local Go overhead benchmark

One GitHub Actions run over 50,000 measured iterations reported:

```text
completed       50,000 / 50,000
average         5.631 us
p50             4.899 us
p95             9.387 us
p99            14.567 us
min             4.659 us
max           289.253 us
```

The measured path is only:

```text
StaticProvider
  -> normalize probabilities
  -> packet/state/choices re-hash and validation
  -> policy gate
  -> Metro route construction
```

It does **not** measure a remote semantic model.

## Relationship to TypeSafe Jev

The architecture is intentionally shaped for a System-One provider that returns typed bounded decisions with confidence/probabilities. TypeSafe publicly describes Jev in similar terms, but this repository does not claim wire/API compatibility with Jev and does not include a Jev adapter in v0.6.

A future adapter may implement the same `Provider` interface once an external provider contract is explicitly integrated and independently tested.

## Claim ceiling

v0.6 proves the local Metro decision contract and policy boundary only.

It does **not** claim:

- TypeSafe Jev latency, accuracy, pricing, or API compatibility;
- any remote-model latency or accuracy;
- LLM inference speed;
- end-to-end AI-agent throughput;
- autonomous safety for side-effecting or high-risk actions;
- that `0.98` confidence is universally calibrated;
- that semantic choice is execution authority or execution proof.
