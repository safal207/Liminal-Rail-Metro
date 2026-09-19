---
name: route-action
description: Route a bounded Codex coding action through Liminal Rail before execution.
---

# Route a bounded action

Use this workflow when a coding task has multiple valid execution paths or when a route should be explicitly proven before execution.

1. Define one stable `action_id` before dispatch.
2. Define one stable `request_id` for this decision attempt.
3. Keep the action goal and inputs bounded.
4. Supply only allowed choices and targets. Never invent an extra target after the decision request is created.
5. Mark `side_effect=true` for external, irreversible, production, payment, account, deployment, or other effectful operations.
6. Call `liminal_decide`.
7. Interpret the disposition strictly:
   - `AUTO_ROUTE`: only the returned target may be used.
   - `ESCALATE_SYSTEM2`: do more reasoning; do not treat the previous choice as authority.
   - `REQUIRE_APPROVAL`: do not perform the side effect without explicit approval.
8. Treat the returned route as authority to route, not proof that execution happened.
9. After execution, obtain a Metro receipt and call `liminal_verify` before claiming completion.
10. If the execution state is unknown, preserve UNKNOWN. Do not silently redispatch a possibly-effectful action.
