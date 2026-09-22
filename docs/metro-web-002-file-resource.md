# Metro Web 002: a versioned file resource

This stage connects the existing read-only graph to an operator-selected JSON
file. A route-memory hit reads the file again. Editing a price changes the next
result and resource version without discarding a still-valid route template.
The bundled menu remains fictional; this is real filesystem and HTTP I/O, not
an integration with a cafe or an LLM.

## Run

From the repository root, with Go 1.23.12:

```sh
go run ./cmd/metro-web-demo -resource examples/metro-web/menu.json
```

Open `http://127.0.0.1:8787/`, run the route, and use **Проверить файл по паспорту**.
The browser fetches the exact bytes and independently computes SHA-256 and size.
Change `price_rub` in the file, run again, and compare the result and passport.
For concurrent publishing, replace the complete file atomically; an in-place
edit may be rejected while the document is incomplete. Each accepted snapshot
identifies the bytes actually read, not an atomic transaction with the writer.

The original synthetic mode remains available by omitting `-resource`.
`-export` still records nine synthetic runs and rejects use with `-resource`,
so an offline recording cannot silently claim to reflect a supplied file.

## Discovery and delivery

| Endpoint | Result |
|---|---|
| `GET /api/manifest` | Graph, including `resources[].manifest` and its `read_edge` |
| `GET /api/resource` | Current file's passport; validates and reads fresh bytes |
| `GET /api/resource/content?sha256=<digest>` | Exact current bytes, only if the requested digest matches |
| `POST /api/run` | Existing `{ "budget": 300, "scenario": "normal" }` route request |

The passport includes protocol `metro.web.resource.v0.1`, stable ID `menu`,
title, schema, media type, exact-byte SHA-256, byte count, `sha256:<digest>`
version, pinned content URL and the graph adapter's `menu.read` scope.
Changing whitespace also changes the version. `resource_hash` in file-mode
results is the exact file digest; synthetic mode retains its JSON item hash.
Every successful file-mode step's receipt binds both the digest and passport.

Content requests without exactly one valid lowercase SHA-256 return 400. A
valid old digest returns 409 after the file changes: re-fetch the passport.
No historical archive is provided. A missing or invalid file returns 503 from
resource endpoints; the route returns a `REJECTED` outcome, with no success
receipt or cached result substituted. The existing route memory survives for
use after the file is repaired. Unknown query keys and client-selected paths
are rejected. File mode exposes only the path selected at server startup.

For an agent, fetch the graph, discover the passport, retrieve its `href`, then
verify SHA-256 and byte count before interpreting the document. For example,
against this local server, this Python standard-library reader verifies bytes:

```python
import hashlib, json, urllib.request

base = "http://127.0.0.1:8787"
def read(path):
    with urllib.request.urlopen(base + path, timeout=5) as response:
        return response.read(1048577)

graph = json.loads(read("/api/manifest"))
passport = json.loads(read(graph["resources"][0]["manifest"]))
content = read(passport["href"])
assert len(content) == passport["size_bytes"] <= 1048576
assert hashlib.sha256(content).hexdigest() == passport["sha256"]
menu = json.loads(content)
print(menu["title"], passport["version"])
```

Discovery, content retrieval and route execution are separate snapshots. If the
file changes between them, compare the route's own `resource.sha256` against the
passport you fetched. A route always uses one fresh snapshot throughout its
four steps; it does not accept a client-specified version pin for execution.

## Menu format and limits

See `examples/metro-web/menu.json`. UTF-8 JSON must contain `schema`, `title` and
an `items` array; schema is `metro.web.menu.v0.1`. Empty arrays are supported.
The adapter rejects unknown fields and trailing JSON, files above 1 MiB,
non-regular files, over 128 items, duplicate/empty item IDs, empty names, prices
outside 1–1,000,000 whole rubles, and missing/empty ingredients. IDs are bounded
at 80 UTF-8 bytes; titles, names and each ingredient at 200; ingredients at 32.
Availability defaults to false if omitted. Free items are outside this schema.

The `new_version` scenario changes the graph version in file mode, without
altering the file or its data. Other fault scenarios retain their 001 behavior.
Denial and planning failures stop before the route opens the resource file.

## Evidence boundary

The server remains IPv4 loopback-only with the existing Host/Origin checks.
Scopes and the denied scenario are demonstrations of adapter policy, not user
authentication: local API callers can read the configured file. Do not select a
secret-bearing file. There is no upload, arbitrary path access, external fetch,
write operation, historical CAS, signature or independent external attestation.
SHA-256 proves agreement with the passport's bytes, not publisher identity.

Validation covers file edits with route reuse, original receipt stability,
passport tampering, lost files and recovery, exact bytes, stale versions,
discovery, HTTP guards, malformed data, and the existing graph scenarios.
