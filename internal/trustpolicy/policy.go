package trustpolicy

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
)

const (
	PolicyProtocol   = "liminal.trust-policy.v1.0"
	EvidenceProtocol = "liminal.trust-evidence.v1.0"
	DecisionProtocol = "liminal.trust-decision.v1.0"
)

type Requirements struct {
	ExternalIdentity         bool `json:"external_identity"`
	PortablePublication     bool `json:"portable_publication"`
	HardwareBacked          bool `json:"hardware_backed"`
	RemoteHardwareAttestation bool `json:"remote_hardware_attestation"`
}

type Policy struct {
	Protocol     string       `json:"protocol"`
	PolicyID     string       `json:"policy_id"`
	Requirements Requirements `json:"requirements"`
	PolicyHash   string       `json:"policy_hash"`
}

type policyMaterial struct {
	Protocol     string       `json:"protocol"`
	PolicyID     string       `json:"policy_id"`
	Requirements Requirements `json:"requirements"`
}

func NewPolicy(policyID string, requirements Requirements) (Policy, error) {
	p := Policy{Protocol: PolicyProtocol, PolicyID: policyID, Requirements: requirements}
	if err := p.validateMaterial(); err != nil {
		return Policy{}, err
	}
	h, err := hashJSON(p.material())
	if err != nil {
		return Policy{}, err
	}
	p.PolicyHash = h
	return p, nil
}

func (p Policy) material() policyMaterial {
	return policyMaterial{Protocol: p.Protocol, PolicyID: p.PolicyID, Requirements: p.Requirements}
}

func (p Policy) validateMaterial() error {
	if p.Protocol != PolicyProtocol || p.PolicyID == "" {
		return errors.New("trust policy protocol/policy_id is invalid")
	}
	if p.Requirements.RemoteHardwareAttestation && !p.Requirements.HardwareBacked {
		return errors.New("remote hardware attestation requires hardware_backed")
	}
	return nil
}

func (p Policy) Validate() error {
	if err := p.validateMaterial(); err != nil {
		return err
	}
	if !shaHex(p.PolicyHash) {
		return errors.New("trust policy hash must be sha256 hex")
	}
	expected, err := hashJSON(p.material())
	if err != nil {
		return err
	}
	if expected != p.PolicyHash {
		return errors.New("trust policy hash mismatch")
	}
	return nil
}

type EvidenceInput struct {
	ExternalIdentityVerified  bool
	PortablePublicationVerified bool
	HardwareBacked            bool
	RemoteHardwareAttestation bool
	SourceProofHash            string
	DiscoveryHash              string
	PortableProofHash          string
}

type Evidence struct {
	Protocol                    string `json:"protocol"`
	ExternalIdentityVerified    bool   `json:"external_identity_verified"`
	PortablePublicationVerified bool   `json:"portable_publication_verified"`
	HardwareBacked              bool   `json:"hardware_backed"`
	RemoteHardwareAttestation   bool   `json:"remote_hardware_attestation"`
	SourceProofHash              string `json:"source_proof_hash"`
	DiscoveryHash                string `json:"discovery_hash,omitempty"`
	PortableProofHash            string `json:"portable_proof_hash,omitempty"`
	EvidenceHash                 string `json:"evidence_hash"`
}

type evidenceMaterial struct {
	Protocol                    string `json:"protocol"`
	ExternalIdentityVerified    bool   `json:"external_identity_verified"`
	PortablePublicationVerified bool   `json:"portable_publication_verified"`
	HardwareBacked              bool   `json:"hardware_backed"`
	RemoteHardwareAttestation   bool   `json:"remote_hardware_attestation"`
	SourceProofHash              string `json:"source_proof_hash"`
	DiscoveryHash                string `json:"discovery_hash,omitempty"`
	PortableProofHash            string `json:"portable_proof_hash,omitempty"`
}

func NewEvidence(in EvidenceInput) (Evidence, error) {
	e := Evidence{
		Protocol:                    EvidenceProtocol,
		ExternalIdentityVerified:    in.ExternalIdentityVerified,
		PortablePublicationVerified: in.PortablePublicationVerified,
		HardwareBacked:              in.HardwareBacked,
		RemoteHardwareAttestation:   in.RemoteHardwareAttestation,
		SourceProofHash:              in.SourceProofHash,
		DiscoveryHash:                in.DiscoveryHash,
		PortableProofHash:            in.PortableProofHash,
	}
	if err := e.validateMaterial(); err != nil {
		return Evidence{}, err
	}
	h, err := hashJSON(e.material())
	if err != nil {
		return Evidence{}, err
	}
	e.EvidenceHash = h
	return e, nil
}

func (e Evidence) material() evidenceMaterial {
	return evidenceMaterial{
		Protocol:                    e.Protocol,
		ExternalIdentityVerified:    e.ExternalIdentityVerified,
		PortablePublicationVerified: e.PortablePublicationVerified,
		HardwareBacked:              e.HardwareBacked,
		RemoteHardwareAttestation:   e.RemoteHardwareAttestation,
		SourceProofHash:              e.SourceProofHash,
		DiscoveryHash:                e.DiscoveryHash,
		PortableProofHash:            e.PortableProofHash,
	}
}

