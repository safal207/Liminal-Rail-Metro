package lifetrabridge

import (
	"errors"
	"fmt"

	"github.com/safal207/Liminal-Rail-Metro/internal/metro"
)

const (
	ObservationProtocol = "lifetra.observation.v0.1"
	DecisionProtocol    = "lifetra.decision.v0.1"

	VerdictAllow           = "ALLOW"
	VerdictRequireApproval = "REQUIRE_APPROVAL"
	VerdictBlock           = "BLOCK"
)

type Observation struct {
	Protocol        string   `json:"protocol"`
	ObservationID   string   `json:"observation_id"`
	ActionID        string   `json:"action_id"`
	ReceiptID       string   `json:"receipt_id"`
	ReceiptStatus   string   `json:"receipt_status"`
	ReceiptHash     string   `json:"receipt_hash"`
	HashAlgorithm   string   `json:"hash_algorithm"`
	ProofRefs       []string `json:"proof_refs"`
	PreviousBeadRef string   `json:"previous_bead_ref,omitempty"`
	ObservedAt      string   `json:"observed_at"`
}

type NextAction struct {
	ActionID       string         `json:"action_id"`
	Goal           string         `json:"goal"`
	Kind           string         `json:"kind"`
	Inputs         map[string]any `json:"inputs"`
	AllowedTargets []string       `json:"allowed_targets"`
	TimeoutMS      int            `json:"timeout_ms,omitempty"`
	SideEffect     bool           `json:"side_effect"`
}

type Decision struct {
	Protocol            string      `json:"protocol"`
	DecisionID          string      `json:"decision_id"`
	SourceObservationID string      `json:"source_observation_id"`
	SourceReceiptRef    string      `json:"source_receipt_ref"`
	CausedByActionID    string      `json:"caused_by_action_id"`
	SourceBeadRef       string      `json:"source_bead_ref,omitempty"`
	Verdict             string      `json:"verdict"`
	AuthorityProofRef   string      `json:"authority_proof_ref,omitempty"`
	NextAction          *NextAction `json:"next_action,omitempty"`
	DecidedAt           string      `json:"decided_at"`
}

func ReceiptToObservation(receipt metro.Receipt, previousBeadRef string) (Observation, error) {
	if receipt.Protocol != metro.ReceiptProtocol {
		return Observation{}, fmt.Errorf("unsupported receipt protocol %q", receipt.Protocol)
	}
	if receipt.ReceiptID == "" || receipt.ActionID == "" {
		return Observation{}, errors.New("receipt_id and action_id are required")
	}
	if receipt.CompletedAt == "" {
		return Observation{}, errors.New("completed_at is required to create an observation")
	}
	if !validReceiptStatus(receipt.Status) {
		return Observation{}, fmt.Errorf("unsupported receipt status %q", receipt.Status)
	}

	receiptHash, err := metro.HashJSON(receipt)
	if err != nil {
		return Observation{}, fmt.Errorf("hash receipt: %w", err)
	}

	return Observation{
		Protocol:        ObservationProtocol,
		ObservationID:   "observation-" + receipt.ActionID,
		ActionID:        receipt.ActionID,
		ReceiptID:       receipt.ReceiptID,
		ReceiptStatus:   receipt.Status,
		ReceiptHash:     receiptHash,
		HashAlgorithm:   "sha256",
		ProofRefs:       []string{"metro-receipt://" + receipt.ReceiptID},
		PreviousBeadRef: previousBeadRef,
		ObservedAt:      receipt.CompletedAt,
	}, nil
}

func DecisionToPacket(decision Decision) (metro.Packet, error) {
	if decision.Protocol != DecisionProtocol {
		return metro.Packet{}, fmt.Errorf("unsupported decision protocol %q", decision.Protocol)
	}
	if decision.DecisionID == "" || decision.SourceObservationID == "" || decision.SourceReceiptRef == "" || decision.CausedByActionID == "" {
		return metro.Packet{}, errors.New("decision_id, source_observation_id, source_receipt_ref, and caused_by_action_id are required")
	}
	if decision.Verdict != VerdictAllow {
		return metro.Packet{}, fmt.Errorf("decision verdict %q is not dispatch authority", decision.Verdict)
	}
	if decision.AuthorityProofRef == "" {
		return metro.Packet{}, errors.New("authority_proof_ref is required for ALLOW")
	}
	if decision.NextAction == nil {
		return metro.Packet{}, errors.New("next_action is required for ALLOW")
	}

	next := decision.NextAction
	if next.ActionID == "" || next.Goal == "" || next.Kind == "" {
		return metro.Packet{}, errors.New("next_action action_id, goal, and kind are required")
	}
	if len(next.AllowedTargets) == 0 {
		return metro.Packet{}, errors.New("next_action requires at least one allowed target")
	}
	if !uniqueNonEmpty(next.AllowedTargets) {
		return metro.Packet{}, errors.New("allowed targets must be non-empty and unique")
	}
	if next.TimeoutMS < 0 {
		return metro.Packet{}, errors.New("timeout_ms cannot be negative")
	}
	if next.ActionID == decision.CausedByActionID {
		return metro.Packet{}, errors.New("new action must not silently reuse the prior action_id")
	}

	inputs := next.Inputs
	if inputs == nil {
		inputs = map[string]any{}
	}

	packet := metro.NewPacket(
		next.ActionID,
		"lifetra-control",
		next.Goal,
		metro.Action{Kind: next.Kind, Inputs: inputs},
		next.AllowedTargets,
	)
	packet.Constraints = metro.Constraints{
		TimeoutMS:  next.TimeoutMS,
		SideEffect: next.SideEffect,
	}
	packet.PreviousReceiptRef = decision.SourceReceiptRef
	packet.ContextRefs = append(packet.ContextRefs,
		"lifetra-observation://"+decision.SourceObservationID,
		decision.AuthorityProofRef,
	)
	if decision.SourceBeadRef != "" {
		packet.ContextRefs = append(packet.ContextRefs, decision.SourceBeadRef)
	}

	return packet, nil
}

func validReceiptStatus(status string) bool {
	switch status {
	case "SUCCEEDED", "FAILED", "UNKNOWN", "REJECTED":
		return true
	default:
		return false
	}
}

func uniqueNonEmpty(values []string) bool {
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		if value == "" {
			return false
		}
		if _, ok := seen[value]; ok {
			return false
		}
		seen[value] = struct{}{}
	}
	return true
}
