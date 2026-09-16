package main

import (
	"crypto/ed25519"
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
	Epoch       string                    `json:"epoch"`
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

type signedProofFlags struct {
	RootSignatureVerified         bool `json:"root_signature_verified"`
	PreRotationNewActionRejected  bool `json:"pre_rotation_new_action_rejected"`
	PreRotationChangedJournal     bool `json:"pre_rotation_changed_journal"`
	RotationVerified              bool `json:"rotation_verified"`
	RotationAnchorMatched         bool `json:"rotation_anchor_matched"`
	MissingRotationRejected       bool `json:"missing_rotation_rejected"`
	TamperedRotationRejected      bool `json:"tampered_rotation_rejected"`
	UnanchoredSelfSignedRejected  bool `json:"unanchored_self_signed_rejected"`
	IssuerKeyRotated              bool `json:"issuer_key_rotated"`
	NewlyAuthorizedActionExecuted bool `json:"newly_authorized_action_executed"`
	RestartStatePreserved         bool `json:"restart_state_preserved"`
	SignedProofRefsPresent        bool `json:"signed_proof_refs_present"`
}

type proof struct {
	Protocol       string                      `json:"protocol"`
	Environment    adaptive.HostState          `json:"environment"`
	Root           adaptive.AuthorityTrustRoot `json:"trust_root"`
	AuthorityChain adaptive.AuthorityChain     `json:"authority_chain"`
	Rotation       adaptive.AuthorityRotation  `json:"rotation"`
	Epoch1Journal  string                      `json:"epoch1_journal"`
	Epoch2Journal  string                      `json:"epoch2_journal"`
	NewAction      string                      `json:"newly_authorized_action"`
	Epoch1Trials   []trial                     `json:"epoch1_trials"`
	Epoch2Trials   []trial                     `json:"epoch2_trials"`
	Flags          signedProofFlags            `json:"flags"`
	Claim          string                      `json:"claim"`
}

func main() {
	maxWorkers := runtime.NumCPU()
	if maxWorkers > 5 {
		maxWorkers = 5
	}
	if maxWorkers < 1 {
		maxWorkers = 1
	}
	rootActions := make([]string, 0, maxWorkers)
	for i := 1; i <= maxWorkers; i++ {
		rootActions = append(rootActions, fmt.Sprintf("workers=%d", i))
	}
	newAction := fmt.Sprintf("workers=%d", maxWorkers+1)

	rootGrant, err := adaptive.NewAuthorityGrant("local-cpu-runtime", "epoch-001", rootActions)
	must(err)
	rootPrivate := deterministicPrivateKey("liminal-v05-root-issuer")
	rootSigned, err := adaptive.SignAuthorityGrant(rootGrant, "local-root-issuer", rootPrivate)
	must(err)
	rootChain, err := adaptive.NewRootAuthorityChain(rootSigned)
	must(err)

	const epoch1Journal = "hardware-adaptive-v0.5-epoch1.journal.jsonl"
	const epoch2Journal = "hardware-adaptive-v0.5-epoch2.journal.jsonl"
	_ = os.Remove(epoch1Journal)
	_ = os.Remove(epoch2Journal)

	learner1, err := adaptive.OpenChainedAuthorityLearner(epoch1Journal, rootChain, 0.9)
	must(err)

	var epoch1Trials []trial
	previousReceiptRef := ""
	previousExperienceRef := ""
	for round := 1; round <= 6; round++ {
		tr := runRound(learner1, "epoch-001", round, previousReceiptRef, previousExperienceRef)
		epoch1Trials = append(epoch1Trials, tr)
		previousReceiptRef = "metro-receipt://" + tr.Receipt.ReceiptID
		previousExperienceRef = tr.Experience.Ref()
	}

	seqBeforeAttack, headBeforeAttack := learner1.JournalHead()
	lastContext := epoch1Trials[len(epoch1Trials)-1].Context
	attackErr := attemptUnauthorized(learner1, lastContext, newAction)
	seqAfterAttack, headAfterAttack := learner1.JournalHead()

	nextActions := append(append([]string(nil), rootActions...), newAction)
	nextGrant, err := adaptive.NewAuthorityGrant(rootGrant.AuthorityID, "epoch-002", nextActions)
	must(err)
	nextPrivate := deterministicPrivateKey("liminal-v05-next-issuer")
	nextSigned, err := adaptive.SignAuthorityGrant(nextGrant, "local-next-issuer", nextPrivate)
	must(err)

	learner2, rotation, err := learner1.Rotate(nextSigned, epoch2Journal, rootPrivate, 0.9)
	must(err)
	finalChain := learner2.Chain()
	rotationVerified := rotation.Verify(rootSigned, nextSigned) == nil && finalChain.Verify() == nil
	rotationAnchorMatched := rotation.SourceJournalSequence == seqBeforeAttack && rotation.SourceJournalHead == headBeforeAttack

	missingRotation := rootChain
	missingRotation.Grants = append(missingRotation.Grants, nextSigned)
	_, missingRotationErr := adaptive.OpenChainedAuthorityLearner(epoch2Journal, missingRotation, 0.9)

	tamperedChain := learner2.Chain()
	tamperedChain.Rotations[0].SourceJournalHead = strings.Repeat("0", 64)
	tamperedRotationErr := tamperedChain.Verify()

	attackerGrant, err := adaptive.NewAuthorityGrant(rootGrant.AuthorityID, "epoch-attacker", nextActions)
	must(err)
	attackerSigned, err := adaptive.SignAuthorityGrant(attackerGrant, "attacker", deterministicPrivateKey("attacker"))
	must(err)
	unanchored := adaptive.AuthorityChain{Protocol: adaptive.AuthorityChainProtocol, Root: finalChain.Root, Grants: []adaptive.SignedAuthorityGrant{attackerSigned}}
	unanchoredErr := unanchored.Verify()

	var epoch2Trials []trial
	newActionExecuted := false
	for round := 1; round <= len(nextActions); round++ {
		tr := runRound(learner2, "epoch-002", round, previousReceiptRef, previousExperienceRef)
		epoch2Trials = append(epoch2Trials, tr)
		if tr.Route.SelectedTarget == newAction {
			newActionExecuted = true
		}
		previousReceiptRef = "metro-receipt://" + tr.Receipt.ReceiptID
		previousExperienceRef = tr.Experience.Ref()
	}

	snapshotBefore, err := learner2.SnapshotContext(epoch2Trials[len(epoch2Trials)-1].Context)
	must(err)
	reopened2, err := adaptive.OpenChainedAuthorityLearner(epoch2Journal, learner2.Chain(), 0.9)
	must(err)
	snapshotAfter, err := reopened2.SnapshotContext(epoch2Trials[len(epoch2Trials)-1].Context)
	must(err)

	proofRefsPresent := signedRefsPresent(epoch1Trials, rootSigned, "") && signedRefsPresent(epoch2Trials, nextSigned, rotation.RotationHash)
	rootTrust, err := adaptive.NewAuthorityTrustRoot(rootSigned)
	must(err)

	flags := signedProofFlags{
		RootSignatureVerified:         rootSigned.SelfVerify() == nil,
		PreRotationNewActionRejected:  attackErr != nil,
		PreRotationChangedJournal:     seqBeforeAttack != seqAfterAttack || headBeforeAttack != headAfterAttack,
		RotationVerified:              rotationVerified,
		RotationAnchorMatched:         rotationAnchorMatched,
		MissingRotationRejected:       missingRotationErr != nil,
		TamperedRotationRejected:      tamperedRotationErr != nil,
		UnanchoredSelfSignedRejected:  unanchoredErr != nil,
		IssuerKeyRotated:              rootSigned.IssuerKeyID != nextSigned.IssuerKeyID,
		NewlyAuthorizedActionExecuted: newActionExecuted,
		RestartStatePreserved:         sameStats(snapshotBefore, snapshotAfter),
		SignedProofRefsPresent:        proofRefsPresent,
	}

	out := proof{
		Protocol:       "liminal.hardware-adaptive-proof.v0.5",
		Environment:    adaptive.SenseHost(),
		Root:           rootTrust,
		AuthorityChain: finalChain,
		Rotation:       rotation,
		Epoch1Journal:  epoch1Journal,
		Epoch2Journal:  epoch2Journal,
		NewAction:      newAction,
		Epoch1Trials:   epoch1Trials,
		Epoch2Trials:   epoch2Trials,
		Flags:          flags,
		Claim:          "Under one pinned local trust root, the runtime accepted an Ed25519-signed authority grant, rejected a newly requested action before rotation without changing the journal, accepted a new issuer key and expanded allow-list only through a rotation record signed by the prior trusted issuer and anchored to the actual prior journal head, then executed the newly authorized action under the new epoch. Each learned result was bound to the exact signed authority and Lifetra proof refs carried the signed grant and signer identity; rotated-epoch observations also carried the rotation hash. This is a local cryptographic authorization proof, not remote attestation, PKI, hardware-rooted identity, or automatic safety of newly authorized actions.",
	}
	b, err := json.MarshalIndent(out, "", "  ")
	must(err)
	must(os.WriteFile("hardware-adaptive-proof-v0.5.json", b, 0o644))

	fmt.Printf("root_signed_authority=%s\n", rootSigned.SignedAuthorityHash)
	fmt.Printf("rotation_hash=%s\n", rotation.RotationHash)
	fmt.Printf("next_signed_authority=%s\n", nextSigned.SignedAuthorityHash)
	fmt.Printf("pre_rotation_rejected=%v journal_changed=%v rotation_verified=%v anchor_matched=%v missing_rotation_rejected=%v tampered_rotation_rejected=%v unanchored_rejected=%v key_rotated=%v new_action_executed=%v restart_preserved=%v proof_refs=%v\n",
		flags.PreRotationNewActionRejected, flags.PreRotationChangedJournal, flags.RotationVerified, flags.RotationAnchorMatched, flags.MissingRotationRejected, flags.TamperedRotationRejected, flags.UnanchoredSelfSignedRejected, flags.IssuerKeyRotated, flags.NewlyAuthorizedActionExecuted, flags.RestartStatePreserved, flags.SignedProofRefsPresent)
	seq1, head1 := learner1.JournalHead()
	seq2, head2 := reopened2.JournalHead()
	fmt.Printf("epoch1_journal_head=%s sequence=%d\n", head1, seq1)
	fmt.Printf("epoch2_journal_head=%s sequence=%d\n", head2, seq2)
	fmt.Println("proof=hardware-adaptive-proof-v0.5.json")
}

func runRound(learner *adaptive.ChainedAuthorityLearner, epoch string, round int, previousReceiptRef, previousExperienceRef string) trial {
	const iterations = 80000
	const payloadBytes = 2048
	const workload = "sha256-2k"

	state := adaptive.SenseHost()
	ctx, err := adaptive.ContextFromHost(workload, state)
	must(err)
	decision, err := learner.Choose(ctx)
	must(err)
	signed := learner.CurrentSignedGrant()
	actionID := fmt.Sprintf("hardware-adaptive-v05-%s-%02d", strings.TrimPrefix(epoch, "epoch-"), round)
	experienceID := "experience-" + actionID
	rotationHash := ""
	if latest := learner.Chain().LatestRotation(); latest != nil {
		rotationHash = latest.RotationHash
	}

	packet := metro.NewPacket(actionID, "liminal-signed-authority-runtime", "Improve bounded CPU execution choice under a cryptographically chained authority epoch.", metro.Action{Kind: "hardware.cpu.sha256", Inputs: map[string]any{"workload": workload, "iterations": iterations, "payload_bytes": payloadBytes, "context_key": decision.ContextKey, "signed_authority_hash": signed.SignedAuthorityHash}}, signed.Grant.AllowedActions)
	packet.PreviousReceiptRef = previousReceiptRef
	packet.ContextRefs = []string{"adaptive-context://" + decision.ContextKey, signed.Grant.Ref(), signed.Ref()}
	if rotationHash != "" {
		packet.ContextRefs = append(packet.ContextRefs, "adaptive-authority-rotation://sha256/"+rotationHash)
	}
	if previousExperienceRef != "" {
		packet.ContextRefs = append(packet.ContextRefs, previousExperienceRef)
	}

	route := metro.Route{Protocol: metro.RouteProtocol, RouteID: "route-" + actionID, ActionID: actionID, RouterID: "liminal-signed-authority-ucb1", DecisionMode: "adaptive-signed-authority", SelectedTarget: decision.Decision.Action, Candidates: []metro.Candidate{{Target: decision.Decision.Action, Score: 1}}, PolicyRef: "policy://adaptive/contextual-ucb1/v0.5/" + signed.SignedAuthorityHash, DecidedAt: metro.NowISO()}

	workers := mustWorkers(decision.Decision.Action)
	duration := cpuProof(workers, iterations, payloadBytes)
	mib := float64(iterations*payloadBytes) / (1024 * 1024)
	throughput := mib / duration.Seconds()
	reward := throughput
	baseResult := map[string]any{"selected_action": decision.Decision.Action, "context_key": decision.ContextKey, "duration_ms": float64(duration.Microseconds()) / 1000, "throughput_mib_s": throughput, "reward": reward, "reward_unit": "MiB/s"}
	result, err := adaptive.BindSignedAuthorityResult(baseResult, signed)
	must(err)

	receipt, err := metro.MakeSuccessReceipt(packet, route, result, "adaptive-experience://"+experienceID)
	must(err)
	predicted, err := adaptive.PredictStatsAfter(decision.Decision.Stats, decision.Decision.Action, reward)
	must(err)
	exp, err := adaptive.NewExperience(adaptive.ExperienceInput{ExperienceID: experienceID, ActionID: actionID, RouteID: route.RouteID, ReceiptID: receipt.ReceiptID, ReceiptResultHash: receipt.ResultHash, Context: ctx, SelectedAction: decision.Decision.Action, Reward: reward, RewardUnit: "MiB/s", PolicyStatsBefore: decision.Decision.Stats, PolicyStatsAfter: predicted, PreviousExperienceRef: previousExperienceRef})
	must(err)
	applied, err := learner.Apply(receipt, result, exp)
	must(err)
	observation, err := lifetrabridge.ReceiptToSignedAuthorityObservation(receipt, previousExperienceRef, applied.EntryHash, signed.Grant.AuthorityHash, signed.SignedAuthorityHash, signed.IssuerKeyID, rotationHash)
	must(err)

	fmt.Printf("epoch=%s round=%02d action=%-9s throughput=%8.1f MiB/s seq=%02d signer=%s\n", epoch, round, decision.Decision.Action, throughput, applied.Sequence, signed.IssuerKeyID[:12])
	return trial{Epoch: epoch, Round: round, State: state, Context: ctx, ContextKey: decision.ContextKey, Packet: packet, Route: route, Result: result, Receipt: receipt, Experience: exp, Apply: applied, Observation: observation}
}

func attemptUnauthorized(learner *adaptive.ChainedAuthorityLearner, ctx adaptive.Context, unauthorized string) error {
	signed := learner.CurrentSignedGrant()
	key, err := ctx.Key()
	if err != nil {
		return err
	}
	baseResult := map[string]any{"selected_action": unauthorized, "context_key": key, "reward": 999999.0, "reward_unit": "MiB/s"}
	result, err := adaptive.BindSignedAuthorityResult(baseResult, signed)
	if err != nil {
		return err
	}
	actionID := "hardware-adaptive-v05-pre-rotation-unauthorized"
	experienceID := "experience-" + actionID
	packet := metro.NewPacket(actionID, "attack-simulation", "Attempt learning a future-epoch action before signed rotation.", metro.Action{Kind: "hardware.cpu.sha256", Inputs: map[string]any{"context_key": key}}, []string{unauthorized})
	route := metro.Route{Protocol: metro.RouteProtocol, RouteID: "route-" + actionID, ActionID: actionID, RouterID: "attack-simulation", DecisionMode: "forged", SelectedTarget: unauthorized, DecidedAt: metro.NowISO()}
	receipt, err := metro.MakeSuccessReceipt(packet, route, result, "adaptive-experience://"+experienceID)
	if err != nil {
		return err
	}
	exp := adaptive.Experience{Protocol: adaptive.ExperienceProtocol, ExperienceID: experienceID, ActionID: actionID, RouteID: route.RouteID, ReceiptID: receipt.ReceiptID, ReceiptResultHash: receipt.ResultHash, Context: ctx, ContextKey: key, SelectedAction: unauthorized, Reward: 999999, RewardUnit: "MiB/s", PolicyStatsBefore: map[string]adaptive.ActionStat{}, PolicyStatsAfter: map[string]adaptive.ActionStat{}, MeasuredAt: metro.NowISO()}
	_, err = learner.Apply(receipt, result, exp)
	return err
}

func signedRefsPresent(trials []trial, signed adaptive.SignedAuthorityGrant, rotationHash string) bool {
	wantSigned := lifetrabridge.SignedAuthorityRefPrefix + signed.SignedAuthorityHash
	wantSigner := lifetrabridge.AuthoritySignerRefPrefix + signed.IssuerKeyID
	wantRotation := ""
	if rotationHash != "" {
		wantRotation = lifetrabridge.AuthorityRotationRefPrefix + rotationHash
	}
	for _, tr := range trials {
		seenSigned, seenSigner, seenRotation := false, false, wantRotation == ""
		for _, ref := range tr.Observation.ProofRefs {
			switch ref {
			case wantSigned:
				seenSigned = true
			case wantSigner:
				seenSigner = true
			case wantRotation:
				if wantRotation != "" {
					seenRotation = true
				}
			}
		}
		if !seenSigned || !seenSigner || !seenRotation {
			return false
		}
	}
	return true
}

func deterministicPrivateKey(label string) ed25519.PrivateKey {
	seed := sha256.Sum256([]byte("liminal-v0.5-demo-only:" + label))
	return ed25519.NewKeyFromSeed(seed[:])
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

func sameStats(a, b map[string]adaptive.ActionStat) bool {
	if len(a) != len(b) {
		return false
	}
	for key, av := range a {
		bv, ok := b[key]
		if !ok || av != bv {
			return false
		}
	}
	return true
}

func must(err error) {
	if err != nil {
		panic(err)
	}
}
