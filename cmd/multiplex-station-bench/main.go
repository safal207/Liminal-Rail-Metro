package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/safal207/Liminal-Rail-Metro/internal/lifetrabridge"
	"github.com/safal207/Liminal-Rail-Metro/internal/lifetrastation"
	"github.com/safal207/Liminal-Rail-Metro/internal/metro"
)

type scenario struct {
	PoolSize              int     `json:"pool_size"`
	Concurrency           int     `json:"concurrency"`
	Requests              int     `json:"requests"`
	DistinctPIDs          int     `json:"distinct_pids"`
	P50US                 int64   `json:"p50_us"`
	P95US                 int64   `json:"p95_us"`
	P99US                 int64   `json:"p99_us"`
	MinUS                 int64   `json:"min_us"`
	MaxUS                 int64   `json:"max_us"`
	AverageUS             float64 `json:"average_us"`
	ThroughputPerSecond   float64 `json:"throughput_per_second"`
	CompletedWithoutError int     `json:"completed_without_error"`
}

type benchmarkProof struct {
	Protocol     string     `json:"protocol"`
	StationPath  string     `json:"station_path"`
	WorkerCount  int        `json:"workers_per_rust_pid"`
	Scenarios    []scenario `json:"scenarios"`
	ClaimCeiling string     `json:"claim_ceiling"`
	Verdict      string     `json:"verdict"`
}

func main() {
	var stationBin string
	var concurrencyRaw string
	var poolsRaw string
	var requests int
	var workers int
	flag.StringVar(&stationBin, "station-bin", "", "path to prebuilt Lifetra metro_station_mux binary")
	flag.StringVar(&concurrencyRaw, "concurrency", "1,4,16,64", "comma-separated in-flight request levels")
	flag.StringVar(&poolsRaw, "pools", "1,4", "comma-separated persistent Rust process counts")
	flag.IntVar(&requests, "requests", 2048, "measured requests per scenario")
	flag.IntVar(&workers, "workers", 16, "worker threads inside each Rust station process")
	flag.Parse()

	if stationBin == "" {
		fatal(fmt.Errorf("station-bin is required"))
	}
	if requests < 1 || workers < 1 || workers > 64 {
		fatal(fmt.Errorf("requests must be positive and workers must be in 1..64"))
	}
	concurrencyLevels, err := parsePositiveList(concurrencyRaw)
	fatal(err)
	poolSizes, err := parsePositiveList(poolsRaw)
	fatal(err)

	proof := benchmarkProof{
		Protocol:     "liminal.rail.multiplex-benchmark.v0.4",
		StationPath:  "Go -> request-correlated multiplex Rust Lifetra -> Go",
		WorkerCount:  workers,
		ClaimCeiling: "Measures local NDJSON control-plane correlation, concurrency and persistent process pooling on one runner. It is not an LLM inference, network, distributed-agent, or real tool-execution benchmark.",
		Verdict:      "PASS",
	}

	for _, poolSize := range poolSizes {
		config := lifetrastation.ProcessConfig{
			Command: stationBin,
			Env:     []string{fmt.Sprintf("LIFETRA_METRO_WORKERS=%d", workers)},
			Timeout: 5 * time.Second,
		}
		pool, err := lifetrastation.StartPool(config, poolSize)
		fatal(err)

		if err := warmPool(pool, poolSize); err != nil {
			_ = pool.Close()
			fatal(err)
		}

		for _, concurrency := range concurrencyLevels {
			result, err := runScenario(pool, poolSize, concurrency, requests)
			if err != nil {
				_ = pool.Close()
				fatal(err)
			}
			proof.Scenarios = append(proof.Scenarios, result)
		}

		fatal(pool.Close())
	}

	payload, err := json.MarshalIndent(proof, "", "  ")
	fatal(err)
	fmt.Println(string(payload))
}

func warmPool(pool *lifetrastation.Pool, size int) error {
	for index := 0; index < size; index++ {
		actionID := fmt.Sprintf("warm-action-%02d", index)
		requestID := fmt.Sprintf("warm-request-%02d", index)
		decision, err := pool.Evaluate(context.Background(), requestID, benchmarkRequest(actionID, "warm-next-"+actionID))
		if err != nil {
			return fmt.Errorf("warm station %d: %w", index, err)
		}
		if decision.Verdict != lifetrabridge.VerdictAllow {
			return fmt.Errorf("warm station %d returned %q", index, decision.Verdict)
		}
	}
	return nil
}

