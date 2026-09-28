# Try Liminal Rail locally

**Experimental localhost demo / release candidate, not a hosted product or
production gateway.** This package wraps `internal/codexadapter`; it does not
change the existing authority policy or connect to live providers.

## What you can try

Send text over HTTP, obtain its SHA-256 plus a bound Metro receipt, and verify
that proof separately. The bundled demo requires rejection of a duplicate action
and a tampered proof, and checks that an external action does not execute.

No account, model API key, Moltbook token, Rust toolchain, wallet or payment is
needed. The initial build downloads its Go image and any required modules;
runtime actions are local only.

## Get the code

Use a new directory so existing work is not changed:

```sh
git clone https://github.com/safal207/Liminal-Rail-Metro.git liminal-rail-preview
cd liminal-rail-preview
git rev-parse HEAD
```

Record the printed SHA. A result on one SHA does not validate a later revision.
For an external acceptance report, follow the exact revision and checkout
instructions in [#31](https://github.com/safal207/Liminal-Rail-Metro/issues/31).
Do not substitute a different revision when the requested one cannot be fetched.
Before this PR is merged, a root clone of `main` will not yet contain these files;
PR reviewers should check out the exact candidate head recorded in PR #29.

## Start with Docker

Use Docker Engine/Desktop with Compose v2 supporting `up --wait`:

```sh
docker compose up --build --wait rail
docker compose run --rm --no-deps demo
```

The second command must print JSON containing:

```json
{"demo":"PASS","scope":"local_consistency_only","live_provider":false}
```

Its `checks` array must contain exactly these five names:

```text
local_hash_and_receipt
separate_verification
duplicate_refused
tampering_refused
external_action_not_dispatched
```

A failed check exits nonzero. The demo does not automatically retry failed or
uncertain action requests. Its duplicate call is a deliberate negative test,
not recovery after a lost response. Running the demo again uses new random IDs.

The API is published on **127.0.0.1:8788 only**, not the LAN or internet. Inside
Compose, `0.0.0.0` listening requires the explicit `-container` flag. Do not change
the host port mapping to expose this unauthenticated preview. Other local users,
processes and containers on its network are not isolated from it.

Stop the preview:

```sh
docker compose down
```

There are no credentials, mounted wallets, host folders or persistent volumes.
The runtime image is non-root and read-only, with a static executable and
`/LICENSE`. The build context allowlists Go sources, module metadata and LICENSE.

## Without Docker

With Go installed (the quickstart workflow/build image pins Go 1.27.1):

```sh
go build -o liminal-rail ./cmd/liminal-rail
./liminal-rail serve
```

In another terminal:

```sh
./liminal-rail demo
```

On Windows build `liminal-rail.exe`, then use `.\liminal-rail.exe serve` and
`.\liminal-rail.exe demo`. Native Windows support is not proved by Linux CI.
Docker Desktop uses a Linux container on Windows/macOS.

## Call it from an agent or script

`GET /healthz` reports preview mode, local consistency scope, no authenticated
identity and no live provider. `POST /v1/actions` accepts JSON such as:

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

Save it as `action.json` and run:

```sh
curl --fail-with-body -sS http://127.0.0.1:8788/v1/actions -H 'Content-Type: application/json' --data-binary @action.json -o proof.json
curl --fail-with-body -sS http://127.0.0.1:8788/v1/verify -H 'Content-Type: application/json' --data-binary @proof.json
```

In PowerShell use `curl.exe` and UTF-8 JSON without BOM, or use the bundled demo.
Do not submit secrets: the returned proof includes the text so the result can be
recomputed. The server does not retain a receipt history. Reusing these sample
IDs in the same instance returns 409; that is expected, not an installation error.

A successful hash response contains `evidence.dispatched=true`, the digest in
`evidence.result.sha256`, and a `SUCCEEDED` receipt. Send the **complete** response
to `/v1/verify`. Its `verified=true` means local consistency only.

An `external_action` with a description and `side_effect=true` can return HTTP 200
with `REQUIRE_APPROVAL`, `dispatched=false` and **no receipt**. There is no approval
endpoint or external executor. HTTP 200 alone does not mean execution succeeded.
The `codex` label is adapter provenance, not evidence of a live Codex session.

## Admission, errors and replay limits

Action JSON is bounded to 64 KiB, text to 32 KiB and proof JSON to 256 KiB.
The strict decoder rejects duplicate/unknown/incorrectly-cased fields, invalid
Unicode, explicit nulls and excessive nesting. Browser Origin requests,
unexpected Host values and query strings are rejected; no CORS access is added.
The server has timeouts and 64 concurrent handler slots.

An admitted action ID is reserved before adapter validation/execution. Rejected,
cancelled or disconnected admitted requests remain consumed in that instance.
Malformed JSON, invalid IDs and pre-admission rejections do not consume an ID.
The 1,024-ID registry never evicts. A restart clears it: **this is not durable,
distributed, cross-process, cross-replica or exactly-once protection**.
Changing IDs/restarting is not a safe recovery protocol for uncertain effects.
This demo has only a pure local hash executor; do not generalize to payments/writes.

| HTTP | Meaning |
| --- | --- |
| 400 | Invalid JSON/action ID, or unsupported query. |
| 403 | Browser Origin or unexpected Host. |
| 404 / 405 / 415 | Unknown path, wrong method, or non-JSON/encoded input. |
| 409 | Action ID already consumed; no re-execution. |
| 422 | Action rejected/uncertain, or inconsistent proof. |
| 503 | Handler admission slots or action registry full. |

## Troubleshooting

If port 8788 is occupied, stop the conflicting local demo before starting this one;
do not solve the conflict by publishing an unauthenticated listener externally.
If `--wait` is unknown, update Compose v2 rather than claiming the startup check
passed. If an image/module download fails, the build is blocked by the environment:
do not report a successful demo. Record the error and exact versions without secrets.

## Evidence and release boundary

The `Developer Quickstart` workflow checks out the exact candidate on a PR and
exact `main` commit on a push. It runs full Go tests, focused race tests and vet,
builds the real image, checks non-root identity and the packaged license, and
runs all five demo checks. Artifacts contain revision/environment metadata,
sanitary demo output and an archive of the tracked source at that revision.

CI success is not an independent security audit or third-party clean-room report.
The formal preview release remains gated by [#30](https://github.com/safal207/Liminal-Rail-Metro/issues/30)
and [#31](https://github.com/safal207/Liminal-Rail-Metro/issues/31).
An unsigned, internally consistent receipt does not authenticate the submitter,
attest hardware/provider identity, witness an external event or certify safety.

The package does not add live Moltbook/AgentProof execution, a web UI, multi-tenant
authentication, TLS, durable receipt storage, billing or production deployment.
See [README scope and known limitations](README.md#scope-and-known-limitations)
for separate experimental paths and open review work.
