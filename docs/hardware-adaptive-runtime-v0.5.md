# Hardware-Adaptive Runtime v0.5 — Signed Authority + Explicit Rotation

v0.5 closes the trust gap left deliberately open in v0.4.

v0.4 could prove that one immutable authority epoch bounded learning, but the authority object itself was only content-addressed. v0.5 adds an Ed25519 signature and a chain anchored to a pinned local trust root.

The narrow claim is:

> Under a fixed trust root, can Liminal Rail reject self-authorized action-space expansion and accept a new authority epoch only through an explicit rotation signed by the previously trusted issuer?

## Loop

```text
PINNED TRUST ROOT
      |
      v
SIGNED AUTHORITY EPOCH
      |
      v
SENSE -> CONTEXT -> CHOOSE -> ACT -> MEASURE
                                 |
                                 v
                         SIGNED-AUTHORITY-BOUND RESULT
                                 |
                                 v
                           METRO RECEIPT
                                 |
                                 v
                     VERIFY + FSYNC JOURNAL
                                 |
                                 v
                              LEARN
                                 |
                                 v
                         LIFETRA PROOF

rotation:
old signed epoch + actual journal head
      |
      | Ed25519 signature by old trusted issuer
      v
rotation record -> next signed epoch + next issuer key
```

## Trust model

A `SignedAuthorityGrant` contains:

- the exact v0.4 `AuthorityGrant`;
- issuer identity label;
- issuer Ed25519 public key;
- SHA-256 key fingerprint;
- issuance timestamp;
- Ed25519 signature;
- a SHA-256 content hash over the signed object.

A valid self-signature is **not** sufficient for trust.

`AuthorityChain.Verify` requires the first grant to match a pinned `AuthorityTrustRoot`, including:

- authority identity;
- unsigned authority hash;
- signed-authority hash;
- issuer identity;
- issuer key fingerprint.

Every later grant requires exactly one verified rotation from the preceding grant.

## Explicit rotation

`AuthorityRotation` commits to:

- old and new epochs;
- old and new authority hashes;
- old and new signed-authority hashes;
- old and new issuer identities and key fingerprints;
- the exact source journal sequence and SHA-256 head;
- issuance time.

The record is signed by the **old** trusted issuer key.

This means a rotation can deliberately authorize both:

1. a changed allow-list; and
2. a changed issuer key.

Neither change is inferred from model behavior or reward quality.

`ChainedAuthorityLearner.Rotate` obtains the journal sequence/head directly from the active learner, signs that anchor, appends the verified transition to the chain, and opens a distinct journal for the next epoch.

Policy state is intentionally **not** migrated automatically across authority epochs in v0.5. The next epoch starts a fresh learning journal.

## Reward binding

Every v0.5 result is bound before Metro hashes it to:

```text
authority_hash
authority_id
authority_epoch
signed_authority_hash
authority_issuer_id
authority_issuer_key_id
```

Therefore a persisted v0.5 reward cannot silently be replayed as evidence for a different signed authority.

A v0.4 journal without the signed-authority fields is rejected by the v0.5 chained entry point rather than silently upgraded.

## Lifetra proof chain

A signed-authority observation retains the existing receipt/result/journal/authority refs and adds:

```text
adaptive-signed-authority://sha256/<signed_authority_hash>
adaptive-authority-signer://ed25519/sha256/<issuer_key_id>
```

For rotated epochs it also adds:

```text
adaptive-authority-rotation://sha256/<rotation_hash>
```

## Executable proof

```bash
go test ./...
rm -f hardware-adaptive-v0.5-epoch1.journal.jsonl \
      hardware-adaptive-v0.5-epoch2.journal.jsonl \
      hardware-adaptive-proof-v0.5.json
go run ./cmd/hardware-adaptive-v05-demo
```

The demo performs real SHA-256 CPU work in two authority epochs.

Epoch 1 uses the bounded worker-count set available to the runtime. Before rotation, the demo attempts to inject a very high reward for one additional worker-count action. The learner must reject it without advancing the journal.

The old trusted issuer then signs a rotation to epoch 2. That rotation explicitly authorizes:

- one additional worker-count action; and
- a different issuer key.

Epoch 2 samples the newly authorized action as part of normal bounded exploration.

The proof also checks:

- root grant signature verification;
- rotation signature verification;
- rotation anchoring to the actual source journal head;
- rejection of a missing rotation;
- rejection of a tampered rotation;
- rejection of a valid self-signed grant that is not anchored to the pinned root;
- restart of the rotated epoch from the same verified authority chain;
- signed-authority and rotation proof refs in Lifetra observations.

## GitHub Actions proof

`Hardware Adaptive v0.5` passed on the pull-request merge ref on 2026-09-16.

The runner exercised epoch 1 with `workers=1..4`. The next action, `workers=5`, was rejected before rotation without changing the epoch-1 journal. A rotation signed by the old issuer then authorized epoch 2, a new issuer key, and `workers=5`; the epoch-2 exploration executed that newly authorized action.

Server-side proof values from that run:

```text
root_signed_authority = 2536698ff3084bb3ac0cecf52105f05587052acc80a8321d61951c372693a988
rotation_hash         = 088a583105042782256e7eac886cf2aff14ad424b2942959e3f70c3d6a3861c5
next_signed_authority = f05702a9495835c9c0095f8d6eb53b8ddcc3d6bc9d9cb6bb2d8b507b044382b8

epoch1 sequence = 6
epoch2 sequence = 5
```

The CI verifier confirmed:

```text
root_signature_verified          = true
pre_rotation_new_action_rejected = true
pre_rotation_changed_journal     = false
rotation_verified                = true
rotation_anchor_matched          = true
missing_rotation_rejected        = true
tampered_rotation_rejected       = true
unanchored_self_signed_rejected  = true
issuer_key_rotated               = true
newly_authorized_action_executed = true
restart_state_preserved          = true
signed_proof_refs_present        = true
```

## Protocol artifacts

- `protocol/adaptive.signed-authority.v0.1.json`
- `protocol/adaptive.authority-rotation.v0.1.json`

## Claim ceiling

v0.5 demonstrates local cryptographic authorization continuity under a pinned trust root.

It does **not** demonstrate:

- that an issuer label corresponds to a real-world person or organization;
- PKI, certificate-chain validation, revocation, expiry policy, or key recovery;
- TPM, HSM, TEE, Secure Enclave, or remote attestation;
- distributed consensus over authority changes;
- safe semantics of every newly authorized hardware action;
- automatic policy-state migration between authority epochs;
- model-weight learning;
- unrestricted hardware control.

The demo uses deterministic Ed25519 keys solely to make the executable proof reproducible. Those demo private keys are public-by-construction and must never be treated as production secrets.

## Next bead

A useful v0.6 is hardware-rooted / remotely attestable authority: separate the software trust chain proven here from evidence that the signer key and executing runtime are actually rooted in a specific trusted device or enclave.
