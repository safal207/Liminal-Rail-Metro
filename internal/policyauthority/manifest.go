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
	SignatureEd25519       = "ed25519"
)

type Binding struct {
	OperationClass string             `json:"operation_class"`
	Policy         trustpolicy.Policy `json:"policy"`
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
		if out[i].OperationClass == "" {
			return nil, errors.New("operation class must not be empty")
		}
		if err := out[i].Policy.Validate(); err != nil {
			return nil, fmt.Errorf("binding %q has invalid policy: %w", out[i].OperationClass, err)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].OperationClass < out[j].OperationClass })
	for i := 1; i < len(out); i++ {
		if out[i-1].OperationClass == out[i].OperationClass {
			return nil, fmt.Errorf("duplicate operation class %q", out[i].OperationClass)
		}
	}
	return out, nil
}
