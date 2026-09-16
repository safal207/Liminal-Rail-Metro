# Hardware-Adaptive Runtime v0.8 — Provider Discovery + External Trust Probe

v0.8 answers a different question from v0.7:

> Before choosing an attestation provider, can Liminal Rail inspect the current runtime, distinguish a merely present trust source from a provider-ready one, verify a genuinely external identity source when available, and still refuse to inflate its assurance level?

## Discovery flow

```text
runtime
  |
  +--> TPM 2.0 device-node probe
  +--> SEV-SNP guest-device probe
  +--> TDX guest-device probe
  +--> GitHub Actions OIDC probe
             |
             +--> request short-lived JWT
             +--> validate issuer + audience + time window
             +--> fetch GitHub OIDC configuration + JWKS
             +--> verify RS256 signature by kid
             +--> discard raw JWT
             +--> retain safe verification receipt + token SHA-256

                 discovery report
                       |
                       v
             choose provider honestly
                       |
             no provider-ready HW source
                       |
                       v
          software provider remains selected
                       |
                       v
       adaptive action -> receipt -> journal -> Lifetra
```

## Important distinction

The discovery protocol separates:

- `present`: the runtime can observe a candidate source;
- `provider_ready`: Liminal Rail has an implemented and verified provider that can safely consume that source;
- `hardware_backed`: the source would represent a hardware-backed root if a verified provider were available;
- `external_identity`: the source is controlled by an external issuer rather than the local process.

A TPM device node being present does **not** make `provider_ready=true`.

Likewise, a GitHub OIDC token being correctly signed does not turn the existing software attestation provider into a hardware-backed provider.

## GitHub Actions OIDC

The v0.8 CI grants only:

```yaml
permissions:
  contents: read
  id-token: write
```

The OIDC probe uses `ACTIONS_ID_TOKEN_REQUEST_URL` and `ACTIONS_ID_TOKEN_REQUEST_TOKEN` to request a token with audience:

```text
liminal-rail-metro-v0.8
```

Verification requires:

1. a three-part JWT;
2. `alg=RS256` and a non-empty `kid`;
3. issuer host `token.actions.githubusercontent.com`;
4. exact requested audience;
5. a valid `exp` / `nbf` / `iat` window;
6. GitHub's published OIDC configuration;
7. a matching RSA key from GitHub's JWKS;
8. a valid RS256 signature over the exact JWT header and payload.

The raw JWT is deliberately not written to the proof artifact or journal. The exported verification receipt contains only selected claims, signing-key id, algorithm, token SHA-256, and the fact/time of successful signature verification.

This means v0.8 proves that the workflow observed and cryptographically verified a live externally issued workload identity. It does **not** make that receipt a replayable OIDC credential or a complete offline re-verification bundle.

## Receipt binding

The complete discovery report is content-addressed as `report_hash`.

Every v0.8 measured result adds:

```text
attestation_discovery_protocol
attestation_discovery_hash
attestation_selected_provider_id
attestation_selected_assurance_level
attestation_hardware_upgrade_selected
attestation_external_identity_verified
```

Those fields are present before Metro computes the result hash, so the receipt commits to the exact discovery decision.

Lifetra adds:

```text
adaptive-attestation-discovery://sha256/<report_hash>
```

alongside the existing authority/provider/receipt/journal proof refs.

## Executable proof

```bash
go test ./...
go run ./cmd/hardware-adaptive-v08-demo
```

The dedicated GitHub Actions workflow also requires a live OIDC verification and independently checks that:

- the GitHub OIDC issuer signature verified;
- the raw JWT was not persisted;
- TPM2 / SEV-SNP / TDX candidates did not become provider-ready merely because a path existed;
- `hardware_upgrade_selected=false` when there is no verified provider-ready hardware source;
- the selected provider remains `software-observed-ed25519-v0.1` with `software-bound` assurance;
- six adaptive CPU rewards are bound to the same discovery report hash;
- every Lifetra observation contains the discovery proof ref.

## Claim ceiling

v0.8 demonstrates **provider discovery and live external workload-identity verification**.

It does not demonstrate:

- a TPM 2.0 quote or PCR validation;
- measured boot;
- SEV-SNP, TDX, SGX, TrustZone, or Secure Enclave attestation;
- a non-exportable hardware key;
- a provider-ready GitHub OIDC attestation implementation through the generic provider contract;
- an offline-replayable OIDC proof bundle;
- equivalence between workload identity and hardware identity;
- automatic safe upgrade from software-bound to hardware-backed assurance.

The next useful bead is v0.9: turn the externally verified workload identity into a non-replayable, independently verifiable provider artifact (for example via a short-lived certificate / transparency-backed signing flow), or implement TPM 2.0 directly when a target runtime exposes a usable TPM API.
