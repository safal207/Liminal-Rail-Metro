# Remote Provider Boundary v0.7

v0.7 adds an HTTP remote-provider transport on top of the v0.6 bounded Fast Decision Plane without changing Metro's semantic policy or routing core.

## Purpose

The goal is not to guess any vendor API. The goal is to prove the boundary Metro will use when a real remote System-One provider is available.

```text
Metro Packet
    |
    v
v0.6 Decision Request
packet_hash + state_hash + choices_hash
    |
    v
RemoteProvider / HTTP JSON
    |
    v
untrusted Decision
    |
    +--> expected provider_id
    +--> request_id
    +--> action_id
    +--> packet_hash
    +--> state_hash
    +--> choices_hash
    |
    v
v0.6 ApplyDecision
    |
    +--> AUTO_ROUTE
    +--> ESCALATE_SYSTEM2
    +--> REQUIRE_APPROVAL
```

The remote transport does not replace v0.6 validation. It adds an earlier transport/binding gate and then hands the result to the existing v0.6 policy gate.

## Transport invariants

`RemoteProvider` requires an explicit HTTP(S) endpoint and expected provider identity.

The boundary is fail-closed:

- only HTTP(S) endpoint schemes are accepted;
- redirects are not followed;
- `Authorization`, `Content-Type`, and `Accept` cannot be overridden through arbitrary custom headers;
- an optional bearer token is attached only to the configured endpoint request;
- timeout is explicit and context cancellation is propagated through `http.NewRequestWithContext`;
- response bytes are bounded before JSON decoding;
- non-2xx responses surface as typed `RemoteHTTPError` values;
- response JSON uses `DisallowUnknownFields`;
- exactly one JSON value is accepted;
- returned `provider_id` must equal the configured expected identity;
- remote `request_id`, `action_id`, `packet_hash`, `state_hash`, and `choices_hash` must all match the outbound decision request.

Even after these checks pass, Metro still calls the v0.6 decision gate, which re-hashes packet/state/choices and validates the complete probability distribution, confidence, top-choice margin, allowed target, and side-effect policy.

## CI proof

The v0.7 workflow runs:

```text
gofmt                                                PASS
go test ./...                                        PASS
go test -race ./internal/decisionplane \
  ./internal/lifetrastation                          PASS
loopback HTTP remote-provider proof                  PASS
artifact upload                                      PASS
```

The successful proof performs a real HTTP POST to a local test endpoint, receives a bound probabilistic decision, passes the v0.6 gate, and produces:

```text
AUTO_ROUTE -> code-agent
```

Unit tests also cover:

- bearer/custom header propagation;
- provider identity mismatch;
- stale hash binding;
- timeout;
- HTTP 503 as typed error;
- unknown JSON response fields;
- oversized response bodies;
- unsupported endpoint schemes;
- reserved-header configuration;
- redirect rejection without contacting the redirect target.

## Regression proof

The same PR head also passes the existing layers:

```text
v0.2 Go -> Rust -> Go E2E          PASS
v0.3 persistent station            PASS
v0.4 multiplex station             PASS
v0.5 adaptive backpressure         PASS
v0.6 fast decision plane           PASS
v0.7 remote provider boundary      PASS
```

## TypeSafe / Jev boundary

v0.7 is intentionally **not** a TypeSafe/Jev adapter.

The generic remote endpoint is expected to speak Metro's own decision request/result contract. A future vendor adapter may translate between a vendor-specific SDK/API and these Metro types, but it must not weaken any v0.6/v0.7 binding or policy invariant.

Until the real external API contract is available and tested, this repository makes no claim of TypeSafe/Jev API compatibility.

## Claim ceiling

v0.7 proves only the local correctness of Metro's HTTP remote-provider transport and its integration with the existing decision gate.

It does **not** prove or measure:

- TypeSafe/Jev API compatibility;
- TypeSafe/Jev latency or accuracy;
- internet/WAN latency;
- provider calibration quality;
- remote-service availability;
- LLM inference speed;
- Lifetra execution speed;
- end-to-end AI-agent throughput.

The next vendor-specific bead should begin only from an actual provider contract, not a guessed endpoint or guessed JSON shape.
