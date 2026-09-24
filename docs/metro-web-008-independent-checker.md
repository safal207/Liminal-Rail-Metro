# Metro Web 008: independent route and receipt checker

Stage 008 adds a separate command for someone who wants to inspect a
[Metro Web 007](metro-web-007-site-map.md) site without using the browser or
trusting the demo reader's internal code path.
The checker implements its own bounded wire parser, graph planner and byte
verification. It uses only the public Stage 007 HTTP endpoints and does not
call the demo's private `siteService` or its HTTP resource reader.

## Run the sample

In one terminal, publish the Stage 007 example from the repository root:

```sh
go run ./cmd/metro-web-demo -addr 127.0.0.1:8788 -site-config ./examples/metro-web/site-007/site.json
```

In another terminal, ask the independent checker to inspect `spec`:

```sh
go run ./cmd/metro-web-check -origin http://127.0.0.1:8788 -target spec
```

Point `-origin` at the **publisher**, not at a Metro reader: Stage 007 refuses
reader-to-reader chains. Outside literal loopback IP addresses, the checker
requires HTTPS. It does not use system HTTP proxies or follow redirects.

The command should exit successfully only after it finds a bounded path from
`entry` through `guide` to `spec`, fetches and verifies both resources, and
cross-checks the demo's route and receipts against those independently read
bytes. It prints a machine-readable `VERIFIED_BYTES_AND_TRANSCRIPT` result with the
ordered edge/resource IDs, SHA-256 values and receipt IDs. Explicit
`run_freshness_verified: false` and `publisher_identity_verified: false`
fields keep those unproven properties visible to other agents. A failed or
uncertain check exits nonzero with `REJECTED` or `UNKNOWN` respectively; it
must not present a partial read as confirmed.

## What this checks

The operator supplies one fixed HTTP(S) origin and one resource ID. The
checker obtains a fresh `metro.web.site.v0.1` map, validates the graph and
resource bindings, and builds asset requests from validated IDs. It never
follows URLs supplied in the map or passport. For every transition on the
chosen path it checks the resource passport, pinned full-file transfer,
whole-file SHA-256 and every advertised 64 KiB chunk digest. It then calls
`POST /api/site/run` and checks that its map, ordered transitions, Packet,
Route and successful Receipt fields agree with the independently obtained
resource bytes and the requested goal. A final map read detects a version
change during the check.

The local demo remains the producer of its receipts. This command verifies
the bytes fetched by **its own** requests and the consistency of the returned
route/receipt transcript. A precomputed transcript for unchanged bytes could
pass; there is no client challenge proving that the publisher actually
re-read files during `POST /api/site/run`. The result therefore must not be
read as proof of execution freshness. It also does not attest the publisher's
real-world identity, grant permission, prove that the publisher's information
is true, or turn this preview into a general web browser. It runs read-only
transitions and cannot upload, write or execute publisher content.

## Verification

Run the checker's targeted tests and the Stage 007 demo tests:

```sh
go test -race -count=1 -timeout 120s ./cmd/metro-web-check ./cmd/metro-web-demo ./internal/metro
go vet ./cmd/metro-web-check ./cmd/metro-web-demo ./internal/metro
```

Record a live clean-clone run from the checked-out commit before claiming
outside interoperability. A successful local test is not a report from an
independent developer.
