# Metro Web 006: one bounded binary file

Stage 006 adds a separate read-only transport for one operator-selected file of
any byte content. Its use is independent of the menu actions and graphs in
stages 001–005. Those modes keep their existing flags and contracts.

In one terminal, start the publisher:

```sh
go run ./cmd/metro-web-demo -addr 127.0.0.1:8788 -asset ./example.bin
```

In another terminal, start the reader:

```sh
go run ./cmd/metro-web-demo -addr 127.0.0.1:8787 -remote-asset http://127.0.0.1:8788
```

Open the reader on `http://127.0.0.1:8787`. Refresh the passport, verify one
block, or fetch all blocks to verify the whole file. Both servers bind only to
IPv4 loopback. The publisher's file is fixed at startup; API callers cannot
select another path. Remote origins are fixed at startup and require HTTPS
except for literal loopback IPs. Client requests cannot supply an arbitrary URL.

## Protocol

`GET /api/asset` returns `metro.web.asset.v0.1` JSON with fixed ID `asset`,
`application/octet-stream`, byte size, whole-file SHA-256, 64 KiB chunk size,
ordered chunk SHA-256 values, a `sha256:` version, and the fixed content path.
The maximum file size is 8 MiB, so the hash list has at most 128 entries. An
empty file has zero entries and the standard SHA-256 digest of empty bytes.

`GET /api/asset/content?sha256=<whole-hash>&index=<zero-based-decimal>` returns
only one block, with `application/octet-stream` and attachment headers. The
publisher rereads the configured file and returns HTTP 409 if its whole-file
hash differs from the pinned hash. Duplicate/extra query keys, malformed
hashes, out-of-range chunk indices, redirects, compressed responses and
oversized data are refused. No automatic transport retry is performed.

A remote reader first validates the bounded passport, then asks its fixed
publisher for the selected pinned block. It checks the exact block length and
SHA-256 before relaying the bytes. A checked block proves agreement with the
publisher's passport; it does **not** verify the whole-file digest independently.
The browser's “Проверить весь файл” action fetches every block, verifies each
block, concatenates their bytes and checks the total size and whole SHA-256.
The passport itself is a publisher claim, not an external signature or proof
of business truth. No file bytes are interpreted or executed by the server.

## Boundaries

The local publisher reuses the no-follow, pinned-directory file reader. A
symlinked parent/leaf or nonregular file is rejected; changed content fails
the requested digest pin. The HTTP reader reuses the existing fixed-origin
transport with no proxy, redirects, compression or automatic retry. Relay
markers prevent cycles between two readers. Both UI and API apply the same
loopback Host/Origin guard.

The publisher currently reads the bounded whole file to compute a fresh
passport on each request. This trades local I/O for simple version pinning;
chunk requests save network transfer but are not constant-I/O on the publisher.
The 8 MiB cap, one configured file, no general graph action for that file, and
no write/upload capability are explicit limits of this stage. Later graph
adapters can refer to this resource without treating publisher-declared
actions as executable authority.

## Verification

The test suite covers arbitrary bytes including NUL/non-UTF-8, block boundaries,
empty and maximum-size files, file changes between passport and block reads,
malformed claims, forbidden links, source/reader loops, cancellation, and menu
regression. Exact validation results are recorded with the stage deliverables.
