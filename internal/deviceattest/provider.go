package deviceattest

import (
	"crypto/ed25519"
	"encoding/json"
	"errors"
	"fmt"
)

const (
	ProviderDescriptorProtocol  = "liminal.attestation.provider.v0.1"
	ProviderAnchorProtocol      = "liminal.attestation.provider-anchor.v0.1"
	ProviderEvidenceProtocol    = "liminal.attestation.provider-evidence.v0.1"
	ProviderAttestationProtocol = "liminal.attestation.provider-attestation.v0.1"

	AssuranceSoftwareBound = "software-bound"
)

type ProviderDescriptor struct {
	Protocol         string `json:"protocol"`
	ProviderID       string `json:"provider_id"`
	RootType         string `json:"root_type"`
	AssuranceLevel   string `json:"assurance_level"`
	HardwareBacked   bool   `json:"hardware_backed"`
	RemoteVerifiable bool   `json:"remote_verifiable"`
}

func (d ProviderDescriptor) Validate() error {
	if d.Protocol != ProviderDescriptorProtocol {
		return fmt.Errorf("unsupported attestation provider protocol %q", d.Protocol)
	}
	if d.ProviderID == "" || d.RootType == "" || d.AssuranceLevel == "" {
		return errors.New("attestation provider descriptor is incomplete")
	}
	return nil
}

func (d ProviderDescriptor) Hash() (string, error) {
	if err := d.Validate(); err != nil {
		return "", err
	}
	return hashJSON(d)
}

type ProviderBinding struct {
	Nonce                string `json:"nonce"`
	SignedAuthorityHash  string `json:"signed_authority_hash"`
	AuthorityIssuerKeyID string `json:"authority_issuer_key_id"`
	JournalSequence      uint64 `json:"journal_sequence"`
	JournalHead          string `json:"journal_head,omitempty"`
	Workload             string `json:"workload"`
	ContextKey           string `json:"context_key"`
}

func (b ProviderBinding) Validate() error {
	if b.Nonce == "" || b.Workload == "" || b.ContextKey == "" || !shaHex(b.SignedAuthorityHash) || !shaHex(b.AuthorityIssuerKeyID) {
		return errors.New("attestation provider binding is incomplete")
	}
	if b.JournalSequence == 0 && b.JournalHead != "" {
		return errors.New("empty journal requires empty head")
	}
	if b.JournalSequence > 0 && !shaHex(b.JournalHead) {
		return errors.New("non-empty journal requires sha256 head")
	}
	return nil
}

type ProviderTrustAnchor struct {
	Protocol       string             `json:"protocol"`
	Descriptor     ProviderDescriptor `json:"descriptor"`
	DescriptorHash string             `json:"descriptor_hash"`
	AnchorHash     string             `json:"anchor_hash"`
	Payload        json.RawMessage    `json:"payload"`
}

type ProviderEvidence struct {
	Protocol       string             `json:"protocol"`
	Descriptor     ProviderDescriptor `json:"descriptor"`
	DescriptorHash string             `json:"descriptor_hash"`
	EvidenceHash   string             `json:"evidence_hash"`
	Payload        json.RawMessage    `json:"payload"`
}

type ProviderAttestation struct {
	Protocol        string             `json:"protocol"`
	Descriptor      ProviderDescriptor `json:"descriptor"`
	DescriptorHash  string             `json:"descriptor_hash"`
	EvidenceHash    string             `json:"evidence_hash"`
	AttestationHash string             `json:"attestation_hash"`
	Payload         json.RawMessage    `json:"payload"`
}

type Provider interface {
	Descriptor() ProviderDescriptor
	Enroll() (ProviderTrustAnchor, ProviderEvidence, error)
	Evidence() (ProviderEvidence, error)
	ValidateAnchor(ProviderTrustAnchor) error
	Attest(ProviderBinding) (ProviderAttestation, error)
	Verify(ProviderAttestation, ProviderTrustAnchor, ProviderBinding) error
}

type SoftwareProvider struct {
	privateKey ed25519.PrivateKey
}

func NewSoftwareProvider(privateKey ed25519.PrivateKey) (*SoftwareProvider, error) {
	if len(privateKey) != ed25519.PrivateKeySize {
		return nil, errors.New("invalid software attestation private key")
	}
	return &SoftwareProvider{privateKey: append(ed25519.PrivateKey(nil), privateKey...)}, nil
}

func (p *SoftwareProvider) Descriptor() ProviderDescriptor {
	return ProviderDescriptor{
		Protocol:         ProviderDescriptorProtocol,
		ProviderID:       "software-observed-ed25519-v0.1",
		RootType:         RootTypeSoftware,
		AssuranceLevel:   AssuranceSoftwareBound,
		HardwareBacked:   false,
		RemoteVerifiable: false,
	}
}

