# Liminal Rail v1.0 — Trust Policy Gate

v1.0 changes the question from:

> What action should the adaptive runtime prefer?

into:

> Does the currently proven trust chain satisfy the minimum assurance required for this operation at all?

## Core objects

```text
TrustPolicy
  what the operation requires

TrustEvidence
  what the current proof chain actually establishes

TrustDecision
  deterministic ALLOW / DENY + unmet requirements
```

All three objects are content-addressed with SHA-256.

## Requirements

The v1.0 policy vocabulary is intentionally small and monotonic:

- `external_identity`: an external issuer identity was verified;
- `portable_publication`: the proof was independently verified as a portable keyless publication;
- `hardware_backed`: the selected runtime proof is hardware-backed;
- `remote_hardware_attestation`: a remote hardware-attestation proof exists.

`remote_hardware_attestation=true` is invalid unless `hardware_backed=true` is also required/proven.

## Current evidence

The current v0.8/v0.9 chain establishes:

```text
external_identity_verified     = true
portable_publication_verified  = true
hardware_backed                = false
remote_hardware_attestation    = false
```

Therefore two policies evaluated against the *same* evidence must produce different deterministic outcomes:

```text
external-portable-required
  external_identity     = true
  portable_publication = true
                       -> ALLOW

hardware-critical
  external_identity            = true
  portable_publication        = true
  hardware_backed             = true
  remote_hardware_attestation = true
                              -> DENY
                                 unmet: hardware_backed,
                                        remote_hardware_attestation
```

## Fail-closed path

The v1.0 guard exposes separate execution and learning gates.

```text
policy + evidence
      |
      v
 trust decision
   /      \
ALLOW     DENY
  |         |
execute   no callback
learn     no callback
  |         |
receipt   no receipt
```

The executable proof checks that the DENY path causes:

- zero side-effect callback invocations;
- zero learning callback invocations;
- no denied Metro receipt;
- no change to the source v0.8 adaptive journal.

The ALLOW path performs one real bounded SHA-256 CPU effect, creates a Metro receipt, performs exactly one fsynced learning append, and propagates the trust policy/evidence/decision refs into Lifetra.

## Receipt binding

Before Metro hashes the allowed result, v1.0 adds:

```text
trust_decision_protocol
trust_policy_id
trust_policy_hash
trust_evidence_hash
trust_decision_hash
trust_allowed
```

Thus a successful effect receipt commits to the exact trust decision that authorized it.

Lifetra receives:

```text
adaptive-trust-policy://sha256/<policy_hash>
adaptive-trust-evidence://sha256/<evidence_hash>
adaptive-trust-decision://sha256/<decision_hash>
```

This keeps effect proof and authorization proof separate but linked.

## Evidence provenance

The dedicated v1.0 CI does not manufacture `portable_publication_verified=true` manually. It:

1. reproduces a fresh v0.8 proof;
2. obtains live GitHub Actions OIDC identity;
3. keyless-signs the exact v0.8 payload with pinned Cosign;
4. independently verifies the Sigstore bundle against the expected workflow identity;
5. creates the v0.9 portable receipt from those successful steps;
6. passes those exact hashes/flags into the v1.0 TrustEvidence constructor.

## Claim ceiling

v1.0 proves fail-closed trust-policy consumption in the guarded Liminal Rail execution/learning path.

It does not claim:

- hardware-backed attestation where none exists;
- that all arbitrary future code is technically incapable of bypassing the guard API;
- OS/kernel mandatory-access-control enforcement;
- TPM/TEE measured boot;
- a universal assurance ordering beyond the explicit four v1.0 requirements;
- that portable publication changes the underlying runtime assurance.

The safety property demonstrated is narrower and testable: when an operation uses the v1.0 guard, unmet trust requirements prevent both its execution callback and learning callback before either can run.
