package decisionplane

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"sort"
	"time"

	"github.com/safal207/Liminal-Rail-Metro/internal/metro"
)

const (
	RequestProtocol = "metro.decision.request.v0.1"
	ResultProtocol  = "metro.decision.result.v0.1"

	DispositionAutoRoute       = "AUTO_ROUTE"
	DispositionEscalateSystem2 = "ESCALATE_SYSTEM2"
	DispositionRequireApproval = "REQUIRE_APPROVAL"
)

type Choice struct {
	ID     string `json:"id"`
	Target string `json:"target"`
	Label  string `json:"label,omitempty"`
}

type Request struct {
	Protocol    string         `json:"protocol"`
	RequestID   string         `json:"request_id"`
	ActionID    string         `json:"action_id"`
	State       map[string]any `json:"state,omitempty"`
	Choices     []Choice       `json:"choices"`
	ChoicesHash string         `json:"choices_hash"`
	RequestedAt string         `json:"requested_at"`
}

type Probability struct {
	ChoiceID    string  `json:"choice_id"`
	Probability float64 `json:"probability"`
}

type Decision struct {
	Protocol         string        `json:"protocol"`
	RequestID        string        `json:"request_id"`
	ActionID         string        `json:"action_id"`
	ProviderID       string        `json:"provider_id"`
	ChoicesHash      string        `json:"choices_hash"`
	SelectedChoiceID string        `json:"selected_choice_id"`
	Probabilities    []Probability `json:"probabilities"`
	Confidence       float64       `json:"confidence"`
	DecidedAt        string        `json:"decided_at"`
}

type Provider interface {
	Decide(context.Context, Request) (Decision, error)
}

type GatePolicy struct {
	AutoRouteConfidence           float64
	MinTopMargin                  float64
	RequireApprovalForSideEffects bool
	PolicyRef                     string
}

func DefaultGatePolicy() GatePolicy {
	return GatePolicy{
		AutoRouteConfidence:           0.98,
		MinTopMargin:                  0.15,
		RequireApprovalForSideEffects: true,
		PolicyRef:                     "policy://decision-plane/v0.6/default",
	}
}

type GateResult struct {
	Disposition string       `json:"disposition"`
	ReasonCode  string       `json:"reason_code"`
	Confidence  float64      `json:"confidence"`
	TopMargin   float64      `json:"top_margin"`
	Route       *metro.Route `json:"route,omitempty"`
}

func NewRequest(packet metro.Packet, requestID string, state map[string]any, choices []Choice) (Request, error) {
	if packet.ActionID == "" {
		return Request{}, errors.New("packet action_id is required")
	}
	if requestID == "" {
		return Request{}, errors.New("decision request_id is required")
	}
	if len(choices) == 0 {
		return Request{}, errors.New("at least one decision choice is required")
	}

	seenIDs := make(map[string]struct{}, len(choices))
	seenTargets := make(map[string]struct{}, len(choices))
	for _, choice := range choices {
		if choice.ID == "" || choice.Target == "" {
			return Request{}, errors.New("choice id and target are required")
		}
		if _, duplicate := seenIDs[choice.ID]; duplicate {
			return Request{}, fmt.Errorf("duplicate choice id %q", choice.ID)
		}
		if _, duplicate := seenTargets[choice.Target]; duplicate {
			return Request{}, fmt.Errorf("duplicate choice target %q", choice.Target)
		}
		if !metro.Contains(packet.AllowedTargets, choice.Target) {
			return Request{}, fmt.Errorf("choice target %q is not allowed by packet", choice.Target)
		}
		seenIDs[choice.ID] = struct{}{}
		seenTargets[choice.Target] = struct{}{}
	}

	choicesHash, err := HashChoices(choices)
	if err != nil {
		return Request{}, err
	}
	return Request{
		Protocol:    RequestProtocol,
		RequestID:   requestID,
		ActionID:    packet.ActionID,
		State:       state,
		Choices:     append([]Choice(nil), choices...),
		ChoicesHash: choicesHash,
		RequestedAt: metro.NowISO(),
	}, nil
}

