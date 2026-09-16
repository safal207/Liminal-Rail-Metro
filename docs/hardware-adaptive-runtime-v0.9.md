# Hardware-Adaptive Runtime v0.9 — Portable Keyless Proof

v0.9 turns the already verified v0.8 runtime proof into a portable external verification artifact.

The new question is:

> Can an independent verifier receive the proof payload plus one bundle and verify who signed it, that the signature matched the exact payload, and that transparency-log material was verified — without trusting the live Liminal Rail process that produced it?

## Publication flow

```text
v0.8 proof JSON
      |
      v
SHA-256 payload identity
      |
      v
GitHub Actions OIDC
      |
      v
Sigstore keyless signing
  ephemeral key
      +
  short-lived Fulcio certificate
      +
  transparency-log material
      |
      v
*.sigstore.json bundle
      |
      v
cosign verify-blob
      |
      v
portable-proof-v0.9.json receipt
```

## Why this is different from v0.8

v0.8 verified a live external GitHub Actions workload identity and intentionally discarded the raw OIDC JWT.

v0.9 uses a second short-lived workload identity through Sigstore to sign the exact v0.8 proof payload. The resulting Sigstore bundle is meant to travel with the payload. A verifier no longer needs access to the original Liminal Rail process or its journal directory to check the publication signature.

## Server workflow

The dedicated workflow:

1. reruns `go test ./...`;
2. regenerates a complete v0.8 proof;
3. installs pinned Cosign `v3.1.3`;
4. runs keyless `cosign sign-blob` with a bundle output;
5. verifies the same blob against the expected GitHub workflow identity and issuer;
6. writes a content-addressed v0.9 portable-proof receipt;
7. rechecks payload and bundle SHA-256 values;
8. scans exported material to ensure no raw JWT-like credential was persisted;
9. uploads payload, journal, execution log, Sigstore bundle, verification log, Cosign version, and the v0.9 receipt.

The expected signer is constrained to this workflow path:

```text
https://github.com/safal207/Liminal-Rail-Metro/.github/workflows/hardware-adaptive-v0.9.yml@refs/...
```

with OIDC issuer:

```text
https://token.actions.githubusercontent.com
```

## Independent verification

With a current Cosign installation, the portable artifact can be verified outside the original runtime:

```bash
cosign verify-blob hardware-adaptive-proof-v0.8.json \
  --bundle hardware-adaptive-proof-v0.9.sigstore.json \
  --certificate-identity-regexp '^https://github\.com/safal207/Liminal-Rail-Metro/\.github/workflows/hardware-adaptive-v0\.9\.yml@refs/(pull/[0-9]+/merge|heads/.+)$' \
  --certificate-oidc-issuer https://token.actions.githubusercontent.com
```

The v0.9 receipt separately commits to the SHA-256 of both the payload and the Sigstore bundle.

## Claim ceiling

v0.9 proves portable keyless publication of the v0.8 proof under the expected GitHub Actions workflow identity, plus successful Cosign verification of the resulting bundle.

It does **not** prove:

- that the source runtime was hardware-backed;
- TPM, SEV-SNP, TDX, SGX, HSM, Secure Enclave, or measured-boot provenance;
- that GitHub-hosted runner hardware has a stable physical identity;
- fully hermetic/offline verification without an appropriate Sigstore trusted-root snapshot;
- that the GitHub repository owner is a particular real-world person or organization;
- permanent protection of the ephemeral signing key beyond the short-lived keyless signing flow.

Most importantly, publication does not upgrade the source claim. If v0.8 says `hardware_upgrade_selected=false`, v0.9 must preserve that fact.

## Next bead

A useful v1.0 boundary is **policy-gated trust consumption**: instead of merely proving that stronger or external trust evidence exists, let a caller specify a minimum required assurance profile and make Rail fail closed when the discovered/provider/publication evidence does not satisfy that profile.
