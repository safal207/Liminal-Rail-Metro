package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"time"

	"github.com/safal207/Liminal-Rail-Metro/internal/decisionplane"
	"github.com/safal207/Liminal-Rail-Metro/internal/metro"
)

type proof struct {
	Protocol       string                   `json:"protocol"`
	Transport      string                   `json:"transport"`
	ProviderID     string                   `json:"provider_id"`
	RequestID      string                   `json:"request_id"`
	PacketHash     string                   `json:"packet_hash"`
	StateHash      string                   `json:"state_hash"`
	ChoicesHash    string                   `json:"choices_hash"`
	Decision       decisionplane.Decision   `json:"decision"`
	Gate           decisionplane.GateResult `json:"gate"`
	SelectedTarget string                   `json:"selected_target"`
	ClaimCeiling   string                   `json:"claim_ceiling"`
	Verdict        string                   `json:"verdict"`
}

func main() {
	packet := metro.NewPacket(
		"action-remote-v0.7-demo",
		"planner-agent",
		"Choose the next bounded execution capability through a remote provider boundary.",
		metro.Action{Kind: "code.change", Inputs: map[string]any{"scope": "one bounded patch"}},
		[]string{"research-agent", "code-agent", "qa-agent"},
	)
	request, err := decisionplane.NewRequest(packet, "remote-provider-demo-1", map[string]any{
		"stage": "implementation",
		"goal":  packet.Goal,
	}, []decisionplane.Choice{
		{ID: "research", Target: "research-agent", Label: "Research"},
		{ID: "code", Target: "code-agent", Label: "Code"},
		{ID: "qa", Target: "qa-agent", Label: "QA"},
	})
	fatal(err)

	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, httpRequest *http.Request) {
		decoder := json.NewDecoder(httpRequest.Body)
		decoder.DisallowUnknownFields()
		var received decisionplane.Request
		if err := decoder.Decode(&received); err != nil {
			http.Error(writer, err.Error(), http.StatusBadRequest)
			return
		}
		decision := decisionplane.NewDecision(received, "remote-proof", "code", []decisionplane.Probability{
			{ChoiceID: "research", Probability: 0.005},
			{ChoiceID: "code", Probability: 0.99},
			{ChoiceID: "qa", Probability: 0.005},
		}, 0.99)
		writer.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(writer).Encode(decision)
	}))
	defer server.Close()

	provider, err := decisionplane.NewRemoteProvider(decisionplane.RemoteProviderConfig{
		Endpoint:   server.URL,
		ProviderID: "remote-proof",
		Timeout:    time.Second,
	})
	fatal(err)
	decision, err := provider.Decide(context.Background(), request)
	fatal(err)
	gate, err := decisionplane.ApplyDecision(packet, request, decision, decisionplane.DefaultGatePolicy())
	fatal(err)
	if gate.Route == nil || gate.Disposition != decisionplane.DispositionAutoRoute {
		fatal(fmt.Errorf("remote decision did not produce bounded auto-route: %#v", gate))
	}

	payload, err := json.MarshalIndent(proof{
		Protocol:       "liminal.rail.remote-provider-proof.v0.7",
		Transport:      "HTTP JSON loopback proof",
		ProviderID:     decision.ProviderID,
		RequestID:      request.RequestID,
		PacketHash:     request.PacketHash,
		StateHash:      request.StateHash,
		ChoicesHash:    request.ChoicesHash,
		Decision:       decision,
		Gate:           gate,
		SelectedTarget: gate.Route.SelectedTarget,
		ClaimCeiling:   "Proves Metro remote-provider HTTP transport and v0.6 binding/policy integration on loopback only. It is not a TypeSafe/Jev API, latency, accuracy, remote-network, or end-to-end agent benchmark.",
		Verdict:        "PASS",
	}, "", "  ")
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
