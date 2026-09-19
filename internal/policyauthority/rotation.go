package policyauthority

import (
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/safal207/Liminal-Rail-Metro/internal/trustpolicy"
)

type Rotation struct {
	Protocol               string   `json:"protocol"`
	AuthorityID            string   `json:"authority_id"`
	FromGeneration         uint64   `json:"from_generation"`
	ToGeneration           uint64   `json:"to_generation"`
	FromManifestHash       string   `json:"from_manifest_hash"`
	ToManifestHash         string   `json:"to_manifest_hash"`
	FromSignedManifestHash string   `json:"from_signed_manifest_hash"`
	ToSignedManifestHash   string   `json:"to_signed_manifest_hash"`
	FromIssuerID           string   `json:"from_issuer_id"`
	FromIssuerKeyID        string   `json:"from_issuer_key_id"`
	ToIssuerID             string   `json:"to_issuer_id"`
	ToIssuerKeyID          string   `json:"to_issuer_key_id"`
	AllowWeakening         bool     `json:"allow_weakening"`
	WeakenedOperations     []string `json:"weakened_operations,omitempty"`
	IssuedAt               string   `json:"issued_at"`
	SignatureAlgorithm     string   `json:"signature_algorithm"`
	Signature              string   `json:"signature_base64"`
	RotationHash           string   `json:"rotation_hash"`
}

type rotationMaterial struct {
	Protocol               string   `json:"protocol"`
	AuthorityID            string   `json:"authority_id"`
	FromGeneration         uint64   `json:"from_generation"`
	ToGeneration           uint64   `json:"to_generation"`
	FromManifestHash       string   `json:"from_manifest_hash"`
	ToManifestHash         string   `json:"to_manifest_hash"`
	FromSignedManifestHash string   `json:"from_signed_manifest_hash"`
	ToSignedManifestHash   string   `json:"to_signed_manifest_hash"`
	FromIssuerID           string   `json:"from_issuer_id"`
	FromIssuerKeyID        string   `json:"from_issuer_key_id"`
	ToIssuerID             string   `json:"to_issuer_id"`
	ToIssuerKeyID          string   `json:"to_issuer_key_id"`
	AllowWeakening         bool     `json:"allow_weakening"`
	WeakenedOperations     []string `json:"weakened_operations,omitempty"`
	IssuedAt               string   `json:"issued_at"`
	SignatureAlgorithm     string   `json:"signature_algorithm"`
}

type rotationHashMaterial struct {
	Material  rotationMaterial `json:"material"`
	Signature string           `json:"signature_base64"`
}

func (r Rotation) material() rotationMaterial {
	return rotationMaterial{
		Protocol:               r.Protocol,
		AuthorityID:            r.AuthorityID,
		FromGeneration:         r.FromGeneration,
		ToGeneration:           r.ToGeneration,
		FromManifestHash:       r.FromManifestHash,
		ToManifestHash:         r.ToManifestHash,
		FromSignedManifestHash: r.FromSignedManifestHash,
		ToSignedManifestHash:   r.ToSignedManifestHash,
		FromIssuerID:           r.FromIssuerID,
		FromIssuerKeyID:        r.FromIssuerKeyID,
		ToIssuerID:             r.ToIssuerID,
		ToIssuerKeyID:          r.ToIssuerKeyID,
		AllowWeakening:         r.AllowWeakening,
		WeakenedOperations:     append([]string(nil), r.WeakenedOperations...),
		IssuedAt:               r.IssuedAt,
		SignatureAlgorithm:     r.SignatureAlgorithm,
	}
}

func SignRotation(current, next SignedManifest, currentPrivateKey ed25519.PrivateKey, allowWeakening bool) (Rotation, error) {
	if err := current.SelfVerify(); err != nil {
		return Rotation{}, err
	}
	if err := next.SelfVerify(); err != nil {
		return Rotation{}, err
	}
	if current.Manifest.AuthorityID != next.Manifest.AuthorityID {
		return Rotation{}, errors.New("policy authority rotation cannot change authority_id")
	}
	if next.Manifest.Generation != current.Manifest.Generation+1 {
		return Rotation{}, errors.New("policy authority rotation requires exactly next generation")
	}
	if len(currentPrivateKey) != ed25519.PrivateKeySize {
		return Rotation{}, errors.New("invalid current policy authority private key")
	}
	publicKey, ok := currentPrivateKey.Public().(ed25519.PublicKey)
	if !ok || keyID(publicKey) != current.IssuerKeyID {
		return Rotation{}, errors.New("rotation private key does not match current policy authority issuer")
	}
	weakened, err := weakenedOperations(current.Manifest, next.Manifest)
	if err != nil {
		return Rotation{}, err
	}
	if len(weakened) > 0 && !allowWeakening {
		return Rotation{}, fmt.Errorf("policy authority rotation would weaken operations: %v", weakened)
	}
	r := Rotation{
		Protocol:               RotationProtocol,
		AuthorityID:            current.Manifest.AuthorityID,
		FromGeneration:         current.Manifest.Generation,
		ToGeneration:           next.Manifest.Generation,
		FromManifestHash:       current.Manifest.ManifestHash,
		ToManifestHash:         next.Manifest.ManifestHash,
		FromSignedManifestHash: current.SignedManifestHash,
		ToSignedManifestHash:   next.SignedManifestHash,
		FromIssuerID:           current.IssuerID,
		FromIssuerKeyID:        current.IssuerKeyID,
		ToIssuerID:             next.IssuerID,
		ToIssuerKeyID:          next.IssuerKeyID,
		AllowWeakening:         allowWeakening,
		WeakenedOperations:     weakened,
		IssuedAt:               time.Now().UTC().Format(time.RFC3339Nano),
		SignatureAlgorithm:     SignatureEd25519,
	}
	materialBytes, err := json.Marshal(r.material())
	if err != nil {
		return Rotation{}, err
	}
	r.Signature = base64.StdEncoding.EncodeToString(ed25519.Sign(currentPrivateKey, materialBytes))
	r.RotationHash, err = hashJSON(rotationHashMaterial{Material: r.material(), Signature: r.Signature})
	if err != nil {
		return Rotation{}, err
	}
	return r, nil
}

