package moltbook

import (
	"errors"
	"fmt"
	"sync"

	"github.com/safal207/Liminal-Rail-Metro/internal/metro"
)

const (
	IntentVerifyEvidence = "VERIFY_EVIDENCE"
	TargetAgentProof     = "agentproof"
)

type VerifiedIdentity struct {
	AgentID  string `json:"agent_id"`
	Verified bool   `json:"verified"`
}

type IdentityVerifier interface {
	VerifyIdentity(token string) (VerifiedIdentity, error)
}

type EvidenceVerifier interface {
	VerifyEvidence(evidenceRef string) (map[string]any, error)
}

type Request struct {
	IdentityToken   string `json:"identity_token"`
	ActionID        string `json:"action_id"`
	Intent          string `json:"intent"`
	Target          string `json:"target"`
	EvidenceRef     string `json:"evidence_ref"`
	ExternalEffects bool   `json:"external_effects"`
}

type Result struct {
	IdentityRef   string         `json:"identity_ref"`
	PacketHash    string         `json:"packet_hash"`
	Packet        metro.Packet   `json:"packet"`
	Route         metro.Route    `json:"route"`
	Verification  map[string]any `json:"verification"`
	ReceiptResult map[string]any `json:"receipt_result"`
	Receipt       metro.Receipt  `json:"receipt"`
}

type Station struct {
	identity IdentityVerifier
	evidence EvidenceVerifier
	router   metro.Router

	mu       sync.Mutex
	consumed map[string]struct{}
}

func NewStation(identity IdentityVerifier, evidence EvidenceVerifier) (*Station, error) {
	if identity == nil {
		return nil, errors.New("identity verifier is required")
	}
	if evidence == nil {
		return nil, errors.New("evidence verifier is required")
	}

	return &Station{
		identity: identity,
		evidence: evidence,
		router: metro.Router{
			ID: "moltbook-station-v0.1",
			Policy: map[string]string{
				"moltbook.verify_evidence": TargetAgentProof,
			},
		},
		consumed: make(map[string]struct{}),
	}, nil
}

func (s *Station) Execute(req Request) (Result, error) {
	if req.ActionID == "" {
		return Result{}, errors.New("action_id is required")
	}
	if req.IdentityToken == "" {
		return Result{}, errors.New("identity token is required")
	}
	if req.Intent != IntentVerifyEvidence {
		return Result{}, fmt.Errorf("unsupported intent %q", req.Intent)
	}
	if req.Target != TargetAgentProof {
		return Result{}, fmt.Errorf("target %q is not allowlisted", req.Target)
	}
	if req.ExternalEffects {
		return Result{}, errors.New("external effects are forbidden in Moltbook Station v0.1")
	}
	if req.EvidenceRef == "" {
		return Result{}, errors.New("evidence_ref is required")
	}

	identity, err := s.identity.VerifyIdentity(req.IdentityToken)
	if err != nil {
		return Result{}, fmt.Errorf("identity verification failed: %w", err)
	}
	if !identity.Verified || identity.AgentID == "" {
		return Result{}, errors.New("identity is not verified")
	}

	identityRef, err := metro.HashJSON(identity)
	if err != nil {
		return Result{}, fmt.Errorf("hash identity: %w", err)
	}

	packet := metro.NewPacket(
		req.ActionID,
		"moltbook:"+identityRef,
		"Verify supplied evidence without external effects",
		metro.Action{
			Kind: "moltbook.verify_evidence",
			Inputs: map[string]any{
				"identity_ref": identityRef,
				"evidence_ref": req.EvidenceRef,
				"intent":       req.Intent,
			},
		},
		[]string{TargetAgentProof},
	)
	packet.Constraints.SideEffect = false

	route, err := s.router.Route(packet)
	if err != nil {
		return Result{}, fmt.Errorf("route request: %w", err)
	}

	if err := s.consume(req.ActionID); err != nil {
		return Result{}, err
	}

	verification, err := s.evidence.VerifyEvidence(req.EvidenceRef)
	if err != nil {
		return Result{}, fmt.Errorf("verification execution failed: %w", err)
	}
	if verification == nil {
		return Result{}, errors.New("verification result is missing")
	}

	verdict, ok := nonEmptyString(verification["verdict"])
	if !ok {
		return Result{}, errors.New("verification verdict is missing")
	}
	evidenceHash, ok := nonEmptyString(verification["evidence_sha256"])
	if !ok {
		return Result{}, errors.New("verification evidence_sha256 is missing")
	}

	packetHash, err := metro.HashJSON(packet)
	if err != nil {
		return Result{}, fmt.Errorf("hash packet: %w", err)
	}
	verificationHash, err := metro.HashJSON(verification)
	if err != nil {
		return Result{}, fmt.Errorf("hash verification result: %w", err)
	}

	receiptResult := map[string]any{
		"identity_ref":      identityRef,
		"packet_hash":       packetHash,
		"target":            route.SelectedTarget,
		"verdict":           verdict,
		"evidence_sha256":   evidenceHash,
		"verification_hash": verificationHash,
	}

	receipt, err := metro.MakeSuccessReceipt(
		packet,
		route,
		receiptResult,
		"moltbook://verification/"+req.ActionID,
	)
	if err != nil {
		return Result{}, fmt.Errorf("make receipt: %w", err)
	}

	result := Result{
		IdentityRef:   identityRef,
		PacketHash:    packetHash,
		Packet:        packet,
		Route:         route,
		Verification:  verification,
		ReceiptResult: receiptResult,
		Receipt:       receipt,
	}
	if err := VerifyResult(result); err != nil {
		return Result{}, fmt.Errorf("verify station result: %w", err)
	}

	return result, nil
}

