package adaptive

import (
	"encoding/json"
	"errors"
	"fmt"

	"github.com/safal207/Liminal-Rail-Metro/internal/metro"
	"github.com/safal207/Liminal-Rail-Metro/internal/policyauthority"
	"github.com/safal207/Liminal-Rail-Metro/internal/trustpolicy"
)

var (
	ErrPolicyAuthorityMismatch = errors.New("requested trust policy does not match signed policy authority")
	ErrOperationClassMismatch  = errors.New("declared operation class does not match signed execution binding")
)

type OperationRequest struct {
	DeclaredClass string
	Action        metro.Action
	Target        string
	SideEffect    bool
}

type BoundOperation struct {
	OperationClass string
	Action         metro.Action
	Target         string
	SideEffect     bool
	Descriptor     policyauthority.OperationDescriptor
}

func (o BoundOperation) Validate() error {
	if o.OperationClass == "" || o.OperationClass != o.Descriptor.OperationClass {
		return errors.New("bound operation class mismatch")
	}
	return policyauthority.VerifyOperationDescriptor(o.Descriptor, o.Action.Kind, o.Action.Inputs, o.Target, o.SideEffect)
}

type OperationLearner interface {
	Learn(BoundOperation) error
}

type LearnFunc func(BoundOperation) error

func (f LearnFunc) Learn(op BoundOperation) error { return f(op) }

type PolicyAuthorityGate struct {
	trust         TrustGate
	runtime       *PolicyAuthorityRuntime
	authorization policyauthority.Authorization
	operation     BoundOperation
}

func newPolicyAuthorityGate(runtime *PolicyAuthorityRuntime, request OperationRequest, requested *trustpolicy.Policy, evidence trustpolicy.Evidence) (PolicyAuthorityGate, error) {
	if runtime == nil || runtime.resolver == nil {
		return PolicyAuthorityGate{}, errors.New("policy authority runtime is required")
	}
	policy, descriptor, authorization, err := runtime.resolver.ResolveOperation(request.Action.Kind, request.Action.Inputs, request.Target, request.SideEffect)
	if err != nil {
		return PolicyAuthorityGate{}, err
	}
	if request.DeclaredClass != "" && request.DeclaredClass != descriptor.OperationClass {
		return PolicyAuthorityGate{}, fmt.Errorf("%w: declared=%s signed=%s", ErrOperationClassMismatch, request.DeclaredClass, descriptor.OperationClass)
	}
	if requested != nil {
		if err := policyauthority.VerifyRequestedPolicy(authorization, *requested); err != nil {
			return PolicyAuthorityGate{}, fmt.Errorf("%w: %v", ErrPolicyAuthorityMismatch, err)
		}
		if requested.PolicyHash != policy.PolicyHash {
			return PolicyAuthorityGate{}, fmt.Errorf("%w: resolved policy hash mismatch", ErrPolicyAuthorityMismatch)
		}
	}
	action, err := cloneAction(request.Action)
	if err != nil {
		return PolicyAuthorityGate{}, err
	}
	op := BoundOperation{
		OperationClass: descriptor.OperationClass, Action: action, Target: request.Target,
		SideEffect: request.SideEffect, Descriptor: descriptor,
	}
	if err := op.Validate(); err != nil {
		return PolicyAuthorityGate{}, err
	}
	if _, err := runtime.handlerFor(op); err != nil {
		return PolicyAuthorityGate{}, err
	}
	trust, err := NewTrustGate(policy, evidence)
	if err != nil {
		return PolicyAuthorityGate{}, err
	}
	return PolicyAuthorityGate{trust: trust, runtime: runtime, authorization: authorization, operation: op}, nil
}

func cloneAction(action metro.Action) (metro.Action, error) {
	b, err := json.Marshal(action)
	if err != nil {
		return metro.Action{}, err
	}
	var out metro.Action
	if err := json.Unmarshal(b, &out); err != nil {
		return metro.Action{}, err
	}
	return out, nil
}

func (g PolicyAuthorityGate) Authorization() policyauthority.Authorization { return g.authorization }
func (g PolicyAuthorityGate) Policy() trustpolicy.Policy                   { return g.trust.Policy() }
func (g PolicyAuthorityGate) Evidence() trustpolicy.Evidence               { return g.trust.Evidence() }

func (g PolicyAuthorityGate) Operation() (BoundOperation, error) {
	action, err := cloneAction(g.operation.Action)
	if err != nil {
		return BoundOperation{}, err
	}
	op := g.operation
	op.Action = action
	return op, op.Validate()
}

func (g PolicyAuthorityGate) Decision() (trustpolicy.Decision, error) {
	return g.trust.Decision()
}

func (g PolicyAuthorityGate) RequireAllowed() (trustpolicy.Decision, error) {
	return g.trust.RequireAllowed()
}

