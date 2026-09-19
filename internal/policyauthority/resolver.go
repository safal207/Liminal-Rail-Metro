package policyauthority

import (
	"errors"
	"fmt"

	"github.com/safal207/Liminal-Rail-Metro/internal/trustpolicy"
)

type Authorization struct {
	Protocol                string `json:"protocol"`
	AuthorityID             string `json:"authority_id"`
	Generation              uint64 `json:"generation"`
	OperationClass          string `json:"operation_class"`
	ActionKind              string `json:"action_kind"`
	Target                  string `json:"target"`
	SideEffect              bool   `json:"side_effect"`
	InputHash               string `json:"input_hash"`
	OperationDescriptorHash string `json:"operation_descriptor_hash"`
	PolicyHash              string `json:"policy_hash"`
	ManifestHash            string `json:"manifest_hash"`
	SignedManifestHash      string `json:"signed_manifest_hash"`
	IssuerKeyID             string `json:"issuer_key_id"`
	RotationHash            string `json:"rotation_hash,omitempty"`
	ChainHeadHash           string `json:"chain_head_hash"`
	AuthorizationHash       string `json:"authorization_hash"`
}

type authorizationMaterial struct {
	Protocol                string `json:"protocol"`
	AuthorityID             string `json:"authority_id"`
	Generation              uint64 `json:"generation"`
	OperationClass          string `json:"operation_class"`
	ActionKind              string `json:"action_kind"`
	Target                  string `json:"target"`
	SideEffect              bool   `json:"side_effect"`
	InputHash               string `json:"input_hash"`
	OperationDescriptorHash string `json:"operation_descriptor_hash"`
	PolicyHash              string `json:"policy_hash"`
	ManifestHash            string `json:"manifest_hash"`
	SignedManifestHash      string `json:"signed_manifest_hash"`
	IssuerKeyID             string `json:"issuer_key_id"`
	RotationHash            string `json:"rotation_hash,omitempty"`
	ChainHeadHash           string `json:"chain_head_hash"`
}

func (a Authorization) material() authorizationMaterial {
	return authorizationMaterial{
		Protocol: a.Protocol, AuthorityID: a.AuthorityID, Generation: a.Generation,
		OperationClass: a.OperationClass, ActionKind: a.ActionKind, Target: a.Target, SideEffect: a.SideEffect,
		InputHash: a.InputHash, OperationDescriptorHash: a.OperationDescriptorHash, PolicyHash: a.PolicyHash,
		ManifestHash: a.ManifestHash, SignedManifestHash: a.SignedManifestHash, IssuerKeyID: a.IssuerKeyID,
		RotationHash: a.RotationHash, ChainHeadHash: a.ChainHeadHash,
	}
}

func (a Authorization) Validate() error {
	if a.Protocol != AuthorizationProtocol || a.AuthorityID == "" || a.Generation == 0 ||
		a.OperationClass == "" || a.ActionKind == "" || a.Target == "" {
		return errors.New("policy authorization fields are incomplete")
	}
	for _, value := range []string{
		a.InputHash, a.OperationDescriptorHash, a.PolicyHash, a.ManifestHash,
		a.SignedManifestHash, a.IssuerKeyID, a.ChainHeadHash, a.AuthorizationHash,
	} {
		if !shaHex(value) {
			return errors.New("policy authorization requires sha256 identifiers")
		}
	}
	if a.RotationHash != "" && !shaHex(a.RotationHash) {
		return errors.New("policy authorization rotation_hash must be sha256 hex")
	}
	expected, err := hashJSON(a.material())
	if err != nil {
		return err
	}
	if expected != a.AuthorizationHash {
		return errors.New("policy authorization hash mismatch")
	}
	return nil
}

type Resolver struct {
	root      TrustRoot
	manifests []SignedManifest
	rotations []Rotation
	head      ChainHead
}

