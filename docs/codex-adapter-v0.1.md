# Codex adapter v0.1

## Official integration check (2026-09-17)

OpenAI documents plugins containing skills, MCP connections, or both. The current
portable format uses a root `plugin.json`; the scaffolded
`.codex-plugin/plugin.json` remains a supported compatibility format.

Sources checked before implementation:

- [OpenAI: Build plugins](https://learn.chatgpt.com/docs/build-plugins)
- [OpenAI: Package your plugin](https://developers.openai.com/plugins/build/plugins)
- [OpenAI: MCP for Codex](https://learn.chatgpt.com/docs/extend/mcp?surface=cli)
- [OpenAI: CLI use cases](https://developers.openai.com/codex/use-cases)

Decision: use the supported **skill + CLI/tool boundary** for this narrow version.
The compatibility package contains one skill invoking an ordinary Go executable.
MCP STDIO is another documented option, but v0.1 does not implement MCP, claim to
be an MCP server, or use a private Codex action API. No OpenAI API calls, model
credentials, Jev contracts, hooks or interception of other Codex tools are needed.

## Build and run

Requires Go 1.23 or newer. No new Go modules or runtime dependencies are added.
From the repository root:

```sh
go build -o metro-codex ./cmd/metro-codex
./metro-codex run < examples/codex-hash-text.json > response.json
./metro-codex verify < response.json
./metro-codex run < examples/codex-approval.json > approval.json
./metro-codex verify < approval.json
```

PowerShell 7 (use an explicit UTF-8 pipeline encoding):

```powershell
$OutputEncoding = [System.Text.UTF8Encoding]::new($false)
go build -o metro-codex.exe ./cmd/metro-codex
Get-Content -Raw -Encoding utf8 examples/codex-hash-text.json |
  ./metro-codex.exe run | Set-Content -Encoding utf8 response.json
if ($LASTEXITCODE -ne 0) { throw 'Metro rejected the action' }
Get-Content -Raw -Encoding utf8 response.json | ./metro-codex.exe verify
```

The CLI reads one JSON value to EOF and exits. stdin must be closed by the caller;
it is not a persistent request server. `run` returns exit 0 for a valid policy
outcome, including `REQUIRE_APPROVAL`; always inspect `evidence.gate.disposition`
and `evidence.dispatched`. Invalid input, invalid decisions and invalid proofs
return exit 1 with stderr diagnostics and no success output. As with any CLI,
an interrupted output write can leave a partial file; require successful exit
and verification before consuming it.

For use from Codex, run `go install ./cmd/metro-codex` from the trusted checkout
and put Go's binary directory on the PATH visible to Codex. Build/install the
binary explicitly; plugin installation does not compile or download executors.
The plugin source is `plugins/liminal-rail-metro`. It can be added to a Codex local
marketplace using the documented Plugin Creator workflow, then installed and
tested in a new task. The skill can also be used as an ordinary project skill by
copying `skills/metro-action` from the plugin into the project's `.agents/skills`.
No user-level configuration is changed by building or testing this repository.

## Contract and trust boundaries

```text
Codex action JSON (untrusted)
  -> strict bounded decode / fixed action catalog
  -> Metro Packet (stable action_id; adapter-owned target)
  -> Decision Request (request_id + packet_hash + state_hash + choices_hash)
  -> isolated deterministic provider input
  -> unchanged v0.6 validation and default gate
  -> AUTO_ROUTE -> local SHA-256 -> real local receipt -> binding verification
  -> REQUIRE_APPROVAL / ESCALATE_SYSTEM2 -> no route, result or receipt
  -> response with inline evidence and SHA-256 evidence_hash
```

`metro.codex.action.v0.1` is this repository's contract, not an OpenAI API.

| Field | Meaning |
| --- | --- |
| `protocol` | Exactly `metro.codex.action.v0.1` |
| `action_id`, `request_id` | Required identifiers, 1-128 ASCII letters/digits/`._:-`; first character alphanumeric |
| `kind` | `hash_text` or `external_action` only |
| `text` | Required for `hash_text`; UTF-8 text, at most 32768 bytes |
| `description` | Required for `external_action`; nonblank, at most 4096 bytes |
| `side_effect` | Required boolean; external actions must use `true` |
| `state` | Optional string-to-string object, at most 64 entries; keys <=128 bytes, values <=1024 bytes |

The entire UTF-8 input is capped at 65536 bytes, including JSON escaping and
whitespace. Duplicate keys at every depth, unknown/miscased fields, nulls,
trailing JSON, invalid UTF-8/UTF-16 escapes and excessive nesting fail closed.
Do not put shell arguments or file references in command text; serialize values
into a JSON file and redirect stdin. The adapter performs no shell execution,
file reads/writes, URL fetches or external dispatch.

`hash_text` maps to `codex-local-sha256`. Its only output is SHA-256 of the exact
decoded UTF-8 text (without normalization). `external_action` maps to a bounded
approval-only choice and has **no executor**. `hash_text` with `side_effect:true`
also requires approval. A false external effect flag is rejected before provider
invocation. There is no caller-controlled target, confidence, provider endpoint,
policy, `approved` flag or approval-resume operation.

The default provider is deterministic with exactly one admissible choice. A
probability/confidence of 1 expresses this fixed mapping, not model accuracy.
Provider inputs are copied before invocation; results must bind the trusted
original request and the fixed provider identity. The existing v0.6 gate re-hashes
packet/state/choices and validates the full distribution before making a route.
The local executor separately refuses any effectful packet or wrong target.

## Proof and receipt ceiling

`metro.codex.response.v0.1` carries the Packet, Decision Request, Decision,
GateResult, dispatch flag and (only after local execution) result and Metro
Receipt. `evidence_hash` is `metro.HashJSON(evidence)` using Go JSON serialization
and SHA-256; it is not RFC 8785 canonical JSON or a hash of pretty-printed stdout.

`verify` rechecks the evidence digest, action catalog, all three decision hashes,
identity and provider bindings, the default policy outcome, route, reproduced
local result and receipt. Updating only the outer digest cannot hide stale inner
bindings. `UNKNOWN` or a missing/mismatched receipt cannot become success.

These unsigned records prove local consistency and the deterministic result.
They cannot prove who ran the process, when it ran, or authorize an external
effect. This is not a Mirror `ProofEnvelope`, trusted-policy attestation, durable
CAS record or cryptographic signature. `result_ref` is intentionally omitted;
the result is inline. No external evidence/authority is promoted.

Stable caller-supplied action IDs survive the boundary, but there is no durable
deduplication or exactly-once guarantee. Re-running the pure hash is safe; do not
infer retry authority for external actions. Approval requires a future separately
designed trusted authority/executor boundary. Neither high confidence nor a
Codex approval of the CLI process grants that authority.

## Validation and compatibility

```sh
go test ./...
go test -race ./internal/codexadapter ./cmd/metro-codex ./internal/decisionplane ./internal/lifetrastation
go test ./cmd/metro-codex -run '^$' -fuzz FuzzCLIInput -fuzztime 10s
```

The separate Codex Adapter v0.1 workflow checks the plugin package, formatting,
unit/adversarial tests, race checks, bounded fuzzing, a compiled-CLI round trip,
approval behavior and proof verification, and uploads the two proof responses.
Existing v0.2-v0.7 workflows and core packages are left intact and continue to
run their established Go/Rust and remote-provider regressions on pull requests.
