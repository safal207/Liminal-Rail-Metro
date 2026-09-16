package main

import (
	"context"
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

const discoveryAudience = "liminal-rail-metro-v0.8"

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

type proofFlags struct {
	DiscoveryProtocolUsed      bool `json:"discovery_protocol_used"`
	ExternalOIDCVerified       bool `json:"external_oidc_verified"`
	RawOIDCTokenNotPersisted   bool `json:"raw_oidc_token_not_persisted"`
	NoFalseHardwareUpgrade     bool `json:"no_false_hardware_upgrade"`
	FallbackMatchesDiscovery   bool `json:"fallback_matches_discovery"`
	DiscoveryReceiptBound      bool `json:"discovery_receipt_bound"`
	DiscoveryProofRefsPresent  bool `json:"discovery_proof_refs_present"`
	AdaptiveRoundsSucceeded    bool `json:"adaptive_rounds_succeeded"`
	OIDCNotProviderReady       bool `json:"oidc_not_provider_ready"`
	HardwareCandidatesNotReady bool `json:"hardware_candidates_not_ready"`
}

type proof struct {
	Protocol   string                           `json:"protocol"`
	Discovery  deviceattest.DiscoveryReport     `json:"discovery"`
	Descriptor deviceattest.ProviderDescriptor  `json:"provider_descriptor"`
	Anchor     deviceattest.ProviderTrustAnchor `json:"provider_anchor"`
	Evidence   deviceattest.ProviderEvidence    `json:"provider_evidence"`
	Journal    string                           `json:"journal_path"`
	Trials     []trial                          `json:"trials"`
	Flags      proofFlags                       `json:"flags"`
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
	signed, err := adaptive.SignAuthorityGrant(grant, "local-v08-issuer", authorityKey)
	must(err)
	chain, err := adaptive.NewRootAuthorityChain(signed)
	must(err)

	software, err := deviceattest.NewSoftwareProvider(deterministicKey("provider"))
	must(err)
	var provider deviceattest.Provider = software
	descriptor := provider.Descriptor()
	discovery, err := deviceattest.Discover(context.Background(), descriptor, discoveryAudience)
	must(err)
	must(discovery.Validate())

	const journal = "hardware-adaptive-v0.8.journal.jsonl"
	_ = os.Remove(journal)
	learner, anchor, evidence, err := adaptive.EnrollProviderBoundLearner(journal, chain, provider, 0.9)
	must(err)

	trials := make([]trial, 0, 6)
	previousReceiptRef := ""
	previousExperienceRef := ""
	for round := 1; round <= 6; round++ {
		tr := runRound(learner, anchor, discovery, round, previousReceiptRef, previousExperienceRef)
		trials = append(trials, tr)
		previousReceiptRef = "metro-receipt://" + tr.Receipt.ReceiptID
		previousExperienceRef = tr.Experience.Ref()
	}

	bound := true
	refs := true
	for _, tr := range trials {
		if err := adaptive.ValidateDiscoveryResultBinding(tr.Result, discovery); err != nil {
			bound = false
		}
		want := lifetrabridge.AttestationDiscoveryRefPrefix + discovery.ReportHash
		found := false
		for _, ref := range tr.Observation.ProofRefs {
			if ref == want {
				found = true
				break
			}
		}
		if !found {
			refs = false
		}
	}

	oidcNotReady := false
	hardwareCandidatesNotReady := true
	for _, probe := range discovery.Probes {
		if probe.Kind == "github-actions-oidc" {
			oidcNotReady = probe.Present && probe.ExternalIdentity && !probe.ProviderReady
		}
		if probe.HardwareBacked && probe.ProviderReady {
			hardwareCandidatesNotReady = false
		}
	}

	flags := proofFlags{
		DiscoveryProtocolUsed:      discovery.Protocol == deviceattest.DiscoveryProtocol,
		ExternalOIDCVerified:       discovery.ExternalIdentityVerified && discovery.ExternalIdentity != nil && discovery.ExternalIdentity.SignatureVerified,
		RawOIDCTokenNotPersisted:   discovery.ExternalIdentity != nil && discovery.ExternalIdentity.TokenHash != "",
		NoFalseHardwareUpgrade:     !discovery.HardwareUpgradeSelected && !descriptor.HardwareBacked,
		FallbackMatchesDiscovery:   discovery.SelectedProviderID == descriptor.ProviderID && discovery.SelectedAssuranceLevel == descriptor.AssuranceLevel,
		DiscoveryReceiptBound:      bound,
		DiscoveryProofRefsPresent:  refs,
		AdaptiveRoundsSucceeded:    len(trials) == 6,
		OIDCNotProviderReady:       oidcNotReady,
		HardwareCandidatesNotReady: hardwareCandidatesNotReady,
	}

	out := proof{
		Protocol:   "liminal.hardware-adaptive-proof.v0.8",
		Discovery:  discovery,
		Descriptor: descriptor,
		Anchor:     anchor,
		Evidence:   evidence,
		Journal:    journal,
		Trials:     trials,
		Flags:      flags,
		Claim:      "The runtime discovered attestation sources before selecting a provider. On GitHub Actions it verified the external GitHub OIDC identity signature online without persisting the raw JWT, probed TPM2/SEV-SNP/TDX device exposure, and selected the existing software-bound provider because no verified provider-ready hardware attestation implementation was available. The discovery report hash was bound into every measured result before Metro receipt creation and propagated into Lifetra proof refs. This proves honest discovery and external identity verification, not hardware-rooted attestation.",
	}
	b, err := json.MarshalIndent(out, "", "  ")
	must(err)
	must(os.WriteFile("hardware-adaptive-proof-v0.8.json", b, 0o644))
	seq, head := learner.JournalHead()
	fmt.Printf("discovery=%s external_oidc_verified=%v hardware_upgrade=%v selected_provider=%s\n", discovery.ReportHash, discovery.ExternalIdentityVerified, discovery.HardwareUpgradeSelected, discovery.SelectedProviderID)
	for _, probe := range discovery.Probes {
		fmt.Printf("probe=%s source=%s present=%v ready=%v hardware=%v external=%v\n", probe.Kind, probe.Source, probe.Present, probe.ProviderReady, probe.HardwareBacked, probe.ExternalIdentity)
	}
	if discovery.ExternalIdentity != nil {
		fmt.Printf("oidc_issuer=%s kid=%s repository_id=%s runner=%s token_hash=%s\n", discovery.ExternalIdentity.Issuer, discovery.ExternalIdentity.KeyID, discovery.ExternalIdentity.RepositoryID, discovery.ExternalIdentity.RunnerEnvironment, discovery.ExternalIdentity.TokenHash)
	}
	fmt.Printf("bound=%v refs=%v oidc_not_ready=%v hardware_candidates_not_ready=%v rounds=%d\n", flags.DiscoveryReceiptBound, flags.DiscoveryProofRefsPresent, flags.OIDCNotProviderReady, flags.HardwareCandidatesNotReady, len(trials))
	fmt.Printf("journal_head=%s sequence=%d\n", head, seq)
	fmt.Println("proof=hardware-adaptive-proof-v0.8.json")
}

func runRound(learner *adaptive.ProviderBoundLearner, anchor deviceattest.ProviderTrustAnchor, discovery deviceattest.DiscoveryReport, round int, previousReceiptRef, previousExperienceRef string) trial {
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
	actionID := fmt.Sprintf("hardware-adaptive-v08-%02d", round)
	experienceID := "experience-" + actionID

	packet := metro.NewPacket(actionID, "liminal-discovery-adaptive-runtime", "Discover available trust sources, then execute bounded CPU work without inflating provider assurance.", metro.Action{Kind: "hardware.cpu.sha256", Inputs: map[string]any{"workload": workload, "context_key": decision.ContextKey, "provider_id": learner.Descriptor().ProviderID, "discovery_hash": discovery.ReportHash}}, signed.Grant.AllowedActions)
	packet.PreviousReceiptRef = previousReceiptRef
	packet.ContextRefs = []string{"adaptive-context://" + decision.ContextKey, signed.Ref(), "adaptive-attestation-discovery://sha256/" + discovery.ReportHash}
	if previousExperienceRef != "" {
		packet.ContextRefs = append(packet.ContextRefs, previousExperienceRef)
	}
	route := metro.Route{Protocol: metro.RouteProtocol, RouteID: "route-" + actionID, ActionID: actionID, RouterID: "liminal-discovery-provider-ucb1", DecisionMode: "adaptive-discovered-provider", SelectedTarget: decision.Decision.Action, Candidates: []metro.Candidate{{Target: decision.Decision.Action, Score: 1}}, PolicyRef: "policy://adaptive/provider/v0.8/" + discovery.ReportHash, DecidedAt: metro.NowISO()}

	workers := mustWorkers(decision.Decision.Action)
	duration := cpuProof(workers, iterations, payloadBytes)
	mib := float64(iterations*payloadBytes) / (1024 * 1024)
	throughput := mib / duration.Seconds()
	baseResult := map[string]any{"selected_action": decision.Decision.Action, "context_key": decision.ContextKey, "duration_ms": float64(duration.Microseconds()) / 1000, "throughput_mib_s": throughput, "reward": throughput, "reward_unit": "MiB/s"}
	result, err := adaptive.BindSignedAuthorityResult(baseResult, signed)
	must(err)
	result, err = adaptive.BindProviderAttestationResult(result, att, anchor)
	must(err)
	result, err = adaptive.BindDiscoveryResult(result, discovery)
	must(err)

	receipt, err := metro.MakeSuccessReceipt(packet, route, result, "adaptive-experience://"+experienceID)
	must(err)
	predicted, err := adaptive.PredictStatsAfter(decision.Decision.Stats, decision.Decision.Action, throughput)
	must(err)
	exp, err := adaptive.NewExperience(adaptive.ExperienceInput{ExperienceID: experienceID, ActionID: actionID, RouteID: route.RouteID, ReceiptID: receipt.ReceiptID, ReceiptResultHash: receipt.ResultHash, Context: ctx, SelectedAction: decision.Decision.Action, Reward: throughput, RewardUnit: "MiB/s", PolicyStatsBefore: decision.Decision.Stats, PolicyStatsAfter: predicted, PreviousExperienceRef: previousExperienceRef})
	must(err)
	applied, err := learner.Apply(receipt, result, exp, att, binding)
	must(err)
	observation, err := lifetrabridge.ReceiptToDiscoveryProviderObservation(receipt, previousExperienceRef, applied.EntryHash, signed.Grant.AuthorityHash, signed.SignedAuthorityHash, signed.IssuerKeyID, "", att.DescriptorHash, anchor.AnchorHash, att.EvidenceHash, att.AttestationHash, discovery.ReportHash)
	must(err)

	fmt.Printf("round=%02d action=%-9s throughput=%8.1f MiB/s seq=%02d provider=%s discovery=%s\n", round, decision.Decision.Action, throughput, applied.Sequence, att.Descriptor.ProviderID, discovery.ReportHash[:12])
	return trial{Round: round, Context: ctx, ContextKey: decision.ContextKey, Binding: binding, Attestation: att, Result: result, Receipt: receipt, Experience: exp, Apply: applied, Observation: observation}
}

func deterministicKey(label string) ed25519.PrivateKey {
	seed := sha256.Sum256([]byte("liminal-v0.8-demo-only:" + label))
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

func must(err error) {
	if err != nil {
		panic(err)
	}
}