func openVerifiedResolver(root TrustRoot, manifests []SignedManifest, rotations []Rotation) (*Resolver, error) {
	if len(manifests) == 0 {
		return nil, errors.New("policy authority resolver requires at least the root signed manifest")
	}
	if len(rotations) != len(manifests)-1 {
		return nil, errors.New("policy authority resolver requires one rotation per manifest transition")
	}
	if err := root.verifyRootManifest(manifests[0]); err != nil {
		return nil, err
	}
	for i := 1; i < len(manifests); i++ {
		if err := rotations[i-1].Verify(manifests[i-1], manifests[i]); err != nil {
			return nil, fmt.Errorf("policy authority transition %d rejected: %w", i, err)
		}
	}
	head, err := chainHeadFor(manifests, rotations)
	if err != nil {
		return nil, err
	}
	return &Resolver{
		root: root, manifests: append([]SignedManifest(nil), manifests...),
		rotations: append([]Rotation(nil), rotations...), head: head,
	}, nil
}

func (r *Resolver) ResolveOperation(actionKind string, inputs map[string]any, target string, sideEffect bool) (trustpolicy.Policy, OperationDescriptor, Authorization, error) {
	if r == nil || len(r.manifests) == 0 {
		return trustpolicy.Policy{}, OperationDescriptor{}, Authorization{}, errors.New("policy authority resolver is not initialized")
	}
	current := r.manifests[len(r.manifests)-1]
	policy, descriptor, err := current.Manifest.ResolveOperation(actionKind, inputs, target, sideEffect)
	if err != nil {
		return trustpolicy.Policy{}, OperationDescriptor{}, Authorization{}, err
	}
	a := Authorization{
		Protocol: AuthorizationProtocol, AuthorityID: current.Manifest.AuthorityID, Generation: current.Manifest.Generation,
		OperationClass: descriptor.OperationClass, ActionKind: descriptor.ActionKind, Target: descriptor.Target, SideEffect: descriptor.SideEffect,
		InputHash: descriptor.InputHash, OperationDescriptorHash: descriptor.DescriptorHash, PolicyHash: policy.PolicyHash,
		ManifestHash: current.Manifest.ManifestHash, SignedManifestHash: current.SignedManifestHash, IssuerKeyID: current.IssuerKeyID,
		ChainHeadHash: r.head.HeadHash,
	}
	if len(r.rotations) > 0 {
		a.RotationHash = r.rotations[len(r.rotations)-1].RotationHash
	}
	a.AuthorizationHash, err = hashJSON(a.material())
	if err != nil {
		return trustpolicy.Policy{}, OperationDescriptor{}, Authorization{}, err
	}
	if err := r.VerifyAuthorization(a); err != nil {
		return trustpolicy.Policy{}, OperationDescriptor{}, Authorization{}, err
	}
	return policy, descriptor, a, nil
}

func (r *Resolver) VerifyAuthorization(a Authorization) error {
	if r == nil || len(r.manifests) == 0 {
		return errors.New("policy authority resolver is not initialized")
	}
	if err := a.Validate(); err != nil {
		return err
	}
	if a.ChainHeadHash != r.head.HeadHash || a.Generation != r.head.Generation {
		return errors.New("policy authorization is not bound to the accepted chain head")
	}
	current := r.manifests[len(r.manifests)-1]
	if a.AuthorityID != current.Manifest.AuthorityID || a.ManifestHash != current.Manifest.ManifestHash ||
		a.SignedManifestHash != current.SignedManifestHash || a.IssuerKeyID != current.IssuerKeyID {
		return errors.New("policy authorization is not bound to the current signed manifest")
	}
	var binding *Binding
	for i := range current.Manifest.Bindings {
		b := &current.Manifest.Bindings[i]
		if b.OperationClass == a.OperationClass && b.ActionKind == a.ActionKind &&
			b.Target == a.Target && b.SideEffect == a.SideEffect {
			binding = b
			break
		}
	}
	if binding == nil || binding.Policy.PolicyHash != a.PolicyHash {
		return errors.New("policy authorization is not derived from the current signed binding")
	}
	if a.RotationHash != r.head.RotationHash {
		return errors.New("policy authorization rotation does not match accepted chain head")
	}
	return nil
}

func (r *Resolver) Head() ChainHead {
	if r == nil {
		return ChainHead{}
	}
	return r.head
}

func VerifyRequestedPolicy(auth Authorization, requested trustpolicy.Policy) error {
	if err := auth.Validate(); err != nil {
		return err
	}
	if err := requested.Validate(); err != nil {
		return err
	}
	if requested.PolicyHash != auth.PolicyHash {
		return fmt.Errorf("requested trust policy %s does not match authority-bound policy %s for operation %s", requested.PolicyHash, auth.PolicyHash, auth.OperationClass)
	}
	return nil
}
