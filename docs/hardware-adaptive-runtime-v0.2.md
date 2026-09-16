# Hardware-Adaptive Runtime v0.2

This bead tests a narrow claim:

> Can Liminal Rail improve a bounded hardware-facing execution choice from measured runtime evidence, keep learned evidence separate by workload/runtime context, and preserve the choice/reward provenance through Metro and Lifetra without changing model weights?

The v0.2 answer is an executable Go proof around CPU worker-count selection.

## Loop

```text
SENSE -> CONTEXT -> CHOOSE -> ACT -> MEASURE
                              |        |
                              |        v
                              |     RESULT
                              |        |
                              v        v
                         METRO ROUTE -> RECEIPT
                                         |
                                         v
                              ADAPTIVE EXPERIENCE
                                         |
                                         v
                               LIFETRA OBSERVATION
                                         |
                                         v
                                      ADAPT
                                         |
                                         +-----> next CHOOSE
```

## What changed from v0.1

1. `ContextFromHost` buckets workload identity, logical CPU class, load class, and available-memory class.
2. `ContextualUCB1Policy` keeps independent UCB1 evidence tables per context key.
3. Each execution creates a normal Metro packet and adaptive route.
4. The measured result is hashed into a Metro receipt.
5. `receipt.result_ref` names the corresponding `liminal.adaptive.experience.v0.1` record.
6. The experience records the context, selected action, reward, and policy statistics before/after the observation.
7. `ReceiptToAdaptiveObservation` carries the scoped adaptive experience ref into Lifetra `proof_refs` without changing the base bridge semantics.
8. The next packet links to the previous receipt and previous adaptive experience.

That gives an inspectable chain:

```text
packet(action_id=N)
  -> route(selected_action)
  -> measured result(reward)
  -> receipt(result_hash, result_ref)
  -> adaptive experience(context + reward + policy delta)
  -> Lifetra observation(receipt_hash + proof_refs)
  -> next packet(previous_receipt_ref + previous experience context ref)
```

## Run

```bash
go test ./...
go run ./cmd/hardware-adaptive-v02-demo
```

The demo writes `hardware-adaptive-proof-v0.2.json`.

Results are intentionally environment-specific. A faster observed action in one container, laptop, server, or thermal state is not claimed to be globally optimal elsewhere.

## Safety boundary

The policy cannot invent commands, devices, targets, or worker counts. It can only select from the construction-time allow-list. Context changes choose a different evidence table; they do not expand authority.

The runtime only reads coarse host telemetry and executes the bounded demo workload. It does not control clocks, voltages, fans, firmware, kernel policy, or arbitrary shell commands.

## Claim ceiling

v0.2 demonstrates:

- online policy updates from real execution measurements;
- bounded selection among pre-authorized hardware-facing actions;
- context-local learning keyed by coarse workload/runtime state;
- stable context identifiers derived from canonical context JSON;
- Metro packet/route/receipt provenance for each adaptive action;
- SHA-256 binding of the measured result through the Metro receipt;
- an adaptive experience record containing reward and pre/post policy evidence;
- propagation of a scoped adaptive `receipt.result_ref` into Lifetra observation proof refs;
- causal chaining into the next packet through prior receipt/experience refs;
- deterministic tests for context separation, evidence advancement, learning, and allow-list rejection.

v0.2 does **not** demonstrate:

- modification of LLM/model weights;
- GPU/NPU/accelerator scheduling;
- globally optimal scheduling;
- guaranteed convergence under non-stationary workloads;
- safe control of privileged hardware settings;
- durable policy persistence across process restarts;
- content-addressed immutability of the adaptive experience object itself;
- distributed coordination between multiple adaptive runtimes;
- production-grade authorization for hardware side effects.

## Next bead

A useful v0.3 is durable, replay-safe adaptation: persist context-policy state and content-addressed measurement evidence, then prove that restart/replay cannot double-apply one reward or silently learn from an unverified receipt.
