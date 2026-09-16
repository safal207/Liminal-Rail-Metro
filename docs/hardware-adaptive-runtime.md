# Hardware-Adaptive Runtime v0.1

This bead tests one narrow claim:

> Can Liminal Rail improve a bounded hardware-facing execution choice from measured runtime evidence, without changing model weights or granting arbitrary hardware authority?

The v0.1 answer is an executable Go proof around CPU worker-count selection.

## Loop

```text
SENSE -> CHOOSE -> ACT -> MEASURE -> OBSERVE -> CHOOSE ...
```

1. `SenseHost` captures small, read-only runtime telemetry.
2. `UCB1Policy` selects only from an explicit allow-list such as `workers=1..5`.
3. The demo executes a fixed SHA-256 workload using the selected worker count.
4. Measured throughput is converted directly into a reward.
5. `Observe` updates the policy online.
6. The next decision uses the accumulated measurements.

The executable writes `hardware-adaptive-proof.json` so the run leaves inspectable evidence rather than only console output.

## Run

```bash
go test ./...
go run ./cmd/hardware-adaptive-demo
```

Example shape:

```text
round=01 action=workers=1 throughput=... MiB/s
round=02 action=workers=2 throughput=... MiB/s
...
best_observed=workers=N mean_reward=... MiB/s
proof=hardware-adaptive-proof.json
```

Results are intentionally environment-specific. A faster observed action in one container, laptop, server, or thermal state is not claimed to be globally optimal elsewhere.

## Safety boundary

The policy cannot invent commands, devices, targets, or worker counts. It can only select from the actions supplied when the policy is created. Rewards for actions outside that allow-list are rejected.

This bead is therefore a bounded decision layer, not autonomous hardware control.

## Claim ceiling

v0.1 demonstrates:

- online policy updates from real execution measurements;
- bounded selection among pre-authorized hardware-facing actions;
- read-only host telemetry with no privileged dependency;
- an inspectable JSON proof artifact;
- deterministic tests for exploration, learning, and allow-list rejection.

v0.1 does **not** demonstrate:

- modification of LLM/model weights;
- GPU/NPU/accelerator scheduling;
- globally optimal scheduling;
- adaptation guarantees under rapidly changing workloads;
- safe control of fans, voltages, clocks, power limits, firmware, or other privileged hardware settings;
- production-grade persistence or distributed coordination of learned policy state.

## Next bead

The next useful step is `v0.2 contextual policy`: include workload identity and runtime state in the decision key, then preserve the selected action and measured reward in a Metro receipt / Lifetra observation so adaptation remains evidence-bound across runs.
