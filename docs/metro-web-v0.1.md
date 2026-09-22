# METRO-WEB-001 — read-only graph and route-memory prototype

Status: developer prototype; local synthetic fixture only. Not a public gateway.

## Run

From the repository root with the project's Go toolchain:

```sh
go run ./cmd/metro-web-demo
```

Open `http://127.0.0.1:8787`. The process only binds IPv4 loopback. The menu is
synthetic Robis-themed data, not the real cafe's menu. No account or model key is
required. Stop the process with Ctrl+C; route memory is then lost.

For a portable, offline viewer of actual recorded local runs:

```sh
go run ./cmd/metro-web-demo -export metro-web-replay.html
```

The exported page explicitly says it replays captured results. Clicking it does
not execute Go, make network requests, or conduct a new verification. Animation
is presentation only, never a performance measurement.

## Implemented slice

A bounded breadth-first planner selects transition IDs before Metro chooses an
executor. Four distinct read-only actions can use the same executor:

```text
start --read_menu--> menu --filter--> filtered
      --ingredients--> details --present--> done
```

The advertised `purchase` edge is never executable in this prototype, even if
its scope is present. Graph data does not confer authority. A fixed runtime
allowlist additionally binds each implemented operation to its required source
and destination states. This is not the general v0.6 semantic provider or a
Lifetra authority integration; those existing modules are unchanged.

Each executed transition creates a fresh `metro.Packet`, uses `metro.Router`,
creates a local success receipt and checks its bindings with `metro.Verify`.
Result payloads include the graph digest, resource digest, next state and items.
No success receipt is exposed for an unknown or rejected step.

`GET /api/manifest` returns the same graph rendered by the browser after a run.
`POST /api/run` accepts only a bounded budget and a closed set of demonstration
scenarios. It never accepts a URL, arbitrary graph, credentials or permission
grant. HTTP guards cover Host/Origin, methods, JSON media type, unknown fields,
trailing JSON and a 2 KiB body limit. These are local-demo defenses, not a
production authentication or multi-tenant authorization system.

## Memory boundary

Memory stores only confirmed transition sequences. Its key includes the exact
Go JSON graph digest, goal, adapter version and effective permission snapshot.
It contains no cached menu responses, prices, credentials or rights grants.

A memory hit is revalidated against the current graph, scopes and step budget.
Every run rereads the fixture and refilters the current budget. New logical
runs use fresh action IDs; no retry/recovery protocol is implemented. Failure
or uncertainty never becomes new successful experience. Prior confirmed route
templates may remain available, but they still require revalidation and fresh
execution. At most 16 templates are retained; the demo clears that cache when
inserting beyond this bound. Runs are serialized, not throughput-optimized.

This is process-local route reuse, not model training, generalized skill
learning, durable memory, cross-agent experience sharing or speedup evidence.

## Fault demonstrations

| Scenario | Expected result | New successful experience |
| --- | --- | --- |
| First normal read | CONFIRMED_LOCAL; graph search | Yes |
| Repeat / changed budget | CONFIRMED_LOCAL; memory revalidated; fresh read | Yes |
| Changed graph version | New graph key and fresh planning | Yes if successful |
| Denied read | DENIED before dispatch | No |
| Injected response loss | UNKNOWN; one local read; no retry | No |
| Result modified after receipt creation | REJECTED | No |
| Cycle without a reachable goal | NO_ROUTE; bounded search | No |
| Two-step limit on a four-step path | STEP_LIMIT before dispatch | No |

Faults are deterministic local injections, not actual network/provider failures.
`CONFIRMED_LOCAL` asserts only the fixture execution, local bindings and goal
predicates checked here. A receipt hash is neither a signature nor an independent
external observation, and does not establish source truth or authorization.
The JSON hashing uses the existing Go implementation; no cross-language
canonicalization standard is claimed.

## Verification

```sh
go test -count=1 ./cmd/metro-web-demo
go test -race -count=1 ./cmd/metro-web-demo
go vet ./cmd/metro-web-demo
```

Author-side local checks: 16 top-level tests and 24 subtests passed; targeted
race checks and vet passed. Nine real loopback HTTP scenario runs passed. Nine
recorded UI scenarios were checked in Chromium using offline HTML rendering;
mobile layout had no horizontal overflow and no JavaScript runtime errors were
observed. Browser navigation to the live loopback endpoint was blocked by the
execution environment, so browser-to-live-server end-to-end coverage remains
unverified. The actual HTTP API and recorded UI were checked separately.

Local Go version was 1.23.2 with `GOTOOLCHAIN=local GOPROXY=off`. The workspace
contained the new package plus exact upstream `go.mod` and `internal/metro/metro.go`
from base `a239c6db21b0596f7ec672936db4205a43f2dfca`. The latter's Git blob was
verified as `a08f1c96eab8745ca4531def25ec9ec1ec3637b2`. This is targeted package
validation, NOT a full-repository regression or independent external review.

## Not included / next boundary

No real Robis connection, MCP adapter, LLM, remote resource fetching, upload,
CAS publication, renderer plugins, E2EE, new URI-scheme support, distributed
hosting, payment or model/token/latency benchmark is included. No existing core,
workflow, dependency, license, deployment, or default branch is changed by this
slice. The next acceptance boundary is one explicitly authorized, real read-only
resource and a client that consumes the graph without any new write authority.
