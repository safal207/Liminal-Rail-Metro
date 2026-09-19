# Coding workflow v0.3 — patch-bound local verification (preview)

## One real case

Issue #14 records a real defect in `internal/metro/metro.go` at base
`3600fae8cf1f7bf1ae9ade429e154aab38ff0f2c`: `Verify` did not compare
`route.action_id` to `packet.action_id`. A matching receipt could coexist with a
route for another action. The new regression must fail on the original source
and pass after the three-line binding fix. This is the first acceptance case.

The new `coding-workflow` skill and `metro-check` Go CLI provide:

```
GitHub issue + owner-approved contract
  -> Codex/user proposes and commits one patch
  -> explicit local test authorization
  -> snapshot HEAD / Git tree / base..HEAD patch / all file bytes / index status
  -> fixed go-test + go-vet profile
  -> named tests and test source hashes checked
  -> matching post-test snapshot
  -> Metro validation receipt
  -> owner-pinned evidence digest + current-tree verification
  -> VERIFIED or HOLD
```

The existing public MCP server does NOT acquire shell execution. Its version and
three tools are unchanged. Planning with `liminal_decide` is advisory; the
static provider is not a model, and confidence is not authority. `metro-check`
marks local test execution as effectful and requires `--allow-tests`.

## Supported scope and prerequisites

Linux/macOS, Git SHA-1 repositories, Go modules, a clean committed candidate,
and a reviewed local verifier. Tests run executable code; use a disposable
runner without secrets. Download pinned Go dependencies beforehand; the checker
sets `GOPROXY=off`, `GOWORK=off`, `GOENV=off`, `GOTOOLCHAIN=local`, and
`GOFLAGS=-mod=readonly`. Toolchain and caches must live outside the candidate.
These settings are not an OS sandbox or a guarantee that test code has no network.

The contract fixes the repository, existing issue URL, base SHA, action identity,
profile, and exact required test file hashes/names. The runner does not fetch or
invent issue requirements; the skill/owner performs that read and approval.
Only `go test -json -count=1 ./...` and `go vet ./...` may run. There is no caller
shell command, callback, retry loop, or remote executor.

## Run from an owner-reviewed checkout

Build `metro-check` from reviewed tooling and keep the executable outside the
candidate. Copy `examples/coding-issue14.contract.json` into an owner-controlled
location, review it, and do not let the coding agent change that copy.

```bash
go build -o ../metro-check ./cmd/metro-check
cp examples/coding-issue14.contract.json ../issue14.contract.json
# Candidate changes must already be committed in this checkout.
../metro-check check --repo "$PWD" \
  --contract ../issue14.contract.json --proof ../issue14.proof.json --allow-tests
```

Capture the printed `evidence_sha256` outside the agent's writable workspace.
Supply THAT digest to `verify` (replace the token below with the captured value):

```bash
../metro-check verify --repo "$PWD" \
  --contract ../issue14.contract.json --proof ../issue14.proof.json \
  --expected-evidence-sha256 OWNER_PINNED_SHA256
```

`check` reserves a new proof file before any execution and refuses overwrite.
`verify` does not execute tests. CLI exit codes: 0 VERIFIED, 2 HOLD, 1 invalid
invocation or preflight/IO failure. A failed preflight may leave an empty reserved
file; this is not proof and is not silently retried. A new attempt needs explicit
owner approval and a new proof file, not an automatic retry.

## Evidence and rejection rules

The JSON proof carries the owner contract/digest, pre/post snapshots, exact
command arguments, actual exit codes, bounded stdout/stderr plus hashes, runner
Go/platform version, timestamps and Metro Packet/Route/Result/Receipt. Success
receipts are issued only for accepted test runs, not for a model's claim of success.
The pinned output digest binds these bytes. The CI artifact additionally records
the built verifier SHA-256 and checkout SHA.

`VERIFIED` is scoped to the required checks, not proof that all defects are absent.
`STALE_PATCH` takes precedence when a snapshot changes. Failed tests stay
`TEST_FAILED` even if `agent_claim` says `fixed`. Skipped or absent required named
tests are not accepted; modifying their pinned source yields `ACCEPTANCE_CHANGED`.
Timeout, missing checks, output truncation, changed logs, missing/changed expected
pins, invalid protocols or mismatched receipts cannot produce acceptance.

All files outside the root `.git` entry participate in the content snapshot,
including ignored/untracked files. Git status also binds index changes. Snapshot
limits: 10,000 files, 8 MiB per file and 64 MiB total. Symlinks, nested repositories
and special files are rejected. Store evidence, generated files and caches outside
the checkout. Concurrent edits during verification are unsupported: stop the
coding agent first. A malicious edit-and-restore between snapshots is outside the
claim; immutable/disposable execution infrastructure is needed for that threat.

## Trust and claim ceiling

The owner/QA-approved contract, verifier binary, Go/Git tools, runtime, and
out-of-workspace evidence pin form the trust boundary. This prototype does not
protect against a malicious process that can alter the verifier or both the
proof and its expected pin. The digest is not a signature, timestamp service,
remote attestation, proof of author identity, or a credential. The Git remote
string is checked for consistency, not treated as cryptographic repository auth.

An arbitrary agent can still fabricate a self-consistent unsigned Metro receipt;
therefore the generic HTTPS `liminal_verify` response is NOT sufficient coding
acceptance. Do not upload repository contents/test logs to the anonymous service
by default. We do not claim a live authenticated Codex authoring session ran when
only local/CI checks and an isolated regression harness ran.

No auto-merge, auto-deploy, issue closure, approval resumption, Jev integration,
generic shell MCP, or guarantee of exactly-once external effects is introduced.
Existing Metro/Lifetra workflows remain required regressions. Keep this preview
PR open for independent QA before production changes.

## Reproducible proof

`.github/workflows/coding-workflow-v0.3.yml` runs local boundary/race checks,
builds the verifier outside the checkout, extracts the exact base implementation,
and runs `scripts/coding-workflow-proof.py`. The isolated matrix demonstrates
baseline TEST_FAILED, patched VERIFIED, STALE_PATCH, EVIDENCE_MISMATCH, and refused
proof-file reuse using actual Go subprocesses. It is not a mocked LLM benchmark.
A separate step checks the ACTUAL full candidate repository once and verifies its
receipt without rerunning tests. Artifacts retain both scopes distinctly.

Official integration references (checked 2026-09-19):
- https://developers.openai.com/plugins/build/skills
- https://developers.openai.com/codex/noninteractive
- https://developers.openai.com/codex/mcp

The supported integration is a packaged skill plus a local CLI invoked inside
Codex's existing permissions/sandbox. No undocumented Codex plugin runtime API
or credential is required for the checker itself.
