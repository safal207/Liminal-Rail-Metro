# Metro Web 005: persistent route memory

Stage 005 adds optional route memory across reader process restarts. It builds on
stage 004; the executable action registry remains the same read-only menu adapter.

```sh
go run ./cmd/metro-web-demo -addr 127.0.0.1:8788 -resource examples/metro-web/menu.json -graph-file examples/metro-web/graph.json
go run ./cmd/metro-web-demo -addr 127.0.0.1:8787 -remote http://127.0.0.1:8788 -remote-graph -memory ./routes.json
```

Use a dedicated file in an existing, operator-controlled local directory. The file
is created when absent; an empty file means empty memory. Stop the reader before
moving, editing, or deleting it. Only one reader may open a given file at once.
Without `-memory`, memory remains process-local. Offline `-export` cannot use it.

## What is remembered

The versioned JSON document contains at most 16 route entries, each at most 16
transition IDs; its input is capped at 32 KiB. Keys bind the effective graph,
configured remote origin, goal, adapter version and permissions. A successful
17th distinct entry clears the bounded cache before inserting the new route.
No menu items, prices, results, receipts, permissions, or executable code are saved.

A restored route is untrusted. Each run still fetches and validates the graph,
revalidates transitions and local action contracts, reads fresh menu bytes, checks
the current budget and produces new packets and receipts. A poisoned route hint
falls back to planning against the fresh graph; it does not execute its entries.
This replanning behavior also applies to corrupted process-local route hints.
Graph discovery failure still stops the run without using an old map.

Only confirmed runs write the cache. Denied, unknown, tampered, cancelled or
step-limited runs do not save. Existing confirmed routes can remain for a future
permitted run. The file's contents do not prove that any prior run succeeded.

## Persistence and reporting

The API adds `memory_storage` (`process` or `file`), `memory_restored` (selected
route loaded at startup and revalidated), `memory_saved` (this run's write and
file sync succeeded), and an optional `memory_warning`. `learned` continues to
mean that the successful route is in process memory. A disk failure does not
change an already verified action's `CONFIRMED_LOCAL` result; the API and UI report
the failed save separately. Local filesystem paths are not returned to browsers.

Linux uses a pinned no-follow directory-relative file open and an advisory lock.
Windows uses a directory-relative `NtCreateFile`, rejects reparse points and does
not share write/delete access. Both reject hard-linked files at open time, hold
the opened file for all writes, and refuse symlinked parents. Linux locks coordinate
cooperating readers; this is not protection from another privileged local writer.
File permissions/ACLs and directory ownership remain the operator's responsibility.
Native Windows API reference: [NtCreateFile](https://learn.microsoft.com/en-us/windows/win32/api/winternl/nf-winternl-ntcreatefile).

Writes are bounded in-place JSON followed by truncate and file sync. They are not
atomic transactions or a crash-recovery journal: power loss can lose or corrupt
the cache, including the first directory entry on Linux. A malformed nonempty file
fails startup without replacement. Recover by stopping the reader and selecting a
new empty file or removing the damaged cache, then learn routes again. The memory
file is loaded only at startup; edits are not hot-reloaded. Linux pathname removal
or replacement while running can detach the held file, so do not rename it live.

## Verification

Tests exercise restart with a changed remote menu and budget; new topology and
publisher origin; denial, response loss, receipt tampering, step limits and
cancellation; poisoned transition hints; bounded malformed files without overwrite;
exclusive open; symlinks, parent links and hard links; eviction; and save failures
that preserve the confirmed result while reporting persistence failure.
