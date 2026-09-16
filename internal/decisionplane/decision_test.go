package decisionplane

import (
	"context"
	"strings"
	"testing"

	"github.com/safal207/Liminal-Rail-Metro/internal/metro"
)

func TestNewRequestRejectsChoiceOutsidePacketTargets(t *testing.T) {
	packet := decisionTestPacket(false)
	_, err := NewRequest(packet, "decision-1", nil, []Choice{{ID: "bad", Target: "prod-admin"}})
	if err == nil || !strings.Contains(err.Error(), "not allowed") {
		t.Fatalf("expected disallowed choice to fail, got %v", err)
	}
}

func TestNewRequestCopiesStateBeforeHashBinding(t *testing.T) {
	packet := decisionTestPacket(false)
	state := map[string]any{"phase": "research", "nested": map[string]any{"attempt": float64(1)}}
	request, err := NewRequest(packet, "copy-state", state, []Choice{{ID: "code", Target: "code-agent"}})
	if err != nil {
		t.Fatal(err)
	}
	state["phase"] = "deploy"
	state["nested"].(map[string]any)["attempt"] = float64(2)
	if request.State["phase"] != "research" {
		t.Fatalf("request state followed caller mutation: %#v", request.State)
	}
	nested, ok := request.State["nested"].(map[string]any)
	if !ok || nested["attempt"] != float64(1) {
		t.Fatalf("nested request state followed caller mutation: %#v", request.State)
	}
}

func TestHighConfidenceBoundedDecisionCreatesRoute(t *testing.T) {
	packet := decisionTestPacket(false)
	request := mustDecisionRequest(t, packet)
	decision := NewDecision(request, "system-one-proof", "code", []Probability{
		{ChoiceID: "research", Probability: 0.005},
		{ChoiceID: "code", Probability: 0.99},
		{ChoiceID: "qa", Probability: 0.005},
	}, 0.99)

	result, err := ApplyDecision(packet, request, decision, DefaultGatePolicy())
	if err != nil {
		t.Fatal(err)
	}
	if result.Disposition != DispositionAutoRoute || result.Route == nil {
		t.Fatalf("expected auto route, got %#v", result)
	}
	if result.Route.SelectedTarget != "code-agent" {
		t.Fatalf("unexpected target %q", result.Route.SelectedTarget)
	}
	if result.Route.DecisionMode != "system-one" {
		t.Fatalf("unexpected decision mode %q", result.Route.DecisionMode)
	}
}

func TestLowConfidenceDecisionEscalatesWithoutRoute(t *testing.T) {
	packet := decisionTestPacket(false)
	request := mustDecisionRequest(t, packet)
	decision := NewDecision(request, "system-one-proof", "code", []Probability{
		{ChoiceID: "research", Probability: 0.10},
		{ChoiceID: "code", Probability: 0.80},
		{ChoiceID: "qa", Probability: 0.10},
	}, 0.80)

	result, err := ApplyDecision(packet, request, decision, DefaultGatePolicy())
	if err != nil {
		t.Fatal(err)
	}
	if result.Disposition != DispositionEscalateSystem2 || result.Route != nil {
		t.Fatalf("expected System-2 escalation without route, got %#v", result)
	}
}

func TestAmbiguousTopChoicesEscalateEvenAtHighConfidence(t *testing.T) {
	packet := decisionTestPacket(false)
	request := mustDecisionRequest(t, packet)
	policy := DefaultGatePolicy()
	policy.AutoRouteConfidence = 0.49
	policy.MinTopMargin = 0.10
	decision := NewDecision(request, "system-one-proof", "code", []Probability{
		{ChoiceID: "research", Probability: 0.01},
		{ChoiceID: "code", Probability: 0.50},
		{ChoiceID: "qa", Probability: 0.49},
	}, 0.99)

	result, err := ApplyDecision(packet, request, decision, policy)
	if err != nil {
		t.Fatal(err)
	}
	if result.Disposition != DispositionEscalateSystem2 || result.ReasonCode != "top_choice_margin_too_small" || result.Route != nil {
		t.Fatalf("expected ambiguous decision to escalate, got %#v", result)
	}
}

