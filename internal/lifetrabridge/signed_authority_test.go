package lifetrabridge

import (
	"strings"
	"testing"

	"github.com/safal207/Liminal-Rail-Metro/internal/metro"
)

func TestSignedAuthorityObservationCarriesSignerAndRotationProofs(t *testing.T) {
	resultHash := strings.Repeat("a", 64)
	journalHash := strings.Repeat("b", 64)
	authorityHash := strings.Repeat("c", 64)
	signedHash := strings.Repeat("d", 64)
	keyID := strings.Repeat("e", 64)
	rotationHash := strings.Repeat("f", 64)
	receipt := metro.Receipt{Protocol: metro.ReceiptProtocol, ReceiptID: "receipt-action-001", ActionID: "action-001", RouteID: "route-action-001", ExecutorID: "workers=1", Status: "SUCCEEDED", HashAlgorithm: "sha256", InputHash: resultHash, ResultHash: resultHash, ResultRef: "adaptive-experience://experience-action-001", StartedAt: "2026-09-16T20:00:00Z", CompletedAt: "2026-09-16T20:00:01Z"}

	obs, err := ReceiptToSignedAuthorityObservation(receipt, "", journalHash, authorityHash, signedHash, keyID, rotationHash)
	if err != nil {
		t.Fatal(err)
	}
	wanted := []string{
		SignedAuthorityRefPrefix + signedHash,
		AuthoritySignerRefPrefix + keyID,
		AuthorityRotationRefPrefix + rotationHash,
	}
	for _, want := range wanted {
		found := false
		for _, ref := range obs.ProofRefs {
			if ref == want {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("proof ref %q missing: %#v", want, obs.ProofRefs)
		}
	}
}

func TestSignedAuthorityObservationAllowsRootEpochWithoutRotation(t *testing.T) {
	h := strings.Repeat("a", 64)
	receipt := metro.Receipt{Protocol: metro.ReceiptProtocol, ReceiptID: "receipt-action-002", ActionID: "action-002", RouteID: "route-action-002", ExecutorID: "workers=1", Status: "SUCCEEDED", HashAlgorithm: "sha256", InputHash: h, ResultHash: h, ResultRef: "adaptive-experience://experience-action-002", StartedAt: "2026-09-16T20:00:00Z", CompletedAt: "2026-09-16T20:00:01Z"}
	if _, err := ReceiptToSignedAuthorityObservation(receipt, "", strings.Repeat("b", 64), strings.Repeat("c", 64), strings.Repeat("d", 64), strings.Repeat("e", 64), ""); err != nil {
		t.Fatal(err)
	}
}
