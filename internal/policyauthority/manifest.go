package policyauthority

import (
	"errors"
	"fmt"
	"sort"

	"github.com/safal207/Liminal-Rail-Metro/internal/trustpolicy"
)

const (
	ManifestProtocol       = "liminal.policy-authority.manifest.v1.1"
	SignedManifestProtocol = "liminal.policy-authority.signed-manifest.v1.1"
	TrustRootProtocol      = "liminal.policy-authority.trust-root.v1.1"
	RotationProtocol       = "liminal.policy-authority.rotation.v1.1"
	AuthorizationProtocol  = "liminal.policy-authority.authorization.v1.1"
	OperationDescriptorProtocol = "liminal.policy-authority.operation-descriptor.v1.1"
	ChainHeadProtocol = "liminal.policy-authority.chain-head.v1.1"
	SignatureEd25519       = "ed25519"
)

type Binding struct {
	OperationClass string             `json:"operation_class"`
	ActionKind     string             `json:"action_kind"`
	Target         string             `json:"target"`
	SideEffect     bool               `json:"side_effect"`
	Policy         trustpolicy.Policy `json:"policy"`
}

type OperationDescriptor struct {
	Protocol       string `json:"protocol"`
	OperationClass string `json:"operation_class"`
	ActionKind     string `json:"action_kind"`
	Target         string `json:"target"`
	SideEffect     bool   `json:"side_effect"`
	InputHash      string `json:"input_hash"`
	DescriptorHash string `json:"descriptor_hash"`
}

type operationDescriptorMaterial struct {
	Protocol       string `json:"protocol"`
	OperationClass string `json:"operation_class"`
	ActionKind     string `json:"action_kind"`
	Target         string `json:"target"`
	SideEffect     bool   `json:"side_effect"`
	InputHash      string `json:"input_hash"`
}

type Manifest struct {
	Protocol     string    `json:"protocol"`
	AuthorityID  string    `json:"authority_id"`
	Generation   uint64    `json:"generation"`
	Bindings     []Binding `json:"bindings"`
	ManifestHash string    `json:"manifest_hash"`
}

type manifestMaterial struct {
	Protocol    string    `json:"protocol"`
	AuthorityID string    `json:"authority_id"`
	Generation  uint64    `json:"generation"`
	Bindings    []Binding `json:"bindings"`
}

func NewManifest(authorityID string, generation uint64, bindings []Binding) (Manifest, error) {
	if authorityID == "" || generation == 0 {
		return Manifest{}, errors.New("policy authority_id and generation are required")
	}
	canonical, err := canonicalBindings(bindings)
	if err != nil {
		return Manifest{}, err
	}
	m := Manifest{
		Protocol:    ManifestProtocol,
		AuthorityID: authorityID,
		Generation:  generation,
		Bindings:    canonical,
	}
	m.ManifestHash, err = hashJSON(m.material())
	if err != nil {
		return Manifest{}, err
	}
	return m, nil
}

func (m Manifest) material() manifestMaterial {
	return manifestMaterial{
		Protocol:    m.Protocol,
		AuthorityID: m.AuthorityID,
		Generation:  m.Generation,
		Bindings:    cloneBindings(m.Bindings),
	}
}

func (m Manifest) Validate() error {
	if m.Protocol != ManifestProtocol {
		return fmt.Errorf("unsupported policy manifest protocol %q", m.Protocol)
	}
	if m.AuthorityID == "" || m.Generation == 0 {
		return errors.New("policy authority_id and generation are required")
	}
	canonical, err := canonicalBindings(m.Bindings)
	if err != nil {
		return err
	}
	if len(canonical) != len(m.Bindings) {
		return errors.New("policy manifest bindings are invalid")
	}
	for i := range canonical {
		if canonical[i].OperationClass != m.Bindings[i].OperationClass || canonical[i].Policy.PolicyHash != m.Bindings[i].Policy.PolicyHash {
			return errors.New("policy manifest bindings must use canonical lexical order")
		}
	}
	if !shaHex(m.ManifestHash) {
		return errors.New("manifest_hash must be sha256 hex")
	}
	expected, err := hashJSON(m.material())
	if err != nil {
		return err
	}
	if expected != m.ManifestHash {
		return errors.New("policy manifest hash mismatch")
	}
	return nil
}

func (m Manifest) Resolve(operationClass string) (trustpolicy.Policy, error) {
	if err := m.Validate(); err != nil {
		return trustpolicy.Policy{}, err
	}
	if operationClass == "" {
		return trustpolicy.Policy{}, errors.New("operation class is required")
	}
	i := sort.Search(len(m.Bindings), func(i int) bool { return m.Bindings[i].OperationClass >= operationClass })
	if i >= len(m.Bindings) || m.Bindings[i].OperationClass != operationClass {
		return trustpolicy.Policy{}, fmt.Errorf("operation class %q is not authorized by policy manifest", operationClass)
	}
	return m.Bindings[i].Policy, nil
}

