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

type authorityProof struct {
	StatePreserved             bool `json:"state_preserved"`
	UnauthorizedRewardRejected bool `json:"unauthorized_reward_rejected"`
	UnauthorizedChangedJournal bool `json:"unauthorized_changed_journal"`
	ExpandedSameEpochRejected  bool `json:"expanded_same_epoch_rejected"`
	RotatedEpochRejected       bool `json:"rotated_epoch_rejected"`
	AuthorityProofRefPresent   bool `json:"authority_proof_ref_present"`
}

type proof struct {
	Protocol       string                  `json:"protocol"`
	Environment    adaptive.HostState      `json:"environment"`
	Authority      adaptive.AuthorityGrant `json:"authority"`
	JournalPath    string                  `json:"journal_path"`
	Trials         []trial                 `json:"trials"`
	AuthorityProof authorityProof          `json:"authority_proof"`
	Claim          string                  `json:"claim"`
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
	grant, err := adaptive.NewAuthorityGrant("local-cpu-runtime", "epoch-001", actions)
	must(err)

	const journalPath = "hardware-adaptive-v0.4.journal.jsonl"
	_ = os.Remove(journalPath)
	learner, err := adaptive.OpenAuthorityLearner(journalPath, grant, 0.9)
	must(err)

	trials := make([]trial, 0, 12)
	previousReceiptRef := ""
	previousExperienceRef := ""
	for round := 1; round <= 6; round++ {
		tr := runRound(learner, round, previousReceiptRef, previousExperienceRef)
		trials = append(trials, tr)
		previousReceiptRef = "metro-receipt://" + tr.Receipt.ReceiptID
		previousExperienceRef = tr.Experience.Ref()
	}

	last := trials[len(trials)-1]
	snapBefore, err := learner.SnapshotContext(last.Context)
	must(err)
	learner, err = adaptive.OpenAuthorityLearner(journalPath, grant, 0.9)
	must(err)
	snapAfter, err := learner.SnapshotContext(last.Context)
	must(err)
	statePreserved := sameStats(snapBefore, snapAfter)

	seqBeforeAttack, _ := learner.JournalHead()
	unauthorized := fmt.Sprintf("workers=%d", maxWorkers+1)
	unauthorizedErr := attemptUnauthorized(learner, grant, last.Context, unauthorized)
	seqAfterAttack, _ := learner.JournalHead()

	expandedActions := append(append([]string(nil), actions...), unauthorized)
	expandedGrant, err := adaptive.NewAuthorityGrant(grant.AuthorityID, grant.Epoch, expandedActions)
	must(err)
	_, expandedErr := adaptive.OpenAuthorityLearner(journalPath, expandedGrant, 0.9)

	rotatedGrant, err := adaptive.NewAuthorityGrant(grant.AuthorityID, "epoch-002", actions)
	must(err)
	_, rotatedErr := adaptive.OpenAuthorityLearner(journalPath, rotatedGrant, 0.9)

	for round := 7; round <= 12; round++ {
		tr := runRound(learner, round, previousReceiptRef, previousExperienceRef)
		trials = append(trials, tr)
		previousReceiptRef = "metro-receipt://" + tr.Receipt.ReceiptID
		previousExperienceRef = tr.Experience.Ref()
	}

	authorityRefPresent := true
	wantAuthorityRef := grant.Ref()
	for _, tr := range trials {
		found := false
		for _, ref := range tr.Observation.ProofRefs {
			if ref == wantAuthorityRef {
				found = true
				break
			}
		}
		if !found {
			authorityRefPresent = false
			break
		}
	}

	flags := authorityProof{
		StatePreserved:             statePreserved,
		UnauthorizedRewardRejected: unauthorizedErr != nil,
		UnauthorizedChangedJournal: seqBeforeAttack != seqAfterAttack,
		ExpandedSameEpochRejected:  expandedErr != nil,
		RotatedEpochRejected:       rotatedErr != nil,
		AuthorityProofRefPresent:   authorityRefPresent,
	}
	out := proof{
		Protocol:       "liminal.hardware-adaptive-proof.v0.4",
		Environment:    adaptive.SenseHost(),
		Authority:      grant,
		JournalPath:    journalPath,
		Trials:         trials,
		AuthorityProof: flags,
		Claim:          "A durable contextual learner selected and learned only actions named by one content-addressed authority epoch. The authority id, epoch, and allow-list hash were included in the exact measured result hashed by every Metro receipt; unauthorized reward injection, same-epoch allow-list expansion, and implicit epoch rotation were rejected without advancing the journal. Lifetra observations carried the authority content hash as a proof reference. The authority grant is locally trusted content, not a digital signature or remote attestation.",
	}
	b, err := json.MarshalIndent(out, "", "  ")
	must(err)
	must(os.WriteFile("hardware-adaptive-proof-v0.4.json", b, 0o644))
	fmt.Printf("authority_hash=%s\n", grant.AuthorityHash)
	fmt.Printf("state_preserved=%v unauthorized_rejected=%v unauthorized_changed_journal=%v expanded_same_epoch_rejected=%v rotated_epoch_rejected=%v authority_ref_present=%v\n", flags.StatePreserved, flags.UnauthorizedRewardRejected, flags.UnauthorizedChangedJournal, flags.ExpandedSameEpochRejected, flags.RotatedEpochRejected, flags.AuthorityProofRefPresent)
	seq, head := learner.JournalHead()
	fmt.Printf("journal_head=%s sequence=%d\n", head, seq)
	fmt.Println("proof=hardware-adaptive-proof-v0.4.json")
}

