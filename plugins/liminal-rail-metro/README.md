# Liminal Rail Metro Codex plugin v0.2

Portable Codex/ChatGPT plugin package for the Liminal Rail Metro decision and
proof boundary.

## Public MCP endpoint

The canonical plugin now points to the live HTTPS MCP service:

```text
https://liminal-rail-codex-mcp-production.up.railway.app/mcp
```

Health endpoint:

```text
https://liminal-rail-codex-mcp-production.up.railway.app/healthz
```

Externally verified on v0.2:

- `GET /healthz` -> HTTP 200 with `{"status":"ok","version":"0.2.0"}`;
- MCP `initialize` -> HTTP 200, `application/json`, server `liminal-rail-codex` version `0.2.0`.

The current public surface is deliberately anonymous because it is stateless,
does not read user-specific data, and does not execute external side effects.
OAuth 2.1 is a required future boundary before user-specific or effectful tools
are added.

## Tools

- `liminal_decide` — bounded decision + Metro policy gate.
- `liminal_verify` — Packet + Route + Result + Receipt verification.
- `liminal_status` — read-only runtime/boundary status.

The server defaults to deterministic static-proof mode. A `liminal_decide`
call must supply bounded choices and a non-negative `scores` map. These scores
are deterministic proof input, not model confidence.

A real remote provider can still be configured on the server through the
existing v0.7 environment variables:

```bash
export LIMINAL_REMOTE_PROVIDER_URL="https://provider.example/decision"
export LIMINAL_REMOTE_PROVIDER_ID="provider-id"
export LIMINAL_REMOTE_PROVIDER_TOKEN="..."
export LIMINAL_REMOTE_TIMEOUT_MS="10000"
```

Remote decisions remain untrusted. Metro revalidates:

```text
request_id + action_id + packet_hash + state_hash + choices_hash
```

before applying the v0.6 confidence, margin, and side-effect policy.

## Completion boundary

A route is not completion proof. `liminal_verify` accepts completion only when:

- Packet, Route, and Receipt protocols match;
- receipt hash algorithm is SHA-256;
- receipt status is `SUCCEEDED`;
- action, route, executor, input hash, and result hash all match.

`UNKNOWN` remains unverified.

## Local development

The public URL is checked into `mcp.json`. For local server development, run:

```bash
go run ./cmd/codex-mcp
```

The local server defaults to `127.0.0.1:8787`. Hosted deployments can bind to
the platform port via `PORT`, or explicitly with `LIMINAL_LISTEN_ADDR`.

## CLI fallback

The existing `metro-codex` binary remains the narrow local fallback. It
executes only local SHA-256 for `hash_text`; `external_action` is
approval-only and never executes an external effect.

See [installation and contracts](../../docs/codex-adapter-v0.1.md).

## Claim ceiling

v0.2 proves a live HTTPS MCP endpoint and portable plugin package. It does not
claim TypeSafe/Jev API compatibility, user-account access, autonomous external
execution, exactly-once distributed effects, or completion without a verified
receipt.
