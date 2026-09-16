# Hardware-Adaptive Runtime v0.4 — Authority-Bound Adaptation

v0.4 narrows one control-plane question:

> Can Liminal Rail keep learning from real hardware evidence without allowing learning itself to silently expand what the agent is authorized to do?

The executable proof answers that question for one local, content-addressed authority epoch.

## Loop

```text
TRUSTED AUTHORITY GRANT
        |
        v
SENSE -> CONTEXT -> CHOOSE -> ACT -> MEASURE
                         |          |
                         |          v
                         |   authority-bound result
                         |          |
                         v          v
                    Metro receipt -> VERIFY
                                      |
                                      v
                           fsynced hash-chain journal
                                      |
                                      v
                                  LEARN
                                      |
                                      v
                              Lifetra proof refs
```

Every accepted result includes:

- `authority_protocol`
- `authority_id`
- `authority_epoch`
- `authority_hash`

Those fields are part of the result payload hashed by the Metro receipt. The authority hash commits to the canonical action allow-list plus the authority id and epoch.

## Authority epoch invariant

One v0.4 journal is scoped to one immutable authority epoch.

Reopening the journal with:

- an extra action under the same epoch;
- a removed action;
- a different epoch;
- or a different authority id/hash

fails closed before learned state is made available.

An explicit epoch rotation therefore requires a new journal or a future explicit migration protocol. v0.4 intentionally does not infer that migration.

## Replay and learning boundary

v0.4 inherits v0.3 replay safety and adds an authority check before durable apply:

1. selected action must exist in the trusted grant;
2. measured result authority metadata must exactly match the active grant;
3. Metro result hash must match the full measured payload;
4. durable receipt/experience validation must pass;
5. only then may the journal advance and the policy learn.

A forged high reward for an out-of-authority action therefore cannot change policy state or journal sequence.

## Lifetra provenance

Authority-aware observations preserve the existing proof refs and add:

```text
adaptive-authority://sha256/<authority_hash>
```

A downstream observer can therefore identify the exact content-addressed authority epoch under which a learned hardware action was accepted.

## Run

```bash
go test ./...
go run ./cmd/hardware-adaptive-v04-demo
```

The demo emits:

- `hardware-adaptive-proof-v0.4.json`
- `hardware-adaptive-v0.4.journal.jsonl`

It forces a restart after six real CPU measurements, attempts an unauthorized reward injection, attempts same-epoch allow-list expansion, attempts implicit epoch rotation, then continues learning to 12 durable entries.

## Claim ceiling

v0.4 demonstrates:

- content-addressed binding of one authority epoch to every accepted measurement result;
- selection only from the authority allow-list;
- restart-safe learning under the same authority grant;
- fail-closed rejection of out-of-authority reward injection;
- fail-closed rejection of silent allow-list expansion;
- fail-closed rejection of implicit epoch rotation on the same journal;
- authority proof propagation into Lifetra observations.

v0.4 does **not** demonstrate:

- a cryptographic signature from an external authority;
- remote attestation, TPM/TEE-backed claims, or secure boot verification;
- distributed consensus over authority state;
- automatic safe migration between authority epochs;
- protection against an attacker who can replace both the trusted local grant and the program itself;
- model-weight learning or arbitrary hardware control.

The authority hash is a content commitment, not proof of who issued the authority.

## Next bead

A useful v0.5 is **signed authority + explicit rotation**: verify an issuer signature over the authority epoch, then allow a new epoch only through an independently verifiable rotation record rather than by changing local configuration silently.
