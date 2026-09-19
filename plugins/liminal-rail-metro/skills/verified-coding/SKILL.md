---
name: verified-coding
description: Implement one public GitHub issue under a server-signed Liminal Rail coding contract and claim completion only after the exact PR head, changed paths, and required GitHub checks are verified.
---

# Verified coding workflow

Use this skill for a bounded coding task tied to one public GitHub issue.

## Before editing

1. Identify the exact public repository, issue number, and current base commit SHA.
2. Define stable `workflow_id`, `request_id`, and `action_id`.
3. Define the smallest allowed path prefixes that can satisfy the issue.
4. Define one or more required GitHub check names.
5. Call `liminal_coding_start` before changing code.
6. The start call must return `AUTHORIZED` and a server-signed contract.
7. Preserve the returned contract unchanged. Do not recreate, edit, re-sign, or synthesize it.
8. If start fails, do not continue the Liminal verified-coding workflow.

In static-proof mode, provide bounded choices and deterministic scores. These
scores are proof input, not model confidence.

## Implement

Use Codex's normal repository tools to edit code, run local tests, and create or
update the pull request. Liminal Rail does not grant write authority to GitHub
and does not bypass any normal tool approval.

Do not change files outside the contract's `allowed_path_prefixes`.
Do not silently change the contract's base SHA or required check names after
implementation begins.

## Verify

After the pull request exists and the required checks have completed, call
`liminal_coding_verify` with the exact original contract and pull request number.

Interpret the result strictly:

- `VERIFIED`: completion may be reported with the returned signed receipt.
- `HOLD / stale_base`: the PR is no longer based on the contracted base; create a new contract before continuing.
- `HOLD / path_outside_contract`: scope escaped; do not claim completion.
- `HOLD / required_check_missing`: required evidence is absent on the exact PR head.
- `HOLD / required_check_not_success`: pending, cancelled, skipped, neutral, or failed checks are not success.
- `HOLD / invalid_contract`: contract hash/signature/issuer binding failed.

The verifier uses public GitHub evidence for the exact PR head SHA. A previous
green run on another SHA does not satisfy the contract. When multiple runs have
the same required check name, the latest run for the exact head is authoritative.

## Boundaries

This workflow verifies bounded GitHub evidence, not arbitrary semantic
correctness. It does not support private repositories, GitHub write tokens,
automatic merge, shell execution on the MCP server, or exactly-once external
effects.
