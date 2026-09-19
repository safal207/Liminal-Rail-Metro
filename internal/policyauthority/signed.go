package policyauthority

import (
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"errors"
	"time"
)

type SignedManifest struct {
	Protocol           string   `json:"protocol"`
	Manifest           Manifest `json:"manifest"`
	IssuerID           string   `json:"issuer_id"`
	IssuerKeyID        string   `json:"issuer_key_id"`
	IssuerPublicKey    string   `json:"issuer_public_key_base64"`
	SignatureAlgorithm string   `json:"signature_algorithm"`
	IssuedAt           string   `json:"issued_at"`
	Signature          string   `json:"signature_base64"`
	SignedManifestHash string   `json:"signed_manifest_hash"`
}

type signedManifestMaterial struct {
	Protocol           string   `json:"protocol"`
	Manifest           Manifest `json:"manifest"`
	IssuerID           string   `json:"issuer_id"`
	IssuerKeyID        string   `json:"issuer_key_id"`
	IssuerPublicKey    string   `json:"issuer_public_key_base64"`
	SignatureAlgorithm string   `json:"signature_algorithm"`
	IssuedAt           string   `json:"issued_at"`
}

type signedManifestHashMaterial struct {
	Material  signedManifestMaterial `json:"material"`
	Signature string                 `json:"signature_base64"`
}

func SignManifest(manifest Manifest, issuerID string, privateKey ed25519.PrivateKey) (SignedManifest, error) {
	if err := manifest.Validate(); err != nil {
		return SignedManifest{}, err
	}
	if issuerID == "" {
		return SignedManifest{}, errors.New("issuer_id is required")
	}
	if len(privateKey) != ed25519.PrivateKeySize {
		return SignedManifest{}, errors.New("invalid ed25519 private key")
	}
	publicKey, ok := privateKey.Public().(ed25519.PublicKey)
	if !ok || len(publicKey) != ed25519.PublicKeySize {
		return SignedManifest{}, errors.New("invalid ed25519 public key")
	}
	s := SignedManifest{
		Protocol:           SignedManifestProtocol,
		Manifest:           manifest,
		IssuerID:           issuerID,
		IssuerKeyID:        keyID(publicKey),
		IssuerPublicKey:    base64.StdEncoding.EncodeToString(publicKey),
		SignatureAlgorithm: SignatureEd25519,
		IssuedAt:           time.Now().UTC().Format(time.RFC3339Nano),
	}
	materialBytes, err := json.Marshal(s.material())
	if err != nil {
		return SignedManifest{}, err
	}
	s.Signature = base64.StdEncoding.EncodeToString(ed25519.Sign(privateKey, materialBytes))
	s.SignedManifestHash, err = hashJSON(signedManifestHashMaterial{Material: s.material(), Signature: s.Signature})
	if err != nil {
		return SignedManifest{}, err
	}
	return s, nil
}

func (s SignedManifest) material() signedManifestMaterial {
	return signedManifestMaterial{
		Protocol:           s.Protocol,
		Manifest:           s.Manifest,
		IssuerID:           s.IssuerID,
		IssuerKeyID:        s.IssuerKeyID,
		IssuerPublicKey:    s.IssuerPublicKey,
		SignatureAlgorithm: s.SignatureAlgorithm,
		IssuedAt:           s.IssuedAt,
	}
}

func (s SignedManifest) SelfVerify() error {
	if s.Protocol != SignedManifestProtocol || s.SignatureAlgorithm != SignatureEd25519 {
		return errors.New("unsupported signed policy manifest protocol or signature algorithm")
	}
	if err := s.Manifest.Validate(); err != nil {
		return err
	}
	if s.IssuerID == "" || s.IssuerKeyID == "" || s.IssuerPublicKey == "" || s.IssuedAt == "" {
		return errors.New("signed policy manifest requires issuer identity, key, and issued_at")
	}
	publicKey, err := decodePublicKey(s.IssuerPublicKey)
	if err != nil {
		return err
	}
	if keyID(publicKey) != s.IssuerKeyID {
		return errors.New("issuer_key_id does not match issuer public key")
	}
	signature, err := base64.StdEncoding.DecodeString(s.Signature)
	if err != nil || len(signature) != ed25519.SignatureSize {
		return errors.New("invalid ed25519 policy manifest signature encoding")
	}
	materialBytes, err := json.Marshal(s.material())
	if err != nil {
		return err
	}
	if !ed25519.Verify(publicKey, materialBytes, signature) {
		return errors.New("policy manifest signature verification failed")
	}
	if !shaHex(s.SignedManifestHash) {
		return errors.New("signed_manifest_hash must be sha256 hex")
	}
	expected, err := hashJSON(signedManifestHashMaterial{Material: s.material(), Signature: s.Signature})
	if err != nil {
		return err
	}
	if expected != s.SignedManifestHash {
		return errors.New("signed policy manifest hash mismatch")
	}
	return nil
}

func (s SignedManifest) PublicKey() (ed25519.PublicKey, error) {
	return decodePublicKey(s.IssuerPublicKey)
}

type TrustRoot struct {
	Protocol               string `json:"protocol"`
	AuthorityID            string `json:"authority_id"`
	RootGeneration         uint64 `json:"root_generation"`
	RootManifestHash       string `json:"root_manifest_hash"`
	RootSignedManifestHash string `json:"root_signed_manifest_hash"`
	IssuerID               string `json:"issuer_id"`
	IssuerKeyID            string `json:"issuer_key_id"`
}

func NewTrustRoot(root SignedManifest) (TrustRoot, error) {
	if err := root.SelfVerify(); err != nil {
		return TrustRoot{}, err
	}
	return TrustRoot{
		Protocol:               TrustRootProtocol,
		AuthorityID:            root.Manifest.AuthorityID,
		RootGeneration:         root.Manifest.Generation,
		RootManifestHash:       root.Manifest.ManifestHash,
		RootSignedManifestHash: root.SignedManifestHash,
		IssuerID:               root.IssuerID,
		IssuerKeyID:            root.IssuerKeyID,
	}, nil
}

func (r TrustRoot) Validate() error {
	if r.Protocol != TrustRootProtocol || r.AuthorityID == "" || r.RootGeneration == 0 || r.IssuerID == "" {
		return errors.New("policy authority trust root fields are incomplete")
	}
	if !shaHex(r.RootManifestHash) || !shaHex(r.RootSignedManifestHash) || !shaHex(r.IssuerKeyID) {
		return errors.New("policy authority trust root requires sha256 identifiers")
	}
	return nil
}

func (r TrustRoot) verifyRootManifest(root SignedManifest) error {
	if err := r.Validate(); err != nil {
		return err
	}
	if err := root.SelfVerify(); err != nil {
		return err
	}
	if root.Manifest.AuthorityID != r.AuthorityID || root.Manifest.Generation != r.RootGeneration ||
		root.Manifest.ManifestHash != r.RootManifestHash || root.SignedManifestHash != r.RootSignedManifestHash ||
		root.IssuerID != r.IssuerID || root.IssuerKeyID != r.IssuerKeyID {
		return errors.New("root signed manifest does not match pinned policy authority trust root")
	}
	return nil
}
