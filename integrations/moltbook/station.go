package moltbook

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/safal207/Liminal-Rail-Metro/internal/decisionplane"
	"github.com/safal207/Liminal-Rail-Metro/internal/metro"
)

const (
	IntentVerifyEvidence = "VERIFY_EVIDENCE"
	TargetAgentProof     = "agentproof"

	DispositionAllow         = "ALLOW"
	DispositionAutoRoute     = decisionplane.DispositionAutoRoute
	ExecutionStatusSucceeded = "SUCCEEDED"
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

type AuthorityDecision struct {
	Disposition string       `json:"disposition"`
	ActionID    string       `json:"action_id"`
	PacketHash  string       `json:"packet_hash"`
	Target      string       `json:"target"`
	ProviderID  string       `json:"provider_id"`
	ReasonCode  string       `json:"reason_code,omitempty"`
	Route       *metro.Route `json:"route,omitempty"`
}

type AuthorityGate interface {
	Authorize(context.Context, metro.Packet, string) (AuthorityDecision, error)
}

type DecisionPlaneAuthority struct {
	Provider decisionplane.Provider
	Policy   decisionplane.GatePolicy
}

func NewDecisionPlaneAuthority(provider decisionplane.Provider) (DecisionPlaneAuthority, error) {
	if provider == nil {
		return DecisionPlaneAuthority{}, errors.New("decision provider is required")
	}
	return DecisionPlaneAuthority{
		Provider: provider,
		Policy:   decisionplane.DefaultGatePolicy(),
	}, nil
}

func (a DecisionPlaneAuthority) Authorize(ctx context.Context, packet metro.Packet, target string) (AuthorityDecision, error) {
	if a.Provider == nil {
		return AuthorityDecision{}, errors.New("decision provider is required")
	}
	if target == "" {
		return AuthorityDecision{}, errors.New("authority target is required")
	}

	request, err := decisionplane.NewRequest(
		packet,
		"moltbook-authority-"+packet.ActionID,
		map[string]any{
			"intent": packet.Action.Kind,
			"target": target,
		},
		[]decisionplane.Choice{{
			ID:     "moltbook-target",
			Target: target,
			Label:  "MOLT-001 allowlisted verifier",
		}},
	)
	if err != nil {
		return AuthorityDecision{}, fmt.Errorf("build authority request: %w", err)
	}

	decision, err := a.Provider.Decide(ctx, request)
	if err != nil {
		return AuthorityDecision{}, fmt.Errorf("authority provider decision: %w", err)
	}

	policy := a.Policy
	if policy.AutoRouteConfidence == 0 && policy.MinTopMargin == 0 && policy.PolicyRef == "" && !policy.RequireApprovalForSideEffects {
		policy = decisionplane.DefaultGatePolicy()
	}
	gate, err := decisionplane.ApplyDecision(packet, request, decision, policy)
	if err != nil {
		return AuthorityDecision{}, fmt.Errorf("apply authority decision: %w", err)
	}

	return AuthorityDecision{
		Disposition: gate.Disposition,
		ActionID:    packet.ActionID,
		PacketHash:  request.PacketHash,
		Target:      target,
		ProviderID:  decision.ProviderID,
		ReasonCode:  gate.ReasonCode,
		Route:       gate.Route,
	}, nil
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
	IdentityRef   string            `json:"identity_ref"`
	PacketHash    string            `json:"packet_hash"`
	Packet        metro.Packet      `json:"packet"`
	Authority     AuthorityDecision `json:"authority"`
	AuthorityHash string            `json:"authority_hash"`
	Route         metro.Route       `json:"route"`
	Verification  map[string]any    `json:"verification"`
	ReceiptResult map[string]any    `json:"receipt_result"`
	Receipt       metro.Receipt     `json:"receipt"`
}

type Station struct {
	identity  IdentityVerifier
	evidence  EvidenceVerifier
	authority AuthorityGate

	mu       sync.Mutex
	consumed map[string]struct{}
}

func NewStation(identity IdentityVerifier, evidence EvidenceVerifier, authority AuthorityGate) (*Station, error) {
	if identity == nil {
		return nil, errors.New("identity verifier is required")
	}
	if evidence == nil {
		return nil, errors.New("evidence verifier is required")
	}
	if authority == nil {
		return nil, errors.New("authority gate is required")
	}

	return &Station{
		identity:  identity,
		evidence:  evidence,
		authority: authority,
		consumed:  make(map[string]struct{}),
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

	authority, err := s.authority.Authorize(context.Background(), packet, req.Target)
	if err != nil {
		return Result{}, fmt.Errorf("authority decision failed: %w", err)
	}
	route, err := validateAuthorityDecision(packet, req.Target, authority)
	if err != nil {
		return Result{}, err
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
	authorityHash, err := metro.HashJSON(authority)
	if err != nil {
		return Result{}, fmt.Errorf("hash authority decision: %w", err)
	}
	verificationHash, err := metro.HashJSON(verification)
	if err != nil {
		return Result{}, fmt.Errorf("hash verification result: %w", err)
	}

	receiptResult := map[string]any{
		"identity_ref":      identityRef,
		"packet_hash":       packetHash,
		"authority_hash":    authorityHash,
		"authority_result":  authority.Disposition,
		"target":            route.SelectedTarget,
		"verdict":           verdict,
		"evidence_sha256":   evidenceHash,
		"verification_hash": verificationHash,
		"execution_status":  ExecutionStatusSucceeded,
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
	if receipt.Status != ExecutionStatusSucceeded {
		return Result{}, fmt.Errorf("unexpected receipt execution status %q", receipt.Status)
	}

	result := Result{
		IdentityRef:   identityRef,
		PacketHash:    packetHash,
		Packet:        packet,
		Authority:     authority,
		AuthorityHash: authorityHash,
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

func validateAuthorityDecision(packet metro.Packet, target string, authority AuthorityDecision) (metro.Route, error) {
	packetHash, err := metro.HashJSON(packet)
	if err != nil {
		return metro.Route{}, fmt.Errorf("hash authority packet: %w", err)
	}
	if authority.PacketHash != packetHash {
		return metro.Route{}, errors.New("authority packet_hash mismatch")
	}
	if authority.ActionID != packet.ActionID {
		return metro.Route{}, errors.New("authority action_id mismatch")
	}
	if authority.Target != target {
		return metro.Route{}, errors.New("authority target mismatch")
	}
	if authority.Disposition != DispositionAllow && authority.Disposition != DispositionAutoRoute {
		return metro.Route{}, fmt.Errorf("authority disposition %q does not permit dispatch", authority.Disposition)
	}
	if authority.ProviderID == "" {
		return metro.Route{}, errors.New("authority provider_id is required")
	}
	if authority.Route == nil {
		return metro.Route{}, errors.New("permitting authority decision requires a bound route")
	}
	if authority.Route.ActionID != packet.ActionID {
		return metro.Route{}, errors.New("authority route action_id mismatch")
	}
	if authority.Route.SelectedTarget != target {
		return metro.Route{}, errors.New("authority route target mismatch")
	}
	if !metro.Contains(packet.AllowedTargets, authority.Route.SelectedTarget) {
		return metro.Route{}, errors.New("authority route selected disallowed target")
	}
	return *authority.Route, nil
}

// VerifyResult verifies Moltbook-specific identity, authority, packet, and
// verifier-result bindings before delegating generic receipt checks to Metro.
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

	authorityRoute, err := validateAuthorityDecision(result.Packet, result.Route.SelectedTarget, result.Authority)
	if err != nil {
		return err
	}
	authorityRouteHash, err := metro.HashJSON(authorityRoute)
	if err != nil {
		return fmt.Errorf("hash authority route: %w", err)
	}
	resultRouteHash, err := metro.HashJSON(result.Route)
	if err != nil {
		return fmt.Errorf("hash result route: %w", err)
	}
	if authorityRouteHash != resultRouteHash {
		return errors.New("result route is not bound to authority route")
	}

	authorityHash, err := metro.HashJSON(result.Authority)
	if err != nil {
		return fmt.Errorf("hash authority decision: %w", err)
	}
	if result.AuthorityHash != authorityHash {
		return errors.New("authority hash mismatch")
	}
	receiptAuthorityHash, ok := nonEmptyString(result.ReceiptResult["authority_hash"])
	if !ok || receiptAuthorityHash != authorityHash {
		return errors.New("receipt authority hash mismatch")
	}
	receiptAuthorityResult, ok := nonEmptyString(result.ReceiptResult["authority_result"])
	if !ok || receiptAuthorityResult != result.Authority.Disposition {
		return errors.New("receipt authority disposition mismatch")
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

	executionStatus, ok := nonEmptyString(result.ReceiptResult["execution_status"])
	if !ok {
		return errors.New("receipt execution_status is missing")
	}
	if executionStatus != result.Receipt.Status {
		return errors.New("receipt execution status mismatch")
	}
	if executionStatus != ExecutionStatusSucceeded {
		return fmt.Errorf("receipt execution status %q is not successful", executionStatus)
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