func TestSideEffectRequiresApprovalRegardlessOfConfidence(t *testing.T) {
	packet := decisionTestPacket(true)
	request := mustDecisionRequest(t, packet)
	decision := NewDecision(request, "system-one-proof", "code", []Probability{
		{ChoiceID: "research", Probability: 0.001},
		{ChoiceID: "code", Probability: 0.998},
		{ChoiceID: "qa", Probability: 0.001},
	}, 0.999)

	result, err := ApplyDecision(packet, request, decision, DefaultGatePolicy())
	if err != nil {
		t.Fatal(err)
	}
	if result.Disposition != DispositionRequireApproval || result.Route != nil {
		t.Fatalf("expected approval gate without route, got %#v", result)
	}
}

func TestTamperedPacketBindingFailsClosed(t *testing.T) {
	packet := decisionTestPacket(false)
	request := mustDecisionRequest(t, packet)
	decision := NewDecision(request, "system-one-proof", "code", []Probability{
		{ChoiceID: "research", Probability: 0.005},
		{ChoiceID: "code", Probability: 0.99},
		{ChoiceID: "qa", Probability: 0.005},
	}, 0.99)
	decision.PacketHash = strings.Repeat("0", 64)

	if _, err := ApplyDecision(packet, request, decision, DefaultGatePolicy()); err == nil {
		t.Fatal("expected packet_hash mismatch to fail closed")
	}
}

func TestMutatedBoundPacketFailsClosed(t *testing.T) {
	packet := decisionTestPacket(false)
	request := mustDecisionRequest(t, packet)
	decision := NewDecision(request, "system-one-proof", "code", []Probability{
		{ChoiceID: "research", Probability: 0.005},
		{ChoiceID: "code", Probability: 0.99},
		{ChoiceID: "qa", Probability: 0.005},
	}, 0.99)
	packet.Action.Inputs["scope"] = "changed-after-binding"

	if _, err := ApplyDecision(packet, request, decision, DefaultGatePolicy()); err == nil {
		t.Fatal("expected post-binding packet mutation to fail closed")
	}
}

func TestTamperedStateBindingFailsClosed(t *testing.T) {
	packet := decisionTestPacket(false)
	request := mustDecisionRequest(t, packet)
	decision := NewDecision(request, "system-one-proof", "code", []Probability{
		{ChoiceID: "research", Probability: 0.005},
		{ChoiceID: "code", Probability: 0.99},
		{ChoiceID: "qa", Probability: 0.005},
	}, 0.99)
	decision.StateHash = strings.Repeat("0", 64)

	if _, err := ApplyDecision(packet, request, decision, DefaultGatePolicy()); err == nil {
		t.Fatal("expected state_hash mismatch to fail closed")
	}
}

func TestMutatedBoundRequestStateFailsClosed(t *testing.T) {
	packet := decisionTestPacket(false)
	request := mustDecisionRequest(t, packet)
	decision := NewDecision(request, "system-one-proof", "code", []Probability{
		{ChoiceID: "research", Probability: 0.005},
		{ChoiceID: "code", Probability: 0.99},
		{ChoiceID: "qa", Probability: 0.005},
	}, 0.99)
	request.State["kind"] = "deploy.production"

	if _, err := ApplyDecision(packet, request, decision, DefaultGatePolicy()); err == nil {
		t.Fatal("expected post-binding request state mutation to fail closed")
	}
}

