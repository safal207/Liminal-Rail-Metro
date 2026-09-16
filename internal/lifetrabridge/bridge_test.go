package lifetrabridge

import (
	"testing"

	"github.com/safal207/Liminal-Rail-Metro/internal/metro"
)

func TestReceiptToObservationPreservesUnknown(t *testing.T) {
	receipt := metro.Receipt{
		Protocol:      metro.ReceiptProtocol,
		ReceiptID:     "receipt-action-001",
		ActionID:      "action-001",
		RouteID:       "route-action-001",
		ExecutorID:    "tool-agent",
		Status:        "UNKNOWN",
		HashAlgorithm: "sha256",
		InputHash:     "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		StartedAt:     "2026-09-16T13:00:00Z",
		CompletedAt:   "2026-09-16T13:00:01Z",
	}

	observation, err := ReceiptToObservation(receipt, "lifetra-bead://bead-7")
	if err != nil {
		t.Fatal(err)
	}
	if observation.ReceiptStatus != "UNKNOWN" {
		t.Fatalf("expected UNKNOWN, got %q", observation.ReceiptStatus)
	}
	if observation.ActionID != receipt.ActionID {
		t.Fatal("action identity was not preserved")
	}
	if observation.ReceiptHash == "" {
		t.Fatal("receipt hash must be present")
	}
}

func TestDecisionToPacketRequiresAllow(t *testing.T) {
	_, err := DecisionToPacket(Decision{
		Protocol: DecisionProtocol,
		Verdict:  VerdictRequireApproval,
	})
	if err == nil {
		t.Fatal("expected non-ALLOW decision to be rejected")
	}
}

func TestDecisionToPacketCreatesNewCausallyLinkedAction(t *testing.T) {
	decision := Decision{
		Protocol:            DecisionProtocol,
		DecisionID:          "decision-008",
		SourceObservationID: "observation-action-001",
		SourceReceiptRef:    "metro-receipt://receipt-action-001",
		CausedByActionID:    "action-001",
		SourceBeadRef:       "lifetra-bead://bead-8",
		Verdict:             VerdictAllow,
		AuthorityProofRef:   "lifetra-authority://ticket-008",
		NextAction: &NextAction{
			ActionID:       "action-002",
			Goal:           "Verify the corrected implementation.",
			Kind:           "qa.verify",
			Inputs:         map[string]any{"artifact_ref": "artifact://build-002"},
			AllowedTargets: []string{"qa-agent"},
			TimeoutMS:      5000,
			SideEffect:     false,
		},
		DecidedAt: "2026-09-16T13:01:00Z",
	}

	packet, err := DecisionToPacket(decision)
	if err != nil {
		t.Fatal(err)
	}
	if packet.ActionID != "action-002" {
		t.Fatalf("expected new action id, got %q", packet.ActionID)
	}
	if packet.PreviousReceiptRef != decision.SourceReceiptRef {
		t.Fatal("source receipt provenance was not preserved")
	}
	if packet.SourceAgent != "lifetra-control" {
		t.Fatalf("unexpected source agent %q", packet.SourceAgent)
	}
}

func TestDecisionToPacketRejectsSilentActionIDReuse(t *testing.T) {
	_, err := DecisionToPacket(Decision{
		Protocol:          DecisionProtocol,
		Verdict:           VerdictAllow,
		AuthorityProofRef: "lifetra-authority://ticket-009",
		CausedByActionID:  "action-001",
		NextAction: &NextAction{
			ActionID:       "action-001",
			Goal:           "Retry without explicit recovery authority.",
			Kind:           "tool.call",
			Inputs:         map[string]any{},
			AllowedTargets: []string{"tool-agent"},
		},
	})
	if err == nil {
		t.Fatal("expected silent action_id reuse to be rejected")
	}
}
