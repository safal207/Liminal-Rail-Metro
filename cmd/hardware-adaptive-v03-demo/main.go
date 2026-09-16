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
	Experience  adaptive.Experience       `json:"experience"`
	Apply       adaptive.ApplyResult      `json:"apply"`
	Observation lifetrabridge.Observation `json:"lifetra_observation"`
}

type restartProof struct {
	SequenceBeforeRestart uint64 `json:"sequence_before_restart"`
	SequenceAfterRestart  uint64 `json:"sequence_after_restart"`
	HeadBeforeRestart     string `json:"head_before_restart"`
	HeadAfterRestart      string `json:"head_after_restart"`
	StatePreserved        bool   `json:"state_preserved"`
	DuplicateWasNoop      bool   `json:"duplicate_was_noop"`
	UnverifiedWasRejected bool   `json:"unverified_was_rejected"`
	CountBeforeDuplicate  int    `json:"count_before_duplicate"`
	CountAfterDuplicate   int    `json:"count_after_duplicate"`
}

type proof struct {
	Protocol    string             `json:"protocol"`
	Environment adaptive.HostState `json:"environment"`
	JournalPath string             `json:"journal_path"`
	Trials      []trial            `json:"trials"`
	Restart     restartProof       `json:"restart_proof"`
	Claim       string             `json:"claim"`
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

	const journalPath = "hardware-adaptive-v0.3.journal.jsonl"
	_ = os.Remove(journalPath)

	learner, err := adaptive.OpenDurableLearner(journalPath, actions, 0.9)
	must(err)
	trials := make([]trial, 0, 12)
	previousReceiptRef := ""
	previousExperienceRef := ""

	for round := 1; round <= 6; round++ {
		tr := runRound(learner, actions, round, previousReceiptRef, previousExperienceRef)
		trials = append(trials, tr)
		previousReceiptRef = "metro-receipt://" + tr.Receipt.ReceiptID
		previousExperienceRef = tr.Experience.Ref()
	}

	last := trials[len(trials)-1]
	seqBefore, headBefore := learner.JournalHead()
	snapBefore, err := learner.SnapshotContext(last.Context)
	must(err)

	learner, err = adaptive.OpenDurableLearner(journalPath, actions, 0.9)
	must(err)
	seqAfter, headAfter := learner.JournalHead()
	snapAfter, err := learner.SnapshotContext(last.Context)
	must(err)

	beforeCount := snapAfter[last.Experience.SelectedAction].Count
	dup, err := learner.Apply(last.Receipt, last.Result, last.Experience)
	must(err)
	snapAfterDup, err := learner.SnapshotContext(last.Context)
	must(err)
	afterCount := snapAfterDup[last.Experience.SelectedAction].Count

	unknown := last.Receipt
	unknown.Status = "UNKNOWN"
	_, unknownErr := learner.Apply(unknown, last.Result, last.Experience)
	snapAfterUnknown, err := learner.SnapshotContext(last.Context)
	must(err)

	restart := restartProof{
		SequenceBeforeRestart: seqBefore,
		SequenceAfterRestart:  seqAfter,
		HeadBeforeRestart:     headBefore,
		HeadAfterRestart:      headAfter,
		StatePreserved:        sameStats(snapBefore, snapAfter),
		DuplicateWasNoop:      dup.Duplicate && !dup.Applied && beforeCount == afterCount,
		UnverifiedWasRejected: unknownErr != nil && sameStats(snapAfterDup, snapAfterUnknown),
		CountBeforeDuplicate:  beforeCount,
		CountAfterDuplicate:   afterCount,
	}

	for round := 7; round <= 12; round++ {
		tr := runRound(learner, actions, round, previousReceiptRef, previousExperienceRef)
		trials = append(trials, tr)
		previousReceiptRef = "metro-receipt://" + tr.Receipt.ReceiptID
		previousExperienceRef = tr.Experience.Ref()
	}

	out := proof{
		Protocol:    "liminal.hardware-adaptive-proof.v0.3",
		Environment: adaptive.SenseHost(),
		JournalPath: journalPath,
		Trials:      trials,
		Restart:     restart,
		Claim:       "A bounded contextual policy persisted verified measurement evidence in an fsynced hash-chained journal, rebuilt learned state after restart, treated an already-applied receipt as an idempotent no-op, rejected UNKNOWN receipt evidence before learning, and carried content-addressed measurement plus journal proof references into Lifetra observations. This does not modify model weights or claim crash-proof storage under every filesystem/hardware failure mode.",
	}
	b, err := json.MarshalIndent(out, "", "  ")
	must(err)
	must(os.WriteFile("hardware-adaptive-proof-v0.3.json", b, 0o644))
	fmt.Printf("restart_state_preserved=%v duplicate_noop=%v unverified_rejected=%v\n", restart.StatePreserved, restart.DuplicateWasNoop, restart.UnverifiedWasRejected)
	fmt.Printf("journal_head=%s sequence=%d\n", func() string { _, h := learner.JournalHead(); return h }(), func() uint64 { s, _ := learner.JournalHead(); return s }())
	fmt.Println("proof=hardware-adaptive-proof-v0.3.json")
}

