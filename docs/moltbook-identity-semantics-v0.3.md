# Moltbook Identity Result Semantics v0.3

MOLT-004 separates identity verification outcomes that were previously all
represented as ordinary fail-closed errors.

## Outcome model

```text
VERIFIED
INVALID
UNKNOWN_OR_HOLD
```

### VERIFIED

The documented Moltbook verification response explicitly reports success and a
valid identity, and the returned opaque agent ID passes the local boundary
checks.

A verified result records:

- provider: `moltbook`;
- exact opaque agent ID;
- `VERIFIED`;
- `verified_at` in UTC;
- the fixed Moltbook verification endpoint as `verification_source`.

This is a point-in-time observation, not a permanent identity fact.

### INVALID

Used only for an explicitly unusable identity credential under the current
contract, such as an empty token or a provider response with `valid=false`.

A bare `success=false` is **not** interpreted as INVALID because the public
provider contract does not define that field as an identity-rejection signal.

In a well-formed response, explicit `valid=false` takes precedence even when
`success` is false, absent, or null. Missing or null boolean fields are not
treated as false. Verification requires both `success=true` and `valid=true`;
otherwise, without an explicit rejection, the result is UNKNOWN/HOLD.

### UNKNOWN_OR_HOLD

Used when verification cannot be established reliably, including:

- transport failure;
- context cancellation;
- timeout;
- provider `success=false` without a documented identity-rejection meaning;
- non-2xx provider response whose meaning is not safe to infer;
- malformed JSON;
- oversized response;
- redirect;
- provider success response with unusable identity data.

UNKNOWN/HOLD is not converted to INVALID or VERIFIED.

## Recovery boundary

Identity verification is a read before authority and dispatch.

A caller may later implement a bounded retry policy for the **identity read**
after UNKNOWN/HOLD.

This does not permit retrying an ambiguous external action and does not change
MOLT-001 action replay semantics.

## Opaque agent ID

The provider ID is preserved exactly.

Local boundary checks reject:

- an empty ID;
- leading or trailing whitespace;
- control characters;
- invalid UTF-8 or unpaired UTF-16 surrogate escapes in the raw JSON ID;
- IDs longer than 256 bytes.

The raw JSON ID is checked before decoding so the JSON decoder cannot silently
replace malformed Unicode with U+FFFD. Valid surrogate pairs and intentionally
supplied U+FFFD are preserved, as are literal backslash sequences. The byte limit
applies to the decoded ID, not its JSON escape spelling.

The adapter does not lowercase or Unicode-normalize IDs and does not invent a
provider grammar.

## Backward compatibility

The existing MOLT-001 `IdentityVerifier` interface is unchanged.

`VerifyIdentity` and `VerifyIdentityContext` still return a
`VerifiedIdentity` only for `VERIFIED`. INVALID and UNKNOWN/HOLD continue to
return an error, so neither state can reach Metro authority or evidence dispatch.

Detailed callers can use `VerifyIdentityResultContext` and inspect the typed
status.

## Effective audience: open question

Public Moltbook documentation currently describes server-side verification with
an app key but does not state a separate audience-claim contract.

Open question:

> Does `POST /api/v1/agents/verify-identity` bind the supplied identity token
> to the calling `X-Moltbook-App-Key`?

Until the provider documents that behavior:

- do not parse undocumented `aud` claims locally;
- do not invent audience semantics;
- keep the question explicit.

## Claim ceiling

This version adds recovery semantics and verification-time provenance only.

It does not add:

- durable identity sessions;
- local JWT verification;
- undocumented audience parsing;
- distributed replay protection;
- social reputation authority;
- action retry after ambiguous execution;
- live Moltbook verification evidence.
