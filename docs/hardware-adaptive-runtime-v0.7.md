# Hardware-Adaptive Runtime v0.7 — Attestation Provider Interface

v0.7 stops coupling the adaptive runtime to one attestation mechanism.

The narrow claim is:

> Can the same authority-bound, replay-safe adaptive loop consume attestation through one provider contract while preserving the provider's real assurance level instead of pretending every provider is hardware-backed?

## Provider-neutral loop

```text
SIGNED AUTHORITY
      |
      v
ATTESTATION PROVIDER
      |
      +--> descriptor / assurance capabilities
      +--> enrolled provider anchor
      +--> provider evidence
      +--> fresh provider attestation
      |
      v
BOUND RESULT -> METRO RECEIPT -> DURABLE JOURNAL -> LIFETRA -> LEARN
```

The adaptive layer depends on `deviceattest.Provider`, not on software-specific device fields.

The provider contract exposes:

```text
Descriptor()
Enroll()
Evidence()
ValidateAnchor()
Attest(binding)
Verify(attestation, anchor, binding)
```

`ProviderBinding` preserves the security-critical inputs already proven by v0.6:

- fresh nonce;
- current signed-authority hash;
- authority issuer-key fingerprint;
- current journal sequence/head;
- workload identity;
- contextual-policy key.

## Generic envelopes

Provider-specific objects are wrapped in content-addressed generic envelopes:

- `ProviderTrustAnchor`;
- `ProviderEvidence`;
- `ProviderAttestation`.

Every envelope carries a `ProviderDescriptor` and descriptor hash. This allows future providers to expose different native payloads without changing Metro, Lifetra, or the adaptive-policy contract.

## Honest assurance metadata

The first provider implementation is the existing software-observed Ed25519 provider.

It deliberately reports:

```text
provider_id        = software-observed-ed25519-v0.1
assurance_level    = software-bound
hardware_backed    = false
remote_verifiable  = false
```

A successful proof does not upgrade those flags.

A future TPM/TEE/cloud provider must declare its own capabilities and implement the same contract. v0.7 does not ship or claim such a provider.

## Stable evidence digest

The software provider normalizes the observation timestamp out of its provider-level evidence digest. Freshness is supplied by the challenge nonce and journal binding, not by making the evidence hash change every millisecond.

This keeps the evidence digest stable for one unchanged runtime while the signed attestation remains unique per attempt.

## Result binding

Before Metro creates the receipt, the measured result is bound to:

```text
attestation_provider_protocol
attestation_provider_id
attestation_root_type
attestation_assurance_level
attestation_hardware_backed
attestation_remote_verifiable
attestation_descriptor_hash
provider_anchor_hash
provider_evidence_hash
provider_attestation_hash
```

`ProviderBoundLearner.Apply` re-checks the provider binding against the current authority, journal, workload, and context before delegating to the existing v0.5 durable/signed-authority path.

## Lifetra proof refs

The generic bridge adds:

```text
adaptive-attestation-provider://sha256/<descriptor_hash>
adaptive-provider-anchor://sha256/<anchor_hash>
adaptive-provider-evidence://sha256/<evidence_hash>
adaptive-provider-attestation://sha256/<attestation_hash>
```

Provider-specific proof refs remain available in older/specialized bridges. The v0.7 bridge is intentionally generic.

## Executable proof

```bash
go test ./...
rm -f hardware-adaptive-v0.7.journal.jsonl hardware-adaptive-proof-v0.7.json
go run ./cmd/hardware-adaptive-v07-demo
```

The proof runs six real SHA-256 CPU rounds through a variable typed only as `deviceattest.Provider`.

It checks:

- provider-interface execution succeeds;
- software assurance stays explicitly weak;
- six provider attestations round-trip through the generic contract;
- a stale provider binding is rejected after the journal advances;
- reopening the same provider anchor with another software-provider key is rejected;
- restart restores learned policy state;
- generic provider proof refs are present in every Lifetra observation;
- provider descriptor/anchor/evidence/attestation hashes are bound into each receipt result.

## Protocol artifacts

- `protocol/attestation.provider.v0.1.json`
- `protocol/attestation.provider-attestation.v0.1.json`

## Claim ceiling

v0.7 demonstrates **attestation-provider separation** and preserves the v0.6 software-bound proof through that interface.

It does **not** demonstrate:

- a working TPM 2.0 provider;
- TPM PCR quotes or measured boot;
- SEV-SNP, TDX, SGX, TrustZone, Secure Enclave, or HSM-backed keys;
- cloud workload-identity verification;
- remote attestation by a third party;
- that two providers with different assurance levels are interchangeable for policy purposes;
- automatic provider upgrade/downgrade policy;
- production key provisioning or revocation.

## Next bead

v0.8 should implement the first provider whose trust root is genuinely external to the process — for example TPM 2.0 when `/dev/tpmrm0` is available, or a cloud workload-identity/attestation provider in an environment that exposes verifiable evidence. The provider must reuse the same generic contract rather than adding another special case to Rail.
