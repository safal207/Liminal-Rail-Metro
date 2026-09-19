# Liminal Rail Metro Codex plugin v0.1

Portable Codex/ChatGPT plugin package for the Liminal Rail Metro decision and
proof boundary.

The package now has two compatible surfaces:

1. **MCP tools** — the preferred live plugin surface:
   - `liminal_decide`
   - `liminal_verify`
   - `liminal_status`
2. **`metro-codex` CLI** — the existing narrow local fallback/executor for
   SHA-256 text proofs and approval-only external intents.

The portable manifest is `plugin.json`; `.codex-plugin/plugin.json` remains a
compatibility fallback.

## Local MCP development

From the trusted repository checkout:

```bash
go run ./cmd/codex-mcp
```

The checked-in `mcp.json` points to:

```text
http://127.0.0.1:8787/mcp
```

The MCP server defaults to deterministic local proof mode. A call to
`liminal_decide` must supply bounded choices and a non-negative `scores` map.
Those scores are proof input, not model confidence.

To use the existing v0.7 remote-provider boundary instead:

```bash
export LIMINAL_REMOTE_PROVIDER_URL="https://provider.example/decision"
export LIMINAL_REMOTE_PROVIDER_ID="provider-id"
export LIMINAL_REMOTE_PROVIDER_TOKEN="..."
export LIMINAL_REMOTE_TIMEOUT_MS="10000"
go run ./cmd/codex-mcp
```

The remote decision remains untrusted. Metro still revalidates:

```text
request_id + action_id + packet_hash + state_hash + choices_hash
```

before applying the v0.6 confidence/margin/side-effect policy.

## Completion boundary

A route is not completion proof. `liminal_verify` accepts completion only when:

- Packet, Route, and Receipt protocols match;
- receipt hash algorithm is SHA-256;
- receipt status is `SUCCEEDED`;
- action, route, executor, input hash, and result hash all match.

`UNKNOWN` remains unverified.

## CLI fallback

The existing skill can still use the separately built `metro-codex` binary.
That CLI executes only local SHA-256 for `hash_text`; `external_action` is
approval-only and never executes an external effect.

See [installation and contracts](../../docs/codex-adapter-v0.1.md).

## Public distribution boundary

The localhost MCP URL is deliberately for development. Public plugin submission
requires a deployed HTTPS MCP endpoint and appropriate authentication. This
v0.1 does not claim hosted availability, TypeSafe/Jev API compatibility,
autonomous execution, or exactly-once external effects.
