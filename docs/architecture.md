# Liminal Rail Metro v0.1 Architecture

## Purpose

Liminal Rail Metro explores a narrow infrastructure primitive for AI-agent systems: move one bounded action from a source agent to an execution target through an explicit routing step, while preserving stable action identity and producing independently checkable execution evidence.

The protocol separates three things that are often blurred together in agent frameworks:

1. **Intent** — what bounded action is requested.
2. **Routing** — where that action should go.
3. **Execution evidence** — what target actually handled it and what result was produced.

## Control flow

```text
Source Agent
    |
    | metro.packet.v0.1
    v
+-------------+
| Metro Gate  |
+-------------+
    |
    v
+-------------+
|   Router    |  deterministic / policy / model / human
+-------------+
    |
    | metro.route.v0.1
    v
+-------------+
|  Executor   |
+-------------+
    |
    | metro.receipt.v0.1
    v
Verifier / next hop
```

## Packet

A packet is created **before dispatch** and receives a stable `action_id`.

The packet contains:

- bounded goal;
- action kind and inputs;
- explicit allowed targets;
- optional context references instead of copied history;
- execution constraints;
- optional provenance link to a previous receipt.

The packet is not proof that anything executed.

## Route decision

A route decision binds an `action_id` to one `selected_target` and records how the decision was made.

A router may be:

- deterministic;
- policy-based;
- a fast classifier / System-1 model;
- a larger reasoning model;
- a human approval step.

The selected target must be one of the packet's allowed targets. A route decision is still not proof of execution.

## Receipt

A receipt binds execution evidence back to both the original action and the selected route.

The v0.1 status model is intentionally explicit:

- `SUCCEEDED` — execution completed and result evidence exists;
- `FAILED` — execution completed with failure evidence;
- `REJECTED` — execution did not proceed because a policy, permission, or target rejected it;
- `UNKNOWN` — the system cannot currently prove whether execution completed.

`UNKNOWN` matters for side-effecting actions. A timeout or lost response must not silently become authority to create a new logical action or blindly redispatch the old one.

## Hashes

The demo uses SHA-256 over canonical JSON as a lightweight independently recomputable evidence mechanism.

This is **integrity evidence**, not yet identity or trust evidence. v0.1 does not claim that a hash proves who executed an action. Signed receipts, key management, attestations, or external trust roots belong to later protocol work.

## Fast routing and System 1

Liminal Rail Metro does not require one particular model.

A fast classifier such as a Jev-like System-1 model can be used when the problem is limited to choosing among already-valid actions or targets. More expensive reasoning can remain on the control plane for planning, ambiguity resolution, or high-risk choices.

The protocol boundary is deliberately model-agnostic:

```text
planner -> packet -> router -> route -> executor -> receipt
```

## Composing hops

One verified hop can become provenance for the next:

```text
Research Agent
    |
    v
Code Agent ---- receipt A
    |
    | previous_receipt_ref = receipt A
    v
QA Agent ------ receipt B
```

This supports a core project principle:

> Each external proof can reduce the amount of trust the next hop must assume.

It does not mean later agents should blindly trust prior receipts. They must verify whatever evidence their policy requires.

## Failure boundaries

v0.1 distinguishes at least these states:

```text
route selected != action dispatched

action dispatched != executor accepted

executor accepted != action completed

action completed != caller received response

response received != result independently verified
```

Collapsing these states is a common source of duplicate side effects and false success claims.

## Claim ceiling

v0.1 proves only a local protocol shape and a reproducible toy demonstration.

It does **not** prove:

- exactly-once execution;
- distributed consensus;
- durable recovery across arbitrary failures;
- cryptographic executor identity;
- optimal routing;
- lower latency or token cost than existing systems;
- production safety.

Those claims require separate benchmarks, failure injection, persistence, and external verification.

## Next evidence milestones

A sensible progression is:

1. reproducible local packet -> route -> receipt demo;
2. schema validation in CI;
3. duplicate-dispatch and lost-response tests;
4. durable action registry;
5. externally signed receipts;
6. benchmark fast routing vs full-LLM routing;
7. multi-runtime interoperability.

The project should advance one independently verifiable claim at a time.
