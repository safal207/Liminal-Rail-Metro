# Liminal Rail Metro

**High-speed execution and routing protocol for AI agents.**

> Agents think. Liminal Rail moves.

Liminal Rail Metro is an experimental open protocol and Go engine for moving bounded AI-agent actions across specialized agents and tools with explicit routing, stable action identity, and verifiable execution receipts.

The project starts from one narrow question:

> Can one agent hand off one bounded action to another agent through a router, without re-sending unnecessary context, while preserving enough identity and evidence to verify what actually executed?

## Engine

**Go is the primary runtime for Liminal Rail Metro.**

The engine is intentionally designed around Go's strengths for this problem: lightweight concurrency, networking, predictable deployment, small binaries, and a strong standard library.

Current layout:

```text
cmd/metro-demo/        executable proof
internal/metro/        Go protocol engine
protocol/              JSON protocol schemas
examples/              protocol journeys
docs/                  architecture and claim ceiling
```

## v0.1 scope

The first version intentionally stays small:

1. A **packet** describes one bounded action.
2. A **route decision** selects the next execution target from allowed targets.
3. The target executes the action.
4. A **receipt** records what executed and binds the result back to the original action.

```text
Research Agent
      |
      v
+-------------+
| Metro Gate  |
+-------------+
      |
      v
+-------------+
| Fast Router |
+-------------+
      |
      v
+-------------+
| Code Agent  |
+-------------+
      |
      v
+-------------+
|  QA Agent   |
+-------------+
      |
      v
Execution Receipt
```

## Core artifacts

- `protocol/metro.packet.v0.1.json` — bounded action envelope
- `protocol/metro.route.v0.1.json` — route decision envelope
- `protocol/metro.receipt.v0.1.json` — execution evidence envelope
- `examples/research-code-qa.json` — minimal example journey
- `internal/metro/metro.go` — Go engine core
- `internal/metro/metro_test.go` — protocol invariant tests
- `cmd/metro-demo/main.go` — executable Go demonstration
- `docs/architecture.md` — v0.1 architecture and claim ceiling

## Design principles

### 1. Stable action identity
Every action receives an `action_id` before dispatch. Retries and recovery must refer to the same logical action rather than silently creating a new one.

### 2. Minimal context movement
Packets should carry only the context required for the next bounded action. Large histories should be referenced, not copied by default.

### 3. Routing is separate from reasoning
A fast decision layer may choose among already-valid actions or destinations. Deep planning remains the job of a larger reasoning model when needed.

### 4. Execution must produce evidence
A route decision is not proof of execution. A receipt binds the action, selected target, inputs, and result evidence.

### 5. Fail closed on uncertainty
If execution identity or completion is uncertain, the system should surface `UNKNOWN` rather than claim success or blindly redispatch a side effect.

## Non-goals for v0.1

This repository does **not** yet claim:

- exactly-once execution across arbitrary distributed systems;
- cryptographic trust between independent organizations;
- production-grade scheduling, billing, auth, or service discovery;
- benchmark superiority over existing queues, buses, or agent frameworks;
- autonomous safety for high-risk actions.

The first milestone is only to make the handoff and evidence model explicit, small, and testable.

## Run the Go demo

```bash
go run ./cmd/metro-demo
```

Run the invariant tests:

```bash
go test ./...
```

The demo creates a packet, makes a deterministic route choice from allowed targets, executes a toy bounded action, and emits a receipt whose hashes can be independently recomputed.

## Status

`v0.1` — protocol seed / experimental Go engine.

Contributions should preserve the narrow claim ceiling: make the protocol more independently verifiable before making it more ambitious.

## License

MIT
