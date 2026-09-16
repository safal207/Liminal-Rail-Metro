# Filesystem CAS + `liminal-proof` CLI v0.1

## Goal

Make a content-addressed evidence bundle independently verifiable outside a Go caller.

The verifier should need only:

```text
1 evidence-bundle JSON
+ 1 filesystem CAS directory
```

It must not rely on caller-supplied Packet, Route, Result, Receipt, AuthorityPolicy, ProofEnvelope, or ReplayReport objects.

## Filesystem CAS layout

`FilesystemCAS` stores objects by raw SHA-256 digest:

```text
<root>/
└── sha256/
    └── ab/
        └── abcdef...<64 hex chars total>
```

The corresponding reference is:

```text
cas://sha256/abcdef...
```

### Write semantics

`Put(content)`:

1. hashes the exact input bytes;
2. creates the digest shard directory;
3. writes into a temporary file in that same directory;
4. fsyncs and closes the temporary file;
5. atomically renames it to the digest path;
6. resolves the committed object again and verifies its digest before returning.

If an object already exists at that digest path, it is verified before being reused. A corrupted existing object fails closed rather than being silently replaced.

### Read semantics

`Resolve(ref)`:

1. accepts only `cas://sha256/<64 lowercase hex>` refs;
2. maps the digest to the shard path;
3. reads the exact bytes;
4. recomputes raw-byte SHA-256;
5. returns bytes only if the digest matches the ref.

## CLI

Build:

```bash
go build -o liminal-proof ./cmd/liminal-proof
```

Verify one evidence bundle:

```bash
./liminal-proof verify \
  -bundle ./evidence-bundle.json \
  -cas ./.cas
```

Success output includes:

```text
✓ bundle root <sha256>
✓ 7/7 artifacts resolved and digests matched
✓ replay envelope.validate
✓ replay source.protocols
✓ replay packet.binding
✓ replay route.binding
✓ replay receipt.binding
✓ replay metro.verify
✓ replay claim.result
✓ replay authority.replay
✓ replay status REPRODUCED
VERDICT: REPRODUCED
```

Any missing object, digest mismatch, malformed artifact, binding failure, authority mismatch, policy hash mismatch, or replay failure exits non-zero with:

```text
VERDICT: REJECTED
```

## Trust boundary

The bundle file tells the verifier which digests are required, but a reference is not accepted as content merely because the resolver returned bytes for it.

```text
REFERENCE != CONTENT
```

`VerifyEvidenceBundleFromCAS(...)` recomputes the SHA-256 of the raw resolved bytes before JSON decoding. Only then does it reconstruct and replay the proof.

`FilesystemCAS` provides local durability and content integrity. It does not provide signatures, timestamps, remote attestation, authorization to read the store, replication, or Byzantine storage guarantees.
