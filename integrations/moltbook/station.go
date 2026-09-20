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

	if err := metro.Verify(packet, route, receiptResult, receipt); err != nil {
		return Result{}, fmt.Errorf("verify receipt: %w", err)
	}

	return Result{
		IdentityRef:   identityRef,
		PacketHash:    packetHash,
		Packet:        packet,
		Route:         route,
		Verification:  verification,
		ReceiptResult: receiptResult,
		Receipt:       receipt,
	}, nil
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