func TestTamperedChoiceBindingFailsClosed(t *testing.T) {
	packet := decisionTestPacket(false)
	request := mustDecisionRequest(t, packet)
	decision := NewDecision(request, "system-one-proof", "code", []Probability{
		{ChoiceID: "research", Probability: 0.005},
		{ChoiceID: "code", Probability: 0.99},
		{ChoiceID: "qa", Probability: 0.005},
	}, 0.99)
	decision.ChoicesHash = strings.Repeat("0", 64)

	if _, err := ApplyDecision(packet, request, decision, DefaultGatePolicy()); err == nil {
		t.Fatal("expected choices_hash mismatch to fail closed")
	}
}

func TestMutatedBoundRequestChoicesFailClosed(t *testing.T) {
	packet := decisionTestPacket(false)
	request := mustDecisionRequest(t, packet)
	decision := NewDecision(request, "system-one-proof", "code", []Probability{
		{ChoiceID: "research", Probability: 0.005},
		{ChoiceID: "code", Probability: 0.99},
		{ChoiceID: "qa", Probability: 0.005},
	}, 0.99)
	request.Choices[0].Label = "Changed after binding"

	if _, err := ApplyDecision(packet, request, decision, DefaultGatePolicy()); err == nil {
		t.Fatal("expected post-binding request choice mutation to fail closed")
	}
}

func TestIncompleteProbabilityDistributionFailsClosed(t *testing.T) {
	packet := decisionTestPacket(false)
	request := mustDecisionRequest(t, packet)
	decision := NewDecision(request, "system-one-proof", "code", []Probability{
		{ChoiceID: "research", Probability: 0.01},
		{ChoiceID: "code", Probability: 0.99},
	}, 0.99)

	if _, err := ApplyDecision(packet, request, decision, DefaultGatePolicy()); err == nil {
		t.Fatal("expected incomplete probability distribution to fail closed")
	}
}

func TestProviderCannotSelectUnknownChoice(t *testing.T) {
	packet := decisionTestPacket(false)
	request := mustDecisionRequest(t, packet)
	decision := NewDecision(request, "system-one-proof", "invented", []Probability{
		{ChoiceID: "research", Probability: 0.005},
		{ChoiceID: "code", Probability: 0.99},
		{ChoiceID: "qa", Probability: 0.005},
	}, 0.99)

	if _, err := ApplyDecision(packet, request, decision, DefaultGatePolicy()); err == nil {
		t.Fatal("expected invented choice to fail closed")
	}
}

func TestStaticProviderProducesNormalizedBoundedDecision(t *testing.T) {
	packet := decisionTestPacket(false)
	request := mustDecisionRequest(t, packet)
	provider := StaticProvider{ID: "static-proof", Scores: map[string]float64{
		"research": 1,
		"code":     198,
		"qa":       1,
	}}
	decision, err := provider.Decide(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	if decision.SelectedChoiceID != "code" || decision.Confidence != 0.99 {
		t.Fatalf("unexpected static decision %#v", decision)
	}
	if err := ValidateDecision(packet, request, decision); err != nil {
		t.Fatal(err)
	}
}

func decisionTestPacket(sideEffect bool) metro.Packet {
	packet := metro.NewPacket(
		"action-fast-decision",
		"planner-agent",
		"Choose the next bounded execution capability.",
		metro.Action{Kind: "code.change", Inputs: map[string]any{"scope": "bounded"}},
		[]string{"research-agent", "code-agent", "qa-agent"},
	)
	packet.Constraints.SideEffect = sideEffect
	return packet
}

func mustDecisionRequest(t *testing.T, packet metro.Packet) Request {
	t.Helper()
	request, err := NewRequest(packet, "decision-request-1", map[string]any{
		"goal": packet.Goal,
		"kind": packet.Action.Kind,
	}, []Choice{
		{ID: "research", Target: "research-agent", Label: "Research"},
		{ID: "code", Target: "code-agent", Label: "Code"},
		{ID: "qa", Target: "qa-agent", Label: "QA"},
	})
	if err != nil {
		t.Fatal(err)
	}
	return request
}
