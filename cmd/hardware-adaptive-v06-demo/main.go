package main

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
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
	Round       int                       `json:"round"`
	Nonce       string                    `json:"challenge_nonce"`
	State       adaptive.HostState        `json:"state"`
	Context     adaptive.Context          `json:"context"`
	ContextKey  string                    `json:"context_key"`
	Attestation deviceattest.Attestation  `json:"device_attestation"`
	Packet      metro.Packet              `json:"packet"`
	Route       metro.Route               `json:"route"`
	Result      map[string]any            `json:"result"`
	Receipt     metro.Receipt             `json:"receipt"`
	Experience  adaptive.Experience       `json:"experience"`
	Apply       adaptive.ApplyResult      `json:"apply"`
	Observation lifetrabridge.Observation `json:"lifetra_observation"`
}

type proofFlags struct {
	DeviceFingerprintStable  bool `json:"device_fingerprint_stable"`
	AttestationVerified      bool `json:"attestation_verified"`
	StaleNonceRejected       bool `json:"stale_nonce_rejected"`
	StaleJournalRejected     bool `json:"stale_journal_rejected"`
	WrongDeviceKeyRejected   bool `json:"wrong_device_key_rejected"`
	TamperedEvidenceRejected bool `json:"tampered_evidence_rejected"`
	RestartStatePreserved    bool `json:"restart_state_preserved"`
	DeviceProofRefsPresent   bool `json:"device_proof_refs_present"`
	SourceValuesHashedOnly   bool `json:"source_values_hashed_only"`
}

