package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"sort"
	"time"

	"github.com/safal207/Liminal-Rail-Metro/internal/lifetrabridge"
	"github.com/safal207/Liminal-Rail-Metro/internal/lifetrastation"
	"github.com/safal207/Liminal-Rail-Metro/internal/metro"
)

type metrics struct {
	ProcessStartUS         int64   `json:"process_start_us"`
	FirstResponseUS        int64   `json:"first_response_us"`
	WarmAverageUS          float64 `json:"warm_average_us"`
	WarmP50US              int64   `json:"warm_p50_us"`
	WarmP95US              int64   `json:"warm_p95_us"`
	WarmMinUS              int64   `json:"warm_min_us"`
	WarmMaxUS              int64   `json:"warm_max_us"`
	WarmDecisionsPerSecond float64 `json:"warm_decisions_per_second"`
	FirstToWarmRatio       float64 `json:"first_to_warm_ratio"`
}

type proof struct {
	PersistentPIDReused bool   `json:"persistent_pid_reused"`
	PID                 int    `json:"pid"`
	WarmDecisions       int    `json:"warm_decisions"`
	Verdict             string `json:"verdict"`
	Path                string `json:"path"`
}

func main() {
	var stationBin string
	var lifetraDir string
	var iterations int
	flag.StringVar(&stationBin, "station-bin", "", "path to prebuilt Lifetra metro_station_server binary")
	flag.StringVar(&lifetraDir, "lifetra-dir", "", "Lifetra repository directory; uses cargo run when station-bin is empty")
	flag.IntVar(&iterations, "iterations", 50, "number of measured warm station decisions")
	flag.Parse()

	if iterations < 1 {
		fatal(fmt.Errorf("iterations must be positive"))
	}

	station := &lifetrastation.PersistentProcess{Timeout: 10 * time.Second}
	if stationBin != "" {
		station.Command = stationBin
	} else {
		if lifetraDir == "" {
			fatal(fmt.Errorf("station-bin or lifetra-dir is required"))
		}
		station.Command = "cargo"
		station.Args = []string{"run", "--quiet", "--example", "metro_station_server"}
		station.Dir = lifetraDir
	}

	startedAt := time.Now()
	if err := station.Start(); err != nil {
		fatal(err)
	}
	processStart := time.Since(startedAt)
	pid := station.PID()
	if pid == 0 {
		fatal(fmt.Errorf("persistent station did not expose a PID"))
	}

	warmup := benchmarkRequest("warmup", "warmup-next")
	if _, err := station.Evaluate(context.Background(), warmup); err != nil {
		_ = station.Close()
		fatal(fmt.Errorf("warmup decision: %w", err))
	}
	firstResponse := time.Since(startedAt)
	if station.PID() != pid {
		_ = station.Close()
		fatal(fmt.Errorf("station PID changed during warmup"))
	}

	durations := make([]time.Duration, 0, iterations)
	for index := 0; index < iterations; index++ {
		actionID := fmt.Sprintf("bench-%04d", index)
		nextActionID := fmt.Sprintf("bench-next-%04d", index)
		request := benchmarkRequest(actionID, nextActionID)

		begin := time.Now()
		decision, err := station.Evaluate(context.Background(), request)
		elapsed := time.Since(begin)
		if err != nil {
			_ = station.Close()
			fatal(fmt.Errorf("warm decision %d: %w", index, err))
		}
		if decision.Verdict != lifetrabridge.VerdictAllow {
			_ = station.Close()
			fatal(fmt.Errorf("warm decision %d returned %q", index, decision.Verdict))
		}
		if station.PID() != pid {
			_ = station.Close()
			fatal(fmt.Errorf("station PID changed at warm decision %d", index))
		}
		durations = append(durations, elapsed)
	}

	if err := station.Close(); err != nil {
		fatal(err)
	}

	resultMetrics := summarize(processStart, firstResponse, durations)
	out := map[string]any{
		"protocol": "liminal.rail.persistent-benchmark.v0.3",
		"proof": proof{
			PersistentPIDReused: true,
			PID:                 pid,
			WarmDecisions:       iterations,
			Verdict:             "PASS",
			Path:                "Go -> persistent Rust Lifetra -> Go",
		},
		"metrics":       resultMetrics,
		"claim_ceiling": "Measures one sequential local-process NDJSON station on one runner; it is not a distributed-network or model-inference benchmark.",
	}
	payload, err := json.MarshalIndent(out, "", "  ")
	fatal(err)
	fmt.Println(string(payload))
}

