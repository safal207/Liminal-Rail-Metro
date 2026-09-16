package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/safal207/Liminal-Rail-Metro/internal/lifetrabridge"
	"github.com/safal207/Liminal-Rail-Metro/internal/lifetrastation"
	"github.com/safal207/Liminal-Rail-Metro/internal/metro"
)

type stationResult struct {
	PID         int    `json:"pid"`
	Completed   uint64 `json:"completed"`
	Errors      uint64 `json:"errors"`
	EWMAUS      int64  `json:"ewma_us"`
	MaxInFlight int    `json:"max_in_flight"`
}

type scenario struct {
	PoolSize                   int             `json:"pool_size"`
	MaxInFlightPerStation      int             `json:"max_in_flight_per_station"`
	TotalCapacity              int             `json:"total_capacity"`
	Concurrency                int             `json:"concurrency"`
	Requests                   int             `json:"requests"`
	BackpressureEvents         uint64          `json:"backpressure_events"`
	MaxObservedTotalInFlight   int             `json:"max_observed_total_in_flight"`
	MaxObservedStationInFlight int             `json:"max_observed_station_in_flight"`
	ServiceP50US               int64           `json:"service_p50_us"`
	ServiceP95US               int64           `json:"service_p95_us"`
	ServiceP99US               int64           `json:"service_p99_us"`
	EndToEndP50US              int64           `json:"end_to_end_p50_us"`
	EndToEndP95US              int64           `json:"end_to_end_p95_us"`
	EndToEndP99US              int64           `json:"end_to_end_p99_us"`
	ThroughputPerSecond        float64         `json:"throughput_per_second"`
	CompletedWithoutError      int             `json:"completed_without_error"`
	Stations                   []stationResult `json:"stations"`
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
	var limitsRaw string
	var requests int
	var concurrency int
	var poolSize int
	var workers int
	flag.StringVar(&stationBin, "station-bin", "", "path to prebuilt Lifetra metro_station_mux binary")
	flag.StringVar(&limitsRaw, "limits", "2,8,16", "comma-separated per-station admission caps")
	flag.IntVar(&requests, "requests", 2048, "measured requests per scenario")
	flag.IntVar(&concurrency, "concurrency", 64, "concurrent callers")
	flag.IntVar(&poolSize, "pool", 4, "persistent Rust process count")
	flag.IntVar(&workers, "workers", 16, "worker threads inside each Rust station process")
	flag.Parse()

	if stationBin == "" {
		fatal(errors.New("station-bin is required"))
	}
	if requests < 1 || concurrency < 1 || poolSize < 1 || workers < 1 || workers > 64 {
		fatal(errors.New("requests, concurrency, pool must be positive and workers must be in 1..64"))
	}
	limits, err := parsePositiveList(limitsRaw)
	fatal(err)

	proof := benchmarkProof{
		Protocol:     "liminal.rail.adaptive-backpressure-benchmark.v0.5",
		StationPath:  "Go adaptive admission/router -> request-correlated Rust Lifetra pool -> Go",
		WorkerCount:  workers,
		ClaimCeiling: "Measures local admission control, explicit backpressure, EWMA-informed routing, and bounded in-flight control requests on one runner. It does not measure LLM inference, remote networking, real tool execution, or end-to-end agent throughput.",
		Verdict:      "PASS",
	}

	for _, limit := range limits {
		result, err := runScenario(stationBin, workers, poolSize, limit, concurrency, requests)
		fatal(err)
		proof.Scenarios = append(proof.Scenarios, result)
	}

	payload, err := json.MarshalIndent(proof, "", "  ")
	fatal(err)
	fmt.Println(string(payload))
}

