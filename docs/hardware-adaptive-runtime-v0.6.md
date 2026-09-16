# Hardware-Adaptive Runtime v0.6 — Device-Bound Software Attestation

v0.6 addresses the next question after signed authority:

> Can one learned hardware interaction be proven to come from the enrolled runtime/device identity for this exact attempt, without exposing raw machine identifiers?

The answer in v0.6 is a deliberately bounded **software-observed** attestation proof. It is not presented as TPM, TEE, HSM, Secure Enclave, or remote hardware attestation.

## Loop

```text
PINNED SIGNED AUTHORITY
        +
ENROLLED DEVICE FINGERPRINT + DEVICE KEY
        |
        v
fresh challenge nonce
        |
        v
software-observed device/runtime evidence
        |
        v
Ed25519 device attestation
        |
        +-- signed authority hash
        +-- authority issuer key id
        +-- current journal sequence/head
        +-- workload + context
        +-- device + runtime fingerprints
        |
        v
ACT -> MEASURE -> attestation-bound result
        |
        v
Metro receipt -> verify -> fsync journal -> learn
        |
        v
Lifetra proof refs
```

## Device evidence

`internal/deviceattest` reads only locally available, unprivileged runtime evidence. When available it considers:

- `/etc/machine-id`;
- `/sys/class/dmi/id/product_uuid`;
- `/proc/sys/kernel/random/boot_id`;
- hostname;
- OS/architecture, kernel release, Go runtime version, and logical CPU count.

Raw identifier values are **never emitted into the proof**. The protocol retains only SHA-256 digests in `source_digests`.

Two identifiers are derived:

- `device_fingerprint` — software-observed identity from stable-enough local sources plus OS/architecture;
- `runtime_fingerprint` — device fingerprint plus boot/runtime state.

Because these sources are software-readable and may describe a VM/container rather than physical silicon, they do not establish a hardware root of trust.

## Device enrollment

A device enrollment pins:

```text
device_fingerprint
attestation_key_id
```

The attestation key is Ed25519 and software-held in v0.6.

A different key is rejected even when it can produce a cryptographically valid self-signature over the same visible evidence.

## Per-attempt freshness

Each adaptive action uses a fresh random challenge nonce.

The signed device attestation commits to:

```text
nonce
device_fingerprint
runtime_fingerprint
attestation_key_id
signed_authority_hash
authority_issuer_key_id
journal_sequence
journal_head
workload
context_key
```

The attestation is created before the measured action and is then embedded by hash into the exact result that Metro hashes into the receipt.

A stale nonce or stale journal anchor fails closed before reward application.

## Lifetra proof chain

The existing receipt, experience, journal, authority, signed-authority, and signer refs remain intact. v0.6 adds:

```text
adaptive-device-attestation://sha256/<attestation_hash>
adaptive-device-key://ed25519/sha256/<attestation_key_id>
adaptive-device-fingerprint://sha256/<device_fingerprint>
adaptive-runtime-fingerprint://sha256/<runtime_fingerprint>
```

## Executable proof

```bash
go test ./...
rm -f hardware-adaptive-v0.6.journal.jsonl hardware-adaptive-proof-v0.6.json
go run ./cmd/hardware-adaptive-v06-demo
```

The demo performs real SHA-256 CPU work and proves:

- the device/runtime fingerprint is stable within the runner lifetime;
- a fresh attestation verifies against the pinned device identity;
- a stale nonce is rejected;
- an attestation anchored to an old journal head is rejected after learning advances;
- a different software device key is rejected;
- tampered runtime evidence is rejected;
- the learner restarts on the same runtime/key and restores policy state;
- every Lifetra observation carries the device attestation/key/device/runtime proof refs;
- only SHA-256 digests of source identifiers are emitted.

## Protocol artifacts

- `protocol/device.evidence.v0.1.json`
- `protocol/device.attestation.v0.1.json`

## Claim ceiling

v0.6 demonstrates **software device binding** under a pinned local device key and v0.5 signed authority chain.

It does **not** demonstrate:

- TPM quotes, PCR validation, measured boot, or endorsement-key identity;
- TEE/enclave reports such as SEV-SNP, TDX, SGX, TrustZone, or Secure Enclave;
- HSM-backed non-exportable keys;
- remote attestation by a cloud or hardware vendor;
- that `machine-id`, DMI UUID, hostname, or boot ID cannot be forged by a privileged host;
- that the software-held Ed25519 device key cannot be copied;
- physical-device identity across VM migration or cloning;
- model-weight learning or unrestricted hardware control.

A later hardware-rooted implementation can keep the same proof boundary while replacing `software-observed` evidence and the software key with a real attestation provider.

## Next bead

v0.7 should define an attestation-provider interface and implement the first hardware/cloud-backed provider where the environment actually exposes one (for example TPM 2.0, SEV-SNP/TDX, or a cloud workload identity), while preserving the software provider as a clearly weaker fallback.
