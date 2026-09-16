package lifetrabridge

import (
	"errors"

	"github.com/safal207/Liminal-Rail-Metro/internal/metro"
)

const (
	AttestationProviderRefPrefix = "adaptive-attestation-provider://sha256/"
	ProviderAnchorRefPrefix      = "adaptive-provider-anchor://sha256/"
	ProviderEvidenceRefPrefix    = "adaptive-provider-evidence://sha256/"
	ProviderAttestationRefPrefix = "adaptive-provider-attestation://sha256/"
)

// ReceiptToProviderAttestedObservation keeps Lifetra independent of provider-
// specific payloads while preserving content-addressed references to the
// provider contract, enrolled trust anchor, evidence, and attestation.
func ReceiptToProviderAttestedObservation(receipt metro.Receipt, previousBeadRef, journalEntryHash, authorityHash, signedAuthorityHash, issuerKeyID, rotationHash, descriptorHash, anchorHash, evidenceHash, providerAttestationHash string) (Observation, error) {
	for _, value := range []string{descriptorHash, anchorHash, evidenceHash, providerAttestationHash} {
		if !sha256Hex(value) {
			return Observation{}, errors.New("provider-attested observation requires sha256 provider identifiers")
		}
	}
	obs, err := ReceiptToSignedAuthorityObservation(receipt, previousBeadRef, journalEntryHash, authorityHash, signedAuthorityHash, issuerKeyID, rotationHash)
	if err != nil {
		return Observation{}, err
	}
	obs.ProofRefs = appendUnique(obs.ProofRefs, AttestationProviderRefPrefix+descriptorHash)
	obs.ProofRefs = appendUnique(obs.ProofRefs, ProviderAnchorRefPrefix+anchorHash)
	obs.ProofRefs = appendUnique(obs.ProofRefs, ProviderEvidenceRefPrefix+evidenceHash)
	obs.ProofRefs = appendUnique(obs.ProofRefs, ProviderAttestationRefPrefix+providerAttestationHash)
	return obs, nil
}
