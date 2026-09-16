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
	"github.com/safal207/Liminal-Rail-Metro/internal/deviceattest"
	"github.com/safal207/Liminal-Rail-Metro/internal/lifetrabridge"
	"github.com/safal207/Liminal-Rail-Metro/internal/metro"
)

type trial struct {
	Round       int                              `json:"round"`
	Context     adaptive.Context                 `json:"context"`
	ContextKey  string                           `json:"context_key"`
	Binding     deviceattest.ProviderBinding     `json:"provider_binding"`
	Attestation deviceattest.ProviderAttestation `json:"provider_attestation"`
	Result      map[string]any                   `json:"result"`
	Receipt     metro.Receipt                    `json:"receipt"`
	Experience  adaptive.Experience              `json:"experience"`
	Apply       adaptive.ApplyResult             `json:"apply"`
	Observation lifetrabridge.Observation        `json:"lifetra_observation"`
}

type flags struct {
	ProviderInterfaceUsed        bool `json:"provider_interface_used"`
	SoftwareAssuranceHonest      bool `json:"software_assurance_honest"`
	ProviderRoundTripVerified    bool `json:"provider_roundtrip_verified"`
	StaleBindingRejected         bool `json:"stale_binding_rejected"`
	WrongProviderKeyRejected     bool `json:"wrong_provider_key_rejected"`
	RestartStatePreserved        bool `json:"restart_state_preserved"`
	GenericProviderRefsPresent   bool `json:"generic_provider_refs_present"`
	ProviderMetadataReceiptBound bool `json:"provider_metadata_receipt_bound"`
}

type proof struct {
	Protocol   string                           `json:"protocol"`
	Descriptor deviceattest.ProviderDescriptor  `json:"provider_descriptor"`
	Anchor     deviceattest.ProviderTrustAnchor `json:"provider_anchor"`
	Evidence   deviceattest.ProviderEvidence    `json:"provider_evidence"`
	Journal    string                           `json:"journal_path"`
	Trials     []trial                          `json:"trials"`
	Flags      flags                            `json:"flags"`
	Claim      string                           `json:"claim"`
}

