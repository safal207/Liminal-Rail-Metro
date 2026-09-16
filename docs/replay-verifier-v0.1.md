# Replay Verifier v0.1

## Purpose

A `ProofEnvelope` reports that a verification path succeeded. The replay verifier independently repeats that path from the original artifacts instead of trusting the envelope's `VERIFIED` label.

Core invariant:

```text
REPORTED_VERIFICATION != REPRODUCED_VERIFICATION
```

The replay input is:

```text
ProofEnvelope
+ metro.Packet
+ metro.Route
+ concrete result
+ metro.Receipt
+ AuthorityPolicy
```

The output protocol is `mirror.replay-report.v0.1` with exactly two terminal states:

```text
REPRODUCED
REJECTED
```

## Verification path

`ReplayVerifier.Verify(...)` fails closed and checks:

1. the proof envelope is structurally valid;
2. packet, route, and receipt protocol identifiers are the expected Metro versions;
3. packet `action_id` and `action.kind` match the envelope claim/action;
4. route `action_id`, `route_id`, and selected target remain bound to the packet and envelope;
5. the original receipt fields match the structured receipt carried by the envelope;
6. `metro.Verify(...)` independently reproduces packet/route/input/result bindings;
7. the concrete result hashes to the envelope claim `value_hash`;
8. `AuthorityPolicy.AuthorizeProof(...)` independently reconstructs the executor authority decision and canonical policy hash;
9. the replayed authority proof exactly matches the authority carried by the envelope.

A single mismatch returns `REJECTED` and records the first failing check.

## Why replay is stronger than envelope validation

`ProofEnvelope.Validate()` checks internal consistency. Replay additionally asks whether the original artifacts still reproduce those claims.

For example, a 64-character replacement `policy_hash` can remain structurally well-formed while referring to no policy that actually authorized the executor. Envelope validation alone cannot distinguish that case; replay recomputes the canonical policy hash and rejects it.

Replay also compares receipt fields that `metro.Verify(...)` does not currently use for its hash/binding checks, such as `result_ref`. This prevents a caller from swapping provenance while preserving otherwise valid hashes.

## Scope ceiling

`REPRODUCED` means the supplied original artifacts reproduce the verification path encoded by the envelope under the supplied authority policy. It does not claim that the external world is universally true, and it is not a cryptographic signature or remote-attestation proof.

The replay verifier is designed to reduce trust in the process that produced the envelope by making verification independently repeatable.
