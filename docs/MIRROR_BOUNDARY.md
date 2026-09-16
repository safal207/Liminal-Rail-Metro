# Mirror Boundary Protocol v0.1

## Purpose

The mirror boundary prevents an agent from treating internally generated agreement as proof that the external world changed.

Core invariant:

```text
BELIEF != REFLECTION != EVIDENCE != VERIFIED_STATE
```

A mirror can help an agent decide what to inspect. It cannot, by itself, authorize an effectful commit.

## Threat model

A hall-of-mirrors failure looks like this:

```text
agent says X
  -> local mirror says X
  -> domain mirror says X
  -> system mirror says X
  -> system concludes X is externally true
```

The three mirrors may still be descendants of one internal source. Agreement is useful for consistency checking, but it is not independent evidence.

## Invariants

### M-001: Reflection is not evidence

No internally generated reflection can satisfy an external-evidence requirement.

### M-002: Mirrors cannot certify each other

Any number of matching mirror observations still counts as zero external proofs.

```text
1 internal source -> 100 reflections != 100 independent proofs
```

### M-003: Divergence freezes irreversible effects

If evidence bound to the same claim/action disagrees about the claimed value, an irreversible effect must fail closed even when one matching external proof exists.

Typical irreversible effects include payment, deletion, deployment, publication, and production mutation.

### M-004: Commit requires provenance

A verified external proof only authorizes commit when it is bound to the same claim and action, matches the claimed value, and carries provenance.

The provenance must let the caller identify where the proof came from, for example a durable receipt reference.

## Gate rule

For a claim `C`:

```text
commit(C) =
  matching_verified_external_proof_with_provenance(C)
  AND NOT (C.irreversible AND divergence(C))
```

The implementation is in `internal/mirror`.

## Acceptance scenarios

1. No evidence -> reject.
2. 100 matching mirrors, no external proof -> reject.
3. Matching verified external proof with provenance -> commit.
4. Unverified or unprovenanced external evidence -> reject.
5. External proof bound to a different claim -> reject.
6. Matching external proof plus divergent bound evidence for an irreversible claim -> reject/freeze.

## Metro receipt integration

The existing Metro receipt is a natural candidate for external proof, but only after the receipt itself has been verified against the packet, route, input hash, and result hash.

A future adapter should follow this order:

```text
metro.Packet
    -> metro.Route
    -> executor effect
    -> metro.Receipt
    -> metro.Verify(...)
    -> mirror.Evidence{Source: external, Verified: true, Provenance: [receipt ref]}
    -> mirror.Gate
    -> COMMIT / REJECT
```

A receipt that has not passed `metro.Verify` must not be promoted to verified external evidence.

## Scope and limitation

This protocol does not claim that an external source is universally true. It enforces a narrower engineering boundary: internal reflection cannot satisfy a requirement for external evidence, and irreversible effects fail closed on bound disagreement.

The trust policy for external proof remains domain-specific and should be explicit at the adapter boundary.
