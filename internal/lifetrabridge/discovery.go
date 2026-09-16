package lifetrabridge

import (
	"errors"

	"github.com/safal207/Liminal-Rail-Metro/internal/metro"
)

const AttestationDiscoveryRefPrefix = "adaptive-attestation-discovery://sha256/"

func ReceiptToDiscoveryProviderObservation(receipt metro.Receipt, previousBeadRef, journalEntryHash, authorityHash, signedAuthorityHash, issuerKeyID, rotationHash, descriptorHash, anchorHash, evidenceHash, attestationHash, discoveryHash string) (Observation, error) {
	if !sha256Hex(discoveryHash) {
		return Observation{}, errors.New("provider discovery observation requires a sha256 discovery hash")
	}
	obs, err := ReceiptToProviderAttestedObservation(receipt, previousBeadRef, journalEntryHash, authorityHash, signedAuthorityHash, issuerKeyID, rotationHash, descriptorHash, anchorHash, evidenceHash, attestationHash)
	if err != nil {
		return Observation{}, err
	}
	obs.ProofRefs = appendUnique(obs.ProofRefs, AttestationDiscoveryRefPrefix+discoveryHash)
	return obs, nil
}
