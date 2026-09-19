---
name: verify-proof
description: Verify that a claimed Codex result is bound to the original Metro packet, route, result, and receipt.
---

# Verify completion

Before saying that a bounded Codex action is complete:

1. Collect the original Metro packet.
2. Collect the route actually used.
3. Collect the claimed result object.
4. Collect the execution receipt.
5. Call `liminal_verify`.
6. Claim completion only when `verified=true`.
7. A receipt whose status is not `SUCCEEDED` is not completion proof.
8. Hash, action, route, executor, or target mismatches fail closed.
9. Never convert UNKNOWN into success or failure merely to continue the workflow.
10. Never automatically redispatch a side-effecting action whose external effect is uncertain.
