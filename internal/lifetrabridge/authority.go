package lifetrabridge

import (
	"errors"

	"github.com/safal207/Liminal-Rail-Metro/internal/metro"
)

const AuthorityRefPrefix = "adaptive-authority://sha256/"

// ReceiptToAuthorityAdaptiveObservation extends the durable proof chain with
// the exact authority grant hash that bounded the learned action.
func ReceiptToAuthorityAdaptiveObservation(receipt metro.Receipt, previousBeadRef, journalEntryHash, authorityHash string) (Observation, error) {
	if !sha256Hex(authorityHash) {
		return Observation{}, errors.New("authority adaptive observation requires a sha256 authority hash")
	}
	obs, err := ReceiptToDurableAdaptiveObservation(receipt, previousBeadRef, journalEntryHash)
	if err != nil {
		return Observation{}, err
	}
	obs.ProofRefs = appendUnique(obs.ProofRefs, AuthorityRefPrefix+authorityHash)
	return obs, nil
}