func runScenario(
	stationBin string,
	workers, poolSize, limit, concurrency, requests int,
) (scenario, error) {
	processConfig := lifetrastation.ProcessConfig{
		Command: stationBin,
		Env:     []string{fmt.Sprintf("LIFETRA_METRO_WORKERS=%d", workers)},
		Timeout: 5 * time.Second,
	}
	pool, err := lifetrastation.StartAdaptivePool(
		processConfig,
		poolSize,
		lifetrastation.AdaptivePoolConfig{
			MaxInFlightPerStation: limit,
			InitialLatency:        250 * time.Microsecond,
			EWMAAlpha:             0.20,
		},
	)
	if err != nil {
		return scenario{}, err
	}
	defer func() { _ = pool.Close() }()

	if err := warmPool(pool, poolSize, limit); err != nil {
		return scenario{}, err
	}

	serviceDurations := make([]time.Duration, requests)
	endToEndDurations := make([]time.Duration, requests)
	sem := make(chan struct{}, concurrency)
	errCh := make(chan error, requests)
	var wg sync.WaitGroup
	var backpressure atomic.Uint64
	var maxTotal atomic.Int64
	var maxStation atomic.Int64
	started := time.Now()

	for index := 0; index < requests; index++ {
		sem <- struct{}{}
		wg.Add(1)
		go func(index int) {
			defer wg.Done()
			defer func() { <-sem }()

			actionID := fmt.Sprintf("limit%d-action-%05d", limit, index)
			requestID := fmt.Sprintf("limit%d-request-%05d", limit, index)
			request := benchmarkRequest(actionID, "next-"+actionID)
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			begin := time.Now()

			for {
				serviceBegin := time.Now()
				decision, evaluateErr := pool.Evaluate(ctx, requestID, request)
				serviceElapsed := time.Since(serviceBegin)
				observe(pool, &maxTotal, &maxStation)

				if evaluateErr == nil {
					serviceDurations[index] = serviceElapsed
					endToEndDurations[index] = time.Since(begin)
					if decision.Verdict != lifetrabridge.VerdictAllow {
						errCh <- fmt.Errorf("request %s returned verdict %q", requestID, decision.Verdict)
						return
					}
					if decision.CausedByActionID != actionID {
						errCh <- fmt.Errorf("request %s rebound to action %q", requestID, decision.CausedByActionID)
					}
					return
				}

				if !errors.Is(evaluateErr, lifetrastation.ErrBackpressure) {
					errCh <- fmt.Errorf("request %s: %w", requestID, evaluateErr)
					return
				}
				backpressure.Add(1)
				var pressure *lifetrastation.BackpressureError
				retryAfter := 50 * time.Microsecond
				if errors.As(evaluateErr, &pressure) && pressure.RetryAfter > 0 {
					retryAfter = pressure.RetryAfter
				}
				if retryAfter < 10*time.Microsecond {
					retryAfter = 10 * time.Microsecond
				}
				select {
				case <-ctx.Done():
					errCh <- fmt.Errorf("request %s exhausted retry window: %w", requestID, ctx.Err())
					return
				case <-time.After(retryAfter):
				}
			}
		}(index)
	}

	wg.Wait()
	elapsed := time.Since(started)
	close(errCh)
	for err := range errCh {
		return scenario{}, err
	}

	if int(maxStation.Load()) > limit {
		return scenario{}, fmt.Errorf("admission invariant violated: observed station in-flight %d > limit %d", maxStation.Load(), limit)
	}
	if int(maxTotal.Load()) > poolSize*limit {
		return scenario{}, fmt.Errorf("pool admission invariant violated: observed total in-flight %d > capacity %d", maxTotal.Load(), poolSize*limit)
	}

	serviceSorted := sortedDurations(serviceDurations)
	endToEndSorted := sortedDurations(endToEndDurations)
	result := scenario{
		PoolSize:                   poolSize,
		MaxInFlightPerStation:      limit,
		TotalCapacity:              poolSize * limit,
		Concurrency:                concurrency,
		Requests:                   requests,
		BackpressureEvents:         backpressure.Load(),
		MaxObservedTotalInFlight:   int(maxTotal.Load()),
		MaxObservedStationInFlight: int(maxStation.Load()),
		ServiceP50US:               percentile(serviceSorted, 0.50).Microseconds(),
		ServiceP95US:               percentile(serviceSorted, 0.95).Microseconds(),
		ServiceP99US:               percentile(serviceSorted, 0.99).Microseconds(),
		EndToEndP50US:              percentile(endToEndSorted, 0.50).Microseconds(),
		EndToEndP95US:              percentile(endToEndSorted, 0.95).Microseconds(),
		EndToEndP99US:              percentile(endToEndSorted, 0.99).Microseconds(),
		CompletedWithoutError:      requests,
	}
	if elapsed > 0 {
		result.ThroughputPerSecond = float64(requests) / elapsed.Seconds()
	}
	for _, snapshot := range pool.Snapshots() {
		result.Stations = append(result.Stations, stationResult{
			PID:         snapshot.PID,
			Completed:   snapshot.Completed,
			Errors:      snapshot.Errors,
			EWMAUS:      snapshot.EWMALatency.Microseconds(),
			MaxInFlight: snapshot.MaxInFlight,
		})
	}
	return result, nil
}

func warmPool(pool *lifetrastation.AdaptivePool, poolSize, limit int) error {
	for index := 0; index < poolSize; index++ {
		actionID := fmt.Sprintf("warm-limit%d-action-%02d", limit, index)
		requestID := fmt.Sprintf("warm-limit%d-request-%02d", limit, index)
		decision, err := pool.Evaluate(
			context.Background(),
			requestID,
			benchmarkRequest(actionID, "warm-next-"+actionID),
		)
		if err != nil {
			return fmt.Errorf("warm station %d: %w", index, err)
		}
		if decision.Verdict != lifetrabridge.VerdictAllow {
			return fmt.Errorf("warm station %d returned %q", index, decision.Verdict)
		}
	}
	return nil
}

func observe(pool *lifetrastation.AdaptivePool, maxTotal, maxStation *atomic.Int64) {
	total := 0
	stationMax := 0
	for _, snapshot := range pool.Snapshots() {
		total += snapshot.InFlight
		if snapshot.InFlight > stationMax {
			stationMax = snapshot.InFlight
		}
	}
	updateMax(maxTotal, int64(total))
	updateMax(maxStation, int64(stationMax))
}

func updateMax(target *atomic.Int64, value int64) {
	for {
		current := target.Load()
		if value <= current || target.CompareAndSwap(current, value) {
			return
		}
	}
}

func sortedDurations(values []time.Duration) []time.Duration {
	sorted := append([]time.Duration(nil), values...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i] < sorted[j] })
	return sorted
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
			Goal:           "Verify the bounded adaptive routing benchmark artifact.",
			Kind:           "qa.verify",
			Inputs:         map[string]any{"artifact_ref": "artifact://adaptive-routing-bench"},
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
		return nil, errors.New("at least one positive integer is required")
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