func benchmarkRequest(actionID, nextActionID string) lifetrabridge.StationRequest {
	now := time.Now().UTC()
	observed := lifetrabridge.Orientation{Growth: 0.6, Stability: 0.5, Truth: 0.8, Connection: 0.5}
	return lifetrabridge.StationRequest{
		Protocol: lifetrabridge.StationRequestProtocol,
		Observation: lifetrabridge.Observation{
			Protocol:      lifetrabridge.ObservationProtocol,
			ObservationID: "observation-" + actionID,
			ActionID:      actionID,
			ReceiptID:     "receipt-" + actionID,
			ReceiptStatus: "SUCCEEDED",
			ReceiptHash:   "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
			HashAlgorithm: "sha256",
			ProofRefs:     []string{"metro-receipt://receipt-" + actionID},
			ObservedAt:    now.Format(time.RFC3339Nano),
		},
		ObservedEpochSeconds: now.Unix(),
		DecidedAt:            metro.NowISO(),
		IntendedOrientation:  lifetrabridge.Orientation{Growth: 0.8, Stability: 0.5, Truth: 0.8, Connection: 0.5},
		ObservedOrientation:  &observed,
		Correction: lifetrabridge.CorrectionConfig{
			Gain: 0.5, EngageThreshold: 0.1, ReleaseThreshold: 0.05, MaxStep: 0.1,
		},
		Safety: lifetrabridge.SafetyConfig{
			Autonomy: "bounded_automatic", MinProofRefs: 1, MaxAutonomousAdjustment: 0.1, HardMaxAdjustment: 0.25,
		},
		AuthorityContext: lifetrabridge.AuthorityContext{HumanApproval: "unknown"},
		NextAction: lifetrabridge.NextAction{
			ActionID:       nextActionID,
			Goal:           "Verify the bounded benchmark artifact.",
			Kind:           "qa.verify",
			Inputs:         map[string]any{"artifact_ref": "artifact://persistent-bench"},
			AllowedTargets: []string{"qa-agent"},
			TimeoutMS:      5000,
		},
	}
}

func summarize(processStart, firstResponse time.Duration, values []time.Duration) metrics {
	sorted := append([]time.Duration(nil), values...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i] < sorted[j] })

	var total time.Duration
	for _, value := range sorted {
		total += value
	}
	averageUS := float64(total.Microseconds()) / float64(len(sorted))
	p50 := percentile(sorted, 0.50).Microseconds()
	p95 := percentile(sorted, 0.95).Microseconds()
	throughput := 0.0
	if averageUS > 0 {
		throughput = 1_000_000 / averageUS
	}
	ratio := 0.0
	if averageUS > 0 {
		ratio = float64(firstResponse.Microseconds()) / averageUS
	}

	return metrics{
		ProcessStartUS:         processStart.Microseconds(),
		FirstResponseUS:        firstResponse.Microseconds(),
		WarmAverageUS:          averageUS,
		WarmP50US:              p50,
		WarmP95US:              p95,
		WarmMinUS:              sorted[0].Microseconds(),
		WarmMaxUS:              sorted[len(sorted)-1].Microseconds(),
		WarmDecisionsPerSecond: throughput,
		FirstToWarmRatio:       ratio,
	}
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