func (p *SoftwareProvider) Enroll() (ProviderTrustAnchor, ProviderEvidence, error) {
	evidence, err := Sense()
	if err != nil {
		return ProviderTrustAnchor{}, ProviderEvidence{}, err
	}
	publicKey := p.privateKey.Public().(ed25519.PublicKey)
	anchor, err := NewTrustAnchor(evidence, publicKey)
	if err != nil {
		return ProviderTrustAnchor{}, ProviderEvidence{}, err
	}
	wrappedAnchor, err := wrapProviderAnchor(p.Descriptor(), anchor)
	if err != nil {
		return ProviderTrustAnchor{}, ProviderEvidence{}, err
	}
	wrappedEvidence, err := wrapProviderEvidence(p.Descriptor(), stableSoftwareEvidence(evidence))
	if err != nil {
		return ProviderTrustAnchor{}, ProviderEvidence{}, err
	}
	return wrappedAnchor, wrappedEvidence, nil
}

func (p *SoftwareProvider) Evidence() (ProviderEvidence, error) {
	evidence, err := Sense()
	if err != nil {
		return ProviderEvidence{}, err
	}
	return wrapProviderEvidence(p.Descriptor(), stableSoftwareEvidence(evidence))
}

func (p *SoftwareProvider) ValidateAnchor(anchor ProviderTrustAnchor) error {
	if err := validateProviderAnchorEnvelope(anchor, p.Descriptor()); err != nil {
		return err
	}
	var raw TrustAnchor
	if err := json.Unmarshal(anchor.Payload, &raw); err != nil {
		return fmt.Errorf("decode software provider anchor: %w", err)
	}
	publicKey := p.privateKey.Public().(ed25519.PublicKey)
	if raw.Protocol != TrustAnchorProtocol || raw.RootType != RootTypeSoftware || raw.AttestationKeyID != KeyID(publicKey) {
		return errors.New("software provider anchor does not match provider key")
	}
	current, err := Sense()
	if err != nil {
		return err
	}
	if raw.DeviceFingerprint != current.DeviceFingerprint {
		return errors.New("software provider anchor does not match current device fingerprint")
	}
	return nil
}

func (p *SoftwareProvider) Attest(binding ProviderBinding) (ProviderAttestation, error) {
	if err := binding.Validate(); err != nil {
		return ProviderAttestation{}, err
	}
	evidence, err := Sense()
	if err != nil {
		return ProviderAttestation{}, err
	}
	attestation, err := Sign(evidence, p.privateKey, binding.Nonce, binding.SignedAuthorityHash, binding.AuthorityIssuerKeyID, binding.JournalSequence, binding.JournalHead, binding.Workload, binding.ContextKey)
	if err != nil {
		return ProviderAttestation{}, err
	}
	wrappedEvidence, err := wrapProviderEvidence(p.Descriptor(), stableSoftwareEvidence(evidence))
	if err != nil {
		return ProviderAttestation{}, err
	}
	return wrapProviderAttestation(p.Descriptor(), wrappedEvidence.EvidenceHash, attestation)
}

func (p *SoftwareProvider) Verify(att ProviderAttestation, anchor ProviderTrustAnchor, binding ProviderBinding) error {
	if err := binding.Validate(); err != nil {
		return err
	}
	if err := validateProviderAttestationEnvelope(att, p.Descriptor()); err != nil {
		return err
	}
	if err := p.ValidateAnchor(anchor); err != nil {
		return err
	}
	var rawAnchor TrustAnchor
	if err := json.Unmarshal(anchor.Payload, &rawAnchor); err != nil {
		return err
	}
	var rawAtt Attestation
	if err := json.Unmarshal(att.Payload, &rawAtt); err != nil {
		return err
	}
	current, err := Sense()
	if err != nil {
		return err
	}
	wrappedCurrent, err := wrapProviderEvidence(p.Descriptor(), stableSoftwareEvidence(current))
	if err != nil {
		return err
	}
	if wrappedCurrent.EvidenceHash != att.EvidenceHash {
		return errors.New("provider attestation evidence hash does not match current evidence")
	}
	return VerifyBound(rawAtt, rawAnchor, binding.Nonce, current, binding.SignedAuthorityHash, binding.AuthorityIssuerKeyID, binding.JournalSequence, binding.JournalHead, binding.Workload, binding.ContextKey)
}

func stableSoftwareEvidence(e Evidence) Evidence {
	e.ObservedAt = ""
	return e
}