func runRound(learner *adaptive.AuthorityLearner, round int, previousReceiptRef, previousExperienceRef string) trial {
	const iterations = 80000
	const payloadBytes = 2048
	const workload = "sha256-2k"

	state := adaptive.SenseHost()
	ctx, err := adaptive.ContextFromHost(workload, state)
	must(err)
	decision, err := learner.Choose(ctx)
	must(err)
	grant := learner.Grant()
	actionID := fmt.Sprintf("hardware-adaptive-v04-%02d", round)
	experienceID := "experience-" + actionID

	packet := metro.NewPacket(actionID, "liminal-authority-adaptive-runtime", "Improve bounded CPU execution choice without expanding the active authority epoch.", metro.Action{Kind: "hardware.cpu.sha256", Inputs: map[string]any{"workload": workload, "iterations": iterations, "payload_bytes": payloadBytes, "context_key": decision.ContextKey, "authority_hash": grant.AuthorityHash}}, grant.AllowedActions)
	packet.PreviousReceiptRef = previousReceiptRef
	packet.ContextRefs = []string{"adaptive-context://" + decision.ContextKey, grant.Ref()}
	if previousExperienceRef != "" {
		packet.ContextRefs = append(packet.ContextRefs, previousExperienceRef)
	}

	route := metro.Route{Protocol: metro.RouteProtocol, RouteID: "route-" + actionID, ActionID: actionID, RouterID: "liminal-authority-contextual-ucb1", DecisionMode: "adaptive-authority-bound", SelectedTarget: decision.Decision.Action, Candidates: []metro.Candidate{{Target: decision.Decision.Action, Score: 1}}, PolicyRef: "policy://adaptive/contextual-ucb1/v0.4/" + grant.AuthorityHash, DecidedAt: metro.NowISO()}

	workers := mustWorkers(decision.Decision.Action)
	duration := cpuProof(workers, iterations, payloadBytes)
	mib := float64(iterations*payloadBytes) / (1024 * 1024)
	throughput := mib / duration.Seconds()
	reward := throughput
	baseResult := map[string]any{"selected_action": decision.Decision.Action, "context_key": decision.ContextKey, "duration_ms": float64(duration.Microseconds()) / 1000, "throughput_mib_s": throughput, "reward": reward, "reward_unit": "MiB/s"}
	result, err := adaptive.BindAuthorityResult(baseResult, grant)
	must(err)

	receipt, err := metro.MakeSuccessReceipt(packet, route, result, "adaptive-experience://"+experienceID)
	must(err)
	predicted, err := adaptive.PredictStatsAfter(decision.Decision.Stats, decision.Decision.Action, reward)
	must(err)
	exp, err := adaptive.NewExperience(adaptive.ExperienceInput{ExperienceID: experienceID, ActionID: actionID, RouteID: route.RouteID, ReceiptID: receipt.ReceiptID, ReceiptResultHash: receipt.ResultHash, Context: ctx, SelectedAction: decision.Decision.Action, Reward: reward, RewardUnit: "MiB/s", PolicyStatsBefore: decision.Decision.Stats, PolicyStatsAfter: predicted, PreviousExperienceRef: previousExperienceRef})
	must(err)
	applied, err := learner.Apply(receipt, result, exp)
	must(err)
	observation, err := lifetrabridge.ReceiptToAuthorityAdaptiveObservation(receipt, previousExperienceRef, applied.EntryHash, grant.AuthorityHash)
	must(err)

	fmt.Printf("round=%02d context=%s action=%-9s throughput=%8.1f MiB/s seq=%02d authority=%s\n", round, decision.ContextKey, decision.Decision.Action, throughput, applied.Sequence, grant.AuthorityHash[:12])
	return trial{Round: round, State: state, Context: ctx, ContextKey: decision.ContextKey, Packet: packet, Route: route, Result: result, Receipt: receipt, Experience: exp, Apply: applied, Observation: observation}
}

func attemptUnauthorized(learner *adaptive.AuthorityLearner, grant adaptive.AuthorityGrant, ctx adaptive.Context, unauthorized string) error {
	key, err := ctx.Key()
	if err != nil {
		return err
	}
	baseResult := map[string]any{"selected_action": unauthorized, "context_key": key, "reward": 999999.0, "reward_unit": "MiB/s"}
	result, err := adaptive.BindAuthorityResult(baseResult, grant)
	if err != nil {
		return err
	}
	actionID := "hardware-adaptive-v04-unauthorized"
	experienceID := "experience-" + actionID
	packet := metro.NewPacket(actionID, "attack-simulation", "Attempt out-of-authority learning.", metro.Action{Kind: "hardware.cpu.sha256", Inputs: map[string]any{"context_key": key}}, []string{unauthorized})
	route := metro.Route{Protocol: metro.RouteProtocol, RouteID: "route-" + actionID, ActionID: actionID, RouterID: "attack-simulation", DecisionMode: "forged", SelectedTarget: unauthorized, DecidedAt: metro.NowISO()}
	receipt, err := metro.MakeSuccessReceipt(packet, route, result, "adaptive-experience://"+experienceID)
	if err != nil {
		return err
	}
	exp := adaptive.Experience{Protocol: adaptive.ExperienceProtocol, ExperienceID: experienceID, ActionID: actionID, RouteID: route.RouteID, ReceiptID: receipt.ReceiptID, ReceiptResultHash: receipt.ResultHash, Context: ctx, ContextKey: key, SelectedAction: unauthorized, Reward: 999999, RewardUnit: "MiB/s", PolicyStatsBefore: map[string]adaptive.ActionStat{}, PolicyStatsAfter: map[string]adaptive.ActionStat{}, MeasuredAt: metro.NowISO()}
	_, err = learner.Apply(receipt, result, exp)
	return err
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
