package policyauthority

import (
	"errors"
	"fmt"

	"github.com/safal207/Liminal-Rail-Metro/internal/trustpolicy"
)

type Authorization struct {
	Protocol           string `json:"protocol"`
	AuthorityID        string `json:"authority_id"`
	Generation         uint64 `json:"generation"`
	OperationClass     string `json:"operation_class"`
	PolicyHash         string `json:"policy_hash"`
	ManifestHash       string `json:"manifest_hash"`
	SignedManifestHash string `json:"signed_manifest_hash"`
	IssuerKeyID        string `json:"issuer_key_id"`
	RotationHash       string `json:"rotation_hash,omitempty"`
	AuthorizationHash  string `json:"authorization_hash"`
}

type authorizationMaterial struct {
	Protocol           string `json:"protocol"`
	AuthorityID        string `json:"authority_id"`
	Generation         uint64 `json:"generation"`
	OperationClass     string `json:"operation_class"`
	PolicyHash         string `json:"policy_hash"`
	ManifestHash       string `json:"manifest_hash"`
	SignedManifestHash string `json:"signed_manifest_hash"`
	IssuerKeyID        string `json:"issuer_key_id"`
	RotationHash       string `json:"rotation_hash,omitempty"`
}

func (a Authorization) material() authorizationMaterial {
	return authorizationMaterial{
		Protocol:           a.Protocol,
		AuthorityID:        a.AuthorityID,
		Generation:         a.Generation,
		OperationClass:     a.OperationClass,
		PolicyHash:         a.PolicyHash,
		ManifestHash:       a.ManifestHash,
		SignedManifestHash: a.SignedManifestHash,
		IssuerKeyID:        a.IssuerKeyID,
		RotationHash:       a.RotationHash,
	}
}

func (a Authorization) Validate() error {
	if a.Protocol != AuthorizationProtocol || a.AuthorityID == "" || a.Generation == 0 || a.OperationClass == "" {
		return errors.New("policy authorization fields are incomplete")
	}
	for _, value := range []string{a.PolicyHash, a.ManifestHash, a.SignedManifestHash, a.IssuerKeyID, a.AuthorizationHash} {
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
}

func OpenResolver(root TrustRoot, manifests []SignedManifest, rotations []Rotation) (*Resolver, error) {
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
	return &Resolver{
		root:      root,
		manifests: append([]SignedManifest(nil), manifests...),
		rotations: append([]Rotation(nil), rotations...),
	}, nil
}

func (r *Resolver) Resolve(operationClass string) (trustpolicy.Policy, Authorization, error) {
	if r == nil || len(r.manifests) == 0 {
		return trustpolicy.Policy{}, Authorization{}, errors.New("policy authority resolver is not initialized")
	}
	current := r.manifests[len(r.manifests)-1]
	policy, err := current.Manifest.Resolve(operationClass)
	if err != nil {
		return trustpolicy.Policy{}, Authorization{}, err
	}
	a := Authorization{
		Protocol:           AuthorizationProtocol,
		AuthorityID:        current.Manifest.AuthorityID,
		Generation:         current.Manifest.Generation,
		OperationClass:     operationClass,
		PolicyHash:         policy.PolicyHash,
		ManifestHash:       current.Manifest.ManifestHash,
		SignedManifestHash: current.SignedManifestHash,
		IssuerKeyID:        current.IssuerKeyID,
	}
	if len(r.rotations) > 0 {
		a.RotationHash = r.rotations[len(r.rotations)-1].RotationHash
	}
	a.AuthorizationHash, err = hashJSON(a.material())
	if err != nil {
		return trustpolicy.Policy{}, Authorization{}, err
	}
	if err := a.Validate(); err != nil {
		return trustpolicy.Policy{}, Authorization{}, err
	}
	return policy, a, nil
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
