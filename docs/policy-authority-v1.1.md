# Liminal Rail v1.1 — Signed Policy Authority / Anti-Downgrade

v1.0 proved that a supplied `TrustPolicy` is enforced fail-closed before guarded execution or learning. v1.1 closes two higher-level authority gaps:

> An operation must not choose its own minimum trust requirements.

> A previously accepted stronger policy-authority head must not silently roll back to an older still-valid chain.

## Threat model

Two downgrade classes matter:

1. **policy substitution** — a sensitive operation asks for a weaker but valid policy;
2. **classification / chain rollback** — a sensitive action is mislabeled as a weak operation class, or a restart re-opens an older valid manifest chain after a stronger generation was accepted.

v1.1 therefore binds the execution contract, policy selection and accepted chain head separately.

```text
action kind + target + side-effect flag + exact inputs
    |
    v
signed manifest binding
    |
    +--> derived operation class
    +--> exact TrustPolicy hash
    |
    v
durable accepted chain head
    |
    v
content-addressed Authorization
    |
    v
v1.0 TrustGate
    |
 ALLOW / DENY
```

## Signed execution binding

Each canonical manifest binding contains:

```text
operation_class
action_kind
target
side_effect
complete TrustPolicy
```

The execution contract `action_kind + target + side_effect` must be unique inside one manifest, so a caller cannot choose among multiple classes for the same contract.

At runtime the resolver does **not** select policy from a caller-supplied class. It resolves the concrete action contract and derives the signed operation class. The resulting `OperationDescriptor` additionally commits to the SHA-256 hash of the exact action inputs.

A declared class may be supplied only as an assertion. If it differs from the class derived from the signed execution binding, the gate fails before a `TrustGate`, dispatcher or learner is reachable.

## Signed manifest and policy binding

The complete manifest is content-addressed and signed with Ed25519. Trust does not come from a self-valid signature alone: the first signed manifest is pinned by a small `TrustRoot` containing the exact root manifest hash, signed-manifest hash, issuer identity and issuer key fingerprint.

The runtime `Authorization` commits to:

- authority id and generation;
- derived operation class;
- action kind, target and side-effect flag;
- exact input hash and operation-descriptor hash;
- exact TrustPolicy hash;
- manifest and signed-manifest hashes;
- issuer key id and rotation hash;
- durable accepted chain-head hash.

A weaker caller-selected policy is rejected before the v1.0 gate exists.

## Durable anti-rollback head

`OpenDurableResolver` requires a persistent chain-head state file. The accepted head contains:

```text
authority_id
generation
signed_manifest_hash
rotation_hash
head_hash
```

The write path syncs a temporary file, renames it over the head, then syncs the
parent directory. Linux tests cover this path. On Windows the final directory
sync currently fails after the rename; see [issue #45](https://github.com/safal207/Liminal-Rail-Metro/issues/45).
Do not treat a Windows error as proof that the previous head is still on disk.

Cooperating processes that share a state path serialize the entire read, check,
and write through a persistent sibling `.lock` file. Keep the state directory
trusted and on a local filesystem, keep the lock file in place, and use
`OpenDurableResolver` for every writer. Failure to acquire or release the lock
fails closed. The lock does not protect against a process that directly edits
the head or replaces files in the state directory; network filesystem locking
and power-loss durability outside the tested environment remain unverified.

On restart:

- a lower generation is rejected as `ErrChainRollback`;
- a same-generation different head is rejected as `ErrChainFork`;
- a higher candidate must contain the exact previously accepted head before it can advance state;
- an exact restart preserves the same head and authorization hashes.

Deleting or tampering with the local head file is outside this proof's tamper-resistance claim; malformed state fails closed.

## Bound dispatch

`PolicyAuthorityRuntime` owns the trusted handlers registered during runtime setup. `PolicyAuthorityGate` stores an immutable copy of the concrete action and its verified operation descriptor, and `gate.Execute()` accepts **no call-time callback or dispatcher**. It selects the pre-registered handler only from the bound action kind, target, and side-effect flag, then passes that handler the exact bound operation.

The handler/executor remains a trusted execution boundary: this bead proves that the caller cannot swap in an arbitrary execution callback after authorization and that the trusted handler receives the exact descriptor. It does not semantically verify that a compromised trusted handler physically performed only the intended effect.

## Rotation and weakening semantics

Manifest rotation is explicit and signed by the currently trusted issuer. v1.1 requires exactly the next generation and allows issuer-key rotation.

For existing operation classes, a transition is treated as weakening if it:

- removes the operation;
- drops a previously required trust property; or
- changes the signed execution contract (action kind, target or side-effect flag).

Silent weakening is rejected. Intentional weakening requires `allow_weakening=true` and a signed canonical `weakened_operations` list.

## Receipt and Lifetra binding

Before Metro hashes an allowed result, v1.1 binds:

```text
policy_operation_class
policy_action_kind
policy_target
policy_side_effect
policy_input_hash
policy_operation_descriptor_hash
policy_authority_policy_hash
policy_manifest_hash
policy_signed_manifest_hash
policy_authority_issuer_key_id
policy_authority_rotation_hash
policy_authority_chain_head_hash
policy_authorization_hash
```

Lifetra carries separate refs for the signed manifest, rotation, accepted chain head and authorization, alongside the existing v1.0 policy/evidence/decision refs.

## Executable proof

The dedicated workflow reconstructs the previous trust chain live:

```text
GitHub OIDC
 -> v0.8 external-identity proof
 -> Sigstore keyless publication + verify
 -> v1.0 TrustEvidence / TrustPolicy proof
 -> v1.1 signed execution + policy authority
```

It must demonstrate:

- a hardware-critical action falsely declared as `bounded.cpu.sha256` is rejected before dispatch/learning;
- a weaker policy substituted for the correctly derived hardware class is rejected;
- the exact hardware policy independently DENYs the same software-bound evidence;
- the caller cannot supply an execution callback at `Execute()` time and the pre-registered trusted runtime handler receives the exact authority-bound descriptor;
- restart preserves the accepted chain head and authorization;
- reopening the older still-valid root-only chain after accepting generation 2 fails closed;
- a same-generation fork fails closed in unit tests;
- tampered manifest/signature fails;
- silent weakening fails while explicitly signed weakening is visible;
- Metro and Lifetra bind descriptor, chain-head and authorization identities.

## Claim ceiling

v1.1 demonstrates signed execution classification, policy-selection authority and durable anti-rollback behavior in the guarded Liminal Rail path.

It does **not** demonstrate:

- OS/kernel mandatory access control;
- hardware-rooted or tamper-proof storage of the accepted head or policy-authority keys;
- semantic correctness of a compromised dispatcher/executor;
- real-world identity of the issuer;
- safety after compromise of the pinned policy-authority root;
- impossibility for arbitrary future code to bypass the guarded API.