func (m Manifest) ResolveOperation(actionKind string, inputs map[string]any, target string, sideEffect bool) (trustpolicy.Policy, OperationDescriptor, error) {
	if err := m.Validate(); err != nil {
		return trustpolicy.Policy{}, OperationDescriptor{}, err
	}
	if actionKind == "" || target == "" {
		return trustpolicy.Policy{}, OperationDescriptor{}, errors.New("action kind and target are required")
	}
	for _, binding := range m.Bindings {
		if binding.ActionKind == actionKind && binding.Target == target && binding.SideEffect == sideEffect {
			descriptor, err := NewOperationDescriptor(binding, inputs)
			if err != nil {
				return trustpolicy.Policy{}, OperationDescriptor{}, err
			}
			return binding.Policy, descriptor, nil
		}
	}
	return trustpolicy.Policy{}, OperationDescriptor{}, fmt.Errorf("action contract %q target=%q side_effect=%t is not authorized by policy manifest", actionKind, target, sideEffect)
}

func NewOperationDescriptor(binding Binding, inputs map[string]any) (OperationDescriptor, error) {
	if binding.OperationClass == "" || binding.ActionKind == "" || binding.Target == "" {
		return OperationDescriptor{}, errors.New("operation binding is incomplete")
	}
	if err := binding.Policy.Validate(); err != nil {
		return OperationDescriptor{}, err
	}
	inputHash, err := hashJSON(inputs)
	if err != nil {
		return OperationDescriptor{}, err
	}
	d := OperationDescriptor{
		Protocol:       OperationDescriptorProtocol,
		OperationClass: binding.OperationClass,
		ActionKind:     binding.ActionKind,
		Target:         binding.Target,
		SideEffect:     binding.SideEffect,
		InputHash:      inputHash,
	}
	d.DescriptorHash, err = hashJSON(d.material())
	if err != nil {
		return OperationDescriptor{}, err
	}
	return d, nil
}

func (d OperationDescriptor) material() operationDescriptorMaterial {
	return operationDescriptorMaterial{
		Protocol: d.Protocol, OperationClass: d.OperationClass, ActionKind: d.ActionKind,
		Target: d.Target, SideEffect: d.SideEffect, InputHash: d.InputHash,
	}
}

func (d OperationDescriptor) Validate() error {
	if d.Protocol != OperationDescriptorProtocol || d.OperationClass == "" || d.ActionKind == "" || d.Target == "" {
		return errors.New("operation descriptor fields are incomplete")
	}
	if !shaHex(d.InputHash) || !shaHex(d.DescriptorHash) {
		return errors.New("operation descriptor requires sha256 hashes")
	}
	expected, err := hashJSON(d.material())
	if err != nil {
		return err
	}
	if expected != d.DescriptorHash {
		return errors.New("operation descriptor hash mismatch")
	}
	return nil
}

func VerifyOperationDescriptor(d OperationDescriptor, actionKind string, inputs map[string]any, target string, sideEffect bool) error {
	if err := d.Validate(); err != nil {
		return err
	}
	if d.ActionKind != actionKind || d.Target != target || d.SideEffect != sideEffect {
		return errors.New("operation descriptor does not match execution contract")
	}
	inputHash, err := hashJSON(inputs)
	if err != nil {
		return err
	}
	if d.InputHash != inputHash {
		return errors.New("operation descriptor input hash mismatch")
	}
	return nil
}
func cloneBindings(bindings []Binding) []Binding {
	out := make([]Binding, len(bindings))
	copy(out, bindings)
	return out
}

func canonicalBindings(bindings []Binding) ([]Binding, error) {
	if len(bindings) == 0 {
		return nil, errors.New("policy manifest requires at least one binding")
	}
	out := cloneBindings(bindings)
	for i := range out {
		if out[i].OperationClass == "" || out[i].ActionKind == "" || out[i].Target == "" {
			return nil, errors.New("operation class, action kind, and target must not be empty")
		}
		if err := out[i].Policy.Validate(); err != nil {
			return nil, fmt.Errorf("binding %q has invalid policy: %w", out[i].OperationClass, err)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].OperationClass < out[j].OperationClass })
	contracts := make(map[string]string, len(out))
	for i := range out {
		if i > 0 && out[i-1].OperationClass == out[i].OperationClass {
			return nil, fmt.Errorf("duplicate operation class %q", out[i].OperationClass)
		}
		key := fmt.Sprintf("%s\x00%s\x00%t", out[i].ActionKind, out[i].Target, out[i].SideEffect)
		if previous, ok := contracts[key]; ok {
			return nil, fmt.Errorf("execution contract is ambiguous between %q and %q", previous, out[i].OperationClass)
		}
		contracts[key] = out[i].OperationClass
	}
	return out, nil
}
