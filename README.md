# Liminal Rail Metro

**Bounded agent actions, explicit routes, and checkable receipts.**

Liminal Rail is an experimental Go protocol engine for handing an action to an
allowed target while keeping the action, routing decision and result connected.
Start with a small localhost demo: hash text, verify its receipt, and watch a
duplicate request, a tampered proof and an external-action request get stopped.

**Status: experimental local demo / release candidate. Not production-ready.**
The formal developer-preview release is gated on an independent clean-room run
in [#31](https://github.com/safal207/Liminal-Rail-Metro/issues/31), tracked by
[DEVREL-001](https://github.com/safal207/Liminal-Rail-Metro/issues/30).
CI reproduction and an external developer's reproduction are different evidence.

[Quickstart](QUICKSTART.md) · [Demo-day script](DEMO_DAY.md) ·
[Technical history and architecture](TECHNICAL.md) · [MIT license](LICENSE)

## Try the local demo

Prerequisites: Git and Docker Engine/Desktop with Docker Compose v2, including
`docker compose up --wait`. The first build needs internet access to download its
build image and any required modules. No model API key, account, token, Rust
installation, wallet or paid service is needed.

```sh
git clone https://github.com/safal207/Liminal-Rail-Metro.git
cd Liminal-Rail-Metro
git rev-parse HEAD
docker compose up --build --wait rail
docker compose run --rm --no-deps demo
```

Expect this JSON (object key order may differ):

```json
{
  "demo": "PASS",
  "scope": "local_consistency_only",
  "live_provider": false,
  "checks": [
    "local_hash_and_receipt",
    "separate_verification",
    "duplicate_refused",
    "tampering_refused",
    "external_action_not_dispatched"
  ]
}
```

The five checks exercise the bounded demo, not every security property of the
repository. Each demo invocation uses new action IDs. Stop the preview with:

```sh
docker compose down
```

**Local only:** the API is published at `127.0.0.1:8788`. Do not expose it to your
LAN or the internet. Do not send secrets as demo text: returned proofs contain
that text. This is a CLI/API demo, not a browser UI.

For native Go commands, Windows notes, API examples, errors and replay limits,
read [QUICKSTART.md](QUICKSTART.md).

## What is being demonstrated?

```text
bounded action -> decision gate -> local SHA-256 -> Metro receipt
                                                -> separate verification
external action -> REQUIRE_APPROVAL -> no dispatch / no receipt
```

| Check | Meaning within this local demo |
| --- | --- |
| Local hash and receipt | The SHA-256 result and receipt match the submitted action. |
| Separate verification | The client and a separate API request check the returned proof. |
| Duplicate refusal | The same action ID is not executed again in the same running instance. |
| Tamper refusal | The demo's changed proof is rejected. |
| External action stopped | The request requires approval; no external executor or approval endpoint is supplied. |

`verified=true` means **unsigned local consistency**, not authenticated identity,
hardware attestation, independent witnessing or proof that an external event
happened. The `codex` label is adapter provenance; no live Codex or Moltbook
session is required or demonstrated here.

## Scope and known limitations

The local quickstart has one pure hash executor. It does not exercise every
policy-authority, attestation, coding-workflow or Metro Web experiment in this
repository. Historical CI results and version labels in [TECHNICAL.md](TECHNICAL.md)
refer to their individual stages, not one production release.

Replay state is in memory, limited to 1,024 action IDs, and lost on restart.
There is no durable/distributed exactly-once guarantee, authenticated multi-tenant
access, TLS, persistent receipt history or hostile-host isolation.

Separate review work remains open, including the
[core route/action binding fix](https://github.com/safal207/Liminal-Rail-Metro/pull/38),
[policy-head concurrent-writer fix](https://github.com/safal207/Liminal-Rail-Metro/pull/47)
and [Windows policy-head fix](https://github.com/safal207/Liminal-Rail-Metro/pull/48).
Do not treat this demo or its green checks as approval to use those experimental
paths for payments, privileged writes or safety-critical operations. See each
linked PR for its current status.

Metro Web remains a separate experimental PR stack. Its draft commands are not
the first-run path above and are not claimed as part of this local demo.

## Reproduce and report

Please follow [the clean-room test protocol in #31](https://github.com/safal207/Liminal-Rail-Metro/issues/31).
Report the exact commit SHA, OS/architecture, Docker and Compose versions, all
five observed checks, and any undocumented step. Include sanitized errors only.
A run by the author or their assistant is useful, but is not the independent
external run required for the formal release.

The [Developer Quickstart workflow](https://github.com/safal207/Liminal-Rail-Metro/actions/workflows/developer-quickstart.yml)
runs tests and the actual Docker Compose package. Its artifacts record the exact
checkout and environment. A workflow definition or old green badge alone is not
evidence for a new commit.

## License

[MIT](LICENSE), copyright 2026 Aleksey Safonov. The quickstart image also includes
`/LICENSE`. Separately installed tools and dependencies retain their own licenses.
