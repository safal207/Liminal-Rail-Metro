package mirror

import (
	"testing"

	"github.com/safal207/Liminal-Rail-Metro/internal/metro"
)

func TestAuthorityPolicyAllowsExactExecutorForAction(t *testing.T) {
	policy := AuthorityPolicy{
		ID: "authority-v1",
		ExecutorsByAction: map[string][]string{
			"payment.capture": {"payments-agent"},
		},
	}

	if err := policy.Authorize("payment.capture", "payments-agent"); err != nil {
		t.Fatalf("expected exact authority binding to pass: %v", err)
	}
}

func TestAuthorityPolicyDeniesMissingActionRule(t *testing.T) {
	policy := AuthorityPolicy{
		ID: "authority-v1",
		ExecutorsByAction: map[string][]string{
			"code.implement": {"code-agent"},
		},
	}

	if err := policy.Authorize("payment.capture", "payments-agent"); err == nil {
		t.Fatal("missing action rule must fail closed")
	}
}

func TestAuthorityPolicyDeniesWrongExecutor(t *testing.T) {
	policy := AuthorityPolicy{
		ID: "authority-v1",
		ExecutorsByAction: map[string][]string{
			"payment.capture": {"payments-agent"},
		},
	}

	if err := policy.Authorize("payment.capture", "code-agent"); err == nil {
		t.Fatal("executor authoritative for another domain must not certify payment.capture")
	}
}

func TestEvidencePromotionRejectsValidReceiptFromNonAuthoritativeExecutor(t *testing.T) {
	packet := metro.NewPacket(
		"action-authority-001",
		"research-agent",
		"prove that route permission is not proof authority",
		metro.Action{Kind: "payment.capture", Inputs: map[string]any{"amount": 100, "currency": "USD"}},
		[]string{"code-agent"},
	)

	// The routing layer deliberately permits code-agent here. This demonstrates
	// that a route being valid is not sufficient to make its receipt authoritative.
	router := metro.Router{
		ID: "router-test",
		Policy: map[string]string{
			"payment.capture": "code-agent",
		},
	}
	route, err := router.Route(packet)
	if err != nil {
		t.Fatalf("route packet: %v", err)
	}

	result := map[string]any{"status": "captured", "amount": 100, "currency": "USD"}
	receipt, err := metro.MakeSuccessReceipt(packet, route, result, "provider://payment/receipt-001")
	if err != nil {
		t.Fatalf("make receipt: %v", err)
	}
	if err := metro.Verify(packet, route, result, receipt); err != nil {
		t.Fatalf("receipt should be structurally valid before authority check: %v", err)
	}

	claim := Claim{
		ID:           "claim-authority-001",
		ActionID:     packet.ActionID,
		ValueHash:    mustHash(t, result),
		Irreversible: true,
	}
	policy := AuthorityPolicy{
		ID: "production-authority-v1",
		ExecutorsByAction: map[string][]string{
			"payment.capture": {"payments-agent"},
		},
	}

	if _, err := EvidenceFromVerifiedReceipt(claim, packet, route, result, receipt, policy); err == nil {
		t.Fatal("structurally valid receipt from non-authoritative executor must not cross mirror boundary")
	}
}

func TestAuthorityPolicyRequiresIdentity(t *testing.T) {
	policy := AuthorityPolicy{
		ExecutorsByAction: map[string][]string{
			"code.implement": {"code-agent"},
		},
	}

	if err := policy.Authorize("code.implement", "code-agent"); err == nil {
		t.Fatal("anonymous authority policy must fail closed")
	}
}
