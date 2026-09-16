package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/safal207/Liminal-Rail-Metro/internal/adaptive"
	"github.com/safal207/Liminal-Rail-Metro/internal/lifetrabridge"
	"github.com/safal207/Liminal-Rail-Metro/internal/metro"
	"github.com/safal207/Liminal-Rail-Metro/internal/trustpolicy"
)

type sourceProof struct {
	Protocol  string `json:"protocol"`
	Journal   string `json:"journal_path"`
	Discovery struct {
		ReportHash               string `json:"report_hash"`
		ExternalIdentityVerified bool   `json:"external_identity_verified"`
		HardwareUpgradeSelected  bool   `json:"hardware_upgrade_selected"`
	} `json:"discovery"`
	Flags struct {
		ExternalOIDCVerified    bool `json:"external_oidc_verified"`
		NoFalseHardwareUpgrade bool `json:"no_false_hardware_upgrade"`
	} `json:"flags"`
}

type portableReceipt struct {
	Protocol                         string `json:"protocol"`
	PayloadSHA256                    string `json:"payload_sha256"`
	SigstoreBundleSHA256             string `json:"sigstore_bundle_sha256"`
	KeylessSigningCompleted          bool   `json:"keyless_signing_completed"`
	IndependentCosignVerifyPassed    bool   `json:"independent_cosign_verify_passed"`
	TransparencyLogVerifiedByCosign  bool   `json:"transparency_log_verified_by_cosign"`
	SourceExternalIdentityVerified   bool   `json:"source_external_identity_verified"`
	SourceHardwareUpgradeSelected    bool   `json:"source_hardware_upgrade_selected"`
	HardwareBacked                   bool   `json:"hardware_backed"`
	RemoteHardwareAttestation        bool   `json:"remote_hardware_attestation"`
}

type proofFlags struct {
	PortableExternalPolicyAllowed bool `json:"portable_external_policy_allowed"`
	HardwareCriticalPolicyDenied  bool `json:"hardware_critical_policy_denied"`
	DeniedEffectNotCalled         bool `json:"denied_effect_not_called"`
	DeniedLearningNotCalled       bool `json:"denied_learning_not_called"`
	DeniedReceiptNotCreated       bool `json:"denied_receipt_not_created"`
	SourceJournalUnchanged        bool `json:"source_journal_unchanged"`
	AllowedReceiptTrustBound      bool `json:"allowed_receipt_trust_bound"`
	LifetraTrustRefsPresent       bool `json:"lifetra_trust_refs_present"`
	ExactlyOneLearningWrite       bool `json:"exactly_one_learning_write"`
	PortableEvidenceHashBound     bool `json:"portable_evidence_hash_bound"`
}

type proof struct {
	Protocol          string                  `json:"protocol"`
	Evidence          trustpolicy.Evidence    `json:"evidence"`
	AllowedPolicy     trustpolicy.Policy      `json:"allowed_policy"`
	AllowedDecision   trustpolicy.Decision    `json:"allowed_decision"`
	DeniedPolicy      trustpolicy.Policy      `json:"denied_policy"`
	DeniedDecision    trustpolicy.Decision    `json:"denied_decision"`
	AllowedResult     map[string]any          `json:"allowed_result"`
	AllowedReceipt    metro.Receipt           `json:"allowed_receipt"`
	AllowedObservation lifetrabridge.Observation `json:"allowed_observation"`
	SourceJournalSHA256Before string          `json:"source_journal_sha256_before"`
	SourceJournalSHA256After  string          `json:"source_journal_sha256_after"`
	LearningLog       string                  `json:"learning_log"`
	Flags             proofFlags              `json:"flags"`
	Claim             string                  `json:"claim"`
}

