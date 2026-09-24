# Contributing to Liminal Rail Metro

Thanks for helping make agent handoffs and resource paths easier to inspect and
verify. This is an experimental protocol project. Small reports that reproduce
a problem are as useful as implementation PRs.

## Set up

Install Git and Go 1.23.12 (the version in `go.mod`). Clone the repository and
check the toolchain:

```sh
git clone https://github.com/safal207/Liminal-Rail-Metro.git
cd Liminal-Rail-Metro
go version
go run ./cmd/metro-demo
```

The Metro Web preview is in the separate
[`metro-web-006-binary-resource` branch](https://github.com/safal207/Liminal-Rail-Metro/tree/metro-web-006-binary-resource)
and [draft PR #39](https://github.com/safal207/Liminal-Rail-Metro/pull/39),
not on `main`. Follow the [clean-clone preview steps](README.md#try-the-metro-web-preview)
when reporting a preview issue. The basic Go demos do not need Rust or a model
API key. Cross-runtime tests and benchmarks do need the pinned Lifetra station;
see [architecture](docs/architecture.md) and the relevant CI workflow.

## Good places to start

- **Try a clean clone.** Run the Metro Web README preview on your own OS and
  report the exact command, observed result, and what you expected. Separately,
  [issue #31](https://github.com/safal207/Liminal-Rail-Metro/issues/31)
  requests an independent Docker-based Developer Quickstart test at a specific
  commit; use that issue's instructions when testing it.
- **Improve a task walkthrough.** Show a bounded agent goal, allowed paths,
  denied path, and the receipt that supports the outcome. Keep claims tied to
  what the demo actually executes.
- **Find a boundary case.** Test stale file versions, malformed resource
  passports, graph changes, missing permissions, or lost responses. A small
  reproducible failure is more valuable than a broad speculative rewrite.
- **Propose an adapter.** Explain where Metro adds route/receipt semantics to
  an existing MCP or A2A workflow. An adapter proposal does not imply current
  wire compatibility.

Use the [bug report](https://github.com/safal207/Liminal-Rail-Metro/issues/new/choose)
or feature proposal template. For security-sensitive reports, avoid posting
credentials, tokens, private files, or host details in a public issue.

## Make a change

1. Open an issue or link an existing one for behavior changes. For small docs
   corrections, a direct PR is fine.
2. Branch from the relevant target: `main` for core/docs, or the stage branch
   named by an experimental PR when working on that stage. State your base in
   the PR description.
3. Keep the change focused. Include a before/after example, the evidence that
   proves the new behavior, and any remaining limit.
4. Run the checks relevant to your files. For Go changes, start with:

   ```sh
   go test ./...
   go vet ./...
   ```

   For Metro Web preview changes, also run:

   ```sh
   go test -race ./cmd/metro-web-demo ./internal/metro
   ```

   If a check fails on your platform, include the OS, Go version, command and
   failing output. Some cross-runtime scenarios require the separately pinned
   Rust project; the Go-only preview does not.
5. Open a PR with the exact checks you ran. Do not mark a test as passing if it
   was skipped or the environment could not run it.

## Protocol and safety invariants

- Give each logical action a stable `action_id` before dispatch. A route may
  choose only a target allowed by that action.
- Bind a receipt to the action and selected route. A planned route, a policy
  allowance, a hash, or a successful HTTP response alone is not proof that a
  requested external effect happened.
- Preserve `UNKNOWN` when the outcome of an external effect cannot be proved;
  do not silently retry an action that may have happened.
- Treat model output, publisher metadata, remote content, and graph edges as
  untrusted input. A transition shown in a map does not create authority to
  execute it.
- Keep resource reads bounded and pinned to an explicit source/version. Do not
  turn a caller-provided path or URL into an unrestricted file/network fetch.

The [trust policy](docs/trust-policy-v1.0.md) and [architecture](docs/architecture.md)
give the broader rationale. Please update those boundaries and their tests
when proposing a new capability.

## License status

Check the root `LICENSE` file in your checkout before relying on its terms or
submitting a substantial external contribution. The MIT text is
tracked in [PR #32](https://github.com/safal207/Liminal-Rail-Metro/pull/32);
without a license file in the revision you use, do not assume that MIT applies.
