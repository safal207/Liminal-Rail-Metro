# Liminal Rail Metro Codex plugin v0.1

Skills-only Codex compatibility package. Requires the separately built
`metro-codex` executable on PATH. No MCP server or remote service is bundled.

See [installation and contracts](../../docs/codex-adapter-v0.1.md). The packaged
skill is self-contained and does not depend on a repository-relative binary or
the plugin cache's working directory.

The only execution is local SHA-256 of supplied text. External intents return
`REQUIRE_APPROVAL`; no approval claim supplied by the model can unlock execution.
