# Liminal Rail Metro

> A Go coordination rail for bounded agent actions, receipts, replayable proof, and explicit authority boundaries.

Liminal Rail Metro explores a small systems idea: agents should not gain authority merely because they can produce a plausible next action or agree with their own reflections. Routing, effects, receipts, authority, and verification remain separate layers.

## Current proof path

```text
Packet
  -> Route
  -> Effect
  -> Receipt
  -> metro.Verify
  -> AuthorityPolicy
  -> ProofEnvelope
  -> Mirror Gate
```

Independent verification extends that path:

```text
ProofEnvelope + original artifacts
  -> ReplayVerifier
  -> REPRODUCED / REJECTED
  -> EvidenceBundle
  -> CAS
```

A filesystem-backed bundle can be verified from the command line with no caller-supplied Go objects:

```bash
go build -o liminal-proof ./cmd/liminal-proof
./liminal-proof verify -bundle ./evidence-bundle.json -cas ./.cas
```

The verifier resolves the seven artifacts named by the bundle, checks the exact raw-byte SHA-256 for every `cas://sha256/<digest>` reference, decodes the pinned artifacts, and independently replays receipt and authority verification.

See:

- `docs/architecture.md`
- `docs/e2e-rail-loop.md`
- `docs/lifetra-bridge.md`
- `docs/MIRROR_BOUNDARY.md`
- `docs/replay-verifier-v0.1.md`
- `docs/evidence-bundle-v0.1.md`
- `docs/filesystem-cas-cli-v0.1.md`

## Repository layout

```text
cmd/        runnable demos, benches, and proof CLI
internal/   Go implementation packages
protocol/   versioned JSON schemas
examples/   versioned example artifacts
.github/    CI proof workflows
```

## Principles

```text
BELIEF != REFLECTION != EVIDENCE != VERIFIED_STATE
VALID_RECEIPT != AUTHORITATIVE_RECEIPT
POLICY_ID != POLICY_VERSION
PROVENANCE_STRING != STRUCTURED_PROOF
REPORTED_VERIFICATION != REPRODUCED_VERIFICATION
ARTIFACT_NAME != ARTIFACT_CONTENT
REFERENCE != CONTENT
```

The project intentionally keeps its claim ceiling narrow: these mechanisms provide explicit binding, provenance, replay, and content identity. They are not cryptographic signatures, remote attestation, timestamp authorities, or universal truth proofs.
