# Hardware-Adaptive Runtime v0.3 — durable, replay-safe learning

v0.3 narrows one question:

> Can a bounded contextual policy survive restart without double-learning one reward, while refusing evidence that is not cryptographically and causally bound to a successful Metro receipt?

## Loop

```text
SENSE
  -> CONTEXT
  -> CHOOSE (allow-list only)
  -> ACT
  -> MEASURE result
  -> METRO RECEIPT
  -> VERIFY result hash + provenance
  -> FSYNC HASH-CHAIN JOURNAL
  -> APPLY reward
  -> LIFETRA OBSERVATION
  -> restart / replay
```

## Durable source of truth

Each accepted reward becomes one `liminal.adaptive.reward-entry.v0.1` JSONL entry containing:

- the complete Metro receipt;
- the measured result payload;
- a SHA-256 hash of the receipt;
- the adaptive experience and its SHA-256 hash;
- the previous journal entry hash;
- the current entry hash.

The journal is appended and `fsync` is called before the in-memory policy is updated. On restart, the policy is rebuilt by replaying the journal in sequence. Every line is revalidated before its reward is applied.

## Fail-closed learning gate

A reward is rejected unless all of these hold:

1. receipt protocol is `metro.receipt.v0.1`;
2. receipt status is exactly `SUCCEEDED`;
3. receipt result hash is valid SHA-256;
4. the supplied result payload hashes to the receipt result hash;
5. action / route / receipt identities match the experience;
6. receipt executor matches the selected bounded action;
7. `result_ref` identifies that exact adaptive experience;
8. result action, context, reward and reward unit match the experience;
9. the experience's pre-policy state equals the durable learner state;
10. its post-policy state equals the deterministic one-reward transition.

If any check fails, no journal entry is written and the policy is not updated.

## Replay safety

The durable learner tracks three identities:

- `receipt_id`;
- `experience_id`;
- `action_id`.

Reapplying the exact same verified receipt after restart is an idempotent no-op. Reusing one of those identities with different evidence fails closed.

## Content-addressed proof chain

Lifetra observations for durable adaptive rewards carry four proof shapes:

```text
metro-receipt://<receipt-id>
adaptive-experience://<experience-id>
sha256://<measured-result-hash>
adaptive-journal://sha256/<journal-entry-hash>
```

This does not make the journal globally immutable. It makes mutation of already accepted local evidence detectable during replay.

## Run

```bash
go test ./...
go run ./cmd/hardware-adaptive-v03-demo
```

The demo deliberately restarts the learner after round 6, reapplies the last receipt, and tries to learn from an `UNKNOWN` receipt before continuing.

Expected proof flags:

```text
restart_state_preserved=true
duplicate_noop=true
unverified_rejected=true
```

## Claim ceiling

v0.3 demonstrates:

- persistent online-learning evidence across process restart;
- replay reconstruction of contextual UCB1 policy state;
- no double-application of one already accepted receipt;
- fail-closed rejection of `UNKNOWN`/non-success learning evidence;
- result-payload-to-receipt hash verification;
- hash-chained, fsynced local journal entries;
- content-addressed measurement and journal references in Lifetra observations.

v0.3 does **not** claim:

- model-weight training;
- arbitrary or privileged hardware control;
- crash consistency on every filesystem / storage controller / power-loss mode;
- distributed consensus or multi-writer journal safety;
- signed receipts or hardware-rooted attestation;
- globally optimal scheduling;
- protection from a privileged attacker who can rewrite both journal contents and trusted program binaries.

## Next bead

`v0.4 authority-bound adaptation`: require a signed/verified authority proof for the set of hardware actions that may be explored, and bind that authority epoch to every journal entry so learning cannot silently expand its own action space.
