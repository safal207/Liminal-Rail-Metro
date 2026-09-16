package mirror

import (
	"strings"
	"testing"

	"github.com/safal207/Liminal-Rail-Metro/internal/metro"
)

func TestEvidenceFromVerifiedReceiptPromotesBoundSuccess(t *testing.T) {
	claim, packet, route, result, receipt := verifiedFixture(t, "action-bridge-001")

	evidence, err := EvidenceFromVerifiedReceipt(claim, packet, route, result, receipt, testCodeAuthorityPolicy())
	if err != nil {
		t.Fatalf("promote receipt: %v", err)
	}
	if evidence.Source != SourceExternal || !evidence.Verified {
		t.Fatalf("expected verified external evidence, got source=%q verified=%v", evidence.Source, evidence.Verified)
	}
	if evidence.ClaimID != claim.ID || evidence.ActionID != claim.ActionID || evidence.ValueHash != claim.ValueHash {
		t.Fatal("promoted evidence lost claim/action/value binding")
	}
	if len(evidence.Provenance) < 7 {
		t.Fatalf("expected receipt + immutable authority provenance chain, got %v", evidence.Provenance)
	}
	if !containsPrefix(evidence.Provenance, "authority://") {
		t.Fatalf("expected explicit authority decision provenance, got %v", evidence.Provenance)
	}
	if !containsPrefix(evidence.Provenance, "policy://") {
		t.Fatalf("expected durable authority policy ref, got %v", evidence.Provenance)
	}
	if !containsPrefix(evidence.Provenance, "policy-sha256://") {
		t.Fatalf("expected authority policy content hash, got %v", evidence.Provenance)
	}

	decision := (Gate{}).Evaluate(claim, []Evidence{evidence})
	if !decision.Commit {
		t.Fatalf("expected promoted evidence to authorize gate: %s", decision.Reason)
	}
}

func TestEvidenceFromVerifiedReceiptRejectsTamperedResult(t *testing.T) {
	claim, packet, route, result, receipt := verifiedFixture(t, "action-bridge-002")
	result["status"] = "tampered"

	if _, err := EvidenceFromVerifiedReceipt(claim, packet, route, result, receipt, testCodeAuthorityPolicy()); err == nil {
		t.Fatal("expected tampered result to fail receipt verification")
	}
}

func TestEvidenceFromVerifiedReceiptRejectsFailedStatus(t *testing.T) {
	claim, packet, route, result, receipt := verifiedFixture(t, "action-bridge-003")
	receipt.Status = "FAILED"

	// metro.Verify currently validates binding and hashes, not promotability.
	// The adapter must still refuse a non-success receipt at the boundary.
	if err := metro.Verify(packet, route, result, receipt); err != nil {
		t.Fatalf("fixture should remain hash/binding-valid: %v", err)
	}
	if _, err := EvidenceFromVerifiedReceipt(claim, packet, route, result, receipt, testCodeAuthorityPolicy()); err == nil {
		t.Fatal("expected failed receipt status to be non-promotable")
	}
}

func TestEvidenceFromVerifiedReceiptRejectsDifferentClaimAction(t *testing.T) {
	claim, packet, route, result, receipt := verifiedFixture(t, "action-bridge-004")
	claim.ActionID = "action-other"

	if _, err := EvidenceFromVerifiedReceipt(claim, packet, route, result, receipt, testCodeAuthorityPolicy()); err == nil {
		t.Fatal("expected cross-action receipt promotion to fail closed")
	}
}

func TestEvidenceFromVerifiedReceiptRejectsClaimValueMismatch(t *testing.T) {
	claim, packet, route, result, receipt := verifiedFixture(t, "action-bridge-005")
	claim.ValueHash = mustHash(t, map[string]any{"status": "different"})

	if _, err := EvidenceFromVerifiedReceipt(claim, packet, route, result, receipt, testCodeAuthorityPolicy()); err == nil {
		t.Fatal("expected receipt result hash mismatch to fail closed")
	}
}

func TestEvidenceFromVerifiedReceiptRejectsMissingResultRef(t *testing.T) {
	claim, packet, route, result, receipt := verifiedFixture(t, "action-bridge-006")
	receipt.ResultRef = ""

	if _, err := EvidenceFromVerifiedReceipt(claim, packet, route, result, receipt, testCodeAuthorityPolicy()); err == nil {
		t.Fatal("expected receipt without external result reference to fail closed")
	}
}

func TestEvidenceFromVerifiedReceiptRejectsAuthorityWithoutDurableRef(t *testing.T) {
	claim, packet, route, result, receipt := verifiedFixture(t, "action-bridge-007")
	policy := testCodeAuthorityPolicy()
	policy.PolicyRef = ""

	if _, err := EvidenceFromVerifiedReceipt(claim, packet, route, result, receipt, policy); err == nil {
		t.Fatal("authority policy without durable policy_ref must not authorize external evidence")
	}
}

func verifiedFixture(t *testing.T, actionID string) (Claim, metro.Packet, metro.Route, map[string]any, metro.Receipt) {
	t.Helper()

	packet := metro.NewPacket(
		actionID,
		"research-agent",
		"bridge verified receipt into mirror evidence",
		metro.Action{Kind: "code.implement", Inputs: map[string]any{"task": "mirror bridge"}},
		[]string{"code-agent"},
	)
	router := metro.Router{
		ID: "router-test",
		Policy: map[string]string{
			"code.implement": "code-agent",
		},
	}
	route, err := router.Route(packet)
	if err != nil {
		t.Fatalf("route packet: %v", err)
	}

	result := map[string]any{
		"status":       "implemented",
		"artifact_ref": "artifact://mirror-bridge",
	}
	receipt, err := metro.MakeSuccessReceipt(packet, route, result, "artifact://mirror-bridge")
	if err != nil {
		t.Fatalf("make receipt: %v", err)
	}
	valueHash := mustHash(t, result)
	claim := Claim{
		ID:        "claim-" + actionID,
		ActionID:  actionID,
		ValueHash: valueHash,
	}

	return claim, packet, route, result, receipt
}

func testCodeAuthorityPolicy() AuthorityPolicy {
	return AuthorityPolicy{
		Protocol:  AuthorityPolicyProtocol,
		ID:        "test-authority-v1",
		PolicyRef: "policy://liminal-rail/test-authority/v1",
		ExecutorsByAction: map[string][]string{
			"code.implement": {"code-agent"},
		},
	}
}

func containsPrefix(values []string, prefix string) bool {
	for _, value := range values {
		if strings.HasPrefix(value, prefix) {
			return true
		}
	}
	return false
}
