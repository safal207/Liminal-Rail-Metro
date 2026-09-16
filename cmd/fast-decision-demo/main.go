package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	"github.com/safal207/Liminal-Rail-Metro/internal/decisionplane"
	"github.com/safal207/Liminal-Rail-Metro/internal/metro"
)

type demoProof struct {
	Protocol             string                   `json:"protocol"`
	SafeDecision         decisionplane.Decision   `json:"safe_decision"`
	SafeGate             decisionplane.GateResult `json:"safe_gate"`
	SideEffectGate       decisionplane.GateResult `json:"side_effect_gate"`
	SelectedTarget       string                   `json:"selected_target"`
	ProviderWireContract string                   `json:"provider_wire_contract"`
	Verdict              string                   `json:"verdict"`
}

func main() {
	packet := metro.NewPacket(
		"action-v0.6-demo",
		"planner-agent",
		"Choose the next bounded capability for a code change.",
		metro.Action{Kind: "code.change", Inputs: map[string]any{"scope": "one bounded patch"}},
		[]string{"research-agent", "code-agent", "qa-agent"},
	)
	choices := []decisionplane.Choice{
		{ID: "research", Target: "research-agent", Label: "Research"},
		{ID: "code", Target: "code-agent", Label: "Code"},
		{ID: "qa", Target: "qa-agent", Label: "QA"},
	}
	request, err := decisionplane.NewRequest(packet, "fast-decision-demo-1", map[string]any{
		"goal":        packet.Goal,
		"action_kind": packet.Action.Kind,
		"stage":       "implementation",
	}, choices)
	fatal(err)

	provider := decisionplane.StaticProvider{
		ID: "static-system-one-proof",
		Scores: map[string]float64{
			"research": 1,
			"code":     198,
			"qa":       1,
		},
	}
	decision, err := provider.Decide(context.Background(), request)
	fatal(err)

	policy := decisionplane.DefaultGatePolicy()
	safeGate, err := decisionplane.ApplyDecision(packet, request, decision, policy)
	fatal(err)
	if safeGate.Route == nil || safeGate.Disposition != decisionplane.DispositionAutoRoute {
		fatal(fmt.Errorf("safe decision did not auto-route: %#v", safeGate))
	}

	sideEffectPacket := packet
	sideEffectPacket.Constraints.SideEffect = true
	sideEffectGate, err := decisionplane.ApplyDecision(sideEffectPacket, request, decision, policy)
	fatal(err)
	if sideEffectGate.Route != nil || sideEffectGate.Disposition != decisionplane.DispositionRequireApproval {
		fatal(fmt.Errorf("side-effecting decision escaped approval gate: %#v", sideEffectGate))
	}

	proof := demoProof{
		Protocol:             "liminal.rail.fast-decision-proof.v0.6",
		SafeDecision:         decision,
		SafeGate:             safeGate,
		SideEffectGate:       sideEffectGate,
		SelectedTarget:       safeGate.Route.SelectedTarget,
		ProviderWireContract: "provider-agnostic System-One-shaped typed probabilistic choice; no TypeSafe/Jev API compatibility claim",
		Verdict:              "PASS",
	}
	payload, err := json.MarshalIndent(proof, "", "  ")
	fatal(err)
	fmt.Println(string(payload))
}

func fatal(err error) {
	if err == nil {
		return
	}
	fmt.Fprintln(os.Stderr, err)
	os.Exit(1)
}