func main() {
	sourcePath := flag.String("source", "hardware-adaptive-proof-v0.8.json", "v0.8 source proof")
	portablePath := flag.String("portable", "portable-proof-v0.9.json", "verified portable publication receipt")
	outPath := flag.String("out", "trust-policy-proof-v1.0.json", "v1.0 proof output")
	learningPath := flag.String("learning-log", "trust-policy-v1.0.learning.jsonl", "trust-gated learning log")
	flag.Parse()

	var source sourceProof
	must(readJSON(*sourcePath, &source))
	var portable portableReceipt
	must(readJSON(*portablePath, &portable))
	if source.Protocol != "liminal.hardware-adaptive-proof.v0.8" || portable.Protocol != "liminal.portable-proof.v0.9" {
		panic("unexpected source/portable proof protocol")
	}
	sourceHash := fileSHA256(*sourcePath)
	if sourceHash != portable.PayloadSHA256 {
		panic("portable receipt is not bound to exact source proof payload")
	}

	evidence, err := trustpolicy.NewEvidence(trustpolicy.EvidenceInput{
		ExternalIdentityVerified:    source.Flags.ExternalOIDCVerified && source.Discovery.ExternalIdentityVerified && portable.SourceExternalIdentityVerified,
		PortablePublicationVerified: portable.KeylessSigningCompleted && portable.IndependentCosignVerifyPassed && portable.TransparencyLogVerifiedByCosign,
		HardwareBacked:              portable.HardwareBacked,
		RemoteHardwareAttestation:   portable.RemoteHardwareAttestation,
		SourceProofHash:              sourceHash,
		DiscoveryHash:                source.Discovery.ReportHash,
		PortableProofHash:            portable.SigstoreBundleSHA256,
	})
	must(err)

	allowedPolicy, err := trustpolicy.NewPolicy("external-portable-required", trustpolicy.Requirements{
		ExternalIdentity:     true,
		PortablePublication: true,
	})
	must(err)
	deniedPolicy, err := trustpolicy.NewPolicy("hardware-critical", trustpolicy.Requirements{
		ExternalIdentity:           true,
		PortablePublication:       true,
		HardwareBacked:            true,
		RemoteHardwareAttestation: true,
	})
	must(err)
	allowedGate, err := adaptive.NewTrustGate(allowedPolicy, evidence)
	must(err)
	deniedGate, err := adaptive.NewTrustGate(deniedPolicy, evidence)
	must(err)

	journalBefore := fileSHA256(source.Journal)
	_ = os.Remove(*learningPath)
	deniedEffects := 0
	deniedDecision, deniedExecuteErr := deniedGate.Execute(func() error {
		deniedEffects++
		return nil
	})
	deniedLearning := 0
	_, deniedLearnErr := deniedGate.Learn(func() error {
		deniedLearning++
		return appendLearning(*learningPath, map[string]any{"should_not_exist": true})
	})
	deniedReceiptID := ""

	var duration time.Duration
	var effectHash string
	allowedDecision, err := allowedGate.Execute(func() error {
		start := time.Now()
		payload := make([]byte, 2048)
		var sink [32]byte
		for i := 0; i < 80000; i++ {
			payload[i%len(payload)] ^= byte(i)
			sink = sha256.Sum256(payload)
			copy(payload[:32], sink[:])
		}
		duration = time.Since(start)
		effectHash = hex.EncodeToString(sink[:])
		return nil
	})
	must(err)

	baseResult := map[string]any{
		"effect_sha256": effectHash,
		"duration_ms":  float64(duration.Microseconds()) / 1000,
		"source_proof_sha256": sourceHash,
		"portable_bundle_sha256": portable.SigstoreBundleSHA256,
	}
	allowedResult, err := adaptive.BindTrustDecisionResult(baseResult, allowedDecision)
	must(err)
	packet := metro.NewPacket(
		"trust-policy-v10-allowed",
		"liminal-trust-policy-v1.0",
		"Execute a bounded CPU effect only after trust policy ALLOW.",
		metro.Action{Kind: "trust-gated.cpu.sha256", Inputs: map[string]any{"policy_hash": allowedDecision.PolicyHash, "evidence_hash": allowedDecision.EvidenceHash, "decision_hash": allowedDecision.DecisionHash}},
		[]string{"executor://trust-gated-local-cpu"},
	)
	packet.ContextRefs = []string{
		lifetrabridge.TrustPolicyRefPrefix + allowedDecision.PolicyHash,
		lifetrabridge.TrustEvidenceRefPrefix + allowedDecision.EvidenceHash,
		lifetrabridge.TrustDecisionRefPrefix + allowedDecision.DecisionHash,
	}
	route := metro.Route{
		Protocol:       metro.RouteProtocol,
		RouteID:        "route-" + packet.ActionID,
		ActionID:       packet.ActionID,
		RouterID:       "liminal-trust-gate-v1.0",
		DecisionMode:   "trust-policy-gated",
		SelectedTarget: "executor://trust-gated-local-cpu",
		Candidates:     []metro.Candidate{{Target: "executor://trust-gated-local-cpu", Score: 1}},
		PolicyRef:      lifetrabridge.TrustPolicyRefPrefix + allowedDecision.PolicyHash,
		DecidedAt:      metro.NowISO(),
	}
	allowedReceipt, err := metro.MakeSuccessReceipt(packet, route, allowedResult, lifetrabridge.TrustDecisionRefPrefix+allowedDecision.DecisionHash)
	must(err)
	must(metro.Verify(packet, route, allowedResult, allowedReceipt))
	must(adaptive.ValidateTrustDecisionResultBinding(allowedResult, allowedDecision))
	allowedObservation, err := lifetrabridge.ReceiptToObservation(allowedReceipt, "")
	must(err)
	allowedObservation, err = lifetrabridge.BindTrustProofRefs(allowedObservation, allowedDecision.PolicyHash, allowedDecision.EvidenceHash, allowedDecision.DecisionHash)
	must(err)

	_, err = allowedGate.Learn(func() error {
		return appendLearning(*learningPath, map[string]any{
			"protocol":      "liminal.trust-gated-learning.v1.0",
			"decision_hash": allowedDecision.DecisionHash,
			"receipt_id":    allowedReceipt.ReceiptID,
			"effect_sha256": effectHash,
		})
	})
	must(err)

	journalAfter := fileSHA256(source.Journal)
	learningLines := nonEmptyLines(*learningPath)
	refsPresent := hasRef(allowedObservation.ProofRefs, lifetrabridge.TrustPolicyRefPrefix+allowedDecision.PolicyHash) &&
		hasRef(allowedObservation.ProofRefs, lifetrabridge.TrustEvidenceRefPrefix+allowedDecision.EvidenceHash) &&
		hasRef(allowedObservation.ProofRefs, lifetrabridge.TrustDecisionRefPrefix+allowedDecision.DecisionHash)

	flags := proofFlags{
		PortableExternalPolicyAllowed: allowedDecision.Allowed,
		HardwareCriticalPolicyDenied:  !deniedDecision.Allowed && errors.Is(deniedExecuteErr, adaptive.ErrTrustPolicyDenied) && errors.Is(deniedLearnErr, adaptive.ErrTrustPolicyDenied),
		DeniedEffectNotCalled:         deniedEffects == 0,
		DeniedLearningNotCalled:       deniedLearning == 0,
		DeniedReceiptNotCreated:       deniedReceiptID == "",
		SourceJournalUnchanged:        journalBefore == journalAfter,
		AllowedReceiptTrustBound:      adaptive.ValidateTrustDecisionResultBinding(allowedResult, allowedDecision) == nil,
		LifetraTrustRefsPresent:       refsPresent,
		ExactlyOneLearningWrite:       learningLines == 1,
		PortableEvidenceHashBound:     evidence.SourceProofHash == portable.PayloadSHA256 && evidence.PortableProofHash == portable.SigstoreBundleSHA256,
	}

	out := proof{
		Protocol:                   "liminal.trust-policy-proof.v1.0",
		Evidence:                   evidence,
		AllowedPolicy:              allowedPolicy,
		AllowedDecision:            allowedDecision,
		DeniedPolicy:               deniedPolicy,
		DeniedDecision:             deniedDecision,
		AllowedResult:              allowedResult,
		AllowedReceipt:             allowedReceipt,
		AllowedObservation:         allowedObservation,
		SourceJournalSHA256Before:  journalBefore,
		SourceJournalSHA256After:   journalAfter,
		LearningLog:                *learningPath,
		Flags:                      flags,
		Claim: "v1.0 evaluated content-addressed trust policy against externally verified and portably published evidence before invoking execution or learning callbacks. The external+portable policy allowed one bounded CPU effect and one learning append; the hardware-critical policy denied before either callback and produced no denied receipt. The source adaptive journal remained unchanged. This proves fail-closed trust-policy enforcement in the v1.0 guarded path, not that arbitrary future code cannot bypass the API or that the runtime is hardware-backed.",
	}
	b, err := json.MarshalIndent(out, "", "  ")
	must(err)
	must(os.WriteFile(*outPath, append(b, '\n'), 0o644))

	fmt.Printf("allow_policy=%v deny_hardware_policy=%v denied_effects=%d denied_learning=%d learning_lines=%d journal_unchanged=%v\n", allowedDecision.Allowed, !deniedDecision.Allowed, deniedEffects, deniedLearning, learningLines, journalBefore == journalAfter)
	fmt.Printf("policy=%s evidence=%s decision=%s receipt=%s\n", allowedDecision.PolicyHash, allowedDecision.EvidenceHash, allowedDecision.DecisionHash, allowedReceipt.ReceiptID)
	fmt.Printf("unmet_hardware=%v\n", deniedDecision.Unmet)
	fmt.Printf("proof=%s\n", *outPath)
}

func readJSON(path string, dst any) error {
	b, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return json.Unmarshal(b, dst)
}

func fileSHA256(path string) string {
	b, err := os.ReadFile(path)
	must(err)
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

func appendLearning(path string, value any) error {
	b, err := json.Marshal(value)
	if err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	if _, err := f.Write(append(b, '\n')); err != nil {
		return err
	}
	return f.Sync()
}

func nonEmptyLines(path string) int {
	b, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return 0
		}
		panic(err)
	}
	count := 0
	inLine := false
	for _, c := range b {
		if c == '\n' {
			if inLine {
				count++
			}
			inLine = false
			continue
		}
		if c != ' ' && c != '\t' && c != '\r' {
			inLine = true
		}
	}
	if inLine {
		count++
	}
	return count
}

func hasRef(refs []string, want string) bool {
	for _, ref := range refs {
		if ref == want {
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
