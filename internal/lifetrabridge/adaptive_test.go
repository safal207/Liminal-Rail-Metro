package lifetrabridge

import (
	"testing"

	"github.com/safal207/Liminal-Rail-Metro/internal/metro"
)

func TestReceiptToAdaptiveObservationCarriesExperienceProofRef(t *testing.T) {
	receipt := metro.Receipt{
		Protocol:      metro.ReceiptProtocol,
		ReceiptID:     "receipt-action-002",
		ActionID:      "action-002",
		RouteID:       "route-action-002",
		ExecutorID:    "workers=4",
		Status:        "SUCCEEDED",
		HashAlgorithm: "sha256",
		InputHash:     "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		ResultHash:    "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
		ResultRef:     "adaptive-experience://experience-action-002",
		StartedAt:     "2026-09-16T13:00:00Z",
		CompletedAt:   "2026-09-16T13:00:01Z",
	}
	observation, err := ReceiptToAdaptiveObservation(receipt, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(observation.ProofRefs) != 2 || observation.ProofRefs[1] != receipt.ResultRef {
		t.Fatalf("adaptive experience proof ref not preserved: %v", observation.ProofRefs)
	}
}

func TestReceiptToAdaptiveObservationRejectsUnscopedResultRef(t *testing.T) {
	receipt := metro.Receipt{ResultRef: "artifact://anything"}
	if _, err := ReceiptToAdaptiveObservation(receipt, ""); err == nil {
		t.Fatal("expected non-adaptive result_ref to be rejected")
	}
}
