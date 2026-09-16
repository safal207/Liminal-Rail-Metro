package mirror

import "testing"

func TestBuildEvidenceBundleProducesContentAddressedManifest(t *testing.T) {
	claim, packet, route, result, receipt := verifiedFixture(t, "action-bundle-001")
	policy := testCodeAuthorityPolicy()
	envelope, err := BuildProofEnvelope(claim, packet, route, result, receipt, policy)
	if err != nil {
		t.Fatalf("build proof envelope: %v", err)
	}

	bundle, replay, err := BuildEvidenceBundle(envelope, packet, route, result, receipt, policy)
	if err != nil {
		t.Fatalf("build evidence bundle: %v", err)
	}
	if replay.Status != ReplayStatusReproduced {
		t.Fatalf("expected reproduced replay, got %q", replay.Status)
	}
	if err := bundle.Validate(); err != nil {
		t.Fatalf("validate evidence bundle: %v", err)
	}
	if len(bundle.Artifacts) != len(requiredBundleArtifacts) {
		t.Fatalf("expected %d artifacts, got %d", len(requiredBundleArtifacts), len(bundle.Artifacts))
	}
	for _, artifact := range bundle.Artifacts {
		if artifact.Ref != casRef(artifact.Digest) {
			t.Fatalf("artifact %q is not content-addressed by its digest", artifact.Kind)
		}
	}
}

func TestVerifyEvidenceBundleReproducesExactArtifacts(t *testing.T) {
	claim, packet, route, result, receipt := verifiedFixture(t, "action-bundle-002")
	policy := testCodeAuthorityPolicy()
	envelope, err := BuildProofEnvelope(claim, packet, route, result, receipt, policy)
	if err != nil {
		t.Fatalf("build proof envelope: %v", err)
	}
	bundle, _, err := BuildEvidenceBundle(envelope, packet, route, result, receipt, policy)
	if err != nil {
		t.Fatalf("build evidence bundle: %v", err)
	}

	replay, err := VerifyEvidenceBundle(bundle, envelope, packet, route, result, receipt, policy)
	if err != nil {
		t.Fatalf("verify evidence bundle: %v", err)
	}
	if replay.Status != ReplayStatusReproduced {
		t.Fatalf("expected reproduced replay, got %q", replay.Status)
	}
}

func TestVerifyEvidenceBundleRejectsChangedPacket(t *testing.T) {
	claim, packet, route, result, receipt := verifiedFixture(t, "action-bundle-003")
	policy := testCodeAuthorityPolicy()
	envelope, err := BuildProofEnvelope(claim, packet, route, result, receipt, policy)
	if err != nil {
		t.Fatalf("build proof envelope: %v", err)
	}
	bundle, _, err := BuildEvidenceBundle(envelope, packet, route, result, receipt, policy)
	if err != nil {
		t.Fatalf("build evidence bundle: %v", err)
	}

	packet.Goal = "different goal with same action inputs"
	if _, err := VerifyEvidenceBundle(bundle, envelope, packet, route, result, receipt, policy); err == nil {
		t.Fatal("changed packet content must not match existing evidence bundle")
	}
}

func TestEvidenceBundleValidateRejectsDigestRefMismatch(t *testing.T) {
	claim, packet, route, result, receipt := verifiedFixture(t, "action-bundle-004")
	policy := testCodeAuthorityPolicy()
	envelope, err := BuildProofEnvelope(claim, packet, route, result, receipt, policy)
	if err != nil {
		t.Fatalf("build proof envelope: %v", err)
	}
	bundle, _, err := BuildEvidenceBundle(envelope, packet, route, result, receipt, policy)
	if err != nil {
		t.Fatalf("build evidence bundle: %v", err)
	}

	bundle.Artifacts[0].Ref = "cas://sha256/ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff"
	if err := bundle.Validate(); err == nil {
		t.Fatal("artifact ref that does not match digest must fail closed")
	}
}

func TestEvidenceBundleRootHashIsArtifactOrderIndependent(t *testing.T) {
	claim, packet, route, result, receipt := verifiedFixture(t, "action-bundle-005")
	policy := testCodeAuthorityPolicy()
	envelope, err := BuildProofEnvelope(claim, packet, route, result, receipt, policy)
	if err != nil {
		t.Fatalf("build proof envelope: %v", err)
	}
	bundle, _, err := BuildEvidenceBundle(envelope, packet, route, result, receipt, policy)
	if err != nil {
		t.Fatalf("build evidence bundle: %v", err)
	}

	for left, right := 0, len(bundle.Artifacts)-1; left < right; left, right = left+1, right-1 {
		bundle.Artifacts[left], bundle.Artifacts[right] = bundle.Artifacts[right], bundle.Artifacts[left]
	}
	if err := bundle.Validate(); err != nil {
		t.Fatalf("artifact ordering must not change root hash: %v", err)
	}
}

func TestBuildEvidenceBundleRejectsNonReproducibleInputs(t *testing.T) {
	claim, packet, route, result, receipt := verifiedFixture(t, "action-bundle-006")
	policy := testCodeAuthorityPolicy()
	envelope, err := BuildProofEnvelope(claim, packet, route, result, receipt, policy)
	if err != nil {
		t.Fatalf("build proof envelope: %v", err)
	}

	result["status"] = "tampered"
	bundle, replay, err := BuildEvidenceBundle(envelope, packet, route, result, receipt, policy)
	if err == nil {
		t.Fatal("non-reproducible proof must not produce an evidence bundle")
	}
	if bundle.Protocol != "" {
		t.Fatal("failed bundle build must not return a usable manifest")
	}
	if replay.Status != ReplayStatusRejected {
		t.Fatalf("expected rejected replay, got %q", replay.Status)
	}
}
