# Moltbook Station v0.1

Moltbook Station v0.1 is a **local integration boundary proof** for routing one
verified external agent identity into one bounded, non-mutating Metro action.

It does not connect to the live Moltbook service.

## Proven flow

```text
injected identity verifier
  -> verified normalized identity
  -> bounded Metro Packet
  -> bound decision-plane authority request
  -> explicit ALLOW / AUTO_ROUTE only
  -> allowlisted target: agentproof
  -> injected evidence verifier
  -> Metro Receipt
  -> Moltbook VerifyResult
```

The station reuses the existing Metro packet/receipt primitives and the existing
`internal/decisionplane` binding and gate contract.

## Reproduce

Run the focused proof:

```bash
go test ./integrations/moltbook -v
```

Run the race check for the integration boundary:

```bash
go test -race ./integrations/moltbook
```

Run the repository-wide Go regression suite:

```bash
go test ./...
```

CI runs formatting, the full Go suite, and focused race checks in the
`moltbook-station` workflow.

## Fail-closed contract

v0.1 accepts only:

- intent `VERIFY_EVIDENCE`;
- target `agentproof`;
- `external_effects=false`;
- a non-empty stable `action_id`;
- a non-empty evidence reference;
- an identity that the injected verifier returns as verified;
- an authority result explicitly equal to `ALLOW` or `AUTO_ROUTE`.

Invalid identity, unknown intent, target confusion, or an external-effect
request is rejected before authority evaluation or evidence dispatch as
appropriate.

## Authority boundary

Identity and routing do not confer execution authority.

The default MOLT-001 authority adapter uses the existing bounded fast decision
plane:

```text
Metro Packet
  -> decisionplane.NewRequest
       packet_hash
       action_id
       state_hash
       choices_hash
  -> Provider.Decide
  -> decisionplane.ApplyDecision
  -> AUTO_ROUTE or no dispatch
```

Only one choice is offered in v0.1: the already allowlisted `agentproof`
target. The provider cannot invent another target.

Before evidence dispatch, the station independently validates that the authority
decision is bound to:

- the current full packet SHA-256;
- the same `action_id`;
- the requested allowlisted target;
- a non-empty authority provider ID;
- a route bound to the same action and target.

A disposition other than explicit `ALLOW` or `AUTO_ROUTE` fails closed.
This includes `DENY`, `UNKNOWN`, `ESCALATE_SYSTEM2`, and
`REQUIRE_APPROVAL`.

A decision-plane validation error also fails closed, including mismatched
`packet_hash` or `action_id`. Evidence verification is not called in any of
these cases.

The action ID is consumed only **after** authority has permitted dispatch and
immediately **before** the evidence verifier is called. A verifier error
therefore cannot cause an automatic same-process redispatch of the same action
ID, while a request rejected before dispatch does not consume the action.

## Receipt binding

The normalized identity is SHA-256 hashed and the resulting identity reference
is placed inside the packet inputs.

Before receipt creation, the station hashes the **entire Metro Packet**, the
complete authority decision, and the complete verifier result. The receipt
result contains:

```text
identity_ref
packet_hash
authority_hash
authority_result
target
verdict
evidence_sha256
verification_hash
```

The generic Metro receipt SHA-256 binds that receipt result through
`ResultHash`.

Moltbook `VerifyResult` independently:

1. recomputes the complete packet hash;
2. revalidates the authority decision against that packet/action/target;
3. checks that the returned route is exactly the authority-bound route;
4. recomputes and verifies the authority hash;
5. checks identity, authority disposition, target, verdict, evidence hash, and
   full verifier-result hash;
6. delegates the underlying action-input, route, executor, and result-hash
   checks to `metro.Verify`.

Therefore changes to fields outside `Action.Inputs`, including
`SourceAgent`, `Constraints.SideEffect`, or `AllowedTargets`, invalidate
MOLT-001 verification. Mutating the authority result also invalidates the
receipt binding.

`Receipt.Status=SUCCEEDED` means the **verification operation completed and
its bound result was receipted**. It does not mean an external real-world action
was independently proven successful. The verifier verdict remains a separate
field and may represent a non-success outcome.

Raw identity tokens are not copied into packets or returned station results.

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

The decision-plane provider is an injected bounded authority component. CI uses
a deterministic `StaticProvider`; this is not a claim that Moltbook itself is
an authority source.

The duplicate-action guard is **process-local memory only**. It proves that
repeated requests handled by the same Station instance cannot dispatch the same
`action_id` twice. Restarting the process or using a second Station instance
resets/bypasses that local guard. MOLT-001 therefore makes **no distributed or
durable replay-protection claim**.

An ambiguous verifier execution is not automatically retried by the same
Station instance. Safe recovery across restarts or replicas is explicitly out
of scope and would require a durable shared atomic-claim store.

The identity verifier and evidence verifier are interfaces. CI uses
deterministic fakes. A real Moltbook Identity adapter is follow-up work and must
preserve the same fail-closed boundary.

## Security invariants

```text
identity != authority
route != authority
authority != execution
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