type proof struct {
	Protocol          string                   `json:"protocol"`
	Environment       adaptive.HostState       `json:"environment"`
	DeviceEvidence    deviceattest.Evidence    `json:"device_evidence"`
	DeviceTrustAnchor deviceattest.TrustAnchor `json:"device_trust_anchor"`
	AuthorityChain    adaptive.AuthorityChain  `json:"authority_chain"`
	JournalPath       string                   `json:"journal_path"`
	Trials            []trial                  `json:"trials"`
	Flags             proofFlags               `json:"flags"`
	Claim             string                   `json:"claim"`
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
	authorityKey := deterministicPrivateKey("v06-authority")
	signed, err := adaptive.SignAuthorityGrant(grant, "local-v06-issuer", authorityKey)
	must(err)
	chain, err := adaptive.NewRootAuthorityChain(signed)
	must(err)

	devicePublic, devicePrivate, err := deviceattest.GenerateKey()
	must(err)
	_ = devicePublic
	const journalPath = "hardware-adaptive-v0.6.journal.jsonl"
	_ = os.Remove(journalPath)
	learner, anchor, evidence, err := adaptive.EnrollDeviceBoundLearner(journalPath, chain, devicePrivate, 0.9)
	must(err)
	secondEvidence, err := deviceattest.Sense()
	must(err)

	trials := make([]trial, 0, 6)
	previousReceiptRef := ""
	previousExperienceRef := ""
	for round := 1; round <= 3; round++ {
		tr := runRound(learner, round, previousReceiptRef, previousExperienceRef)
		trials = append(trials, tr)
		previousReceiptRef = "metro-receipt://" + tr.Receipt.ReceiptID
		previousExperienceRef = tr.Experience.Ref()
	}

	first := trials[0]
	staleJournalRejected := learner.VerifyAttestation(first.Attestation, first.Nonce, first.Context.Workload, first.ContextKey) != nil

	currentState := adaptive.SenseHost()
	currentContext, err := adaptive.ContextFromHost("sha256-2k", currentState)
	must(err)
	currentKey, err := currentContext.Key()
	must(err)
	seq, head := learner.JournalHead()
	wrongPublic, wrongPrivate, err := deviceattest.GenerateKey()
	must(err)
	_ = wrongPublic
	wrongNonce, err := deviceattest.RandomNonce()
	must(err)
	wrongAtt, err := deviceattest.Sign(secondEvidence, wrongPrivate, wrongNonce, signed.SignedAuthorityHash, signed.IssuerKeyID, seq, head, currentContext.Workload, currentKey)
	must(err)
	wrongDeviceKeyRejected := learner.VerifyAttestation(wrongAtt, wrongNonce, currentContext.Workload, currentKey) != nil

	goodNonce, err := deviceattest.RandomNonce()
	must(err)
	goodAtt, err := learner.IssueAttestation(goodNonce, currentContext.Workload, currentKey)
	must(err)
	mismatchedNonce, err := deviceattest.RandomNonce()
	must(err)
	staleNonceRejected := learner.VerifyAttestation(goodAtt, mismatchedNonce, currentContext.Workload, currentKey) != nil
	tampered := goodAtt
	tampered.RuntimeFingerprint = strings.Repeat("0", 64)
	tamperedEvidenceRejected := learner.VerifyAttestation(tampered, goodNonce, currentContext.Workload, currentKey) != nil
	attestationVerified := learner.VerifyAttestation(goodAtt, goodNonce, currentContext.Workload, currentKey) == nil

	snapshotBefore, err := learner.SnapshotContext(trials[len(trials)-1].Context)
	must(err)
	reopened, err := adaptive.OpenDeviceBoundLearner(journalPath, chain, anchor, devicePrivate, 0.9)
	must(err)
	snapshotAfter, err := reopened.SnapshotContext(trials[len(trials)-1].Context)
	must(err)
	restartStatePreserved := sameStats(snapshotBefore, snapshotAfter)
	learner = reopened

	for round := 4; round <= 6; round++ {
		tr := runRound(learner, round, previousReceiptRef, previousExperienceRef)
		trials = append(trials, tr)
		previousReceiptRef = "metro-receipt://" + tr.Receipt.ReceiptID
		previousExperienceRef = tr.Experience.Ref()
	}

	deviceProofRefsPresent := true
	for _, tr := range trials {
		wants := []string{
			lifetrabridge.DeviceAttestationRefPrefix + tr.Attestation.AttestationHash,
			lifetrabridge.DeviceKeyRefPrefix + tr.Attestation.AttestationKeyID,
			lifetrabridge.DeviceFingerprintRefPrefix + tr.Attestation.DeviceFingerprint,
			lifetrabridge.RuntimeFingerprintRefPrefix + tr.Attestation.RuntimeFingerprint,
		}
		for _, want := range wants {
			if !contains(tr.Observation.ProofRefs, want) {
				deviceProofRefsPresent = false
			}
		}
	}

	flags := proofFlags{
		DeviceFingerprintStable:  evidence.DeviceFingerprint == secondEvidence.DeviceFingerprint && evidence.RuntimeFingerprint == secondEvidence.RuntimeFingerprint,
		AttestationVerified:      attestationVerified,
		StaleNonceRejected:       staleNonceRejected,
		StaleJournalRejected:     staleJournalRejected,
		WrongDeviceKeyRejected:   wrongDeviceKeyRejected,
		TamperedEvidenceRejected: tamperedEvidenceRejected,
		RestartStatePreserved:    restartStatePreserved,
		DeviceProofRefsPresent:   deviceProofRefsPresent,
		SourceValuesHashedOnly:   digestsOnly(evidence),
	}
	out := proof{
		Protocol:          "liminal.hardware-adaptive-proof.v0.6",
		Environment:       adaptive.SenseHost(),
		DeviceEvidence:    evidence,
		DeviceTrustAnchor: anchor,
		AuthorityChain:    chain,
		JournalPath:       journalPath,
		Trials:            trials,
		Flags:             flags,
		Claim:             "The runtime bound each adaptive reward to a fresh challenge-response Ed25519 attestation over a software-observed device fingerprint, runtime fingerprint, current signed authority, journal head, workload, and context. Stale challenges, stale journal anchors, a different device key, and tampered runtime evidence were rejected before learning. Raw machine identifiers were not emitted; only SHA-256 digests were retained. This is software device binding, not TPM/TEE/HSM-backed key protection or remote hardware attestation.",
	}
	b, err := json.MarshalIndent(out, "", "  ")
	must(err)
	must(os.WriteFile("hardware-adaptive-proof-v0.6.json", b, 0o644))
	seqFinal, headFinal := learner.JournalHead()
	fmt.Printf("device_fingerprint=%s runtime_fingerprint=%s key_id=%s\n", evidence.DeviceFingerprint, evidence.RuntimeFingerprint, anchor.AttestationKeyID)
	fmt.Printf("stable=%v attestation_verified=%v stale_nonce_rejected=%v stale_journal_rejected=%v wrong_key_rejected=%v tampered_rejected=%v restart_preserved=%v refs=%v digests_only=%v\n", flags.DeviceFingerprintStable, flags.AttestationVerified, flags.StaleNonceRejected, flags.StaleJournalRejected, flags.WrongDeviceKeyRejected, flags.TamperedEvidenceRejected, flags.RestartStatePreserved, flags.DeviceProofRefsPresent, flags.SourceValuesHashedOnly)
	fmt.Printf("journal_head=%s sequence=%d\n", headFinal, seqFinal)
	fmt.Println("proof=hardware-adaptive-proof-v0.6.json")
}

