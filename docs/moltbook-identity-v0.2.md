# Moltbook Identity Adapter v0.2

MOLT-002 implements the documented Moltbook server-side identity verification
contract behind the merged MOLT-001 ingress boundary.

## Official contract

Checked against the Moltbook Developer Platform on 2026-09-20:

```text
POST https://www.moltbook.com/api/v1/agents/verify-identity
X-Moltbook-App-Key: moltdev_...
Content-Type: application/json

{"token":"<short-lived identity token>"}
```

The documented identity token lifetime is approximately one hour. The caller's
Moltbook bot API key is not shared with Liminal Rail.

## Adapter behavior

`MoltbookIdentityVerifier`:

- uses the fixed official verification URL;
- requires a server-side app key with the documented `moltdev_` prefix;
- sends the app key only in `X-Moltbook-App-Key`;
- accepts only `success=true`, `valid=true`, and a non-empty `agent.id`;
- normalizes the result to `VerifiedIdentity{AgentID, Verified}`;
- ignores reputation/social fields for authority purposes;
- refuses redirects;
- applies an explicit HTTP timeout;
- bounds verification responses to 256 KiB;
- returns sanitized errors that do not include raw tokens, app keys, or response bodies.

## Deterministic verification

CI does not require a Moltbook account or developer credential.

The HTTP boundary is exercised through an injected `http.Client` transport
while the production request URL remains fixed. Tests cover:

- the documented request method, URL, header, and JSON body;
- valid identity normalization;
- invalid / unsuccessful token responses;
- missing agent ID;
- malformed JSON;
- non-2xx responses;
- context cancellation;
- HTTP timeout;
- redirect rejection without following;
- oversized response rejection;
- missing / malformed app key;
- token/app-key non-disclosure in returned errors;
- Moltbook karma / owner metadata remaining separate from Metro authority;
- end-to-end compatibility with the existing MOLT-001 Station and decision-plane authority.

## Claim ceiling

This repository currently claims:

> HTTP adapter implemented and tested against the documented Moltbook Identity
> contract.

It does **not** yet claim:

> live Moltbook identity verification observed.

No real `MOLTBOOK_APP_KEY` or identity token was used to establish the
deterministic CI proof.

A live claim requires a separately authorized smoke test after Developer Early
Access provides an app key.

## Secret boundary

`MOLTBOOK_APP_KEY` and raw identity tokens must remain outside:

- Git;
- fixtures;
- packets;
- receipts;
- CI artifacts;
- logs;
- error strings.

The adapter intentionally does not persist Moltbook karma, follower counts,
post/comment counts, owner profile data, or `is_claimed`.

## Authority boundary

Moltbook proves caller identity only.

```text
Moltbook identity != Metro authority
social reputation != trust
verified identity != permission to execute
```

The existing MOLT-001 decision-plane authority remains the only dispatch gate.
