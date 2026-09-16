package lifetrabridge

import (
	"strings"
	"testing"

	"github.com/safal207/Liminal-Rail-Metro/internal/metro"
)

func TestDeviceAttestedObservationCarriesDeviceProofRefs(t *testing.T) {
	resultHash := strings.Repeat("a", 64)
	journalHash := strings.Repeat("b", 64)
	authorityHash := strings.Repeat("c", 64)
	signedAuthorityHash := strings.Repeat("d", 64)
	issuerKeyID := strings.Repeat("e", 64)
	attestationHash := strings.Repeat("1", 64)
	deviceKeyID := strings.Repeat("2", 64)
	deviceFingerprint := strings.Repeat("3", 64)
	runtimeFingerprint := strings.Repeat("4", 64)
	receipt := metro.Receipt{Protocol: metro.ReceiptProtocol, ReceiptID: "receipt-action-001", ActionID: "action-001", RouteID: "route-action-001", ExecutorID: "workers=1", Status: "SUCCEEDED", HashAlgorithm: "sha256", InputHash: resultHash, ResultHash: resultHash, ResultRef: "adaptive-experience://experience-action-001", StartedAt: "2026-09-16T20:00:00Z", CompletedAt: "2026-09-16T20:00:01Z"}
	obs, err := ReceiptToDeviceAttestedObservation(receipt, "", journalHash, authorityHash, signedAuthorityHash, issuerKeyID, "", attestationHash, deviceKeyID, deviceFingerprint, runtimeFingerprint)
	if err != nil {
		t.Fatal(err)
	}
	wants := []string{
		DeviceAttestationRefPrefix + attestationHash,
		DeviceKeyRefPrefix + deviceKeyID,
		DeviceFingerprintRefPrefix + deviceFingerprint,
		RuntimeFingerprintRefPrefix + runtimeFingerprint,
	}
	for _, want := range wants {
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