func (r Rotation) Verify(current, next SignedManifest) error {
	if err := current.SelfVerify(); err != nil {
		return err
	}
	if err := next.SelfVerify(); err != nil {
		return err
	}
	if r.Protocol != RotationProtocol || r.SignatureAlgorithm != SignatureEd25519 {
		return errors.New("unsupported policy authority rotation protocol or signature algorithm")
	}
	if next.Manifest.Generation != current.Manifest.Generation+1 ||
		r.AuthorityID != current.Manifest.AuthorityID || r.AuthorityID != next.Manifest.AuthorityID ||
		r.FromGeneration != current.Manifest.Generation || r.ToGeneration != next.Manifest.Generation ||
		r.FromManifestHash != current.Manifest.ManifestHash || r.ToManifestHash != next.Manifest.ManifestHash ||
		r.FromSignedManifestHash != current.SignedManifestHash || r.ToSignedManifestHash != next.SignedManifestHash ||
		r.FromIssuerID != current.IssuerID || r.FromIssuerKeyID != current.IssuerKeyID ||
		r.ToIssuerID != next.IssuerID || r.ToIssuerKeyID != next.IssuerKeyID {
		return errors.New("policy authority rotation does not match signed manifest transition")
	}
	weakened, err := weakenedOperations(current.Manifest, next.Manifest)
	if err != nil {
		return err
	}
	if !equalStrings(weakened, r.WeakenedOperations) {
		return errors.New("policy authority rotation weakening set mismatch")
	}
	if len(weakened) > 0 && !r.AllowWeakening {
		return errors.New("policy authority rotation weakens requirements without explicit authorization")
	}
	publicKey, err := current.PublicKey()
	if err != nil {
		return err
	}
	signature, err := base64.StdEncoding.DecodeString(r.Signature)
	if err != nil || len(signature) != ed25519.SignatureSize {
		return errors.New("invalid policy authority rotation signature")
	}
	materialBytes, err := json.Marshal(r.material())
	if err != nil {
		return err
	}
	if !ed25519.Verify(publicKey, materialBytes, signature) {
		return errors.New("policy authority rotation signature verification failed")
	}
	if !shaHex(r.RotationHash) {
		return errors.New("rotation_hash must be sha256 hex")
	}
	expected, err := hashJSON(rotationHashMaterial{Material: r.material(), Signature: r.Signature})
	if err != nil {
		return err
	}
	if expected != r.RotationHash {
		return errors.New("policy authority rotation hash mismatch")
	}
	return nil
}

func weakenedOperations(current, next Manifest) ([]string, error) {
	if err := current.Validate(); err != nil {
		return nil, err
	}
	if err := next.Validate(); err != nil {
		return nil, err
	}
	weakened := make([]string, 0)
	for _, oldBinding := range current.Bindings {
		newPolicy, err := next.Resolve(oldBinding.OperationClass)
		if err != nil || !requirementsAtLeast(newPolicy.Requirements, oldBinding.Policy.Requirements) {
			weakened = append(weakened, oldBinding.OperationClass)
			continue
		}
		var nextBinding *Binding
		for i := range next.Bindings {
			if next.Bindings[i].OperationClass == oldBinding.OperationClass {
				nextBinding = &next.Bindings[i]
				break
			}
		}
		if nextBinding == nil || nextBinding.ActionKind != oldBinding.ActionKind || nextBinding.Target != oldBinding.Target || nextBinding.SideEffect != oldBinding.SideEffect {
			weakened = append(weakened, oldBinding.OperationClass)
		}
	}
	sort.Strings(weakened)
	return weakened, nil
}

func requirementsAtLeast(next, current trustpolicy.Requirements) bool {
	return (!current.ExternalIdentity || next.ExternalIdentity) &&
		(!current.PortablePublication || next.PortablePublication) &&
		(!current.HardwareBacked || next.HardwareBacked) &&
		(!current.RemoteHardwareAttestation || next.RemoteHardwareAttestation)
}
