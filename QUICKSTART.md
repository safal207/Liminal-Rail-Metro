# Try Liminal Rail locally

**Developer preview, not a hosted product or production gateway.** This package
wraps the existing `internal/codexadapter` without changing Metro, Moltbook, the
public MCP server, deployment configuration, or any existing authority policy.

## What you can try

Send text over HTTP, obtain its SHA-256 plus a bound Metro receipt, and verify
that proof separately. The bundled demo also requires rejection of a duplicate
action and a tampered proof, and checks that an external action does not execute.

No account, model API, Moltbook app key, token, Rust toolchain, or payment is needed.
The initial build downloads a Go container image. Runtime actions are local only.

## Start with Docker

Install a current Docker Engine/Desktop with Docker Compose v2. From this branch's
repository root:

```sh
docker compose up --build --wait rail
docker compose run --rm --no-deps demo
```

The second command must print JSON containing:

```json
{"demo":"PASS","scope":"local_consistency_only","live_provider":false}
```

The actual output also lists five checked conditions. A failed check exits nonzero;
the demo does not automatically retry failed or uncertain action requests. Its
intentional duplicate call is a negative test, not recovery after a lost response.
Each demo uses fresh random action IDs, so running the demo again is a new test.

The API is published on **127.0.0.1:8788 only**, not on the LAN or the internet.
Inside Compose it listens on `0.0.0.0` only because the `-container` flag is explicit.
Do not change the port mapping to expose this unauthenticated preview publicly.
Other local users/processes and containers on the same network are not isolated
from it. No egress-firewall or hostile-host sandbox claim is made.

Stop it with:

```sh
docker compose down
```

There are no secret variables, mounted wallets, host folders, or persistent volumes.
The runtime image is non-root, read-only, and contains only the static executable.
The Docker build context is allowlisted to Go sources and module metadata.

## Without Docker

From the repository root, with a supported Go version (CI uses Go 1.27.1):

```sh
go build -o liminal-rail ./cmd/liminal-rail
./liminal-rail serve
```

In another terminal:

```sh
./liminal-rail demo
```

On Windows, build `liminal-rail.exe` and use `.\liminal-rail.exe serve` and
`.\liminal-rail.exe demo`. Native Windows execution is not implied by Linux CI;
the Docker route uses a Linux container on Windows/macOS Docker Desktop.

## Call it from your own agent or script

`GET /healthz` reports preview mode and explicitly says there is no authenticated
identity or live provider. `POST /v1/actions` accepts the existing adapter JSON
contract, with `Content-Type: application/json`:

```json
{
  "protocol": "metro.codex.action.v0.1",
  "request_id": "my-request-001",
  "action_id": "my-action-001",
  "kind": "hash_text",
  "text": "hello",
  "side_effect": false
}
```

Save that as `action.json`, then:

```sh
curl --fail-with-body -sS http://127.0.0.1:8788/v1/actions -H 'Content-Type: application/json' --data-binary @action.json -o proof.json
curl --fail-with-body -sS http://127.0.0.1:8788/v1/verify -H 'Content-Type: application/json' --data-binary @proof.json
```

On Windows PowerShell use `curl.exe` and save JSON as UTF-8 without BOM, or use the
bundled demo to avoid shell-encoding differences. Do not submit secrets as text:
the returned proof intentionally contains the submitted text so its result can
be independently recomputed. No server-side receipt history is created.

A successful hash response has `evidence.dispatched=true`, a SHA-256 in
`evidence.result.sha256`, and a receipt with `SUCCEEDED`. Submit the **complete**
response to `/v1/verify`; its `verified=true` means local consistency only.

`external_action` requests use a description and `side_effect=true`. They can
produce HTTP 200 with a valid non-dispatch proof: `REQUIRE_APPROVAL`,
`dispatched=false`, and **no receipt**. There is no approval-submission endpoint
and no external executor. HTTP 200 alone is not execution success.

The retained `codex` protocol/source label is adapter provenance, **not** proof
that a real Codex session ran or that a caller identity was authenticated.
The API deliberately does not pretend a synthetic identity is live Moltbook auth.

## Admission, errors and replay limits

Action JSON is bounded to 64 KiB, text to 32 KiB, and proof JSON to 256 KiB.
Duplicate/unknown/incorrectly-cased fields, invalid Unicode, explicit nulls and
excessive nesting are rejected by the existing strict decoder. Browser-origin
requests, unexpected Host values, and query strings are not supported. There is
no CORS allowance. Server timeouts and 64 concurrent handler slots are explicit.

After strict decoding and an action-ID shape check, an admitted ID is reserved
**before** adapter validation/execution. Even a subsequently rejected, cancelled,
or disconnected request consumes that ID; it cannot be retried in this instance.
This conservative admission behavior is separate from MOLT-001 Station semantics.
Malformed JSON, invalid IDs, and requests refused before admission do not consume
an ID. The demo-only store holds 1,024 IDs without eviction; when full, new actions
are refused. Restarting clears the entire store, so this is **not durable,
distributed, cross-process or cross-replica exactly-once protection**.

Changing an action ID or restarting is **not** a safe recovery protocol for
uncertain external effects. This preview has only a pure local hash executor.
Do not generalize its replay behavior to payments, writes or other side effects.

| HTTP | Meaning |
| --- | --- |
| 400 | Invalid JSON, invalid action ID, or unsupported query. |
| 403 | Browser Origin or unexpected Host rejected. |
| 404 / 405 / 415 | Unknown path, wrong method, or non-JSON/encoded input. |
| 409 | Action ID already consumed; no re-execution. |
| 422 | Action rejected/uncertain, or inconsistent proof. No success proof returned. |
| 503 | Admission slots or bounded action registry full. |

## Verification and claim ceiling

`Developer Quickstart` CI checks out the exact PR head (not an implicit merge
ref), runs the full Go suite, focused race tests and vet, then builds the actual
Docker image and runs the Compose demo. It uploads the sanitized demo summary.
CI success is authoring evidence, not an independent security review.

A receipt/evidence hash is unsigned. Anyone can create another internally
consistent proof. Verification does not authenticate the submitter, attest a
host/provider, establish an observed external event, or certify production safety.

This first package does not add live Moltbook/AgentProof calls, a web UI,
multitenant authentication, TLS, durable receipt storage, billing, a managed
service, a production deployment, a license change, or a merge of any draft PR.
