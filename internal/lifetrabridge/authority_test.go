package lifetrabridge

import (
	"strings"
	"testing"

	"github.com/safal207/Liminal-Rail-Metro/internal/metro"
)

func TestAuthorityObservationCarriesAuthorityProof(t *testing.T) {
	resultHash := strings.Repeat("a", 64)
	journalHash := strings.Repeat("b", 64)
	authorityHash := strings.Repeat("c", 64)
	receipt := metro.Receipt{Protocol: metro.ReceiptProtocol, ReceiptID: "receipt-action-001", ActionID: "action-001", RouteID: "route-action-001", ExecutorID: "workers=1", Status: "SUCCEEDED", HashAlgorithm: "sha256", InputHash: resultHash, ResultHash: resultHash, ResultRef: "adaptive-experience://experience-action-001", StartedAt: "2026-09-16T20:00:00Z", CompletedAt: "2026-09-16T20:00:01Z"}
	obs, err := ReceiptToAuthorityAdaptiveObservation(receipt, "", journalHash, authorityHash)
	if err != nil {
		t.Fatal(err)
	}
	want := AuthorityRefPrefix + authorityHash
	for _, ref := range obs.ProofRefs {
		if ref == want {
			return
		}
	}
	t.Fatalf("authority proof ref missing: %#v", obs.ProofRefs)
}
