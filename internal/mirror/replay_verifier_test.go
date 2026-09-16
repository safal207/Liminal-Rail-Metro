package mirror

import (
	"strings"
	"testing"
)

func TestReplayVerifierReproducesVerifiedEnvelope(t *testing.T) {
	claim, packet, route, result, receipt := verifiedFixture(t, "action-replay-001")
	policy := testCodeAuthorityPolicy()
	envelope, err := BuildProofEnvelope(claim, packet, route, result, receipt, policy)
	if err != nil {
		t.Fatalf("build proof envelope: %v", err)
	}

	report, err := (ReplayVerifier{}).Verify(envelope, packet, route, result, receipt, policy)
	if err != nil {
		t.Fatalf("replay proof: %v", err)
	}
	if report.Status != ReplayStatusReproduced {
		t.Fatalf("expected REPRODUCED, got %q", report.Status)
	}
	for _, check := range report.Checks {
		if !check.Passed {
			t.Fatalf("reproduced report contains failed check %q", check.Name)
		}
	}
}

func TestReplayVerifierRejectsValidShapeButWrongPolicyHash(t *testing.T) {
	claim, packet, route, result, receipt := verifiedFixture(t, "action-replay-002")
	policy := testCodeAuthorityPolicy()
	envelope, err := BuildProofEnvelope(claim, packet, route, result, receipt, policy)
	if err != nil {
		t.Fatalf("build proof envelope: %v", err)
	}

	envelope.Authority.PolicyHash = strings.Repeat("a", 64)
	if err := envelope.Validate(); err != nil {
		t.Fatalf("tampered hash should remain structurally valid for replay test: %v", err)
	}

	report, err := (ReplayVerifier{}).Verify(envelope, packet, route, result, receipt, policy)
	if err == nil {
		t.Fatal("replay must reject policy hash that cannot be reproduced")
	}
	if report.Status != ReplayStatusRejected {
		t.Fatalf("expected REJECTED, got %q", report.Status)
	}
}

func TestReplayVerifierRejectsTamperedOriginalResult(t *testing.T) {
	claim, packet, route, result, receipt := verifiedFixture(t, "action-replay-003")
	policy := testCodeAuthorityPolicy()
	envelope, err := BuildProofEnvelope(claim, packet, route, result, receipt, policy)
	if err != nil {
		t.Fatalf("build proof envelope: %v", err)
	}

	result["status"] = "tampered"
	report, err := (ReplayVerifier{}).Verify(envelope, packet, route, result, receipt, policy)
	if err == nil {
		t.Fatal("replay must reject result that no longer matches receipt hash")
	}
	if report.Status != ReplayStatusRejected {
		t.Fatalf("expected REJECTED, got %q", report.Status)
	}
}

func TestReplayVerifierRejectsReceiptFieldMetroVerifyDoesNotBind(t *testing.T) {
	claim, packet, route, result, receipt := verifiedFixture(t, "action-replay-004")
	policy := testCodeAuthorityPolicy()
	envelope, err := BuildProofEnvelope(claim, packet, route, result, receipt, policy)
	if err != nil {
		t.Fatalf("build proof envelope: %v", err)
	}

	receipt.ResultRef = "artifact://different-reference"
	report, err := (ReplayVerifier{}).Verify(envelope, packet, route, result, receipt, policy)
	if err == nil {
		t.Fatal("replay must reject original receipt that differs from envelope")
	}
	if report.Status != ReplayStatusRejected {
		t.Fatalf("expected REJECTED, got %q", report.Status)
	}
}

func TestReplayVerifierRejectsChangedPolicyUnderSameRef(t *testing.T) {
	claim, packet, route, result, receipt := verifiedFixture(t, "action-replay-005")
	policy := testCodeAuthorityPolicy()
	envelope, err := BuildProofEnvelope(claim, packet, route, result, receipt, policy)
	if err != nil {
		t.Fatalf("build proof envelope: %v", err)
	}

	changed := testCodeAuthorityPolicy()
	changed.ExecutorsByAction["code.implement"] = []string{"code-agent", "backup-code-agent"}

	report, err := (ReplayVerifier{}).Verify(envelope, packet, route, result, receipt, changed)
	if err == nil {
		t.Fatal("same policy_ref with changed authority content must not reproduce")
	}
	if report.Status != ReplayStatusRejected {
		t.Fatalf("expected REJECTED, got %q", report.Status)
	}
}

func TestReplayVerifierRejectsRouteBoundToDifferentAction(t *testing.T) {
	claim, packet, route, result, receipt := verifiedFixture(t, "action-replay-006")
	policy := testCodeAuthorityPolicy()
	envelope, err := BuildProofEnvelope(claim, packet, route, result, receipt, policy)
	if err != nil {
		t.Fatalf("build proof envelope: %v", err)
	}

	route.ActionID = "action-other"
	report, err := (ReplayVerifier{}).Verify(envelope, packet, route, result, receipt, policy)
	if err == nil {
		t.Fatal("replay must reject route that is not bound to packet action_id")
	}
	if report.Status != ReplayStatusRejected {
		t.Fatalf("expected REJECTED, got %q", report.Status)
	}
}
