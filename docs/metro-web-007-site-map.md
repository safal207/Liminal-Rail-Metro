# Metro Web 007: a bounded map of named resources

This describes the Stage 007 contract. [Stage 009](metro-web-009-client-challenge.md)
later adds an optional `client_challenge` to route requests while preserving
the request and response shape documented here for older readers.

Stage 007 connects the graph-navigation idea to more than one arbitrary file.
It is an opt-in, read-only local prototype. The existing menu and single-file
modes remain separate and unchanged.

## Run the example

From the repository root, start the publisher in one terminal:

```sh
go run ./cmd/metro-web-demo -addr 127.0.0.1:8788 -site-config ./examples/metro-web/site-007/site.json
```

Start the reader in another terminal:

```sh
go run ./cmd/metro-web-demo -addr 127.0.0.1:8787 -remote-site http://127.0.0.1:8788
```

Open `http://127.0.0.1:8787/`. Choose `spec` or `page` and run the route.
Both targets require passing through `guide`; each reached file is read and
verified. Repeating a target can revalidate a remembered sequence of transition
IDs, but still reads fresh bytes. `page.html` is delivered as a download,
never executed or embedded in the reader page.

## Publisher contract

The operator chooses a local JSON config at startup. Its resource paths resolve
relative to the config file, or may be absolute operator-selected paths. The
publisher opens them with the existing pinned no-follow file reader. Callers
cannot submit paths or URLs. Only resource IDs, labels, graph edges, sizes,
digests, and fixed same-origin endpoints appear in the public map.

`GET /api/site` publishes `metro.web.site.v0.1`: a start node, named
resource nodes, and explicit `open_resource` edges. Every executable edge has
`scope=resource.read`, `side_effect=false`, and a resource ID matching the
destination node. The reader rejects an unknown action, write edge, foreign
manifest URL, ambiguous JSON or invalid binding before content I/O. A
publisher's map describes a possible path; it cannot grant permission.

`GET /api/site/assets/{id}` publishes a versioned binary passport.
`GET /api/site/assets/{id}/full?sha256=<whole-hash>` returns one bounded
snapshot as `application/octet-stream` with attachment headers. An old hash
returns HTTP 409. The reader builds these endpoints from validated resource
IDs; it never follows a publisher-supplied URL. The reader checks the fresh map,
passport, complete SHA-256 and every advertised chunk digest before releasing
the file bytes.

`POST /api/site/run` accepts only `{"target":"resource-id"}`. It plans a
bounded route to that target, executes each read as a fresh Metro Packet and
Route, verifies the returned bytes, and exposes a locally verified Receipt for
each confirmed transition. If a read is incomplete or cannot be verified, the
run stops without a success receipt for that step or automatic retry. Memory
stores only confirmed transition IDs, keyed by the effective map digest,
target, scope and fixed origin; it never stores file contents or rights.

## Limits and trust

The map has 2–8 resources, at most 16 nodes and 32 edges. Each file is capped
at 8 MiB, their total at 16 MiB, and a route at eight transitions under a
bounded deadline. Cycles may be described, but a route cannot revisit a node.
Both local servers bind only IPv4 loopback. A remote origin is fixed at startup;
the existing transport forbids proxies, redirects and automatic retries.
Reader-to-reader chains are refused.

A fresh map currently rereads every configured file to compute current
digests. This intentionally makes content changes visible in the map and route
key, but makes map publication proportional to all configured bytes. The
publisher and reader do not share an atomic snapshot across HTTP requests:
version pins detect changes and stop the route instead of silently mixing
versions.

The receipts prove local binding and verification performed by this demo, not
independent publisher identity, source truth or external authorization.
Stage 007 does not render websites, execute publisher code, discover arbitrary
Internet origins, accept uploads/writes, or provide an MCP/A2A wire adapter.

## Verification

```sh
go test -race -count=1 -timeout 120s ./cmd/metro-web-demo ./internal/metro
go vet ./cmd/metro-web-demo ./internal/metro
```

The site tests exercise two-file and branching routes over real local HTTP,
fresh reads after route-memory hits, stale pins, hostile maps and passports,
reader-chain rejection, cancellation, and size/step limits. Record the actual
platform, command result and any live-demo check with each PR.

On 2026-09-23, Go 1.23.12 on Windows/amd64 passed the targeted race suite and
`go vet` for `cmd/metro-web-demo` and `internal/metro`. A local publisher and
reader also completed a two-transition route to `spec` with two verified
receipts. Repeating it revalidated route memory while making two fresh reads
and using new action IDs. The `page.html` bytes matched the local file;
duplicate request keys returned HTTP 400, and a changed resource rejected an
old pin with HTTP 409. The served HTML and JavaScript syntax were checked;
visual browser interaction was not available in this environment.

On 2026-09-24, after review feedback, the targeted race suite and `go vet`
passed again. A local Node VM with a simulated DOM exercised the browser
script: duplicate labels remain distinguishable by ID, the displayed map
updates from a run response, a still-present target stays selected, a removed
target falls back to the first available resource, and a failed request clears
the previous result. This checks UI logic, not visual layout or browser input.