func HashChoices(choices []Choice) (string, error) {
	canonical := append([]Choice(nil), choices...)
	sort.Slice(canonical, func(i, j int) bool { return canonical[i].ID < canonical[j].ID })
	payload, err := json.Marshal(canonical)
	if err != nil {
		return "", fmt.Errorf("encode choices: %w", err)
	}
	sum := sha256.Sum256(payload)
	return hex.EncodeToString(sum[:]), nil
}

func ValidateDecision(packet metro.Packet, request Request, decision Decision) error {
	if request.Protocol != RequestProtocol {
		return fmt.Errorf("unsupported decision request protocol %q", request.Protocol)
	}
	if decision.Protocol != ResultProtocol {
		return fmt.Errorf("unsupported decision result protocol %q", decision.Protocol)
	}
	if request.ActionID != packet.ActionID || decision.ActionID != packet.ActionID {
		return errors.New("decision action_id is not bound to packet")
	}
	if decision.RequestID != request.RequestID {
		return errors.New("decision request_id mismatch")
	}
	if decision.ChoicesHash != request.ChoicesHash {
		return errors.New("decision choices_hash mismatch")
	}
	if decision.ProviderID == "" {
		return errors.New("decision provider_id is required")
	}
	if decision.SelectedChoiceID == "" {
		return errors.New("selected_choice_id is required")
	}
	if math.IsNaN(decision.Confidence) || decision.Confidence < 0 || decision.Confidence > 1 {
		return errors.New("decision confidence must be in [0,1]")
	}

	choiceByID := make(map[string]Choice, len(request.Choices))
	for _, choice := range request.Choices {
		choiceByID[choice.ID] = choice
	}
	selected, ok := choiceByID[decision.SelectedChoiceID]
	if !ok {
		return fmt.Errorf("provider selected unknown choice %q", decision.SelectedChoiceID)
	}
	if !metro.Contains(packet.AllowedTargets, selected.Target) {
		return fmt.Errorf("provider selected disallowed target %q", selected.Target)
	}

	if len(decision.Probabilities) != len(request.Choices) {
		return fmt.Errorf("probability distribution has %d entries for %d choices", len(decision.Probabilities), len(request.Choices))
	}
	seen := make(map[string]struct{}, len(decision.Probabilities))
	total := 0.0
	selectedProbability := -1.0
	maxProbability := -1.0
	for _, probability := range decision.Probabilities {
		if _, exists := choiceByID[probability.ChoiceID]; !exists {
			return fmt.Errorf("probability references unknown choice %q", probability.ChoiceID)
		}
		if _, duplicate := seen[probability.ChoiceID]; duplicate {
			return fmt.Errorf("duplicate probability for choice %q", probability.ChoiceID)
		}
		if math.IsNaN(probability.Probability) || probability.Probability < 0 || probability.Probability > 1 {
			return fmt.Errorf("probability for %q must be in [0,1]", probability.ChoiceID)
		}
		seen[probability.ChoiceID] = struct{}{}
		total += probability.Probability
		if probability.ChoiceID == decision.SelectedChoiceID {
			selectedProbability = probability.Probability
		}
		if probability.Probability > maxProbability {
			maxProbability = probability.Probability
		}
	}
	if math.Abs(total-1) > 0.000001 {
		return fmt.Errorf("probability distribution must sum to 1, got %.9f", total)
	}
	if selectedProbability < 0 {
		return errors.New("selected choice is missing from probability distribution")
	}
	if selectedProbability+0.000001 < maxProbability {
		return errors.New("selected choice is not a maximum-probability choice")
	}
	return nil
}

