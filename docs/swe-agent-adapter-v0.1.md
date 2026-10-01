# SWE-agent adapter v0.1 — exact-candidate contribution receipts

This adapter is a narrow interoperability proof for the workflow proposed in
[SWE-agent issue #1524](https://github.com/SWE-agent/SWE-agent/issues/1524).
It does **not** modify SWE-agent and it does not claim to implement SWE-agent's
queue selection, claim leases, PR publication, or merge authority.

The v0.1 boundary is deliberately small:

```text
SWE-agent outputs
  patch + trajectory + config + environment + evaluation
                         |
                         v
                    metro-swe pack
                         |
                         v
              contribution_receipt.json
                         |
                exact candidate check
                         |
                         v
                 read-only reviewer
                         |
                         v
                  review_receipt.json
```

## What is bound

`metro-swe pack` hashes and binds:

- repository and issue URL;
- run ID;
- exact base and candidate head SHA;
- candidate patch bytes;
- trajectory bytes;
- agent config bytes;
- environment/setup description bytes;
- evaluation commands/results bytes;
- reported model calls, tokens, cost, and termination reason;
- optional movement telemetry: wall time, time to first evidence, tool calls,
  and measured context bytes moved.

The receipt itself is SHA-256 bound. `metro-swe verify` re-reads every artifact
and rejects a changed head or changed bytes as `STALE_CANDIDATE`.

`metro-swe review` first performs that exact-candidate verification and only
then emits a review receipt bound to the contribution receipt hash, candidate
head, patch digest, reviewer identity, one of the four issue-proposed verdicts,
and review evidence bytes.

Allowed review verdicts are:

```text
approve
changes_required
duplicate
blocked
```

The adapter records `reviewer_mode=read-only` as a required contract property.
It does not provide an OS sandbox or independently attest that the reviewer
process could not write to the checkout.

## Evidence-velocity telemetry

The adapter records raw measurements rather than claiming a universal speedup:

```text
wall_time_ms
time_to_first_evidence_ms
tool_calls
context_bytes
```

These fields make it possible to compare agents later using the same receipt
boundary. They are observations supplied by the runner, not trusted proof of
model performance by themselves.

## Example

```bash
go build -o metro-swe ./cmd/metro-swe

./metro-swe pack \
  -issue https://github.com/SWE-agent/SWE-agent/issues/1524 \
  -repo SWE-agent/SWE-agent \
  -run my-run-id \
  -base <40-char-base-sha> \
  -head <40-char-candidate-sha> \
  -patch candidate.patch \
  -trajectory run.traj \
  -config config.yaml \
  -environment environment.json \
  -evaluation evaluation.json \
  -termination submitted \
  -wall-ms 42000 \
  -first-evidence-ms 7000 \
  -tool-calls 11 \
  -context-bytes 64000

./metro-swe verify \
  -receipt contribution_receipt.json \
  -head <same-candidate-sha> \
  -patch candidate.patch \
  -trajectory run.traj \
  -config config.yaml \
  -environment environment.json \
  -evaluation evaluation.json
```

## Claim ceiling

The CI fixture for this adapter is a **contract fixture tied to the public
SWE-agent #1524 requirements**. It is not a live authenticated SWE-agent run,
not an upstream SWE-agent integration, and not evidence that an independent
reviewer process was sandboxed read-only.

A credible upstream demonstration still requires a real SWE-agent trajectory on
a real repository issue, exact candidate checkout verification, and a separate
reviewer execution. Only after that run should the resulting receipt be offered
as evidence in the upstream thread.

## Deferred scope

The following #1524 requirements are intentionally deferred from v0.1:

- expiring claim leases and duplicate-claim coordination;
- repository-owned candidate selection policy;
- GitHub check/comment claim storage;
- automatic PR publication;
- merge, deploy, issue-priority, or roadmap authority.
