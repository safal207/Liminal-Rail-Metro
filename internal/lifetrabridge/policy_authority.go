package lifetrabridge

import (
	"errors"

	"github.com/safal207/Liminal-Rail-Metro/internal/policyauthority"
)

const (
	PolicyAuthorityManifestRefPrefix = "adaptive-policy-authority-manifest://sha256/"
	PolicyAuthorityRotationRefPrefix = "adaptive-policy-authority-rotation://sha256/"
	PolicyAuthorityHeadRefPrefix     = "adaptive-policy-authority-head://sha256/"
	PolicyAuthorizationRefPrefix     = "adaptive-policy-authorization://sha256/"
)

func BindPolicyAuthorityProofRefs(obs Observation, authorization policyauthority.Authorization) (Observation, error) {
	if err := authorization.Validate(); err != nil {
		return Observation{}, err
	}
	for _, value := range []string{authorization.SignedManifestHash, authorization.ChainHeadHash, authorization.AuthorizationHash} {
		if !sha256Hex(value) {
			return Observation{}, errors.New("policy authority proof refs require sha256 hashes")
		}
	}
	obs.ProofRefs = appendUnique(obs.ProofRefs, PolicyAuthorityManifestRefPrefix+authorization.SignedManifestHash)
	if authorization.RotationHash != "" {
		if !sha256Hex(authorization.RotationHash) {
			return Observation{}, errors.New("policy authority rotation ref requires sha256 hash")
		}
		obs.ProofRefs = appendUnique(obs.ProofRefs, PolicyAuthorityRotationRefPrefix+authorization.RotationHash)
	}
	obs.ProofRefs = appendUnique(obs.ProofRefs, PolicyAuthorityHeadRefPrefix+authorization.ChainHeadHash)
	obs.ProofRefs = appendUnique(obs.ProofRefs, PolicyAuthorizationRefPrefix+authorization.AuthorizationHash)
	return obs, nil
}