func main() {
	maxWorkers := runtime.NumCPU()
	if maxWorkers > 4 {
		maxWorkers = 4
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
	authorityKey := deterministicKey("authority")
	signed, err := adaptive.SignAuthorityGrant(grant, "local-v07-issuer", authorityKey)
	must(err)
	chain, err := adaptive.NewRootAuthorityChain(signed)
	must(err)

	software, err := deviceattest.NewSoftwareProvider(deterministicKey("provider"))
	must(err)
	var provider deviceattest.Provider = software

	const journal = "hardware-adaptive-v0.7.journal.jsonl"
	_ = os.Remove(journal)
	learner, anchor, evidence, err := adaptive.EnrollProviderBoundLearner(journal, chain, provider, 0.9)
	must(err)

	descriptor := provider.Descriptor()
	descriptorHash, err := descriptor.Hash()
	must(err)

	trials := make([]trial, 0, 6)
	previousReceiptRef := ""
	previousExperienceRef := ""
	for round := 1; round <= 3; round++ {
		tr := runRound(learner, anchor, round, previousReceiptRef, previousExperienceRef)
		trials = append(trials, tr)
		previousReceiptRef = "metro-receipt://" + tr.Receipt.ReceiptID
		previousExperienceRef = tr.Experience.Ref()
	}

	stale := trials[0]
	_, staleErr := learner.Apply(stale.Receipt, stale.Result, stale.Experience, stale.Attestation, stale.Binding)

	lastContext := trials[len(trials)-1].Context
	beforeRestart, err := learner.SnapshotContext(lastContext)
	must(err)
	learner, err = adaptive.OpenProviderBoundLearner(journal, chain, provider, anchor, 0.9)
	must(err)
	afterRestart, err := learner.SnapshotContext(lastContext)
	must(err)

	wrongProvider, err := deviceattest.NewSoftwareProvider(deterministicKey("wrong-provider"))
	must(err)
	_, wrongProviderErr := adaptive.OpenProviderBoundLearner(journal, chain, wrongProvider, anchor, 0.9)

	for round := 4; round <= 6; round++ {
		tr := runRound(learner, anchor, round, previousReceiptRef, previousExperienceRef)
		trials = append(trials, tr)
		previousReceiptRef = "metro-receipt://" + tr.Receipt.ReceiptID
		previousExperienceRef = tr.Experience.Ref()
	}

	refsPresent := genericRefsPresent(trials, descriptorHash, anchor.AnchorHash)
	metadataBound := true
	for _, tr := range trials {
		if tr.Result["attestation_provider_id"] != descriptor.ProviderID ||
			tr.Result["attestation_descriptor_hash"] != descriptorHash ||
			tr.Result["provider_anchor_hash"] != anchor.AnchorHash ||
			tr.Result["provider_attestation_hash"] != tr.Attestation.AttestationHash {
			metadataBound = false
			break
		}
	}

	proofFlags := flags{
		ProviderInterfaceUsed:        descriptor.ProviderID != "",
		SoftwareAssuranceHonest:      descriptor.AssuranceLevel == deviceattest.AssuranceSoftwareBound && !descriptor.HardwareBacked && !descriptor.RemoteVerifiable,
		ProviderRoundTripVerified:    len(trials) == 6,
		StaleBindingRejected:         staleErr != nil,
		WrongProviderKeyRejected:     wrongProviderErr != nil,
		RestartStatePreserved:        sameStats(beforeRestart, afterRestart),
		GenericProviderRefsPresent:   refsPresent,
		ProviderMetadataReceiptBound: metadataBound,
	}

	out := proof{
		Protocol:   "liminal.hardware-adaptive-proof.v0.7",
		Descriptor: descriptor,
		Anchor:     anchor,
		Evidence:   evidence,
		Journal:    journal,
		Trials:     trials,
		Flags:      proofFlags,
		Claim:      "The adaptive runtime executed through a provider-neutral attestation interface. The software provider declared software-bound assurance with hardware_backed=false and remote_verifiable=false; each reward was bound to provider descriptor, enrolled anchor, evidence, and attestation hashes before Metro receipt creation. Reusing a stale provider binding and reopening the same anchor with a different provider key failed closed. This proves provider separation and software-provider continuity, not availability or security of TPM/TEE/cloud providers.",
	}
	b, err := json.MarshalIndent(out, "", "  ")
	must(err)
	must(os.WriteFile("hardware-adaptive-proof-v0.7.json", b, 0o644))
	seq, head := learner.JournalHead()
	fmt.Printf("provider=%s assurance=%s hardware_backed=%v remote_verifiable=%v descriptor=%s\n", descriptor.ProviderID, descriptor.AssuranceLevel, descriptor.HardwareBacked, descriptor.RemoteVerifiable, descriptorHash)
	fmt.Printf("stale_rejected=%v wrong_provider_rejected=%v restart_preserved=%v refs=%v metadata_bound=%v\n", proofFlags.StaleBindingRejected, proofFlags.WrongProviderKeyRejected, proofFlags.RestartStatePreserved, proofFlags.GenericProviderRefsPresent, proofFlags.ProviderMetadataReceiptBound)
	fmt.Printf("journal_head=%s sequence=%d\n", head, seq)
	fmt.Println("proof=hardware-adaptive-proof-v0.7.json")
}

func runRound(learner *adaptive.ProviderBoundLearner, anchor deviceattest.ProviderTrustAnchor, round int, previousReceiptRef, previousExperienceRef string) trial {
	const iterations = 80000
	const payloadBytes = 2048
	const workload = "sha256-2k"

	state := adaptive.SenseHost()
	ctx, err := adaptive.ContextFromHost(workload, state)
	must(err)
	decision, err := learner.Choose(ctx)
	must(err)
	nonce, err := deviceattest.RandomNonce()
	must(err)
	att, binding, err := learner.IssueAttestation(nonce, workload, decision.ContextKey)
	must(err)
	signed := learner.CurrentSignedGrant()
	actionID := fmt.Sprintf("hardware-adaptive-v07-%02d", round)
	experienceID := "experience-" + actionID

	packet := metro.NewPacket(actionID, "liminal-provider-adaptive-runtime", "Execute bounded CPU work through a provider-neutral attestation contract.", metro.Action{Kind: "hardware.cpu.sha256", Inputs: map[string]any{"workload": workload, "context_key": decision.ContextKey, "provider_id": learner.Descriptor().ProviderID}}, signed.Grant.AllowedActions)
	packet.PreviousReceiptRef = previousReceiptRef
	packet.ContextRefs = []string{"adaptive-context://" + decision.ContextKey, signed.Ref(), "adaptive-attestation-provider://sha256/" + att.DescriptorHash}
	if previousExperienceRef != "" {
		packet.ContextRefs = append(packet.ContextRefs, previousExperienceRef)
	}
	route := metro.Route{Protocol: metro.RouteProtocol, RouteID: "route-" + actionID, ActionID: actionID, RouterID: "liminal-provider-ucb1", DecisionMode: "adaptive-provider-bound", SelectedTarget: decision.Decision.Action, Candidates: []metro.Candidate{{Target: decision.Decision.Action, Score: 1}}, PolicyRef: "policy://adaptive/provider/v0.7/" + att.DescriptorHash, DecidedAt: metro.NowISO()}

	workers := mustWorkers(decision.Decision.Action)
	duration := cpuProof(workers, iterations, payloadBytes)
	mib := float64(iterations*payloadBytes) / (1024 * 1024)
	throughput := mib / duration.Seconds()
	baseResult := map[string]any{"selected_action": decision.Decision.Action, "context_key": decision.ContextKey, "duration_ms": float64(duration.Microseconds()) / 1000, "throughput_mib_s": throughput, "reward": throughput, "reward_unit": "MiB/s"}
	result, err := adaptive.BindSignedAuthorityResult(baseResult, signed)
	must(err)
	result, err = adaptive.BindProviderAttestationResult(result, att, anchor)
	must(err)

	receipt, err := metro.MakeSuccessReceipt(packet, route, result, "adaptive-experience://"+experienceID)
	must(err)
	predicted, err := adaptive.PredictStatsAfter(decision.Decision.Stats, decision.Decision.Action, throughput)
	must(err)
	exp, err := adaptive.NewExperience(adaptive.ExperienceInput{ExperienceID: experienceID, ActionID: actionID, RouteID: route.RouteID, ReceiptID: receipt.ReceiptID, ReceiptResultHash: receipt.ResultHash, Context: ctx, SelectedAction: decision.Decision.Action, Reward: throughput, RewardUnit: "MiB/s", PolicyStatsBefore: decision.Decision.Stats, PolicyStatsAfter: predicted, PreviousExperienceRef: previousExperienceRef})
	must(err)
	applied, err := learner.Apply(receipt, result, exp, att, binding)
	must(err)
	observation, err := lifetrabridge.ReceiptToProviderAttestedObservation(receipt, previousExperienceRef, applied.EntryHash, signed.Grant.AuthorityHash, signed.SignedAuthorityHash, signed.IssuerKeyID, "", att.DescriptorHash, anchor.AnchorHash, att.EvidenceHash, att.AttestationHash)
	must(err)

	fmt.Printf("round=%02d action=%-9s throughput=%8.1f MiB/s seq=%02d provider=%s\n", round, decision.Decision.Action, throughput, applied.Sequence, att.Descriptor.ProviderID)
	return trial{Round: round, Context: ctx, ContextKey: decision.ContextKey, Binding: binding, Attestation: att, Result: result, Receipt: receipt, Experience: exp, Apply: applied, Observation: observation}
}

func genericRefsPresent(trials []trial, descriptorHash, anchorHash string) bool {
	for _, tr := range trials {
		wants := []string{
			lifetrabridge.AttestationProviderRefPrefix + descriptorHash,
			lifetrabridge.ProviderAnchorRefPrefix + anchorHash,
			lifetrabridge.ProviderEvidenceRefPrefix + tr.Attestation.EvidenceHash,
			lifetrabridge.ProviderAttestationRefPrefix + tr.Attestation.AttestationHash,
		}
		for _, want := range wants {
			found := false
			for _, ref := range tr.Observation.ProofRefs {
				if ref == want {
					found = true
					break
				}
			}
			if !found {
				return false
			}
		}
	}
	return true
}

func deterministicKey(label string) ed25519.PrivateKey {
	seed := sha256.Sum256([]byte("liminal-v0.7-demo-only:" + label))
	return ed25519.NewKeyFromSeed(seed[:])
}

func mustWorkers(action string) int {
	parts := strings.Split(action, "=")
	if len(parts) != 2 || parts[0] != "workers" {
		panic("invalid worker action: " + action)
	}
	n, err := strconv.Atoi(parts[1])
	if err != nil || n < 1 {
		panic("invalid worker action: " + action)
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
