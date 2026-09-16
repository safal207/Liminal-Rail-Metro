package lifetrabridge

import (
	"strings"
	"testing"

	"github.com/safal207/Liminal-Rail-Metro/internal/metro"
)

func TestProviderObservationCarriesGenericProviderRefs(t *testing.T) {
	h := func(ch string) string { return strings.Repeat(ch, 64) }
	receipt := metro.Receipt{Protocol: metro.ReceiptProtocol, ReceiptID: "receipt-action-001", ActionID: "action-001", RouteID: "route-action-001", ExecutorID: "workers=1", Status: "SUCCEEDED", HashAlgorithm: "sha256", InputHash: h("a"), ResultHash: h("b"), ResultRef: "adaptive-experience://experience-action-001", StartedAt: "2026-09-17T00:00:00Z", CompletedAt: "2026-09-17T00:00:01Z"}
	obs, err := ReceiptToProviderAttestedObservation(receipt, "", h("c"), h("d"), h("e"), h("f"), "", h("1"), h("2"), h("3"), h("4"))
	if err != nil {
		t.Fatal(err)
	}
	wants := []string{AttestationProviderRefPrefix + h("1"), ProviderAnchorRefPrefix + h("2"), ProviderEvidenceRefPrefix + h("3"), ProviderAttestationRefPrefix + h("4")}
	for _, want := range wants {
		found := false
		for _, ref := range obs.ProofRefs {
			if ref == want {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("missing provider proof ref %q in %#v", want, obs.ProofRefs)
		}
	}
}