func runRound(learner *adaptive.DurableLearner, actions []string, round int, previousReceiptRef, previousExperienceRef string) trial {
	const iterations = 80000
	const payloadBytes = 2048
	const workload = "sha256-2k"

	state := adaptive.SenseHost()
	ctx, err := adaptive.ContextFromHost(workload, state)
	must(err)
	decision, err := learner.Choose(ctx)
	must(err)
	actionID := fmt.Sprintf("hardware-adaptive-v03-%02d", round)
	experienceID := "experience-" + actionID

	packet := metro.NewPacket(actionID, "liminal-durable-adaptive-runtime", "Improve bounded CPU execution choice from verified durable evidence.", metro.Action{Kind: "hardware.cpu.sha256", Inputs: map[string]any{"workload": workload, "iterations": iterations, "payload_bytes": payloadBytes, "context_key": decision.ContextKey}}, actions)
	packet.PreviousReceiptRef = previousReceiptRef
	packet.ContextRefs = []string{"adaptive-context://" + decision.ContextKey}
	if previousExperienceRef != "" {
		packet.ContextRefs = append(packet.ContextRefs, previousExperienceRef)
	}

	route := metro.Route{Protocol: metro.RouteProtocol, RouteID: "route-" + actionID, ActionID: actionID, RouterID: "liminal-durable-contextual-ucb1", DecisionMode: "adaptive-contextual-durable", SelectedTarget: decision.Decision.Action, Candidates: []metro.Candidate{{Target: decision.Decision.Action, Score: 1}}, PolicyRef: "policy://adaptive/contextual-ucb1/v0.3", DecidedAt: metro.NowISO()}

	workers := mustWorkers(decision.Decision.Action)
	duration := cpuProof(workers, iterations, payloadBytes)
	mib := float64(iterations*payloadBytes) / (1024 * 1024)
	throughput := mib / duration.Seconds()
	reward := throughput
	result := map[string]any{"selected_action": decision.Decision.Action, "context_key": decision.ContextKey, "duration_ms": float64(duration.Microseconds()) / 1000, "throughput_mib_s": throughput, "reward": reward, "reward_unit": "MiB/s"}

	receipt, err := metro.MakeSuccessReceipt(packet, route, result, "adaptive-experience://"+experienceID)
	must(err)
	predicted, err := adaptive.PredictStatsAfter(decision.Decision.Stats, decision.Decision.Action, reward)
	must(err)
	exp, err := adaptive.NewExperience(adaptive.ExperienceInput{ExperienceID: experienceID, ActionID: actionID, RouteID: route.RouteID, ReceiptID: receipt.ReceiptID, ReceiptResultHash: receipt.ResultHash, Context: ctx, SelectedAction: decision.Decision.Action, Reward: reward, RewardUnit: "MiB/s", PolicyStatsBefore: decision.Decision.Stats, PolicyStatsAfter: predicted, PreviousExperienceRef: previousExperienceRef})
	must(err)
	applied, err := learner.Apply(receipt, result, exp)
	must(err)
	observation, err := lifetrabridge.ReceiptToDurableAdaptiveObservation(receipt, previousExperienceRef, applied.EntryHash)
	must(err)

	fmt.Printf("round=%02d context=%s action=%-9s throughput=%8.1f MiB/s seq=%02d\n", round, decision.ContextKey, decision.Decision.Action, throughput, applied.Sequence)
	return trial{Round: round, State: state, Context: ctx, ContextKey: decision.ContextKey, Packet: packet, Route: route, Result: result, Receipt: receipt, Experience: exp, Apply: applied, Observation: observation}
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

func must(err error) {
	if err != nil {
		panic(err)
	}
}

func sameStats(a, b map[string]adaptive.ActionStat) bool {
	if len(a) != len(b) {
		return false
	}
	for k, av := range a {
		bv, ok := b[k]
		if !ok || av != bv {
			return false
		}
	}
	return true
}
