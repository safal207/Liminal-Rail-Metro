# Evidence Bundle v0.1

## Purpose

The replay verifier removes trust from the process that produced a `ProofEnvelope`, but a downstream consumer still needs the exact artifacts that were replayed. `mirror.evidence-bundle.v0.1` gives those artifacts content-addressed identities.

Core invariant:

```text
ARTIFACT_NAME != ARTIFACT_CONTENT
```

and therefore:

```text
replayable proof = exact artifacts + exact digests + independent replay
```

## Manifest

An evidence bundle contains seven required artifacts:

```text
EvidenceBundle
├── metro.packet
├── metro.route
├── effect.result
├── metro.receipt
├── mirror.authority-policy
├── mirror.proof-envelope
└── mirror.replay-report
```

Every entry carries:

```text
kind
hash_algorithm = sha256
digest
ref = cas://sha256/<digest>
```

The `cas://` reference is transport-neutral. It names content by digest; this package does not implement a storage backend or network resolver.

## Build rule

`BuildEvidenceBundle(...)` first runs `ReplayVerifier.Verify(...)`. Only `REPRODUCED` proofs may become bundles.

The packet, route, concrete result, receipt, proof envelope, and replay report are hashed as their deterministic JSON encodings. The authority policy uses the existing canonical semantic policy hash from `AuthorityPolicy.Snapshot()` so irrelevant executor ordering does not create a different policy identity.

The bundle itself receives a root `bundle_hash`. The root is SHA-256 over a canonical manifest containing protocol, claim/action IDs, and artifact entries sorted by artifact kind. Reordering manifest entries therefore does not change the root.

## Independent verification

`VerifyEvidenceBundle(...)` performs two levels of checking:

1. validate the manifest shape, required artifact set, `cas://` refs, digests, and root hash;
2. independently rebuild the replay report and all artifact digests from the supplied originals and compare them to the manifest.

A changed packet goal, result, receipt provenance, authority policy, envelope, or replay result changes at least one digest and cannot match the previous bundle root.

## Required artifact kinds

```text
metro.packet
metro.route
effect.result
metro.receipt
mirror.authority-policy
mirror.proof-envelope
mirror.replay-report
```

v0.1 rejects duplicates, missing kinds, unsupported hash algorithms, malformed digests, and a `cas://` reference that does not contain the entry's digest.

## Scope ceiling

An evidence bundle is a content-addressed manifest, not a content store, signature, timestamp authority, or remote-attestation mechanism. A digest proves identity relative to supplied bytes/semantic canonicalization; availability and origin authentication are separate concerns.

The next storage layer can map `cas://sha256/<digest>` to local files, object storage, IPFS-like storage, or another resolver without changing the proof identity model.
