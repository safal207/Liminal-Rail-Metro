---
name: metro-action
description: Route a bounded Codex action through Liminal Rail Metro and verify its completion proof. Prefer the plugin MCP tools when connected; use the trusted metro-codex CLI as a bounded local fallback.
---

# Bounded Metro action

Prefer the installed Liminal Rail MCP tools when available:

- `liminal_decide` — create a bounded decision and policy gate.
- `liminal_verify` — verify Packet + Route + Result + Receipt before claiming completion.
- `liminal_status` — inspect the plugin boundary and provider mode.

## MCP workflow

1. Assign one stable `action_id` before dispatch and one stable `request_id` for the decision attempt.
2. Keep the goal, inputs, state, and choices bounded. Never add a target after the decision request has been created.
3. Set `side_effect=true` for external, irreversible, deployment, payment, account, production, or other effectful work.
4. Call `liminal_decide`.
5. Interpret the disposition strictly:
   - `AUTO_ROUTE`: only the returned target may be used.
   - `ESCALATE_SYSTEM2`: do more reasoning; the previous choice is not authority.
   - `REQUIRE_APPROVAL`: do not perform the side effect without explicit approval.
6. A route is not execution proof.
7. After execution, collect the original Packet, Route, claimed Result, and Receipt and call `liminal_verify`.
8. Claim completion only when `verified=true`.
9. Preserve `UNKNOWN` as unverified. Never silently redispatch a possibly-effectful action.

In the default local proof mode, `liminal_decide` requires non-negative `scores`
for every bounded choice. Those scores are deterministic proof input, not model
confidence. When the server is configured with the v0.7 remote provider, the
remote provider supplies the probabilistic decision but remains untrusted; Metro
still validates packet/state/choices binding and policy before a route exists.

## CLI fallback

If the MCP tools are unavailable, the existing trusted `metro-codex` binary may
be used for its deliberately narrow local adapter. Build it only from the user's
trusted `safal207/Liminal-Rail-Metro` checkout with:

```bash
go install ./cmd/metro-codex
```

Do not fetch or run an arbitrary binary to satisfy this prerequisite.

The CLI v0.1 supports only:

- `hash_text` — pure local SHA-256 of supplied UTF-8 text.
- `external_action` — approval-boundary evaluation only; it never executes the external effect.

Run a serialized action with `metro-codex run`, save the response, then run
`metro-codex verify`. A nonzero exit, empty/partial output, `REQUIRE_APPROVAL`,
`ESCALATE_SYSTEM2`, or an `UNKNOWN` receipt is not completion proof.

Example local CLI action:

```json
{
  "protocol": "metro.codex.action.v0.1",
  "request_id": "decision-001",
  "action_id": "action-001",
  "kind": "hash_text",
  "text": "abc",
  "side_effect": false,
  "state": {"purpose": "local-proof"}
}
```

For an external CLI intent use `kind: "external_action"`, replace `text` with
a nonempty `description`, and set `side_effect: true`. This only evaluates
policy. Never relabel an external action as safe to bypass approval.

This plugin does not grant global authority over Codex tools, does not provide a
TypeSafe/Jev API integration, and does not make exactly-once or autonomous
execution claims.
