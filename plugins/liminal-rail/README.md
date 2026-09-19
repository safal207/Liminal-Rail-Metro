# Liminal Rail for Codex v0.1

Portable Codex/ChatGPT plugin package for the Liminal Rail Metro proof and routing boundary.

## Local development

Start the MCP server from the repository root:

```bash
go run ./cmd/codex-mcp
```

The portable plugin points at:

```text
http://127.0.0.1:8787/mcp
```

The server exposes:

- `liminal_decide` — bounded semantic decision and Metro route gate.
- `liminal_verify` — receipt/result verification; non-`SUCCEEDED` never verifies completion.
- `liminal_status` — read-only runtime and boundary status.

By default `liminal_decide` runs in deterministic local proof mode and requires a `scores` map. To use the existing v0.7 remote-provider boundary, configure:

```bash
export LIMINAL_REMOTE_PROVIDER_URL="https://provider.example/decision"
export LIMINAL_REMOTE_PROVIDER_ID="provider-id"
export LIMINAL_REMOTE_PROVIDER_TOKEN="..."
export LIMINAL_REMOTE_TIMEOUT_MS="10000"
go run ./cmd/codex-mcp
```

The remote provider remains untrusted: packet/state/choice binding and the v0.6 policy gate are revalidated before a route can exist.

## Distribution boundary

This checked-in `mcp.json` is for local development. OpenAI's portable plugin guidance requires a deployed public HTTPS MCP endpoint for public plugin submission. Replace the localhost URL only after that server is deployed and authenticated.

## Claim ceiling

v0.1 proves the plugin packaging and MCP boundary. It does not claim:
- public hosted availability;
- a TypeSafe/Jev API integration;
- autonomous execution;
- exactly-once external effects;
- completion without a verified receipt.