func (g PolicyAuthorityGate) Execute() (trustpolicy.Decision, error) {
	d, err := g.RequireAllowed()
	if err != nil {
		return d, err
	}
	if g.runtime == nil {
		return d, errors.New("policy authority runtime is not initialized")
	}
	op, err := g.Operation()
	if err != nil {
		return d, err
	}
	handler, err := g.runtime.handlerFor(op)
	if err != nil {
		return d, err
	}
	if err := handler(op); err != nil {
		return d, err
	}
	return d, nil
}

func (g PolicyAuthorityGate) Learn() (trustpolicy.Decision, error) {
	d, err := g.RequireAllowed()
	if err != nil {
		return d, err
	}
	if g.runtime == nil || g.runtime.learner == nil {
		return d, errors.New("trusted runtime learner is required")
	}
	op, err := g.Operation()
	if err != nil {
		return d, err
	}
	if err := g.runtime.learner.Learn(op); err != nil {
		return d, err
	}
	return d, nil
}

func (g PolicyAuthorityGate) BindDecisionResult(result map[string]any, decision trustpolicy.Decision) (map[string]any, error) {
	if err := g.runtime.resolver.VerifyAuthorization(g.authorization); err != nil {
		return nil, err
	}
	if err := g.operation.Validate(); err != nil {
		return nil, err
	}
	if err := decision.Validate(); err != nil {
		return nil, err
	}
	if g.authorization.PolicyHash != decision.PolicyHash {
		return nil, errors.New("policy authority authorization is not bound to trust decision policy")
	}
	bound, err := BindTrustDecisionResult(result, decision)
	if err != nil {
		return nil, err
	}
	a := g.authorization
	bound["policy_authority_protocol"] = a.Protocol
	bound["policy_authority_id"] = a.AuthorityID
	bound["policy_authority_generation"] = a.Generation
	bound["policy_operation_class"] = a.OperationClass
	bound["policy_action_kind"] = a.ActionKind
	bound["policy_target"] = a.Target
	bound["policy_side_effect"] = a.SideEffect
	bound["policy_input_hash"] = a.InputHash
	bound["policy_operation_descriptor_hash"] = a.OperationDescriptorHash
	bound["policy_authority_policy_hash"] = a.PolicyHash
	bound["policy_manifest_hash"] = a.ManifestHash
	bound["policy_signed_manifest_hash"] = a.SignedManifestHash
	bound["policy_authority_issuer_key_id"] = a.IssuerKeyID
	bound["policy_authority_rotation_hash"] = a.RotationHash
	bound["policy_authority_chain_head_hash"] = a.ChainHeadHash
	bound["policy_authorization_hash"] = a.AuthorizationHash
	return bound, nil
}

func (g PolicyAuthorityGate) ValidateDecisionResultBinding(result map[string]any, decision trustpolicy.Decision) error {
	if err := g.runtime.resolver.VerifyAuthorization(g.authorization); err != nil {
		return err
	}
	if err := g.operation.Validate(); err != nil {
		return err
	}
	if err := ValidateTrustDecisionResultBinding(result, decision); err != nil {
		return err
	}
	a := g.authorization
	if a.PolicyHash != decision.PolicyHash {
		return errors.New("policy authority authorization is not bound to trust decision policy")
	}
	if valueString(result, "policy_authority_protocol") != a.Protocol ||
		valueString(result, "policy_authority_id") != a.AuthorityID ||
		valueString(result, "policy_operation_class") != a.OperationClass ||
		valueString(result, "policy_action_kind") != a.ActionKind ||
		valueString(result, "policy_target") != a.Target ||
		valueString(result, "policy_input_hash") != a.InputHash ||
		valueString(result, "policy_operation_descriptor_hash") != a.OperationDescriptorHash ||
		valueString(result, "policy_authority_policy_hash") != a.PolicyHash ||
		valueString(result, "policy_manifest_hash") != a.ManifestHash ||
		valueString(result, "policy_signed_manifest_hash") != a.SignedManifestHash ||
		valueString(result, "policy_authority_issuer_key_id") != a.IssuerKeyID ||
		valueString(result, "policy_authority_rotation_hash") != a.RotationHash ||
		valueString(result, "policy_authority_chain_head_hash") != a.ChainHeadHash ||
		valueString(result, "policy_authorization_hash") != a.AuthorizationHash {
		return errors.New("measured result is not bound to signed policy authority")
	}
	sideEffect, ok := result["policy_side_effect"].(bool)
	if !ok || sideEffect != a.SideEffect {
		return errors.New("measured result policy side-effect flag mismatch")
	}
	generation, ok := result["policy_authority_generation"].(uint64)
	if !ok {
		if f, floatOK := result["policy_authority_generation"].(float64); floatOK && f >= 0 && uint64(f) == a.Generation {
			return nil
		}
		return errors.New("measured result policy authority generation mismatch")
	}
	if generation != a.Generation {
		return errors.New("measured result policy authority generation mismatch")
	}
	return nil
}
