---
name: verify-proof
description: Verify that a claimed Codex action result is bound to the original Metro packet, route, result, and receipt before claiming completion.
---

# Verify completion

Use `liminal_verify` when the MCP tools are connected.

1. Collect the original Metro Packet.
2. Collect the Route actually used.
3. Collect the claimed Result object.
4. Collect the execution Receipt.
5. Call `liminal_verify`.
6. Claim completion only when `verified=true`.
7. A receipt whose status is not `SUCCEEDED` is not completion proof.
8. Packet, route, executor, input-hash, or result-hash mismatches fail closed.
9. Preserve `UNKNOWN` as uncertainty; do not reinterpret it as success or failure.
10. Never automatically redispatch a side-effecting action whose external effect is uncertain.

If MCP is unavailable and the evidence was produced by the bounded
`metro-codex` CLI adapter, use `metro-codex verify` on the saved response.
That verifier proves consistency of the local adapter evidence; its digest is not
a cryptographic signature or external attestation.
