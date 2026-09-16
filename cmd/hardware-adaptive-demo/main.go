package main

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/safal207/Liminal-Rail-Metro/internal/adaptive"
)

type trial struct {
	Round      int                `json:"round"`
	State      adaptive.HostState `json:"state"`
	Action     string             `json:"action"`
	DurationMS float64            `json:"duration_ms"`
	Throughput float64            `json:"throughput_mib_s"`
	Reward     float64            `json:"reward"`
}

type proof struct {
	Protocol       string                         `json:"protocol"`
	Environment    adaptive.HostState             `json:"environment"`
	Trials         []trial                        `json:"trials"`
	LearnedStats   map[string]adaptive.ActionStat `json:"learned_stats"`
	BestAction     string                         `json:"best_action"`
	BestMeanReward float64                        `json:"best_mean_reward"`
	Claim          string                         `json:"claim"`
}

func main() {
	maxWorkers := runtime.NumCPU()
	if maxWorkers > 5 {
		maxWorkers = 5
	}
	if maxWorkers < 1 {
		maxWorkers = 1
	}

	actions := make([]string, 0, maxWorkers)
	for i := 1; i <= maxWorkers; i++ {
		actions = append(actions, fmt.Sprintf("workers=%d", i))
	}

	policy, err := adaptive.NewUCB1Policy(actions, 0.9)
	if err != nil {
		panic(err)
	}

	const rounds = 12
	const iterations = 80000
	const payloadBytes = 2048
	trials := make([]trial, 0, rounds)

	for round := 1; round <= rounds; round++ {
		state := adaptive.SenseHost()
		decision := policy.Choose()
		workers := mustWorkers(decision.Action)
		duration := cpuProof(workers, iterations, payloadBytes)
		mib := float64(iterations*payloadBytes) / (1024 * 1024)
		throughput := mib / duration.Seconds()
		reward := throughput

		if err := policy.Observe(decision.Action, reward); err != nil {
			panic(err)
		}

		trials = append(trials, trial{
			Round:      round,
			State:      state,
			Action:     decision.Action,
			DurationMS: float64(duration.Microseconds()) / 1000,
			Throughput: throughput,
			Reward:     reward,
		})
		fmt.Printf("round=%02d action=%-9s throughput=%8.1f MiB/s load1=%.2f\n", round, decision.Action, throughput, state.Load1)
	}

	best, bestStat, _ := policy.BestObserved()
	out := proof{
		Protocol:       "liminal.hardware-adaptive-proof.v0.1",
		Environment:    adaptive.SenseHost(),
		Trials:         trials,
		LearnedStats:   policy.Snapshot(),
		BestAction:     best,
		BestMeanReward: bestStat.MeanReward,
		Claim:          "The bounded runtime sampled only allow-listed worker counts, measured real execution throughput, and updated its next-action policy from observed rewards. This does not modify model weights or claim globally optimal hardware scheduling.",
	}

	b, err := json.MarshalIndent(out, "", "  ")
	if err != nil {
		panic(err)
	}
	if err := os.WriteFile("hardware-adaptive-proof.json", b, 0o644); err != nil {
		panic(err)
	}
	fmt.Printf("\nbest_observed=%s mean_reward=%.1f MiB/s\n", best, bestStat.MeanReward)
	fmt.Println("proof=hardware-adaptive-proof.json")
}

func mustWorkers(action string) int {
	parts := strings.Split(action, "=")
	if len(parts) != 2 || parts[0] != "workers" {
		panic("invalid bounded action: " + action)
	}
	workers, err := strconv.Atoi(parts[1])
	if err != nil || workers < 1 {
		panic("invalid worker count: " + action)
	}
	return workers
}

func cpuProof(workers, iterations, payloadBytes int) time.Duration {
	start := time.Now()
	var wg sync.WaitGroup
	wg.Add(workers)

	base := iterations / workers
	extra := iterations % workers
	for worker := 0; worker < workers; worker++ {
		count := base
		if worker < extra {
			count++
		}
		go func(workerID, n int) {
			defer wg.Done()
			payload := make([]byte, payloadBytes)
			for i := range payload {
				payload[i] = byte((i + workerID*17) % 251)
			}
			var sink [32]byte
			for i := 0; i < n; i++ {
				sink = sha256.Sum256(payload)
				copy(payload[:32], sink[:])
				payload[32] ^= byte(i)
			}
			if sink[0] == 255 && sink[1] == 255 {
				fmt.Fprint(os.Stderr, "")
			}
		}(worker, count)
	}
	wg.Wait()
	return time.Since(start)
}
