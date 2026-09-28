# Demo-day runbook: one action, one receipt, clear limits

**Positioning:** experimental localhost demo / MIT-licensed research prototype.
Do not announce the formal developer-preview release before the external
clean-room gate in #31 and the remaining DEVREL-001 gates are complete.

## Before presenting

From a clean checkout of the selected candidate, record `git rev-parse HEAD`.
Run the README commands yourself, then save the observed JSON separately from
this runbook. Build the image before the presentation: the first build downloads
dependencies, and venue Wi-Fi should not be part of the demo's success criteria.
Use a computer with Docker/Compose already working. Do not use tokens or secrets.

Keep the tested checkout and local image unchanged for the presentation. A
historical artifact is a backup, not live execution; label it with its actual
commit and run date if replaying it. Do not invent a PASS or conceal a failure.

## Suggested 90-second explanation

> An AI agent's decision to act is not the same as evidence that an action ran.
> Liminal Rail explores that boundary with bounded actions, explicit routes and
> checkable receipts. This small demo performs a local hash, verifies the result,
> refuses a repeated action ID and a changed proof, and stops an external-action
> request before dispatch. No model or paid API is needed for this demo.
>
> This is not a production security gateway. Its receipts are unsigned local
> consistency evidence, and duplicate protection lasts only for one running
> instance. We are looking for external developers to reproduce the five checks
> from a clean checkout and tell us where the instructions break.

## On-screen sequence

```sh
git rev-parse HEAD
docker compose up --build --wait rail
docker compose run --rm --no-deps demo
docker compose down
```

Explain each of the five check names in the README. Show `scope` and
`live_provider` as prominently as `demo`. Do not describe this as a live LLM,
Moltbook, TPM/TEE or distributed exactly-once demonstration.

## External tester invitation

> Could you try one small localhost agent-action demo from a clean checkout?
> It needs Docker Compose, but no accounts or API keys. Please record the exact
> SHA, OS/architecture, Docker/Compose versions, the five check results and any
> undocumented step. Even a reproducible failure is useful. Follow the pinned
> instructions in https://github.com/safal207/Liminal-Rail-Metro/issues/31 and
> post sanitized output there. Please do not expose the service or submit secrets.

An author/assistant rerun or CI runner is not the external tester required by #31.

## Draft announcement — not posted

> I am sharing Liminal Rail Metro, an experimental Go project for bounded agent
> actions and checkable receipts. The starting demo is deliberately small:
> local hashing, separate receipt verification, duplicate/tamper rejection and
> an external action that remains non-dispatched. MIT-licensed; not
> production-ready. Looking for clean-room testers rather than trust-me claims.
>
> Repository: https://github.com/safal207/Liminal-Rail-Metro
>
> What evidence would you require before trusting an agent's claim that it acted?

## Release decision

A live demo can be shown as a research prototype. A tag and formal
`developer-preview-v0.1` release must wait for the independent exact-SHA report
and an explicit final gate check. Do not close #30 or #31 from CI output alone.
