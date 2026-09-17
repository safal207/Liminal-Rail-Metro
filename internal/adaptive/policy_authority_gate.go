package adaptive

import (
	"errors"
	"fmt"

	"github.com/safal207/Liminal-Rail-Metro/internal/policyauthority"
	"github.com/safal207/Liminal-Rail-Metro/internal/trustpolicy"
)

var ErrPolicyAuthorityMismatch = errors.New("requested trust policy does not match signed policy authority")

type PolicyAuthorityGate struct {
	trust         TrustGate
	authorization policyauthority.Authorization
}

func NewPolicyAuthorityGate(resolver *policyauthority.Resolver, operationClass string, evidence trustpolicy.Evidence) (PolicyAuthorityGate, error) {
	policy, authorization, err := resolver.Resolve(operationClass)
	if err != nil {
		return PolicyAuthorityGate{}, err
	}
	trust, err := NewTrustGate(policy, evidence)
	if err != nil {
		return PolicyAuthorityGate{}, err
	}
	return PolicyAuthorityGate{trust: trust, authorization: authorization}, nil
}

func NewPolicyAuthorityGateWithRequestedPolicy(resolver *policyauthority.Resolver, operationClass string, requested trustpolicy.Policy, evidence trustpolicy.Evidence) (PolicyAuthorityGate, error) {
	policy, authorization, err := resolver.Resolve(operationClass)
	if err != nil {
		return PolicyAuthorityGate{}, err
	}
	if err := policyauthority.VerifyRequestedPolicy(authorization, requested); err != nil {
		return PolicyAuthorityGate{}, fmt.Errorf("%w: %v", ErrPolicyAuthorityMismatch, err)
	}
	if requested.PolicyHash != policy.PolicyHash {
		return PolicyAuthorityGate{}, fmt.Errorf("%w: resolved policy hash mismatch", ErrPolicyAuthorityMismatch)
	}
	trust, err := NewTrustGate(policy, evidence)
	if err != nil {
		return PolicyAuthorityGate{}, err
	}
	return PolicyAuthorityGate{trust: trust, authorization: authorization}, nil
}

func (g PolicyAuthorityGate) Authorization() policyauthority.Authorization { return g.authorization }
func (g PolicyAuthorityGate) Policy() trustpolicy.Policy                   { return g.trust.Policy() }
func (g PolicyAuthorityGate) Evidence() trustpolicy.Evidence               { return g.trust.Evidence() }

func (g PolicyAuthorityGate) Decision() (trustpolicy.Decision, error) {
	return g.trust.Decision()
}

func (g PolicyAuthorityGate) RequireAllowed() (trustpolicy.Decision, error) {
	return g.trust.RequireAllowed()
}

func (g PolicyAuthorityGate) Execute(effect func() error) (trustpolicy.Decision, error) {
	return g.trust.Execute(effect)
}

func (g PolicyAuthorityGate) Learn(learn func() error) (trustpolicy.Decision, error) {
	return g.trust.Learn(learn)
}

func BindPolicyAuthorityDecisionResult(result map[string]any, authorization policyauthority.Authorization, decision trustpolicy.Decision) (map[string]any, error) {
	if err := authorization.Validate(); err != nil {
		return nil, err
	}
	if err := decision.Validate(); err != nil {
		return nil, err
	}
	if authorization.PolicyHash != decision.PolicyHash {
		return nil, errors.New("policy authority authorization is not bound to trust decision policy")
	}
	bound, err := BindTrustDecisionResult(result, decision)
	if err != nil {
		return nil, err
	}
	bound["policy_authority_protocol"] = authorization.Protocol
	bound["policy_authority_id"] = authorization.AuthorityID
	bound["policy_authority_generation"] = authorization.Generation
	bound["policy_operation_class"] = authorization.OperationClass
	bound["policy_authority_policy_hash"] = authorization.PolicyHash
	bound["policy_manifest_hash"] = authorization.ManifestHash
	bound["policy_signed_manifest_hash"] = authorization.SignedManifestHash
	bound["policy_authority_issuer_key_id"] = authorization.IssuerKeyID
	bound["policy_authority_rotation_hash"] = authorization.RotationHash
	bound["policy_authorization_hash"] = authorization.AuthorizationHash
	return bound, nil
}

func ValidatePolicyAuthorityDecisionResultBinding(result map[string]any, authorization policyauthority.Authorization, decision trustpolicy.Decision) error {
	if err := authorization.Validate(); err != nil {
		return err
	}
	if err := ValidateTrustDecisionResultBinding(result, decision); err != nil {
		return err
	}
	if authorization.PolicyHash != decision.PolicyHash {
		return errors.New("policy authority authorization is not bound to trust decision policy")
	}
	if valueString(result, "policy_authority_protocol") != authorization.Protocol ||
		valueString(result, "policy_authority_id") != authorization.AuthorityID ||
		valueString(result, "policy_operation_class") != authorization.OperationClass ||
		valueString(result, "policy_authority_policy_hash") != authorization.PolicyHash ||
		valueString(result, "policy_manifest_hash") != authorization.ManifestHash ||
		valueString(result, "policy_signed_manifest_hash") != authorization.SignedManifestHash ||
		valueString(result, "policy_authority_issuer_key_id") != authorization.IssuerKeyID ||
		valueString(result, "policy_authority_rotation_hash") != authorization.RotationHash ||
		valueString(result, "policy_authorization_hash") != authorization.AuthorizationHash {
		return errors.New("measured result is not bound to signed policy authority")
	}
	generation, ok := result["policy_authority_generation"].(uint64)
	if !ok {
		if f, floatOK := result["policy_authority_generation"].(float64); floatOK && f >= 0 && uint64(f) == authorization.Generation {
			return nil
		}
		return errors.New("measured result policy authority generation mismatch")
	}
	if generation != authorization.Generation {
		return errors.New("measured result policy authority generation mismatch")
	}
	return nil
}