func runRound(learner *adaptive.DeviceBoundLearner, round int, previousReceiptRef, previousExperienceRef string) trial {
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
	att, err := learner.IssueAttestation(nonce, workload, decision.ContextKey)
	must(err)
	signed := learner.CurrentSignedGrant()
	actionID := fmt.Sprintf("hardware-adaptive-v06-%02d", round)
	experienceID := "experience-" + actionID

	packet := metro.NewPacket(actionID, "liminal-device-bound-runtime", "Improve bounded CPU execution choice with fresh software device attestation.", metro.Action{Kind: "hardware.cpu.sha256", Inputs: map[string]any{"workload": workload, "iterations": iterations, "payload_bytes": payloadBytes, "context_key": decision.ContextKey, "device_attestation_hash": att.AttestationHash}}, signed.Grant.AllowedActions)
	packet.PreviousReceiptRef = previousReceiptRef
	packet.ContextRefs = []string{"adaptive-context://" + decision.ContextKey, signed.Grant.Ref(), signed.Ref(), "adaptive-device-attestation://sha256/" + att.AttestationHash}
	if previousExperienceRef != "" {
		packet.ContextRefs = append(packet.ContextRefs, previousExperienceRef)
	}

	route := metro.Route{Protocol: metro.RouteProtocol, RouteID: "route-" + actionID, ActionID: actionID, RouterID: "liminal-device-bound-ucb1", DecisionMode: "adaptive-device-bound", SelectedTarget: decision.Decision.Action, Candidates: []metro.Candidate{{Target: decision.Decision.Action, Score: 1}}, PolicyRef: "policy://adaptive/contextual-ucb1/v0.6/" + signed.SignedAuthorityHash, DecidedAt: metro.NowISO()}

	workers := mustWorkers(decision.Decision.Action)
	duration := cpuProof(workers, iterations, payloadBytes)
	mib := float64(iterations*payloadBytes) / (1024 * 1024)
	throughput := mib / duration.Seconds()
	reward := throughput
	baseResult := map[string]any{"selected_action": decision.Decision.Action, "context_key": decision.ContextKey, "duration_ms": float64(duration.Microseconds()) / 1000, "throughput_mib_s": throughput, "reward": reward, "reward_unit": "MiB/s"}
	result, err := adaptive.BindSignedAuthorityResult(baseResult, signed)
	must(err)
	result, err = adaptive.BindDeviceAttestationResult(result, att)
	must(err)

	receipt, err := metro.MakeSuccessReceipt(packet, route, result, "adaptive-experience://"+experienceID)
	must(err)
	predicted, err := adaptive.PredictStatsAfter(decision.Decision.Stats, decision.Decision.Action, reward)
	must(err)
	exp, err := adaptive.NewExperience(adaptive.ExperienceInput{ExperienceID: experienceID, ActionID: actionID, RouteID: route.RouteID, ReceiptID: receipt.ReceiptID, ReceiptResultHash: receipt.ResultHash, Context: ctx, SelectedAction: decision.Decision.Action, Reward: reward, RewardUnit: "MiB/s", PolicyStatsBefore: decision.Decision.Stats, PolicyStatsAfter: predicted, PreviousExperienceRef: previousExperienceRef})
	must(err)
	applied, err := learner.Apply(receipt, result, exp, att, nonce)
	must(err)
	observation, err := lifetrabridge.ReceiptToDeviceAttestedObservation(receipt, previousExperienceRef, applied.EntryHash, signed.Grant.AuthorityHash, signed.SignedAuthorityHash, signed.IssuerKeyID, "", att.AttestationHash, att.AttestationKeyID, att.DeviceFingerprint, att.RuntimeFingerprint)
	must(err)
	fmt.Printf("round=%02d context=%s action=%-9s throughput=%8.1f MiB/s seq=%02d device=%s runtime=%s\n", round, decision.ContextKey, decision.Decision.Action, throughput, applied.Sequence, att.DeviceFingerprint[:12], att.RuntimeFingerprint[:12])
	return trial{Round: round, Nonce: nonce, State: state, Context: ctx, ContextKey: decision.ContextKey, Attestation: att, Packet: packet, Route: route, Result: result, Receipt: receipt, Experience: exp, Apply: applied, Observation: observation}
}

func deterministicPrivateKey(label string) ed25519.PrivateKey {
	seed := sha256.Sum256([]byte("liminal-v0.6-demo-only:" + label))
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

func digestsOnly(e deviceattest.Evidence) bool {
	if len(e.SourceDigests) == 0 {
		return false
	}
	for _, digest := range e.SourceDigests {
		if len(digest) != 64 {
			return false
		}
		if _, err := hex.DecodeString(digest); err != nil {
			return false
		}
	}
	return true
}

func contains(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func must(err error) {
	if err != nil {
		panic(err)
	}
}