// VerifyResult verifies the Moltbook-specific bindings before delegating the
// underlying route/input/result receipt checks to Metro.
func VerifyResult(result Result) error {
	packetHash, err := metro.HashJSON(result.Packet)
	if err != nil {
		return fmt.Errorf("hash packet: %w", err)
	}
	if result.PacketHash != packetHash {
		return errors.New("packet hash mismatch")
	}

	receiptPacketHash, ok := nonEmptyString(result.ReceiptResult["packet_hash"])
	if !ok || receiptPacketHash != packetHash {
		return errors.New("receipt packet hash mismatch")
	}

	packetIdentityRef, ok := nonEmptyString(result.Packet.Action.Inputs["identity_ref"])
	if !ok {
		return errors.New("packet identity_ref is missing")
	}
	if result.IdentityRef != packetIdentityRef {
		return errors.New("result identity_ref mismatch")
	}
	receiptIdentityRef, ok := nonEmptyString(result.ReceiptResult["identity_ref"])
	if !ok || receiptIdentityRef != packetIdentityRef {
		return errors.New("receipt identity_ref mismatch")
	}

	receiptTarget, ok := nonEmptyString(result.ReceiptResult["target"])
	if !ok || receiptTarget != result.Route.SelectedTarget {
		return errors.New("receipt target mismatch")
	}

	verdict, ok := nonEmptyString(result.Verification["verdict"])
	if !ok {
		return errors.New("verification verdict is missing")
	}
	receiptVerdict, ok := nonEmptyString(result.ReceiptResult["verdict"])
	if !ok || receiptVerdict != verdict {
		return errors.New("receipt verdict mismatch")
	}

	evidenceHash, ok := nonEmptyString(result.Verification["evidence_sha256"])
	if !ok {
		return errors.New("verification evidence_sha256 is missing")
	}
	receiptEvidenceHash, ok := nonEmptyString(result.ReceiptResult["evidence_sha256"])
	if !ok || receiptEvidenceHash != evidenceHash {
		return errors.New("receipt evidence hash mismatch")
	}

	verificationHash, err := metro.HashJSON(result.Verification)
	if err != nil {
		return fmt.Errorf("hash verification result: %w", err)
	}
	receiptVerificationHash, ok := nonEmptyString(result.ReceiptResult["verification_hash"])
	if !ok || receiptVerificationHash != verificationHash {
		return errors.New("receipt verification hash mismatch")
	}

	return metro.Verify(result.Packet, result.Route, result.ReceiptResult, result.Receipt)
}

func (s *Station) consume(actionID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, exists := s.consumed[actionID]; exists {
		return fmt.Errorf("action_id %q has already been consumed; no automatic replay", actionID)
	}
	s.consumed[actionID] = struct{}{}
	return nil
}

func nonEmptyString(value any) (string, bool) {
	s, ok := value.(string)
	return s, ok && s != ""
}
