# Liminal Rail Metro Codex plugin v0.3

Portable Codex/ChatGPT plugin package for Liminal Rail Metro decision, proof,
and public-GitHub coding verification.

## Public MCP endpoint

```text
https://liminal-rail-codex-mcp-production.up.railway.app/mcp
```

Health:

```text
https://liminal-rail-codex-mcp-production.up.railway.app/healthz
```

## Tools

- `liminal_decide` — bounded semantic decision + Metro policy gate.
- `liminal_verify` — Packet + Route + Result + Receipt verification.
- `liminal_status` — read-only runtime and authority status.
- `liminal_coding_start` — create a server-signed coding contract from one open public GitHub issue.
- `liminal_coding_verify` — verify one PR against that exact contract using public GitHub evidence.

## v0.3 verified coding workflow

```text
public GitHub issue
      |
      v
liminal_coding_start
      |
      | Ed25519-signed contract
      | base SHA
      | allowed path prefixes
      | required check names
      | Metro decision provenance
      v
Codex edits / tests / PR
      |
      v
public GitHub PR evidence
      |
      +-- exact base SHA
      +-- exact head SHA
      +-- changed files
      +-- check runs on that head
      |
      v
liminal_coding_verify
      |
      +-- mismatch / missing / failed -> HOLD
      |
      v
VERIFIED Ed25519-signed receipt
```

The contract is created **before coding**. The verifier will not accept a client-
fabricated replacement: contract hash and signature must match the configured
server signing authority.

A VERIFIED receipt binds:

```text
workflow_id
+ contract_hash
+ repository
+ issue_number
+ pull_request_number
+ base_sha
+ exact head_sha
+ files_hash
+ checks_hash
+ required_checks
```

If a required check was green on an older SHA, it does not count. If the same
check name has multiple runs on the current SHA, the newest run is authoritative.

## GitHub evidence boundary

v0.3 reads public GitHub evidence only. The production client is pinned to
`https://api.github.com`, rejects redirects, bounds response size and timeout,
and sends an explicit API version and User-Agent.

No GitHub token is used by v0.3. Therefore:

- public repositories are supported;
- private repositories are not;
- the MCP server does not create branches, commits, pull requests, comments, or merges;
- Codex/GitHub write actions remain governed by their own approvals and permissions.

## Signing authority

The public service loads an Ed25519 private key from
`LIMINAL_CODING_SIGNING_KEY_BASE64`. The private key is never returned by an
MCP tool. `liminal_status` exposes only the issuer ID, key ID, and public key.

A coding contract or receipt signed by a different key is rejected by the
configured verifier, even if its self-contained signature is mathematically
valid.

## Decision plane

The server still defaults to deterministic static-proof mode. In this mode,
`liminal_decide` and `liminal_coding_start` require bounded non-negative
scores. Those scores are deterministic proof input, not model confidence.

A remote provider can still be configured through the existing v0.7 variables:

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

before applying the v0.6 policy gate.

## Existing completion boundary

`liminal_verify` still verifies ordinary Metro execution receipts. A route is
not execution proof, and `UNKNOWN` remains unverified.

## Skills

- `metro-action` — bounded Metro decision / execution-receipt workflow.
- `verify-proof` — verify an ordinary Metro receipt.
- `verified-coding` — issue -> contract -> PR -> exact-head GitHub evidence -> VERIFIED/HOLD.

## Local development

```bash
go run ./cmd/codex-mcp
```

The local server defaults to `127.0.0.1:8787`.

To enable coding workflow tools locally, configure an Ed25519 seed or private key:

```bash
export LIMINAL_CODING_ISSUER_ID="liminal-local"
export LIMINAL_CODING_SIGNING_KEY_BASE64="..."
```

## CLI fallback

The existing `metro-codex` binary remains a deliberately narrow local
fallback. It executes only local SHA-256 for `hash_text`;
`external_action` is approval-only and never executes an external effect.

See [installation and contracts](../../docs/codex-adapter-v0.1.md).

## Claim ceiling

v0.3 proves one bounded public-GitHub coding workflow with server-issued signed
contracts and signed receipts based on exact PR-head GitHub evidence.

It does **not** prove:

- arbitrary semantic code correctness;
- private-repository access;
- that a GitHub check operator is trustworthy beyond the selected check name;
- GitHub write or merge authority;
- exactly-once external effects;
- TypeSafe/Jev compatibility;
- completion when the contracted evidence is missing, stale, or non-successful.
