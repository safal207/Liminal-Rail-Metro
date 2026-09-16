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
	"github.com/safal207/Liminal-Rail-Metro/internal/lifetrabridge"
	"github.com/safal207/Liminal-Rail-Metro/internal/metro"
)

type trial struct {
	Round       int                       `json:"round"`
	State       adaptive.HostState        `json:"state"`
	Context     adaptive.Context          `json:"context"`
	ContextKey  string                    `json:"context_key"`
	Packet      metro.Packet              `json:"packet"`
	Route       metro.Route               `json:"route"`
	Result      map[string]any            `json:"result"`
	Receipt     metro.Receipt             `json:"receipt"`
	Observation lifetrabridge.Observation `json:"lifetra_observation"`
	Experience  adaptive.Experience       `json:"experience"`
}

type proof struct {
	Protocol    string                                    `json:"protocol"`
	Environment adaptive.HostState                        `json:"environment"`
	Trials      []trial                                   `json:"trials"`
	Contexts    map[string]map[string]adaptive.ActionStat `json:"context_stats"`
	Claim       string                                    `json:"claim"`
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

	policy, err := adaptive.NewContextualUCB1Policy(actions, 0.9)
	if err != nil {
		panic(err)
	}

	const rounds = 12
	const iterations = 80000
	const payloadBytes = 2048
	const workload = "sha256-2k"
	trials := make([]trial, 0, rounds)
	contextStats := map[string]map[string]adaptive.ActionStat{}
	previousReceiptRef := ""
	previousExperienceRef := ""

	for round := 1; round <= rounds; round++ {
		state := adaptive.SenseHost()
		ctx, err := adaptive.ContextFromHost(workload, state)
		if err != nil {
			panic(err)
		}
		decision, err := policy.Choose(ctx)
		if err != nil {
			panic(err)
		}
		actionID := fmt.Sprintf("hardware-adaptive-%02d", round)
		experienceID := "experience-" + actionID
		resultRef := "adaptive-experience://" + experienceID

		packet := metro.NewPacket(actionID, "liminal-adaptive-runtime", "Improve bounded CPU execution choice from measured evidence.", metro.Action{Kind: "hardware.cpu.sha256", Inputs: map[string]any{"workload": workload, "iterations": iterations, "payload_bytes": payloadBytes, "context_key": decision.ContextKey}}, actions)
		packet.PreviousReceiptRef = previousReceiptRef
		packet.ContextRefs = []string{"adaptive-context://" + decision.ContextKey}
		if previousExperienceRef != "" {
			packet.ContextRefs = append(packet.ContextRefs, previousExperienceRef)
		}

		route := metro.Route{Protocol: metro.RouteProtocol, RouteID: "route-" + actionID, ActionID: actionID, RouterID: "liminal-contextual-ucb1", DecisionMode: "adaptive-contextual", SelectedTarget: decision.Decision.Action, Candidates: []metro.Candidate{{Target: decision.Decision.Action, Score: 1}}, PolicyRef: "policy://adaptive/contextual-ucb1/v0.2", DecidedAt: metro.NowISO()}

		workers := mustWorkers(decision.Decision.Action)
		duration := cpuProof(workers, iterations, payloadBytes)
		mib := float64(iterations*payloadBytes) / (1024 * 1024)
		throughput := mib / duration.Seconds()
		reward := throughput
		result := map[string]any{"selected_action": decision.Decision.Action, "context_key": decision.ContextKey, "duration_ms": float64(duration.Microseconds()) / 1000, "throughput_mib_s": throughput, "reward": reward, "reward_unit": "MiB/s"}

		receipt, err := metro.MakeSuccessReceipt(packet, route, result, resultRef)
		if err != nil {
			panic(err)
		}
		if err := policy.Observe(ctx, decision.Decision.Action, reward); err != nil {
			panic(err)
		}
		after, err := policy.SnapshotContext(ctx)
		if err != nil {
			panic(err)
		}
		experience, err := adaptive.NewExperience(adaptive.ExperienceInput{ExperienceID: experienceID, ActionID: actionID, RouteID: route.RouteID, ReceiptID: receipt.ReceiptID, ReceiptResultHash: receipt.ResultHash, Context: ctx, SelectedAction: decision.Decision.Action, Reward: reward, RewardUnit: "MiB/s", PolicyStatsBefore: decision.Decision.Stats, PolicyStatsAfter: after, PreviousExperienceRef: previousExperienceRef})
		if err != nil {
			panic(err)
		}
		observation, err := lifetrabridge.ReceiptToAdaptiveObservation(receipt, previousExperienceRef)
		if err != nil {
			panic(err)
		}

		trials = append(trials, trial{Round: round, State: state, Context: ctx, ContextKey: decision.ContextKey, Packet: packet, Route: route, Result: result, Receipt: receipt, Observation: observation, Experience: experience})
		contextStats[decision.ContextKey] = after
		previousReceiptRef = "metro-receipt://" + receipt.ReceiptID
		previousExperienceRef = experience.Ref()
		fmt.Printf("round=%02d context=%s action=%-9s throughput=%8.1f MiB/s\n", round, decision.ContextKey, decision.Decision.Action, throughput)
	}

	out := proof{Protocol: "liminal.hardware-adaptive-proof.v0.2", Environment: adaptive.SenseHost(), Trials: trials, Contexts: contextStats, Claim: "The bounded runtime selected only allow-listed worker counts, separated learned evidence by coarse workload/runtime context, measured real CPU throughput, updated the context-local policy, bound the measured result to a Metro receipt, and carried the adaptive experience reference into the Lifetra observation proof chain. This does not modify model weights or claim globally optimal hardware scheduling."}
	b, err := json.MarshalIndent(out, "", "  ")
	if err != nil {
		panic(err)
	}
	if err := os.WriteFile("hardware-adaptive-proof-v0.2.json", b, 0o644); err != nil {
		panic(err)
	}
	fmt.Println("proof=hardware-adaptive-proof-v0.2.json")
}

func mustWorkers(action string) int {
	parts := strings.Split(action, "=")
	if len(parts) != 2 || parts[0] != "workers" {
		panic("invalid bounded action: " + action)
	}
	n, err := strconv.Atoi(parts[1])
	if err != nil || n < 1 {
		panic("invalid worker count: " + action)
	}
	return n
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
