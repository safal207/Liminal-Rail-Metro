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

## Authority invariants

### A-001: Validity does not imply authority

A receipt can be structurally and hash-valid while still being produced by an executor that has no right to certify that effect.

```text
valid receipt != trusted receipt
```

### A-002: Authority is deny-by-default

If no authority rule exists for an action kind, no executor receipt may cross the external-proof boundary for that action.

### A-003: Authority is bound to effect kind

Executor identity is checked against the concrete `packet.action.kind`. Authority for `code.implement` does not imply authority for `payment.capture`, `deploy.production`, or any other effect.

### A-004: Routing permission is not proof authority

A router may validly select an executor while the independent authority policy still rejects that executor as a proof source.

This separation prevents routing configuration from silently expanding the trust boundary.

## Gate rule

For a claim `C`:

```text
commit(C) =
  matching_verified_authoritative_external_proof_with_provenance(C)
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
7. Structurally valid receipt from a non-authoritative executor -> reject.
8. Missing authority rule for an action kind -> reject.
9. Exact executor/action authority binding -> allow promotion.

## Metro receipt integration

Metro receipts can cross the mirror boundary through `EvidenceFromVerifiedReceipt` only after they are independently bound back to the packet, route, concrete result, claim, and an explicit authority policy.

```text
metro.Packet
    -> metro.Route
    -> executor effect
    -> metro.Receipt
    -> metro.Verify(packet, route, result, receipt)
    -> receipt protocol/status/provenance checks
    -> claim/action/result-hash binding
    -> AuthorityPolicy.Authorize(packet.action.kind, receipt.executor_id)
    -> mirror.Evidence{Source: external, Verified: true}
    -> mirror.Gate
    -> COMMIT / REJECT
```

Promotion is fail-closed. The adapter requires:

- the claim and packet to share the same `action_id`;
- `metro.receipt.v0.1` as the receipt protocol;
- receipt status `SUCCEEDED`;
- non-empty `receipt_id` and `result_ref` provenance;
- successful `metro.Verify(...)` binding of packet, route, executor, input hash, and result hash;
- the verified receipt `result_hash` to equal the claim `value_hash`;
- an identified authority policy;
- an exact policy rule allowing the receipt executor to certify the packet action kind.

A receipt that fails any one of those checks is not emitted as `SourceExternal` evidence.

The emitted provenance chain includes the receipt, route, executor, authority decision, and result reference:

```text
receipt://<receipt_id>
route://<route_id>
executor://<executor_id>
authority://<policy_id>/<action_kind>/<executor_id>
<result_ref>
```

This keeps both proof lineage and the trust decision visible rather than collapsing a successful receipt into an untraceable boolean.

## Authority policy example

```go
mirror.AuthorityPolicy{
    ID: "production-authority-v1",
    ExecutorsByAction: map[string][]string{
        "code.implement":  {"code-agent"},
        "payment.capture": {"payments-agent"},
    },
}
```

The policy uses exact action and executor identity matching. It has no wildcard fallback in v0.1.

## Scope and limitation

This protocol does not claim that an external source is universally true. It enforces a narrower engineering boundary: internal reflection cannot satisfy a requirement for external evidence, and a valid receipt cannot become external proof unless its executor is explicitly authoritative for that effect.

Authority policy remains domain-specific and must be configured at the adapter boundary.
