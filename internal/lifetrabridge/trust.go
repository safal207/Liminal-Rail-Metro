package lifetrabridge

import "errors"

const (
	TrustPolicyRefPrefix   = "adaptive-trust-policy://sha256/"
	TrustEvidenceRefPrefix = "adaptive-trust-evidence://sha256/"
	TrustDecisionRefPrefix = "adaptive-trust-decision://sha256/"
)

func BindTrustProofRefs(obs Observation, policyHash, evidenceHash, decisionHash string) (Observation, error) {
	if !sha256Hex(policyHash) || !sha256Hex(evidenceHash) || !sha256Hex(decisionHash) {
		return Observation{}, errors.New("trust proof refs require sha256 hashes")
	}
	obs.ProofRefs = appendUnique(obs.ProofRefs, TrustPolicyRefPrefix+policyHash)
	obs.ProofRefs = appendUnique(obs.ProofRefs, TrustEvidenceRefPrefix+evidenceHash)
	obs.ProofRefs = appendUnique(obs.ProofRefs, TrustDecisionRefPrefix+decisionHash)
	return obs, nil
}
