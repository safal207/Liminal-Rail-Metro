# Liminal Rail v1.1 — Signed Policy Authority / Anti-Downgrade

v1.0 proved that a supplied `TrustPolicy` is enforced fail-closed before guarded execution or learning. v1.1 closes the next authority gap:

> An operation must not be allowed to choose its own minimum trust requirements.

## Threat model

Without policy-selection authority, a caller could take a sensitive operation such as `hardware.critical` and submit a weaker but otherwise valid policy such as `external+portable`. The v1.0 gate would correctly evaluate that weaker policy; the problem is that the wrong policy was selected.

v1.1 therefore separates **policy definition** from **policy selection authority**.

```text
operation class
    |
    v
pinned policy-authority root
    |
    v
signed manifest generation
    |
    +--> exact operation -> TrustPolicy hash
    |
    v
content-addressed authorization
    |
    v
v1.0 TrustGate
    |
 ALLOW / DENY
```

## Signed manifest

A policy-authority manifest contains a canonical lexically sorted set of bindings:

```text
operation_class -> complete TrustPolicy
```

The complete manifest is content-addressed. The manifest is then signed with Ed25519. Trust does not come from a self-valid signature alone: the first signed manifest is pinned by a small `TrustRoot` containing the exact root manifest hash, signed-manifest hash, issuer identity, and issuer key fingerprint.

## Exact policy binding

`Resolver.Resolve(operationClass)` returns both:

1. the exact authoritative `TrustPolicy`; and
2. a content-addressed `Authorization` containing the operation class, policy hash, manifest hash, signed-manifest hash, issuer key id, generation, and rotation hash.

`NewPolicyAuthorityGateWithRequestedPolicy` accepts a caller-supplied policy only when its `policy_hash` is exactly the authority-bound policy hash. A weaker policy is rejected **before a TrustGate exists**, so no guarded effect or learning callback is reachable through that path.

Normal callers should use `NewPolicyAuthorityGate`, which resolves the authoritative policy internally and gives the caller no policy-selection choice.

## Rotation and downgrade semantics

Manifest rotation is explicit and signed by the currently trusted issuer. v1.1 requires exactly the next generation and allows issuer-key rotation.

For every operation already present in the previous manifest, the rotation compares trust requirements monotonically:

- external identity
- portable publication
- hardware backed
- remote hardware attestation

Removing an operation or dropping a previously required property is a weakening.

A weakening cannot be signed with `allow_weakening=false`. If the pinned authority deliberately chooses to weaken policy, the rotation must set `allow_weakening=true` and commit the canonical `weakened_operations` list into the signed rotation. This prevents **silent** weakening; it does not prevent the trusted policy authority from intentionally changing policy.

## Receipt and Lifetra binding

For an allowed effect, the measured result is bound before Metro hashing to:

```text
policy_authority_id
policy_authority_generation
policy_operation_class
policy_authority_policy_hash
policy_manifest_hash
policy_signed_manifest_hash
policy_authority_issuer_key_id
policy_authority_rotation_hash
policy_authorization_hash
```

The v1.0 trust-decision fields remain separately bound. Lifetra adds refs for the signed manifest, rotation, and authorization while preserving the existing trust-policy/evidence/decision refs.

## Executable proof

The dedicated workflow reconstructs the previous trust chain first:

```text
live GitHub OIDC
  -> v0.8 external-identity proof
  -> Sigstore keyless publication + verify
  -> v1.0 TrustEvidence / TrustPolicy proof
  -> v1.1 signed policy authority
```

It then demonstrates:

- pinned root signed manifest verifies;
- generation-2 signed rotation verifies;
- `bounded.cpu.sha256` resolves to the exact external+portable policy and executes one bounded CPU effect;
- `hardware.critical` resolves to the exact hardware-critical policy and denies the same software-bound evidence before effect/learning;
- substituting the weaker external+portable policy for `hardware.critical` is rejected before a TrustGate is constructed;
- tampering manifest content or signature fails verification;
- serialize/reload preserves the exact authorization hash;
- an unsigned/silent weakening cannot be rotated with `allow_weakening=false`;
- an explicitly authorized weakening is marked with `weakened_operations=[hardware.critical]`;
- Metro and Lifetra carry the policy-authority proof refs.

## Claim ceiling

v1.1 demonstrates signed policy-selection authority and anti-downgrade behavior in the guarded Liminal Rail path.

It does **not** demonstrate:

- OS/kernel mandatory access control;
- hardware-rooted storage of the policy-authority private key;
- real-world identity of the issuer;
- compromise resistance if the pinned policy-authority key itself is malicious or stolen;
- impossibility for arbitrary future code to bypass the guard API.

A pinned authority can intentionally weaken a future manifest only through an explicit signed rotation that names the weakened operations. That is governance visibility, not protection from a malicious trusted root.
