// Package codexadapter exposes a bounded, local Codex tool boundary.
// It never executes shell commands, fetches URLs, or grants external authority.
package codexadapter

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/safal207/Liminal-Rail-Metro/internal/decisionplane"
	"github.com/safal207/Liminal-Rail-Metro/internal/metro"
)

const (
	ActionProtocol   = "metro.codex.action.v0.1"
	ResponseProtocol = "metro.codex.response.v0.1"
	ProviderID       = "codex-local-deterministic-v0.1"
	MaxInputBytes    = 64 << 10
	MaxTextBytes     = 32 << 10
)

var identifier = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$`)

// Action is the adapter contract, not an OpenAI API. Target and policy are
// deliberately absent: the caller cannot supply executor authority.
type Action struct {
	Protocol    string            `json:"protocol"`
	RequestID   string            `json:"request_id"`
	ActionID    string            `json:"action_id"`
	Kind        string            `json:"kind"`
	Text        *string           `json:"text,omitempty"`
	Description *string           `json:"description,omitempty"`
	State       map[string]string `json:"state,omitempty"`
	SideEffect  *bool             `json:"side_effect"`
}

type Evidence struct {
	Packet     metro.Packet             `json:"packet"`
	Request    decisionplane.Request    `json:"decision_request"`
	Decision   decisionplane.Decision   `json:"decision"`
	Gate       decisionplane.GateResult `json:"gate"`
	Dispatched bool                     `json:"dispatched"`
	Result     map[string]any           `json:"result,omitempty"`
	Receipt    *metro.Receipt           `json:"receipt,omitempty"`
}

// EvidenceHash is an integrity digest, not a signature or external attestation.
type Response struct {
	Protocol     string   `json:"protocol"`
	Evidence     Evidence `json:"evidence"`
	EvidenceHash string   `json:"evidence_hash"`
}

func (a Action) validate() error {
	if a.Protocol != ActionProtocol || !identifier.MatchString(a.ActionID) || !identifier.MatchString(a.RequestID) {
		return errors.New("valid protocol, action_id and request_id are required")
	}
	if a.SideEffect == nil {
		return errors.New("side_effect must be explicitly true or false")
	}
	if (a.Text != nil && !utf8.ValidString(*a.Text)) || (a.Description != nil && !utf8.ValidString(*a.Description)) {
		return errors.New("text and description must contain valid UTF-8")
	}
	if len(a.State) > 64 {
		return errors.New("state exceeds 64 entries")
	}
	for key, value := range a.State {
		if !utf8.ValidString(key) || !utf8.ValidString(value) {
			return errors.New("state must contain valid UTF-8")
		}
		if len(key) > 128 || len(value) > 1024 {
			return errors.New("state key/value exceeds byte limit")
		}
	}
	switch a.Kind {
	case "hash_text":
		if a.Text == nil || len(*a.Text) > MaxTextBytes || a.Description != nil {
			return errors.New("hash_text requires text (at most 32768 bytes) and no description")
		}
	case "external_action":
		if a.Description == nil || strings.TrimSpace(*a.Description) == "" || len(*a.Description) > 4096 || a.Text != nil {
			return errors.New("external_action requires description (at most 4096 bytes) and no text")
		}
		if !*a.SideEffect {
			return errors.New("external_action cannot be declared side_effect=false")
		}
	default:
		return errors.New("unsupported action kind")
	}
	return nil
}

func prepare(a Action) (metro.Packet, decisionplane.Request, error) {
	if err := a.validate(); err != nil {
		return metro.Packet{}, decisionplane.Request{}, err
	}
	target := "codex-local-sha256"
	inputs := map[string]any{}
	if a.Kind == "hash_text" {
		inputs["text"] = *a.Text
	} else {
		target = "codex-approval-only"
		inputs["description"] = *a.Description
	}
	packet := metro.NewPacket(a.ActionID, "codex", "Bounded Codex action: "+a.Kind,
		metro.Action{Kind: "codex." + a.Kind, Inputs: inputs}, []string{target})
	packet.Constraints = metro.Constraints{TimeoutMS: 1000, SideEffect: *a.SideEffect}
	var state map[string]any
	if len(a.State) > 0 {
		state = make(map[string]any, len(a.State))
	}
	for key, value := range a.State {
		state[key] = value
	}
	request, err := decisionplane.NewRequest(packet, a.RequestID, state,
		[]decisionplane.Choice{{ID: a.Kind, Target: target}})
	return packet, request, err
}

// Run uses a deterministic single-choice provider. Confidence 1 expresses this
// local mapping only; it is not a model confidence or a Jev compatibility claim.
func Run(ctx context.Context, a Action) (Response, error) {
	return run(ctx, a, decisionplane.StaticProvider{ID: ProviderID, Scores: map[string]float64{a.Kind: 1}})
}

func run(ctx context.Context, a Action, provider decisionplane.Provider) (Response, error) {
	packet, request, err := prepare(a)
	if err != nil {
		return Response{}, err
	}
	if err := ctx.Err(); err != nil {
		return Response{}, err
	}
	// Providers get their own copy; mutation cannot alter the trusted request.
	data, err := json.Marshal(request)
	if err != nil {
		return Response{}, err
	}
	var providerRequest decisionplane.Request
	if err := json.Unmarshal(data, &providerRequest); err != nil {
		return Response{}, err
	}
	decision, err := provider.Decide(ctx, providerRequest)
	if err != nil {
		return Response{}, errors.New("decision provider failed; no dispatch")
	}
	if decision.ProviderID != ProviderID {
		return Response{}, errors.New("provider identity mismatch")
	}
	gate, err := decisionplane.ApplyDecision(packet, request, decision, decisionplane.DefaultGatePolicy())
	if err != nil {
		return Response{}, fmt.Errorf("decision rejected: %w", err)
	}
	if err := ctx.Err(); err != nil {
		return Response{}, err
	}
	evidence := Evidence{Packet: packet, Request: request, Decision: decision, Gate: gate}
	if gate.Disposition == decisionplane.DispositionAutoRoute {
		// The only executor is this pure local function. A side-effecting action
		// has no executor, even if a future gate change accidentally permits it.
		if a.Kind != "hash_text" || packet.Constraints.SideEffect || gate.Route == nil || gate.Route.SelectedTarget != "codex-local-sha256" {
			return Response{}, errors.New("executor boundary rejected action")
		}
		result := hashText(*a.Text)
		receipt, err := metro.MakeSuccessReceipt(packet, *gate.Route, result, "")
		if err != nil {
			return Response{}, err
		}
		if err := metro.Verify(packet, *gate.Route, result, receipt); err != nil {
			return Response{}, err
		}
		evidence.Dispatched, evidence.Result, evidence.Receipt = true, result, &receipt
	}
	hash, err := metro.HashJSON(evidence)
	if err != nil {
		return Response{}, err
	}
	response := Response{Protocol: ResponseProtocol, Evidence: evidence, EvidenceHash: hash}
	if err := Verify(response); err != nil {
		return Response{}, err
	}
	return response, nil
}

func hashText(text string) map[string]any {
	sum := sha256.Sum256([]byte(text))
	return map[string]any{"sha256": hex.EncodeToString(sum[:])}
}

// Verify reproduces the binding and policy checks, plus the pure local result.
// It verifies consistency; anyone can construct a new consistent unsigned proof.
func Verify(response Response) error {
	if response.Protocol != ResponseProtocol {
		return errors.New("unsupported response protocol")
	}
	e := response.Evidence
	hash, err := metro.HashJSON(e)
	if err != nil || hash != response.EvidenceHash {
		return errors.New("evidence hash mismatch")
	}
	if e.Packet.Protocol != metro.PacketProtocol || e.Packet.SourceAgent != "codex" || e.Decision.ProviderID != ProviderID {
		return errors.New("invalid packet or provider identity")
	}
	// Reconstruct the adapter's allowed packet shape before trusting gate input.
	a := Action{Protocol: ActionProtocol, RequestID: e.Request.RequestID, ActionID: e.Packet.ActionID,
		Kind: strings.TrimPrefix(e.Packet.Action.Kind, "codex."), SideEffect: &e.Packet.Constraints.SideEffect, State: map[string]string{}}
	for key, value := range e.Request.State {
		text, ok := value.(string)
		if !ok {
			return errors.New("invalid state value")
		}
		a.State[key] = text
	}
	if text, ok := e.Packet.Action.Inputs["text"].(string); ok {
		a.Text = &text
	}
	if description, ok := e.Packet.Action.Inputs["description"].(string); ok {
		a.Description = &description
	}
	expectedPacket, expectedRequest, err := prepare(a)
	if err != nil {
		return err
	}
	expectedPacket.CreatedAt = e.Packet.CreatedAt
	expectedHash, _ := metro.HashJSON(expectedPacket)
	actualHash, _ := metro.HashJSON(e.Packet)
	if expectedHash != actualHash || expectedRequest.ChoicesHash != e.Request.ChoicesHash {
		return errors.New("packet or choices outside adapter bounds")
	}
	gate, err := decisionplane.ApplyDecision(e.Packet, e.Request, e.Decision, decisionplane.DefaultGatePolicy())
	if err != nil {
		return err
	}
	if gate.Route != nil && e.Gate.Route != nil {
		gate.Route.DecidedAt = e.Gate.Route.DecidedAt
	}
	wantGate, _ := metro.HashJSON(gate)
	gotGate, _ := metro.HashJSON(e.Gate)
	if wantGate != gotGate {
		return errors.New("gate or route mismatch")
	}
	if gate.Disposition != decisionplane.DispositionAutoRoute {
		if e.Dispatched || e.Result != nil || e.Receipt != nil {
			return errors.New("non-routed action cannot have execution evidence")
		}
		return nil
	}
	if a.Kind != "hash_text" || *a.SideEffect || !e.Dispatched || e.Receipt == nil || gate.Route == nil {
		return errors.New("invalid execution evidence")
	}
	wantResult, _ := metro.HashJSON(hashText(*a.Text))
	gotResult, err := metro.HashJSON(e.Result)
	if err != nil || wantResult != gotResult {
		return errors.New("local result mismatch")
	}
	r := e.Receipt
	if r.Protocol != metro.ReceiptProtocol || r.Status != "SUCCEEDED" || r.HashAlgorithm != "sha256" || r.ReceiptID != "receipt-"+a.ActionID || r.ResultRef != "" {
		return errors.New("invalid local receipt")
	}
	return metro.Verify(e.Packet, *gate.Route, e.Result, *r)
}
