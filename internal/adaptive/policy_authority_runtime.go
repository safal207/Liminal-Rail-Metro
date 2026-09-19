package adaptive

import (
	"errors"
	"fmt"

	"github.com/safal207/Liminal-Rail-Metro/internal/policyauthority"
	"github.com/safal207/Liminal-Rail-Metro/internal/trustpolicy"
)

type OperationHandler func(BoundOperation) error

type HandlerRegistration struct {
	ActionKind string
	Target     string
	SideEffect bool
	Handler    OperationHandler
}

type PolicyAuthorityRuntime struct {
	resolver *policyauthority.Resolver
	handlers map[string]OperationHandler
	learner  OperationLearner
}

func NewPolicyAuthorityRuntime(resolver *policyauthority.Resolver, registrations []HandlerRegistration, learner OperationLearner) (*PolicyAuthorityRuntime, error) {
	if resolver == nil {
		return nil, errors.New("policy authority resolver is required")
	}
	if len(registrations) == 0 {
		return nil, errors.New("policy authority runtime requires at least one registered operation handler")
	}
	handlers := make(map[string]OperationHandler, len(registrations))
	for _, registration := range registrations {
		if registration.ActionKind == "" || registration.Target == "" || registration.Handler == nil {
			return nil, errors.New("operation handler registration is incomplete")
		}
		key := operationHandlerKey(registration.ActionKind, registration.Target, registration.SideEffect)
		if _, exists := handlers[key]; exists {
			return nil, fmt.Errorf("duplicate operation handler for %s", key)
		}
		handlers[key] = registration.Handler
	}
	return &PolicyAuthorityRuntime{resolver: resolver, handlers: handlers, learner: learner}, nil
}

func operationHandlerKey(actionKind, target string, sideEffect bool) string {
	return fmt.Sprintf("%s\x00%s\x00%t", actionKind, target, sideEffect)
}

func (r *PolicyAuthorityRuntime) handlerFor(op BoundOperation) (OperationHandler, error) {
	if r == nil {
		return nil, errors.New("policy authority runtime is not initialized")
	}
	if err := op.Validate(); err != nil {
		return nil, err
	}
	handler, ok := r.handlers[operationHandlerKey(op.Action.Kind, op.Target, op.SideEffect)]
	if !ok {
		return nil, fmt.Errorf("no trusted runtime handler for action %q target=%q side_effect=%t", op.Action.Kind, op.Target, op.SideEffect)
	}
	return handler, nil
}

func (r *PolicyAuthorityRuntime) NewGate(request OperationRequest, evidence trustpolicy.Evidence) (PolicyAuthorityGate, error) {
	return newPolicyAuthorityGate(r, request, nil, evidence)
}

func (r *PolicyAuthorityRuntime) NewGateWithRequestedPolicy(request OperationRequest, requested trustpolicy.Policy, evidence trustpolicy.Evidence) (PolicyAuthorityGate, error) {
	return newPolicyAuthorityGate(r, request, &requested, evidence)
}
