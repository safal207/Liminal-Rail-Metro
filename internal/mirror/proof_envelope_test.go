package mirror

import "testing"

func TestBuildProofEnvelopeCarriesStructuredAuthority(t *testing.T) {
	claim, packet, route, result, receipt := verifiedFixture(t, "action-envelope-001")

	envelope, err := BuildProofEnvelope(claim, packet, route, result, receipt, testCodeAuthorityPolicy())
	if err != nil {
		t.Fatalf("build proof envelope: %v", err)
	}
	if envelope.Protocol != ProofEnvelopeProtocol || envelope.VerificationStatus != VerificationStatusVerified {
		t.Fatalf("unexpected envelope protocol/status: %q %q", envelope.Protocol, envelope.VerificationStatus)
	}
	if envelope.Authority.PolicyRef == "" || len(envelope.Authority.PolicyHash) != 64 {
		t.Fatalf("expected structured policy identity, got ref=%q hash=%q", envelope.Authority.PolicyRef, envelope.Authority.PolicyHash)
	}
	if envelope.Receipt.ResultHash != claim.ValueHash {
		t.Fatal("receipt result hash must remain bound to claim value")
	}

	evidence, err := envelope.Evidence()
	if err != nil {
		t.Fatalf("envelope evidence: %v", err)
	}
	decision := (Gate{}).Evaluate(claim, []Evidence{evidence})
	if !decision.Commit {
		t.Fatalf("validated proof envelope should authorize mirror gate: %s", decision.Reason)
	}
}

func TestProofEnvelopeRejectsTamperedPolicyHash(t *testing.T) {
	claim, packet, route, result, receipt := verifiedFixture(t, "action-envelope-002")
	envelope, err := BuildProofEnvelope(claim, packet, route, result, receipt, testCodeAuthorityPolicy())
	if err != nil {
		t.Fatalf("build proof envelope: %v", err)
	}

	envelope.Authority.PolicyHash = "not-a-sha256"
	if err := envelope.Validate(); err == nil {
		t.Fatal("tampered policy hash must fail structural proof validation")
	}
}

func TestProofEnvelopeRejectsExecutorAuthorityMismatch(t *testing.T) {
	claim, packet, route, result, receipt := verifiedFixture(t, "action-envelope-003")
	envelope, err := BuildProofEnvelope(claim, packet, route, result, receipt, testCodeAuthorityPolicy())
	if err != nil {
		t.Fatalf("build proof envelope: %v", err)
	}

	envelope.Authority.ExecutorID = "other-agent"
	if err := envelope.Validate(); err == nil {
		t.Fatal("authority executor must remain bound to receipt executor")
	}
}

func TestProofEnvelopeRejectsClaimReceiptMismatch(t *testing.T) {
	claim, packet, route, result, receipt := verifiedFixture(t, "action-envelope-004")
	envelope, err := BuildProofEnvelope(claim, packet, route, result, receipt, testCodeAuthorityPolicy())
	if err != nil {
		t.Fatalf("build proof envelope: %v", err)
	}

	envelope.Claim.ActionID = "action-other"
	if err := envelope.Validate(); err == nil {
		t.Fatal("receipt action must remain bound to envelope claim")
	}
}

func TestProofEnvelopeRejectsUnverifiedStatus(t *testing.T) {
	claim, packet, route, result, receipt := verifiedFixture(t, "action-envelope-005")
	envelope, err := BuildProofEnvelope(claim, packet, route, result, receipt, testCodeAuthorityPolicy())
	if err != nil {
		t.Fatalf("build proof envelope: %v", err)
	}

	envelope.VerificationStatus = "PENDING"
	if _, err := envelope.Evidence(); err == nil {
		t.Fatal("non-verified envelope must not become external evidence")
	}
}

func TestProofEnvelopeRejectsAuthorityActionMismatch(t *testing.T) {
	claim, packet, route, result, receipt := verifiedFixture(t, "action-envelope-006")
	envelope, err := BuildProofEnvelope(claim, packet, route, result, receipt, testCodeAuthorityPolicy())
	if err != nil {
		t.Fatalf("build proof envelope: %v", err)
	}

	envelope.Authority.ActionKind = "payment.capture"
	if err := envelope.Validate(); err == nil {
		t.Fatal("authority action kind must remain bound to envelope action kind")
	}
}
