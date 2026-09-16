package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"sort"
	"time"

	"github.com/safal207/Liminal-Rail-Metro/internal/decisionplane"
	"github.com/safal207/Liminal-Rail-Metro/internal/metro"
)

type benchmarkProof struct {
	Protocol              string  `json:"protocol"`
	Iterations            int     `json:"iterations"`
	CompletedWithoutError int     `json:"completed_without_error"`
	AverageNS             float64 `json:"average_ns"`
	P50NS                 int64   `json:"p50_ns"`
	P95NS                 int64   `json:"p95_ns"`
	P99NS                 int64   `json:"p99_ns"`
	MinNS                 int64   `json:"min_ns"`
	MaxNS                 int64   `json:"max_ns"`
	ClaimCeiling          string  `json:"claim_ceiling"`
	Verdict               string  `json:"verdict"`
}

func main() {
	var iterations int
	flag.IntVar(&iterations, "iterations", 50000, "measured local provider+gate iterations")
	flag.Parse()
	if iterations < 1 {
		fatal(fmt.Errorf("iterations must be positive"))
	}

	packet := metro.NewPacket(
		"action-fast-decision-bench",
		"planner-agent",
		"Choose one bounded execution target.",
		metro.Action{Kind: "code.change", Inputs: map[string]any{"scope": "bench"}},
		[]string{"research-agent", "code-agent", "qa-agent"},
	)
	request, err := decisionplane.NewRequest(packet, "decision-bench-request", map[string]any{
		"goal": packet.Goal,
		"kind": packet.Action.Kind,
	}, []decisionplane.Choice{
		{ID: "research", Target: "research-agent"},
		{ID: "code", Target: "code-agent"},
		{ID: "qa", Target: "qa-agent"},
	})
	fatal(err)
	provider := decisionplane.StaticProvider{
		ID: "static-local-benchmark",
		Scores: map[string]float64{
			"research": 1,
			"code":     198,
			"qa":       1,
		},
	}
	policy := decisionplane.DefaultGatePolicy()

	for index := 0; index < 1000; index++ {
		decision, err := provider.Decide(context.Background(), request)
		fatal(err)
		result, err := decisionplane.ApplyDecision(packet, request, decision, policy)
		fatal(err)
		if result.Route == nil || result.Route.SelectedTarget != "code-agent" {
			fatal(fmt.Errorf("warm decision did not produce expected route"))
		}
	}

	durations := make([]time.Duration, iterations)
	var total time.Duration
	for index := 0; index < iterations; index++ {
		started := time.Now()
		decision, err := provider.Decide(context.Background(), request)
		fatal(err)
		result, err := decisionplane.ApplyDecision(packet, request, decision, policy)
		fatal(err)
		if result.Route == nil || result.Route.SelectedTarget != "code-agent" {
			fatal(fmt.Errorf("iteration %d did not produce expected route", index))
		}
		elapsed := time.Since(started)
		durations[index] = elapsed
		total += elapsed
	}

	sorted := append([]time.Duration(nil), durations...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i] < sorted[j] })
	proof := benchmarkProof{
		Protocol:              "liminal.rail.fast-decision-local-benchmark.v0.6",
		Iterations:            iterations,
		CompletedWithoutError: iterations,
		AverageNS:             float64(total.Nanoseconds()) / float64(iterations),
		P50NS:                 percentile(sorted, 0.50).Nanoseconds(),
		P95NS:                 percentile(sorted, 0.95).Nanoseconds(),
		P99NS:                 percentile(sorted, 0.99).Nanoseconds(),
		MinNS:                 sorted[0].Nanoseconds(),
		MaxNS:                 sorted[len(sorted)-1].Nanoseconds(),
		ClaimCeiling:          "Measures only local Go static-provider normalization, typed decision validation, policy gating, and route construction. It does not measure TypeSafe Jev, any remote model, network latency, LLM inference, Lifetra execution, or end-to-end agent throughput.",
		Verdict:               "PASS",
	}
	payload, err := json.MarshalIndent(proof, "", "  ")
	fatal(err)
	fmt.Println(string(payload))
}

func percentile(values []time.Duration, quantile float64) time.Duration {
	if len(values) == 0 {
		return 0
	}
	index := int(float64(len(values)-1)*quantile + 0.5)
	if index < 0 {
		index = 0
	}
	if index >= len(values) {
		index = len(values) - 1
	}
	return values[index]
}

func fatal(err error) {
	if err == nil {
		return
	}
	fmt.Fprintln(os.Stderr, err)
	os.Exit(1)
}
