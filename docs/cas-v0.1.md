# CAS Resolver v0.1

## Purpose

`EvidenceBundle` already names every replay artifact as `cas://sha256/<digest>`. CAS v0.1 adds the smallest storage boundary needed to resolve those references without requiring the caller to already hold Packet, Route, Result, Receipt, AuthorityPolicy, ProofEnvelope, and ReplayReport objects in memory.

Core invariant:

```text
REFERENCE != CONTENT
```

A resolved object is accepted only when its raw bytes hash to the digest encoded in its reference.

## Interfaces

```go
type CASResolver interface {
    Resolve(ref string) ([]byte, error)
}

type CASStore interface {
    CASResolver
    Put(content []byte) (string, error)
}
```

v0.1 includes `MemoryCAS` as a local reference implementation. Filesystem and remote stores can implement the same interfaces later without changing replay semantics.

## Storage path

`StoreEvidenceBundle(...)` performs the existing independent replay first, builds the seven-artifact evidence bundle, serializes each exact artifact, stores it by SHA-256, and requires the returned CAS ref to equal the ref already pinned in the manifest.

```text
original artifacts
    -> ReplayVerifier
    -> REPRODUCED
    -> EvidenceBundle
    -> serialize exact artifacts
    -> CASStore.Put(bytes)
    -> cas://sha256/<digest>
```

AuthorityPolicy is stored using `AuthorityPolicy.CanonicalJSON()` so its CAS digest is exactly the same canonical policy hash already carried by the authority proof.

## Resolver verification path

`VerifyEvidenceBundleFromCAS(...)` needs only:

```text
EvidenceBundle + CASResolver
```

It:

1. validates the bundle and root hash;
2. resolves all seven `cas://sha256/...` refs;
3. recomputes SHA-256 over the raw returned bytes;
4. decodes Packet, Route, Result, Receipt, AuthorityPolicy, ProofEnvelope, and ReplayReport;
5. rebuilds the bundle from the decoded originals;
6. independently replays the proof;
7. requires the stored replay report itself to be `REPRODUCED` and bound to its manifest digest.

Any missing object, bad ref, digest mismatch, decode failure, replay failure, policy mismatch, or bundle-root mismatch fails closed.

## Scope ceiling

`MemoryCAS` is not durable storage and `cas://` is not a network protocol in v0.1. The interface defines content identity and resolution semantics only. Persistence, authentication, replication, authorization, garbage collection, and remote transport remain outside this bead.
