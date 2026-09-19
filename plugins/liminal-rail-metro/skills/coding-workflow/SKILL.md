---
name: coding-workflow
description: Verify one user-authorized GitHub coding task with an owner-pinned test contract, actual local tests, and a patch-bound Metro receipt. Use for coding acceptance and VERIFIED/HOLD; not for automatic merge or deployment.
---

# Patch-bound coding workflow v0.3 (preview)

This extends the existing Liminal Rail plugin. The HTTPS MCP server remains v0.2;
this workflow uses a separately reviewed local `metro-check` executable. Never
expose it as an anonymous HTTP shell tool, download an arbitrary binary, or treat
this skill as authority to run untrusted code outside the user's sandbox.

## Before coding

1. Read the actual GitHub issue and target repository through the available
   GitHub tools. An issue URL alone is not a fetched issue or acceptance criteria.
2. Ask the owner/QA to approve a `metro.coding.contract.v0.3`: repository, issue
   URL, stable action_id, full base commit SHA, fixed `go-test-vet-v1` profile,
   and required test names plus exact test-file SHA-256. Keep the approved copy
   outside the candidate checkout. Do not alter the contract or weaken tests to
   obtain a pass. The repository's Issue #14 example is a concrete starting case,
   not a contract that may be reused for unrelated tasks.
3. When connected, use `liminal_status` and `liminal_decide` for bounded planning
   only. A selection from the static proof provider is not model intelligence,
   test execution, or authorization. If a gate requires approval, stop; do not
   relabel an effectful action as safe or use another tool to bypass the gate.
4. Implement the patch in a separate branch/worktree. Preserve AGENTS.md and
   existing checks. Commit the proposed patch locally before verification; do
   not push, merge, deploy, or close the issue without the relevant authorization.

## Verify

Use an owner-reviewed `metro-check` binary built outside the candidate checkout.
Its fixed profile runs actual `go test -json -count=1 ./...` and `go vet ./...`.
Tests execute code: obtain explicit authorization and use a disposable Linux or
macOS runner with no production credentials. This CLI is NOT a sandbox.

Serialize the contract and use file paths as separate command arguments. Never
interpolate issue text, source code or agent-generated commands into a shell.

Run `metro-check check --repo <checkout> --contract <approved-contract>
--proof <new-proof-file> --allow-tests` only for the expressly approved attempt.
Both contract and proof must be outside the checkout. The proof file must be new.

Capture the emitted `evidence_sha256` in an owner/CI-controlled location, then
call `metro-check verify --repo <same-checkout> --contract <same-contract>
--proof <same-proof-file> --expected-evidence-sha256 <owner-pinned-digest>`.
Do not derive a replacement expected digest from agent-edited evidence.

## Report and stop conditions

- `VERIFIED` means the required checks actually passed for that exact tested
  tree in the trusted runner. It does not prove all issue requirements or general
  correctness. Report head SHA, patch/worktree digests, test IDs, and proof pin.
- `HOLD / STALE_PATCH`: code, tracked/untracked/ignored content, index state or
  HEAD changed after tests. STOP. Do not automatically rerun, repair or retry.
- `HOLD / TEST_FAILED`, `TEST_TIMEOUT`, `MISSING_REQUIRED_TEST`,
  `ACCEPTANCE_CHANGED`, `OUTPUT_LIMIT` or `EVIDENCE_MISMATCH`: report the reason;
  never let an agent statement such as "fixed" override the result.
- A timeout remains uncertain; no automatic repeat of potentially effectful work.
- Generic `liminal_verify` checks receipt consistency only. It must not replace
  this local test verdict. Do not send source code or test logs to the public
  MCP service by default.

This workflow does not enforce a global gate on Codex's other tools. Report a
live Codex authoring step as NOT_RUN unless an actual authenticated Codex session
ran; a scripted regression harness is not a Codex-authored patch. Leave the issue
and pull request open for independent QA. No auto-merge, auto-deploy, or hidden
fourth attempt.