func ApplyDecision(packet metro.Packet, request Request, decision Decision, policy GatePolicy) (GateResult, error) {
	if err := ValidateDecision(packet, request, decision); err != nil {
		return GateResult{}, err
	}
	if policy.AutoRouteConfidence <= 0 || policy.AutoRouteConfidence > 1 {
		return GateResult{}, errors.New("auto-route confidence must be in (0,1]")
	}
	if policy.MinTopMargin < 0 || policy.MinTopMargin > 1 {
		return GateResult{}, errors.New("minimum top margin must be in [0,1]")
	}
	if policy.PolicyRef == "" {
		policy.PolicyRef = "policy://decision-plane/v0.6/custom"
	}

	selected, selectedProbability, runnerUp, err := selectedStats(request, decision)
	if err != nil {
		return GateResult{}, err
	}
	margin := selectedProbability - runnerUp
	base := GateResult{
		Confidence: decision.Confidence,
		TopMargin:  margin,
	}

	if packet.Constraints.SideEffect && policy.RequireApprovalForSideEffects {
		base.Disposition = DispositionRequireApproval
		base.ReasonCode = "side_effect_requires_approval"
		return base, nil
	}
	if decision.Confidence < policy.AutoRouteConfidence || selectedProbability < policy.AutoRouteConfidence {
		base.Disposition = DispositionEscalateSystem2
		base.ReasonCode = "confidence_below_auto_route_threshold"
		return base, nil
	}
	if margin < policy.MinTopMargin {
		base.Disposition = DispositionEscalateSystem2
		base.ReasonCode = "top_choice_margin_too_small"
		return base, nil
	}

	candidates := make([]metro.Candidate, 0, len(decision.Probabilities))
	for _, probability := range decision.Probabilities {
		choice, ok := choiceForID(request.Choices, probability.ChoiceID)
		if !ok {
			return GateResult{}, fmt.Errorf("missing choice %q while building route", probability.ChoiceID)
		}
		candidates = append(candidates, metro.Candidate{Target: choice.Target, Score: probability.Probability})
	}
	sort.Slice(candidates, func(i, j int) bool {
		if candidates[i].Score == candidates[j].Score {
			return candidates[i].Target < candidates[j].Target
		}
		return candidates[i].Score > candidates[j].Score
	})

	route := metro.Route{
		Protocol:       metro.RouteProtocol,
		RouteID:        "route-" + packet.ActionID,
		ActionID:       packet.ActionID,
		RouterID:       "fast-decision-plane:" + decision.ProviderID,
		DecisionMode:   "system-one",
		SelectedTarget: selected.Target,
		Candidates:     candidates,
		Confidence:     decision.Confidence,
		PolicyRef:      policy.PolicyRef,
		DecidedAt:      metro.NowISO(),
	}
	base.Disposition = DispositionAutoRoute
	base.ReasonCode = "bounded_high_confidence_choice"
	base.Route = &route
	return base, nil
}

func selectedStats(request Request, decision Decision) (Choice, float64, float64, error) {
	selected, ok := choiceForID(request.Choices, decision.SelectedChoiceID)
	if !ok {
		return Choice{}, 0, 0, fmt.Errorf("selected choice %q is missing", decision.SelectedChoiceID)
	}
	selectedProbability := -1.0
	runnerUp := 0.0
	for _, probability := range decision.Probabilities {
		if probability.ChoiceID == decision.SelectedChoiceID {
			selectedProbability = probability.Probability
			continue
		}
		if probability.Probability > runnerUp {
			runnerUp = probability.Probability
		}
	}
	if selectedProbability < 0 {
		return Choice{}, 0, 0, errors.New("selected choice probability is missing")
	}
	return selected, selectedProbability, runnerUp, nil
}

func choiceForID(choices []Choice, id string) (Choice, bool) {
	for _, choice := range choices {
		if choice.ID == id {
			return choice, true
		}
	}
	return Choice{}, false
}

func NewDecision(request Request, providerID, selectedChoiceID string, probabilities []Probability, confidence float64) Decision {
	return Decision{
		Protocol:         ResultProtocol,
		RequestID:        request.RequestID,
		ActionID:         request.ActionID,
		ProviderID:       providerID,
		ChoicesHash:      request.ChoicesHash,
		SelectedChoiceID: selectedChoiceID,
		Probabilities:    append([]Probability(nil), probabilities...),
		Confidence:       confidence,
		DecidedAt:        time.Now().UTC().Format(time.RFC3339Nano),
	}
}
