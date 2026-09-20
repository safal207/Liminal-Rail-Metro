# Moltbook Station v0.1

Moltbook Station v0.1 is a **local integration boundary proof** for routing one
verified external agent identity into one bounded, non-mutating Metro action.

It does not connect to the live Moltbook service.

## Proven flow

```text
injected identity verifier
  -> verified normalized identity
  -> VERIFY_EVIDENCE
  -> bounded Metro Packet
  -> allowlisted target: agentproof
  -> injected evidence verifier
  -> Metro Receipt
```

The station reuses the existing Metro packet, route, hashing, receipt, and
receipt-verification primitives.

## Reproduce

Run the focused proof:

```bash
go test ./integrations/moltbook -v
```

Run the repository-wide Go regression suite:

```bash
go test ./...
```

CI also runs `go test ./...` in the `moltbook-station` workflow.

## Fail-closed contract

v0.1 accepts only:

- intent `VERIFY_EVIDENCE`;
- target `agentproof`;
- `external_effects=false`;
- a non-empty stable `action_id`;
- a non-empty evidence reference;
- an identity that the injected verifier returns as verified.

Invalid identity, unknown intent, target confusion, or an external-effect
request is rejected before evidence dispatch.

The action ID is consumed before the evidence verifier is called. A verifier
error therefore cannot cause an automatic same-process redispatch of the same
action ID.

## Receipt binding

The normalized identity is SHA-256 hashed and the resulting identity reference
is placed inside the packet inputs. Existing Metro receipt construction hashes
those inputs, so changing the identity reference after execution breaks receipt
verification.

The receipt also binds the action ID, route ID, selected executor, input hash,
and result hash through the existing Metro receipt verifier.

## Claim ceilings

This version deliberately does **not** prove any of the following:

- live Moltbook authentication or token validation;
- token issuer, audience, expiry, revocation, or key-rotation handling;
- Moltbook posting, commenting, voting, following, feed reading, or DMs;
- autonomous agent discovery;
- production AgentProof connectivity;
- durable or distributed duplicate suppression;
- cross-process or cross-replica exactly-once execution;
- persistence across process restart;
- source authenticity of an evidence reference;
- that a successful verifier call proves a real-world external effect;
- authorization based on Moltbook karma, reputation, follower count, or other
  social metadata;
- GitHub writes initiated from Moltbook content;
- payments, wallets, credentials, shell execution, or other external effects.

The duplicate-action guard is **process-local memory only**. It proves that two
requests handled by the same Station instance cannot dispatch the same
`action_id` twice. Restarting the process resets that guard.

The identity verifier and evidence verifier are interfaces. CI uses deterministic
fakes. A real Moltbook Identity adapter is follow-up work and must preserve the
same fail-closed boundary.

## Security invariants

```text
identity != authority
route != execution
receipt != source authenticity
social reputation != trust
UNKNOWN != success
```

Moltbook-originated content must remain untrusted input. v0.1 does not execute
arbitrary prompts or commands from that content.

## Follow-up boundary

Only after MOLT-001 is independently reviewed may a later change add a real
Moltbook Identity adapter. That adapter should verify issuer/audience/expiry and
bind the resulting normalized identity metadata without placing raw credentials
or long-lived secrets into packets, receipts, logs, fixtures, Git history, or
CI artifacts.