func wrapProviderAnchor(descriptor ProviderDescriptor, payload any) (ProviderTrustAnchor, error) {
	descriptorHash, err := descriptor.Hash()
	if err != nil {
		return ProviderTrustAnchor{}, err
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return ProviderTrustAnchor{}, err
	}
	anchorHash, err := hashJSON(struct {
		Protocol       string             `json:"protocol"`
		Descriptor     ProviderDescriptor `json:"descriptor"`
		DescriptorHash string             `json:"descriptor_hash"`
		Payload        json.RawMessage    `json:"payload"`
	}{ProviderAnchorProtocol, descriptor, descriptorHash, raw})
	if err != nil {
		return ProviderTrustAnchor{}, err
	}
	return ProviderTrustAnchor{Protocol: ProviderAnchorProtocol, Descriptor: descriptor, DescriptorHash: descriptorHash, AnchorHash: anchorHash, Payload: raw}, nil
}

func wrapProviderEvidence(descriptor ProviderDescriptor, payload any) (ProviderEvidence, error) {
	descriptorHash, err := descriptor.Hash()
	if err != nil {
		return ProviderEvidence{}, err
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return ProviderEvidence{}, err
	}
	evidenceHash, err := hashJSON(struct {
		Protocol       string             `json:"protocol"`
		Descriptor     ProviderDescriptor `json:"descriptor"`
		DescriptorHash string             `json:"descriptor_hash"`
		Payload        json.RawMessage    `json:"payload"`
	}{ProviderEvidenceProtocol, descriptor, descriptorHash, raw})
	if err != nil {
		return ProviderEvidence{}, err
	}
	return ProviderEvidence{Protocol: ProviderEvidenceProtocol, Descriptor: descriptor, DescriptorHash: descriptorHash, EvidenceHash: evidenceHash, Payload: raw}, nil
}

func wrapProviderAttestation(descriptor ProviderDescriptor, evidenceHash string, payload any) (ProviderAttestation, error) {
	descriptorHash, err := descriptor.Hash()
	if err != nil {
		return ProviderAttestation{}, err
	}
	if !shaHex(evidenceHash) {
		return ProviderAttestation{}, errors.New("provider evidence hash must be sha256")
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return ProviderAttestation{}, err
	}
	attestationHash, err := hashJSON(struct {
		Protocol       string             `json:"protocol"`
		Descriptor     ProviderDescriptor `json:"descriptor"`
		DescriptorHash string             `json:"descriptor_hash"`
		EvidenceHash   string             `json:"evidence_hash"`
		Payload        json.RawMessage    `json:"payload"`
	}{ProviderAttestationProtocol, descriptor, descriptorHash, evidenceHash, raw})
	if err != nil {
		return ProviderAttestation{}, err
	}
	return ProviderAttestation{Protocol: ProviderAttestationProtocol, Descriptor: descriptor, DescriptorHash: descriptorHash, EvidenceHash: evidenceHash, AttestationHash: attestationHash, Payload: raw}, nil
}

func validateProviderAnchorEnvelope(anchor ProviderTrustAnchor, expected ProviderDescriptor) error {
	if anchor.Protocol != ProviderAnchorProtocol || anchor.Descriptor != expected {
		return errors.New("provider trust anchor descriptor mismatch")
	}
	descriptorHash, err := expected.Hash()
	if err != nil {
		return err
	}
	if anchor.DescriptorHash != descriptorHash || !shaHex(anchor.AnchorHash) || len(anchor.Payload) == 0 {
		return errors.New("provider trust anchor envelope is incomplete")
	}
	expectedHash, err := hashJSON(struct {
		Protocol       string             `json:"protocol"`
		Descriptor     ProviderDescriptor `json:"descriptor"`
		DescriptorHash string             `json:"descriptor_hash"`
		Payload        json.RawMessage    `json:"payload"`
	}{anchor.Protocol, anchor.Descriptor, anchor.DescriptorHash, anchor.Payload})
	if err != nil {
		return err
	}
	if expectedHash != anchor.AnchorHash {
		return errors.New("provider trust anchor hash mismatch")
	}
	return nil
}

func validateProviderAttestationEnvelope(att ProviderAttestation, expected ProviderDescriptor) error {
	if att.Protocol != ProviderAttestationProtocol || att.Descriptor != expected {
		return errors.New("provider attestation descriptor mismatch")
	}
	descriptorHash, err := expected.Hash()
	if err != nil {
		return err
	}
	if att.DescriptorHash != descriptorHash || !shaHex(att.EvidenceHash) || !shaHex(att.AttestationHash) || len(att.Payload) == 0 {
		return errors.New("provider attestation envelope is incomplete")
	}
	expectedHash, err := hashJSON(struct {
		Protocol       string             `json:"protocol"`
		Descriptor     ProviderDescriptor `json:"descriptor"`
		DescriptorHash string             `json:"descriptor_hash"`
		EvidenceHash   string             `json:"evidence_hash"`
		Payload        json.RawMessage    `json:"payload"`
	}{att.Protocol, att.Descriptor, att.DescriptorHash, att.EvidenceHash, att.Payload})
	if err != nil {
		return err
	}
	if expectedHash != att.AttestationHash {
		return errors.New("provider attestation envelope hash mismatch")
	}
	return nil
}
