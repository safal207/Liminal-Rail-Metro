package adaptive

import (
	"errors"
	"fmt"
	"strings"

	"github.com/safal207/Liminal-Rail-Metro/internal/trustpolicy"
)

var ErrTrustPolicyDenied = errors.New("trust policy denied operation")

type TrustGate struct {
	policy   trustpolicy.Policy
	evidence trustpolicy.Evidence
}

func NewTrustGate(policy trustpolicy.Policy, evidence trustpolicy.Evidence) (TrustGate, error) {
	if err := policy.Validate(); err != nil {
		return TrustGate{}, fmt.Errorf("policy: %w", err)
	}
	if err := evidence.Validate(); err != nil {
		return TrustGate{}, fmt.Errorf("evidence: %w", err)
	}
	return TrustGate{policy: policy, evidence: evidence}, nil
}

func (g TrustGate) Policy() trustpolicy.Policy     { return g.policy }
func (g TrustGate) Evidence() trustpolicy.Evidence { return g.evidence }

func (g TrustGate) Decision() (trustpolicy.Decision, error) {
	return trustpolicy.Evaluate(g.policy, g.evidence)
}

func (g TrustGate) RequireAllowed() (trustpolicy.Decision, error) {
	d, err := g.Decision()
	if err != nil {
		return trustpolicy.Decision{}, err
	}
	if !d.Allowed {
		return d, fmt.Errorf("%w: policy=%s unmet=%s", ErrTrustPolicyDenied, d.PolicyID, strings.Join(d.Unmet, ","))
	}
	return d, nil
}

func (g TrustGate) Execute(effect func() error) (trustpolicy.Decision, error) {
	d, err := g.RequireAllowed()
	if err != nil {
		return d, err
	}
	if effect == nil {
		return d, errors.New("trusted execution callback is required")
	}
	if err := effect(); err != nil {
		return d, err
	}
	return d, nil
}

func (g TrustGate) Learn(learn func() error) (trustpolicy.Decision, error) {
	d, err := g.RequireAllowed()
	if err != nil {
		return d, err
	}
	if learn == nil {
		return d, errors.New("trusted learning callback is required")
	}
	if err := learn(); err != nil {
		return d, err
	}
	return d, nil
}

func BindTrustDecisionResult(result map[string]any, decision trustpolicy.Decision) (map[string]any, error) {
	if err := decision.Validate(); err != nil {
		return nil, err
	}
	bound := make(map[string]any, len(result)+6)
	for key, value := range result {
		bound[key] = value
	}
	bound["trust_decision_protocol"] = decision.Protocol
	bound["trust_policy_id"] = decision.PolicyID
	bound["trust_policy_hash"] = decision.PolicyHash
	bound["trust_evidence_hash"] = decision.EvidenceHash
	bound["trust_decision_hash"] = decision.DecisionHash
	bound["trust_allowed"] = decision.Allowed
	return bound, nil
}

func ValidateTrustDecisionResultBinding(result map[string]any, decision trustpolicy.Decision) error {
	if err := decision.Validate(); err != nil {
		return err
	}
	if valueString(result, "trust_decision_protocol") != decision.Protocol ||
		valueString(result, "trust_policy_id") != decision.PolicyID ||
		valueString(result, "trust_policy_hash") != decision.PolicyHash ||
		valueString(result, "trust_evidence_hash") != decision.EvidenceHash ||
		valueString(result, "trust_decision_hash") != decision.DecisionHash {
		return errors.New("measured result is not bound to trust policy decision")
	}
	allowed, ok := result["trust_allowed"].(bool)
	if !ok || allowed != decision.Allowed {
		return errors.New("measured result trust-allowed flag mismatch")
	}
	return nil
}