func runScenario(pool *lifetrastation.Pool, poolSize, concurrency, requests int) (scenario, error) {
	if concurrency < 1 {
		return scenario{}, fmt.Errorf("concurrency must be positive")
	}

	durations := make([]time.Duration, requests)
	sem := make(chan struct{}, concurrency)
	var wg sync.WaitGroup
	errCh := make(chan error, requests)
	start := time.Now()

	for index := 0; index < requests; index++ {
		sem <- struct{}{}
		wg.Add(1)
		go func(index int) {
			defer wg.Done()
			defer func() { <-sem }()

			actionID := fmt.Sprintf("p%d-c%d-action-%05d", poolSize, concurrency, index)
			requestID := fmt.Sprintf("p%d-c%d-request-%05d", poolSize, concurrency, index)
			request := benchmarkRequest(actionID, "next-"+actionID)
			begin := time.Now()
			decision, err := pool.Evaluate(context.Background(), requestID, request)
			durations[index] = time.Since(begin)
			if err != nil {
				errCh <- fmt.Errorf("request %s: %w", requestID, err)
				return
			}
			if decision.Verdict != lifetrabridge.VerdictAllow {
				errCh <- fmt.Errorf("request %s returned %q", requestID, decision.Verdict)
				return
			}
			if decision.CausedByActionID != actionID {
				errCh <- fmt.Errorf("request %s rebound to action %q", requestID, decision.CausedByActionID)
			}
		}(index)
	}
	wg.Wait()
	elapsed := time.Since(start)
	close(errCh)
	for err := range errCh {
		return scenario{}, err
	}

	return summarize(pool.PIDs(), poolSize, concurrency, durations, elapsed), nil
}

func summarize(pids []int, poolSize, concurrency int, values []time.Duration, elapsed time.Duration) scenario {
	sorted := append([]time.Duration(nil), values...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i] < sorted[j] })

	var total time.Duration
	for _, value := range sorted {
		total += value
	}
	averageUS := float64(total.Microseconds()) / float64(len(sorted))
	throughput := 0.0
	if elapsed > 0 {
		throughput = float64(len(sorted)) / elapsed.Seconds()
	}

	pidSet := map[int]struct{}{}
	for _, pid := range pids {
		if pid > 0 {
			pidSet[pid] = struct{}{}
		}
	}

	return scenario{
		PoolSize:              poolSize,
		Concurrency:           concurrency,
		Requests:              len(sorted),
		DistinctPIDs:          len(pidSet),
		P50US:                 percentile(sorted, 0.50).Microseconds(),
		P95US:                 percentile(sorted, 0.95).Microseconds(),
		P99US:                 percentile(sorted, 0.99).Microseconds(),
		MinUS:                 sorted[0].Microseconds(),
		MaxUS:                 sorted[len(sorted)-1].Microseconds(),
		AverageUS:             averageUS,
		ThroughputPerSecond:   throughput,
		CompletedWithoutError: len(sorted),
	}
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
			Gain:             0.5,
			EngageThreshold:  0.1,
			ReleaseThreshold: 0.05,
			MaxStep:          0.1,
		},
		Safety: lifetrabridge.SafetyConfig{
			Autonomy:                "bounded_automatic",
			MinProofRefs:            1,
			MaxAutonomousAdjustment: 0.1,
			HardMaxAdjustment:       0.25,
		},
		AuthorityContext: lifetrabridge.AuthorityContext{HumanApproval: "unknown"},
		NextAction: lifetrabridge.NextAction{
			ActionID:       nextActionID,
			Goal:           "Verify the bounded multiplex benchmark artifact.",
			Kind:           "qa.verify",
			Inputs:         map[string]any{"artifact_ref": "artifact://multiplex-bench"},
			AllowedTargets: []string{"qa-agent"},
			TimeoutMS:      5000,
		},
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

func parsePositiveList(raw string) ([]int, error) {
	parts := strings.Split(raw, ",")
	values := make([]int, 0, len(parts))
	seen := map[int]struct{}{}
	for _, part := range parts {
		value, err := strconv.Atoi(strings.TrimSpace(part))
		if err != nil || value < 1 {
			return nil, fmt.Errorf("invalid positive integer %q", part)
		}
		if _, duplicate := seen[value]; duplicate {
			continue
		}
		seen[value] = struct{}{}
		values = append(values, value)
	}
	if len(values) == 0 {
		return nil, fmt.Errorf("at least one positive integer is required")
	}
	return values, nil
}

func fatal(err error) {
	if err == nil {
		return
	}
	fmt.Fprintln(os.Stderr, err)
	os.Exit(1)
}
