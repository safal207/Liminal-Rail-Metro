package mirror

import (
	"errors"
	"fmt"

	"github.com/safal207/Liminal-Rail-Metro/internal/metro"
)

const (
	ProofEnvelopeProtocol      = "mirror.proof-envelope.v0.1"
	VerificationStatusVerified = "VERIFIED"
)

// ReceiptProof carries the receipt fields needed by downstream proof-envelope
// verifiers without requiring them to parse generic provenance strings.
type ReceiptProof struct {
	Protocol   string `json:"protocol"`
	ReceiptID  string `json:"receipt_id"`
	ActionID   string `json:"action_id"`
	RouteID    string `json:"route_id"`
	ExecutorID string `json:"executor_id"`
	Status     string `json:"status"`
	InputHash  string `json:"input_hash"`
	ResultHash string `json:"result_hash"`
	ResultRef  string `json:"result_ref"`
}

// ProofEnvelope is the first-class proof artifact carried across the mirror
// boundary. It records claim, receipt, executor authority, exact policy
// identity, and verification status as structured fields.
//
// Validate checks structural bindings inside the envelope. It does not replace
// replaying metro.Verify against the original packet, route, and concrete
// result when those source artifacts are available.
type ProofEnvelope struct {
	Protocol           string         `json:"protocol"`
	Claim              Claim          `json:"claim"`
	ActionKind         string         `json:"action_kind"`
	Receipt            ReceiptProof   `json:"receipt"`
	Authority          AuthorityProof `json:"authority"`
	VerificationStatus string         `json:"verification_status"`
}

// BuildProofEnvelope performs the full receipt promotion path first and emits
// a structured envelope only after Metro verification and authority checks
// succeed.
func BuildProofEnvelope(
	claim Claim,
	packet metro.Packet,
	route metro.Route,
	result map[string]any,
	receipt metro.Receipt,
	authority AuthorityPolicy,
) (ProofEnvelope, error) {
	if _, err := EvidenceFromVerifiedReceipt(claim, packet, route, result, receipt, authority); err != nil {
		return ProofEnvelope{}, fmt.Errorf("build proof envelope: %w", err)
	}

	authorityProof, err := authority.AuthorizeProof(packet.Action.Kind, receipt.ExecutorID)
	if err != nil {
		return ProofEnvelope{}, fmt.Errorf("build authority proof: %w", err)
	}

	envelope := ProofEnvelope{
		Protocol:   ProofEnvelopeProtocol,
		Claim:      claim,
		ActionKind: packet.Action.Kind,
		Receipt: ReceiptProof{
			Protocol:   receipt.Protocol,
			ReceiptID:  receipt.ReceiptID,
			ActionID:   receipt.ActionID,
			RouteID:    receipt.RouteID,
			ExecutorID: receipt.ExecutorID,
			Status:     receipt.Status,
			InputHash:  receipt.InputHash,
			ResultHash: receipt.ResultHash,
			ResultRef:  receipt.ResultRef,
		},
		Authority:          authorityProof,
		VerificationStatus: VerificationStatusVerified,
	}

	if err := envelope.Validate(); err != nil {
		return ProofEnvelope{}, fmt.Errorf("validate built proof envelope: %w", err)
	}
	return envelope, nil
}

// Validate fails closed when any structured binding in the proof envelope is
// incomplete or inconsistent.
func (p ProofEnvelope) Validate() error {
	if p.Protocol != ProofEnvelopeProtocol {
		return fmt.Errorf("unexpected proof envelope protocol %q", p.Protocol)
	}
	if p.VerificationStatus != VerificationStatusVerified {
		return fmt.Errorf("proof envelope verification status %q is not accepted", p.VerificationStatus)
	}
	if p.Claim.ID == "" || p.Claim.ActionID == "" || p.Claim.ValueHash == "" {
		return errors.New("proof envelope claim is incomplete")
	}
	if p.ActionKind == "" {
		return errors.New("proof envelope action_kind is required")
	}
	if p.Receipt.Protocol != metro.ReceiptProtocol {
		return fmt.Errorf("unexpected receipt protocol %q", p.Receipt.Protocol)
	}
	if p.Receipt.Status != "SUCCEEDED" {
		return fmt.Errorf("receipt status %q is not proof of success", p.Receipt.Status)
	}
	if p.Receipt.ReceiptID == "" || p.Receipt.RouteID == "" || p.Receipt.ExecutorID == "" || p.Receipt.ResultRef == "" {
		return errors.New("proof envelope receipt provenance is incomplete")
	}
	if !isSHA256Hex(p.Receipt.InputHash) || !isSHA256Hex(p.Receipt.ResultHash) {
		return errors.New("proof envelope receipt hashes must be sha256 hex")
	}
	if p.Receipt.ActionID != p.Claim.ActionID {
		return errors.New("proof envelope receipt action_id does not match claim")
	}
	if p.Receipt.ResultHash != p.Claim.ValueHash {
		return errors.New("proof envelope receipt result_hash does not match claim value_hash")
	}
	if p.Authority.PolicyID == "" || p.Authority.PolicyRef == "" || !isSHA256Hex(p.Authority.PolicyHash) {
		return errors.New("proof envelope authority policy identity is incomplete")
	}
	if p.Authority.ActionKind != p.ActionKind {
		return errors.New("proof envelope authority action_kind mismatch")
	}
	if p.Authority.ExecutorID != p.Receipt.ExecutorID {
		return errors.New("proof envelope authority executor mismatch")
	}
	return nil
}

// Evidence converts a validated structured envelope into the narrow evidence
// shape consumed by Mirror Gate. No provenance parsing is required.
func (p ProofEnvelope) Evidence() (Evidence, error) {
	if err := p.Validate(); err != nil {
		return Evidence{}, err
	}
	return Evidence{
		ClaimID:   p.Claim.ID,
		ActionID:  p.Claim.ActionID,
		Source:    SourceExternal,
		ValueHash: p.Claim.ValueHash,
		Verified:  true,
		Provenance: []string{
			"receipt://" + p.Receipt.ReceiptID,
			"route://" + p.Receipt.RouteID,
			"executor://" + p.Receipt.ExecutorID,
			p.Authority.DecisionRef(),
			p.Authority.PolicyRef,
			"policy-sha256://" + p.Authority.PolicyHash,
			p.Receipt.ResultRef,
		},
	}, nil
}

func isSHA256Hex(value string) bool {
	if len(value) != 64 {
		return false
	}
	for _, r := range value {
		if (r < '0' || r > '9') && (r < 'a' || r > 'f') {
			return false
		}
	}
	return true
}
