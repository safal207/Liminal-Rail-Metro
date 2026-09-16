package lifetrabridge

import (
	"errors"
	"strings"

	"github.com/safal207/Liminal-Rail-Metro/internal/metro"
)

const AdaptiveExperienceRefPrefix = "adaptive-experience://"

// ReceiptToAdaptiveObservation preserves the ordinary Metro receipt proof and
// additionally carries the bounded adaptive experience reference into Lifetra.
// It does not grant dispatch authority or reinterpret receipt status.
func ReceiptToAdaptiveObservation(receipt metro.Receipt, previousBeadRef string) (Observation, error) {
	if !strings.HasPrefix(receipt.ResultRef, AdaptiveExperienceRefPrefix) {
		return Observation{}, errors.New("adaptive observation requires an adaptive-experience result_ref")
	}
	observation, err := ReceiptToObservation(receipt, previousBeadRef)
	if err != nil {
		return Observation{}, err
	}
	for _, ref := range observation.ProofRefs {
		if ref == receipt.ResultRef {
			return observation, nil
		}
	}
	observation.ProofRefs = append(observation.ProofRefs, receipt.ResultRef)
	return observation, nil
}
