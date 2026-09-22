# Metro Web 004: publisher-supplied route maps

The reader can now discover a bounded graph from its configured publisher before each run. The publisher chooses station and transition IDs, labels, topology and version. The reader owns the small action vocabulary and its permission checks. A supplied graph cannot install code, select another network destination, or grant write access.

## Run the two-process demo

Build the executable, then start these commands in separate terminals:

```sh
go build -o metro-web-demo ./cmd/metro-web-demo
./metro-web-demo -addr 127.0.0.1:8788 -resource examples/metro-web/menu.json -graph-file examples/metro-web/graph.json
./metro-web-demo -addr 127.0.0.1:8787 -remote http://127.0.0.1:8788 -remote-graph
```

Use `metro-web-demo.exe` on Windows. Open the reader at `http://127.0.0.1:8787/` and run the normal scenario twice. The first run plans path A; the second revalidates that path, fetching the map and menu again. Replace the publisher's `graph.json` with `graph-alternate.json`: path A is no longer available, so the next run plans path B. Restore the original graph to try the original remembered path again.

Both paths perform the same four local read operations on the fictional menu. They demonstrate topology and route identity, not distinct external services or model training. Remove both ingredient transitions to produce NO_ROUTE before any menu request. Stop the publisher to produce UNKNOWN at discovery, without falling back to the old map.

## Graph v0.2 contract

The existing `GET /api/manifest` endpoint serves the configured graph's exact file bytes. The reader's opt-in `-remote-graph` mode accepts only `metro.web.graph.v0.2`, limited to 64 KiB, 16 nodes and 32 edges. Unknown fields, trailing data, invalid UTF-8, bad references, duplicate IDs and unsupported contracts are rejected.

Nodes declare a `kind`; transitions declare an `action` separately from their publisher-defined `id`. IDs use ASCII letters, digits, dot, underscore or hyphen. The goal must have kind `done` and the initial node kind `start`. These local contracts are required for every executable transition:

| Action | Required source kind | Result kind |
| --- | --- | --- |
| read_menu | start | menu |
| filter | menu | filtered |
| ingredients | filtered | details |
| present | details | done |

Executable transitions require `menu.read` and `side_effect: false`. Advertised side-effect transitions may remain on the map, but are never executable. Marking a purchase as read-only does not make it a supported action. The map must bind exactly one menu read edge to the fixed `/api/resource` endpoint; publisher-supplied origins and arbitrary endpoint paths are rejected.

The reader selects a permitted path and rechecks each transition's local contract before dispatch. The configured origin is inserted into the effective graph by the reader. Requests still use the startup-configured origin and existing HTTP adapter; graph data never becomes a URL to fetch.

## Snapshots, memory and evidence

Each ordinary run makes one graph request followed by two menu requests (passport and pinned content). `graph_http_attempts` counts discovery separately from menu `http_attempts`. A denied task sends neither kind. A graph without a usable path or a two-step limit makes only the graph request.

Discovery and menu delivery share one three-second deadline once the remote run leaves the cancellable execution queue. Discovery failure produces no packets, receipts, items or new learned path. Previously learned paths remain in memory but cannot run without a freshly validated map.

The semantic effective-graph hash, including configured origin, keys route memory. Changing topology invalidates the key even if a publisher forgets to change the version string. Every packet and successful receipt also binds `graph_source`: source transport, configured origin and the exact received graph byte digest. Whitespace alone changes the byte digest but does not change semantic route identity.

A run uses one validated graph snapshot throughout execution. A publisher changing the graph during that run takes effect on the next run. The map and menu are separate snapshots, not an atomic versioned website bundle. Discovery is metadata retrieval before Metro action dispatch; it does not have its own action receipt.

Hashing records the received bytes; there is no independently trusted graph digest or signature. These checks establish the local action contract and receipt consistency, not the publisher's business truth. The prototype does not archive old graph bytes, execute remote code, support arbitrary action types, or publish a public server.

## Compatibility and verification

The stage-003 fixed-graph `-remote` mode remains available without `-remote-graph`. File and synthetic modes also remain. `-graph-file` requires `-resource`; `-remote-graph` requires `-remote`. Graph readers reject reader-to-reader relay chains with HTTP 508.

The browser draws publisher-defined IDs and branches and shows graph provenance. For a published map, graph version/topology changes are made at the publisher; the UI omits the old local version/cycle injections. Denial, response loss, tampering and step limits remain available.

Tests cover actual graph publication and reads, path changes with an unchanged version, fresh menus with remembered paths, immutable old receipts, rejected graph provenance changes, malformed or unsafe maps before menu I/O, failed discovery without cached fallback, denial, bounded routes, cancellation and relay loops.