func (e Evidence) validateMaterial() error {
	if e.Protocol != EvidenceProtocol || !shaHex(e.SourceProofHash) {
		return errors.New("trust evidence protocol/source proof hash is invalid")
	}
	if e.RemoteHardwareAttestation && !e.HardwareBacked {
		return errors.New("remote hardware attestation evidence requires hardware_backed")
	}
	if e.ExternalIdentityVerified && !shaHex(e.DiscoveryHash) {
		return errors.New("external identity evidence requires discovery_hash")
	}
	if e.PortablePublicationVerified && !shaHex(e.PortableProofHash) {
		return errors.New("portable publication evidence requires portable_proof_hash")
	}
	return nil
}

func (e Evidence) Validate() error {
	if err := e.validateMaterial(); err != nil {
		return err
	}
	if !shaHex(e.EvidenceHash) {
		return errors.New("trust evidence hash must be sha256 hex")
	}
	expected, err := hashJSON(e.material())
	if err != nil {
		return err
	}
	if expected != e.EvidenceHash {
		return errors.New("trust evidence hash mismatch")
	}
	return nil
}

type Decision struct {
	Protocol     string   `json:"protocol"`
	PolicyID     string   `json:"policy_id"`
	PolicyHash   string   `json:"policy_hash"`
	EvidenceHash string   `json:"evidence_hash"`
	Allowed      bool     `json:"allowed"`
	Unmet        []string `json:"unmet,omitempty"`
	DecisionHash string   `json:"decision_hash"`
}

type decisionMaterial struct {
	Protocol     string   `json:"protocol"`
	PolicyID     string   `json:"policy_id"`
	PolicyHash   string   `json:"policy_hash"`
	EvidenceHash string   `json:"evidence_hash"`
	Allowed      bool     `json:"allowed"`
	Unmet        []string `json:"unmet,omitempty"`
}

func Evaluate(policy Policy, evidence Evidence) (Decision, error) {
	if err := policy.Validate(); err != nil {
		return Decision{}, fmt.Errorf("policy: %w", err)
	}
	if err := evidence.Validate(); err != nil {
		return Decision{}, fmt.Errorf("evidence: %w", err)
	}
	unmet := make([]string, 0, 4)
	if policy.Requirements.ExternalIdentity && !evidence.ExternalIdentityVerified {
		unmet = append(unmet, "external_identity")
	}
	if policy.Requirements.PortablePublication && !evidence.PortablePublicationVerified {
		unmet = append(unmet, "portable_publication")
	}
	if policy.Requirements.HardwareBacked && !evidence.HardwareBacked {
		unmet = append(unmet, "hardware_backed")
	}
	if policy.Requirements.RemoteHardwareAttestation && !evidence.RemoteHardwareAttestation {
		unmet = append(unmet, "remote_hardware_attestation")
	}
	d := Decision{
		Protocol:     DecisionProtocol,
		PolicyID:     policy.PolicyID,
		PolicyHash:   policy.PolicyHash,
		EvidenceHash: evidence.EvidenceHash,
		Allowed:      len(unmet) == 0,
		Unmet:        unmet,
	}
	h, err := hashJSON(d.material())
	if err != nil {
		return Decision{}, err
	}
	d.DecisionHash = h
	return d, nil
}

func (d Decision) material() decisionMaterial {
	return decisionMaterial{
		Protocol: d.Protocol, PolicyID: d.PolicyID, PolicyHash: d.PolicyHash,
		EvidenceHash: d.EvidenceHash, Allowed: d.Allowed, Unmet: d.Unmet,
	}
}

func (d Decision) Validate() error {
	if d.Protocol != DecisionProtocol || d.PolicyID == "" || !shaHex(d.PolicyHash) || !shaHex(d.EvidenceHash) || !shaHex(d.DecisionHash) {
		return errors.New("trust decision fields are incomplete")
	}
	if d.Allowed != (len(d.Unmet) == 0) {
		return errors.New("trust decision allowed/unmet invariant failed")
	}
	expected, err := hashJSON(d.material())
	if err != nil {
		return err
	}
	if expected != d.DecisionHash {
		return errors.New("trust decision hash mismatch")
	}
	return nil
}

func VerifyDecision(d Decision, policy Policy, evidence Evidence) error {
	if err := d.Validate(); err != nil {
		return err
	}
	expected, err := Evaluate(policy, evidence)
	if err != nil {
		return err
	}
	if d.DecisionHash != expected.DecisionHash {
		return errors.New("trust decision does not match policy/evidence")
	}
	return nil
}

func hashJSON(v any) (string, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:]), nil
}

func shaHex(v string) bool {
	if len(v) != 64 {
		return false
	}
	_, err := hex.DecodeString(v)
	return err == nil
}
