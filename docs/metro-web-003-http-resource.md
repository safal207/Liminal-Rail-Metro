# Metro Web 003: a resource across HTTP

The reader can now consume a Metro menu from a separate publisher over HTTP(S).
It fetches a fresh passport, then its version-pinned bytes, checks their size
and SHA-256, validates the menu, and only then continues the local graph.
Remembered routes never reuse an old menu result.

## Two-process demonstration

Build once, then run these in separate terminals from the repository root:

```sh
go build -o metro-web-demo ./cmd/metro-web-demo
./metro-web-demo -addr 127.0.0.1:8788 -resource examples/metro-web/menu.json
./metro-web-demo -addr 127.0.0.1:8787 -remote http://127.0.0.1:8788
```

On Windows use `metro-web-demo.exe`. Open the reader at
`http://127.0.0.1:8787/`. The publisher owns the file; the reader is configured
only with its origin URL. Edit a price at the publisher and run again. Stop the
publisher to see an `UNKNOWN` read with no success receipt, automatic retry or
substitution of the old result. Restart it and manually run the route again.
The sample menu remains fictional; these are actual network requests between
two local processes.

## Wire contract

`-remote` accepts an origin, for example `https://publisher.example`, without
credentials, a path, query or fragment. HTTPS uses normal certificate validation.
Plain HTTP is accepted only for a literal loopback IP, never a hostname.
The server itself still listens only on IPv4 loopback.

The adapter supports the existing `metro.web.resource.v0.1` menu contract:

1. `GET /api/resource`, at most 8 KiB, with the exact supported protocol, menu
   schema, scope, digest, title and version fields.
2. `GET /api/resource/content?sha256=<lowercase digest>`, at most 1 MiB.
3. Verify exact byte count and SHA-256, parse the bounded menu document, and
   compare the reconstructed passport against every advertised field.

The second URL must be exactly the expected relative content path; the publisher
cannot choose another endpoint or origin. Both responses must be JSON and have
no Content-Encoding. Redirects, credentials, cookie state and environment
proxies are not used. The HTTP/1 transport opens fresh connections, preventing
automatic retries of idempotent requests on reused idle connections.

One read has a three-second overall deadline shared by both requests. Caller
cancellation is propagated. The engine retains serialized route execution;
queueing time is separate from the network-read deadline. `http_attempts`
counts requests attempted by this adapter, not confirmed server executions.
It is normally 2; planning denial, an unreachable graph or the step limit
produce 0 outbound attempts. `fresh_reads` counts verified complete snapshots.

| Outcome | Route status |
|---|---|
| Both responses verify | `CONFIRMED_LOCAL` after the remaining graph steps |
| Connection loss, timeout, incomplete body, upstream 5xx | `UNKNOWN`; no retry or success receipt |
| HTTP 409 between passport and content | `REJECTED`; manually run again for a new snapshot |
| Invalid passport, bytes, schema, redirect or other status | `REJECTED` |

Failed reads create no learned route and return no cached items. Previously
confirmed route memory remains usable when a later read succeeds.

## Local graph and evidence

The action graph is still the reader's fixed menu adapter, version
`menu-http-1`. This stage does not execute publisher-supplied actions or learn
an arbitrary remote graph. The configured publisher origin is part of graph
identity, packet input, and each successful step's receipt-bound result as
`resource_source`. This provenance comes from startup configuration, not from
the publisher's response.

The reader exposes its existing resource endpoints for the same-origin browser
UI. Each verification request obtains a fresh verified upstream snapshot; it
does not archive previous versions. An old local content pin returns 409 if
the publisher has changed. Reader-to-reader chains are rejected with HTTP 508
when the internal `X-Metro-Resource-Read` marker is present, preventing cycles
between cross-configured demo readers. This marker only restricts relaying; it
does not grant access.

The file reader and offline synthetic replay remain available. `-remote`,
`-resource` and `-export` are mutually exclusive. The local HTTP API does not
accept a destination URL from the browser.

## Validation and limits

Tests cover real publisher-to-reader HTTP, updated files with route reuse,
publisher identity in receipts, rejection of stale local pins and reader chains,
malformed passports, unexpected URLs, changed bytes, oversize responses, lost
bodies, unavailable publishers, cancellation, no requests after denial, and
TLS trust. Existing Linux/Windows file-adapter tests remain in the CI matrix.

An exact hash demonstrates consistency with the supplied passport; it does not
prove the publisher's business claims. This remains a read-only menu prototype:
no public hosting, uploads, publisher signatures, historic CAS, LLM, remote
action execution or integration with a real cafe. A compatible public HTTPS
publisher can be selected by the operator, but the demonstration uses loopback.
