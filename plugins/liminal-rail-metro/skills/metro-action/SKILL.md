---
name: metro-action
description: Run a bounded Liminal Rail Metro action through the metro-codex CLI and verify its receipt. Use for an explicitly requested Metro action, local text SHA-256 proof, or external-action approval-boundary check.
---

# Bounded Metro action

Use the installed `metro-codex` binary, built from the user's trusted
`safal207/Liminal-Rail-Metro` checkout. If it is unavailable, explain that it must
be built with `go install ./cmd/metro-codex` in that checkout and made available on
PATH. Do not fetch or run an arbitrary binary to satisfy this prerequisite.

1. Build one JSON input file with a JSON serializer/file editing tool. Keep user
   text inside JSON data; never interpolate it into shell command text. Assign
   stable `action_id` and `request_id` values before calling the CLI.
2. Invoke `metro-codex run` with that file on stdin, saving stdout as the response.
   POSIX: `metro-codex run < action.json > response.json`.
   PowerShell: `Get-Content -Raw -Encoding utf8 action.json | metro-codex run |
   Set-Content -Encoding utf8 response.json` (PowerShell 7; set
   `$OutputEncoding = [System.Text.UTF8Encoding]::new($false)` first).
   Use paths appropriate to the workspace and inspect the process exit code.
3. On nonzero exit, report rejection. Empty/partial output is not proof. Do not
   retry an external action or change its identity to bypass a failure.
4. Run `metro-codex verify` on the saved response. Report the disposition, target
   when present, and verification result. A digest verifies local consistency;
   it is not a signature or an authoritative external receipt.
5. `REQUIRE_APPROVAL` and `ESCALATE_SYSTEM2` have no dispatch or success receipt.
   Stop the Metro workflow and explain what is required. v0.1 has no approval
   resumption or external executor. Never invent an `approved` flag, relabel an
   external action as safe, or execute it through another tool as part of this
   skill. Any separate user-authorized workflow remains governed by that tool's
   own approvals and permissions.

Example local action (UTF-8 text, not file paths):

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

For an external intent use `kind: "external_action"`, replace `text` with a
nonempty `description`, and set `side_effect: true`. This only evaluates policy.
State is optional and maps strings to strings. Limits: 64 KiB JSON, 32 KiB text,
4 KiB external description, 64 state entries (128-byte keys/1024-byte values).
Unknown fields, duplicate keys, null values and unsupported actions are rejected.
Targets, providers and policy are not caller-configurable.

This skill applies only to explicit Metro calls; it does not enforce a global
gate on Codex's other tools. It does not use an OpenAI API key or a Jev API.
