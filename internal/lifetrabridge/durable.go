package lifetrabridge

import (
	"encoding/hex"
	"errors"
	"strings"

	"github.com/safal207/Liminal-Rail-Metro/internal/metro"
)

const DurableJournalRefPrefix = "adaptive-journal://sha256/"
const ContentHashRefPrefix = "sha256://"

func ReceiptToDurableAdaptiveObservation(receipt metro.Receipt, previousBeadRef, journalEntryHash string) (Observation, error) {
	if receipt.Status != "SUCCEEDED" {
		return Observation{}, errors.New("durable adaptive observation requires a SUCCEEDED receipt")
	}
	if !sha256Hex(receipt.ResultHash) || !sha256Hex(journalEntryHash) {
		return Observation{}, errors.New("durable adaptive observation requires sha256 result and journal hashes")
	}
	obs, err := ReceiptToAdaptiveObservation(receipt, previousBeadRef)
	if err != nil {
		return Observation{}, err
	}
	obs.ProofRefs = appendUnique(obs.ProofRefs, ContentHashRefPrefix+receipt.ResultHash)
	obs.ProofRefs = appendUnique(obs.ProofRefs, DurableJournalRefPrefix+journalEntryHash)
	return obs, nil
}

func appendUnique(values []string, value string) []string {
	for _, existing := range values {
		if existing == value {
			return values
		}
	}
	return append(values, value)
}

func sha256Hex(v string) bool {
	if len(v) != 64 || v != strings.ToLower(v) {
		return false
	}
	b, err := hex.DecodeString(v)
	return err == nil && len(b) == 32
}
