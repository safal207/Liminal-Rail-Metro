package lifetrabridge

import (
	"strings"
	"testing"

	"github.com/safal207/Liminal-Rail-Metro/internal/metro"
)

func TestDurableObservationCarriesContentAndJournalProofs(t *testing.T) {
	hash := strings.Repeat("a", 64)
	receipt := metro.Receipt{Protocol: metro.ReceiptProtocol, ReceiptID: "receipt-action-001", ActionID: "action-001", RouteID: "route-action-001", ExecutorID: "workers=1", Status: "SUCCEEDED", HashAlgorithm: "sha256", InputHash: hash, ResultHash: hash, ResultRef: "adaptive-experience://experience-action-001", StartedAt: "2026-09-16T20:00:00Z", CompletedAt: "2026-09-16T20:00:01Z"}
	obs, err := ReceiptToDurableAdaptiveObservation(receipt, "", strings.Repeat("b", 64))
	if err != nil {
		t.Fatal(err)
	}
	want1 := "sha256://" + hash
	want2 := "adaptive-journal://sha256/" + strings.Repeat("b", 64)
	found1, found2 := false, false
	for _, r := range obs.ProofRefs {
		if r == want1 {
			found1 = true
		}
		if r == want2 {
			found2 = true
		}
	}
	if !found1 || !found2 {
		t.Fatalf("missing durable proof refs: %#v", obs.ProofRefs)
	}
}
