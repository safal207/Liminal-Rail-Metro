# Codex verified coding workflow v0.3

## Question

Can Codex claim one public-GitHub coding task complete only after Liminal Rail
can independently bind the implementation PR to the original issue contract,
allowed file scope, exact base/head identity, and required successful GitHub
checks?

## Flow

```text
public GitHub issue
  -> liminal_coding_start
  -> Metro Packet + Decision + AUTO_ROUTE
  -> Ed25519-signed contract
  -> Codex edits/tests/PR through normal tools
  -> public GitHub PR/files/check-runs
  -> liminal_coding_verify
  -> VERIFIED signed receipt | HOLD
```

## Contract boundary

The server issues the contract before implementation. It binds:

- workflow, request, and action identity;
- repository and issue number;
- a snapshot hash of the open issue;
- exact base commit SHA;
- canonical allowed path prefixes;
- canonical required check names;
- Metro packet/request/decision/gate provenance;
- server Ed25519 issuer identity and key ID.

The contract hash alone is not authority. The contract must carry a valid
Ed25519 signature from the verifier's configured server key.

## GitHub evidence

v0.3 uses a read-only public GitHub client pinned by default to
`https://api.github.com`.

The client:

- uses HTTPS only for production;
- rejects redirects;
- has a bounded timeout and response size;
- sends an explicit GitHub API version and User-Agent;
- carries no GitHub token.

The verifier checks:

1. PR base repository equals the contract repository.
2. PR remains open and is not a draft.
3. PR base SHA exactly equals the contracted base SHA.
4. Changed-file count is bounded and complete.
5. Every changed path is inside one allowed path prefix.
6. Required check evidence is for the exact PR head SHA.
7. For duplicate runs with the same name, the highest check-run ID is treated as latest.
8. Every selected required run is `completed/success`.

Any mismatch produces `HOLD` and no VERIFIED receipt.

## VERIFIED receipt

The signed receipt binds:

```text
workflow_id
contract_hash
repository
issue_number
pull_request_number
pull_request_url
base_sha
head_sha
files_hash
checks_hash
required_checks
verified_at
issuer identity/key
```

The receipt is Ed25519-signed by the same configured server authority.

## Self-proof

Issue #16 is the acceptance contract for v0.3.

The implementation PR runs:

1. `coding-unit` — formatting, full Go tests, vet, race checks, and MCP build.
2. `live-self-proof` — waits for the live Railway service to expose v0.3,
   creates a signed contract for issue #16, then asks that live service to verify
   the implementation PR itself.

The contract requires `coding-unit` as the GitHub check. Therefore the live
proof cannot become VERIFIED until that independent check is green on the exact
PR head.

## Claim ceiling

v0.3 verifies bounded public GitHub evidence. It does not prove arbitrary
semantic correctness, private repository access, trustworthiness of the selected
GitHub check operator, write/merge authority, or exactly-once external effects.
