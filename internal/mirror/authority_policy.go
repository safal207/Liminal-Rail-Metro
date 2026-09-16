package mirror

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
)

const AuthorityPolicyProtocol = "mirror.authority-policy.v0.1"

// AuthorityPolicy defines which executor identities are authoritative for a
// specific action/effect kind at the external-proof boundary.
//
// It is intentionally exact-match and deny-by-default. Routing permission and
// receipt validity do not imply proof authority.
type AuthorityPolicy struct {
	Protocol          string              `json:"protocol,omitempty"`
	ID                string              `json:"id"`
	PolicyRef         string              `json:"policy_ref,omitempty"`
	ExecutorsByAction map[string][]string `json:"executors_by_action"`
}

// PolicySnapshot binds a logical policy reference to the SHA-256 of its
// canonical JSON representation. This lets proof provenance name the exact
// immutable policy content that authorized an executor.
type PolicySnapshot struct {
	PolicyRef     string `json:"policy_ref"`
	HashAlgorithm string `json:"hash_algorithm"`
	PolicyHash    string `json:"policy_hash"`
}

// AuthorityProof is emitted only after an exact action/executor authority
// check and an immutable policy snapshot have both succeeded.
type AuthorityProof struct {
	PolicyID   string `json:"policy_id"`
	PolicyRef  string `json:"policy_ref"`
	PolicyHash string `json:"policy_hash"`
	ActionKind string `json:"action_kind"`
	ExecutorID string `json:"executor_id"`
}

// Authorize returns nil only when executorID is explicitly allowed to certify
// the supplied action kind under this policy.
func (p AuthorityPolicy) Authorize(actionKind, executorID string) error {
	if p.ID == "" {
		return errors.New("authority policy id is required")
	}
	if actionKind == "" {
		return errors.New("action kind is required for authority check")
	}
	if executorID == "" {
		return errors.New("executor id is required for authority check")
	}

	allowed, ok := p.ExecutorsByAction[actionKind]
	if !ok || len(allowed) == 0 {
		return fmt.Errorf("no authoritative executor configured for action kind %q", actionKind)
	}

	for _, candidate := range allowed {
		if candidate == executorID {
			return nil
		}
	}

	return fmt.Errorf("executor %q is not authoritative for action kind %q", executorID, actionKind)
}

// AuthorizeProof performs the authority check and also requires a durable,
// content-addressed snapshot of the policy that made the decision.
func (p AuthorityPolicy) AuthorizeProof(actionKind, executorID string) (AuthorityProof, error) {
	if err := p.Authorize(actionKind, executorID); err != nil {
		return AuthorityProof{}, err
	}
	snapshot, err := p.Snapshot()
	if err != nil {
		return AuthorityProof{}, err
	}
	return AuthorityProof{
		PolicyID:   p.ID,
		PolicyRef:  snapshot.PolicyRef,
		PolicyHash: snapshot.PolicyHash,
		ActionKind: actionKind,
		ExecutorID: executorID,
	}, nil
}

// Snapshot computes a durable reference + canonical content hash for policy.
func (p AuthorityPolicy) Snapshot() (PolicySnapshot, error) {
	if p.PolicyRef == "" {
		return PolicySnapshot{}, errors.New("authority policy_ref is required for immutable provenance")
	}
	canonical, err := p.CanonicalJSON()
	if err != nil {
		return PolicySnapshot{}, err
	}
	sum := sha256.Sum256(canonical)
	return PolicySnapshot{
		PolicyRef:     p.PolicyRef,
		HashAlgorithm: "sha256",
		PolicyHash:    hex.EncodeToString(sum[:]),
	}, nil
}

// CanonicalJSON returns a stable semantic representation of the policy.
// Action keys are serialized deterministically by encoding/json; executor lists
// are sorted and deduplicated so equivalent authority sets hash identically.
func (p AuthorityPolicy) CanonicalJSON() ([]byte, error) {
	if p.ID == "" {
		return nil, errors.New("authority policy id is required")
	}
	if p.PolicyRef == "" {
		return nil, errors.New("authority policy_ref is required")
	}
	if p.Protocol != "" && p.Protocol != AuthorityPolicyProtocol {
		return nil, fmt.Errorf("unexpected authority policy protocol %q", p.Protocol)
	}
	if len(p.ExecutorsByAction) == 0 {
		return nil, errors.New("authority policy must contain at least one action rule")
	}

	rules := make(map[string][]string, len(p.ExecutorsByAction))
	for action, executors := range p.ExecutorsByAction {
		if action == "" {
			return nil, errors.New("authority policy action kind must not be empty")
		}
		if len(executors) == 0 {
			return nil, fmt.Errorf("authority policy action %q has no executors", action)
		}

		ordered := append([]string(nil), executors...)
		sort.Strings(ordered)
		unique := ordered[:0]
		for _, executor := range ordered {
			if executor == "" {
				return nil, fmt.Errorf("authority policy action %q contains empty executor id", action)
			}
			if len(unique) == 0 || unique[len(unique)-1] != executor {
				unique = append(unique, executor)
			}
		}
		rules[action] = append([]string(nil), unique...)
	}

	canonical := struct {
		Protocol          string              `json:"protocol"`
		ID                string              `json:"id"`
		PolicyRef         string              `json:"policy_ref"`
		ExecutorsByAction map[string][]string `json:"executors_by_action"`
	}{
		Protocol:          AuthorityPolicyProtocol,
		ID:                p.ID,
		PolicyRef:         p.PolicyRef,
		ExecutorsByAction: rules,
	}

	return json.Marshal(canonical)
}

// Ref returns a compact provenance reference for a successful authority check.
func (p AuthorityPolicy) Ref(actionKind, executorID string) string {
	return fmt.Sprintf("authority://%s/%s/%s", p.ID, actionKind, executorID)
}

// DecisionRef returns the authority decision plus the exact policy hash that
// made it possible.
func (proof AuthorityProof) DecisionRef() string {
	return fmt.Sprintf("authority://%s/%s/%s/%s", proof.PolicyID, proof.PolicyHash, proof.ActionKind, proof.ExecutorID)
}
