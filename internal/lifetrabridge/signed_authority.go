package lifetrabridge

import (
	"errors"

	"github.com/safal207/Liminal-Rail-Metro/internal/metro"
)

const (
	SignedAuthorityRefPrefix = "adaptive-signed-authority://sha256/"
	AuthoritySignerRefPrefix = "adaptive-authority-signer://ed25519/sha256/"
	AuthorityRotationRefPrefix = "adaptive-authority-rotation://sha256/"
)

// ReceiptToSignedAuthorityObservation extends the v0.4 authority proof with
// the exact signed grant, signer key fingerprint, and optional rotation record
// that authorized the active epoch.
func ReceiptToSignedAuthorityObservation(receipt metro.Receipt, previousBeadRef, journalEntryHash, authorityHash, signedAuthorityHash, issuerKeyID, rotationHash string) (Observation, error) {
	if !sha256Hex(signedAuthorityHash) || !sha256Hex(issuerKeyID) {
		return Observation{}, errors.New("signed authority observation requires sha256 signed-authority and issuer-key identifiers")
	}
	if rotationHash != "" && !sha256Hex(rotationHash) {
		return Observation{}, errors.New("signed authority observation requires a sha256 rotation hash when rotation is present")
	}
	obs, err := ReceiptToAuthorityAdaptiveObservation(receipt, previousBeadRef, journalEntryHash, authorityHash)
	if err != nil {
		return Observation{}, err
	}
	obs.ProofRefs = appendUnique(obs.ProofRefs, SignedAuthorityRefPrefix+signedAuthorityHash)
	obs.ProofRefs = appendUnique(obs.ProofRefs, AuthoritySignerRefPrefix+issuerKeyID)
	if rotationHash != "" {
		obs.ProofRefs = appendUnique(obs.ProofRefs, AuthorityRotationRefPrefix+rotationHash)
	}
	return obs, nil
}
