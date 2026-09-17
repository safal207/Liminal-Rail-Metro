package main

import (
	"crypto/ed25519"
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
	"github.com/safal207/Liminal-Rail-Metro/internal/policyauthority"
	"github.com/safal207/Liminal-Rail-Metro/internal/trustpolicy"
)

type v10Proof struct {
	Protocol string               `json:"protocol"`
	Evidence trustpolicy.Evidence `json:"evidence"`
	Flags    map[string]bool      `json:"flags"`
	Claim    string               `json:"claim"`
}

type proofFlags struct {
	RootManifestVerified              bool `json:"root_manifest_verified"`
	OperationPolicyExactBound         bool `json:"operation_policy_exact_bound"`
	WeakPolicySubstitutionRejected    bool `json:"weak_policy_substitution_rejected"`
	WeakSubstitutionEffectNotCalled   bool `json:"weak_substitution_effect_not_called"`
	WeakSubstitutionLearningNotCalled bool `json:"weak_substitution_learning_not_called"`
	HardwareCriticalPolicyDenied      bool `json:"hardware_critical_policy_denied"`
	HardwareDeniedEffectNotCalled     bool `json:"hardware_denied_effect_not_called"`
	HardwareDeniedLearningNotCalled   bool `json:"hardware_denied_learning_not_called"`
	TamperedManifestRejected          bool `json:"tampered_manifest_rejected"`
	TamperedSignatureRejected         bool `json:"tampered_signature_rejected"`
	RestartAuthorizationStable        bool `json:"restart_authorization_stable"`
	ExplicitRotationVerified          bool `json:"explicit_rotation_verified"`
	SilentWeakeningRotationRejected   bool `json:"silent_weakening_rotation_rejected"`
	ExplicitWeakeningMarked           bool `json:"explicit_weakening_marked"`
	AllowedReceiptAuthorityBound      bool `json:"allowed_receipt_authority_bound"`
	LifetraAuthorityRefsPresent       bool `json:"lifetra_authority_refs_present"`
	ExactlyOneLearningWrite           bool `json:"exactly_one_learning_write"`
	V10EvidenceReused                 bool `json:"v1_0_evidence_reused"`
}

type proof struct {
	Protocol              string                         `json:"protocol"`
	SourceV10ProofSHA256  string                         `json:"source_v1_0_proof_sha256"`
	Evidence              trustpolicy.Evidence           `json:"evidence"`
	TrustRoot             policyauthority.TrustRoot      `json:"trust_root"`
	RootSignedManifest    policyauthority.SignedManifest `json:"root_signed_manifest"`
	CurrentSignedManifest policyauthority.SignedManifest `json:"current_signed_manifest"`
	Rotation              policyauthority.Rotation       `json:"rotation"`
	AllowedAuthorization  policyauthority.Authorization  `json:"allowed_authorization"`
	HardwareAuthorization policyauthority.Authorization  `json:"hardware_authorization"`
	AllowedDecision       trustpolicy.Decision           `json:"allowed_decision"`
	HardwareDecision      trustpolicy.Decision           `json:"hardware_decision"`
	AllowedResult         map[string]any                 `json:"allowed_result"`
	AllowedReceipt        metro.Receipt                  `json:"allowed_receipt"`
	AllowedObservation    lifetrabridge.Observation      `json:"allowed_observation"`
	WeakeningRotation     policyauthority.Rotation       `json:"explicit_weakening_rotation"`
	LearningLog           string                         `json:"learning_log"`
	Flags                 proofFlags                     `json:"flags"`
	Claim                 string                         `json:"claim"`
}

func main() {
	sourcePath := flag.String("source", "trust-policy-proof-v1.0.json", "v1.0 trust-policy proof")
	outPath := flag.String("out", "policy-authority-proof-v1.1.json", "v1.1 proof output")
	learningPath := flag.String("learning-log", "policy-authority-v1.1.learning.jsonl", "authority-gated learning log")
	flag.Parse()

	var source v10Proof
	must(readJSON(*sourcePath, &source))
	if source.Protocol != "liminal.trust-policy-proof.v1.0" {
		panic("unexpected v1.0 source protocol")
	}
	must(source.Evidence.Validate())
	sourceHash := fileSHA256(*sourcePath)

	externalPortable, err := trustpolicy.NewPolicy("external-portable-required", trustpolicy.Requirements{
		ExternalIdentity:    true,
		PortablePublication: true,
	})
	must(err)
	hardwareCritical, err := trustpolicy.NewPolicy("hardware-critical", trustpolicy.Requirements{
		ExternalIdentity:          true,
		PortablePublication:       true,
		HardwareBacked:            true,
		RemoteHardwareAttestation: true,
	})
	must(err)
	rootManifest, err := policyauthority.NewManifest("rail-policy-authority", 1, []policyauthority.Binding{
		{OperationClass: "bounded.cpu.sha256", Policy: externalPortable},
		{OperationClass: "hardware.critical", Policy: hardwareCritical},
	})
	must(err)
	rootKey := deterministicKey("liminal-policy-authority-root-v1.1")
	rootSigned, err := policyauthority.SignManifest(rootManifest, "rail-policy-root", rootKey)
	must(err)
	root, err := policyauthority.NewTrustRoot(rootSigned)
	must(err)

	currentManifest, err := policyauthority.NewManifest("rail-policy-authority", 2, []policyauthority.Binding{
		{OperationClass: "bounded.cpu.sha256", Policy: externalPortable},
		{OperationClass: "hardware.critical", Policy: hardwareCritical},
		{OperationClass: "portable.publish", Policy: externalPortable},
	})
	must(err)
	nextKey := deterministicKey("liminal-policy-authority-next-v1.1")
	currentSigned, err := policyauthority.SignManifest(currentManifest, "rail-policy-next", nextKey)
	must(err)
	rotation, err := policyauthority.SignRotation(rootSigned, currentSigned, rootKey, false)
	must(err)
	must(rotation.Verify(rootSigned, currentSigned))
	resolver, err := policyauthority.OpenResolver(root, []policyauthority.SignedManifest{rootSigned, currentSigned}, []policyauthority.Rotation{rotation})
	must(err)

	allowedGate, err := adaptive.NewPolicyAuthorityGateWithRequestedPolicy(resolver, "bounded.cpu.sha256", externalPortable, source.Evidence)
	must(err)
	allowedAuthorization := allowedGate.Authorization()
	allowedDecision, err := allowedGate.RequireAllowed()
	must(err)

	weakEffects := 0
	weakLearning := 0
	_, weakErr := adaptive.NewPolicyAuthorityGateWithRequestedPolicy(resolver, "hardware.critical", externalPortable, source.Evidence)
	if weakErr == nil {
		panic("weaker requested policy unexpectedly accepted")
	}
	_ = weakEffects
	_ = weakLearning

	hardwareGate, err := adaptive.NewPolicyAuthorityGateWithRequestedPolicy(resolver, "hardware.critical", hardwareCritical, source.Evidence)
	must(err)
	hardwareAuthorization := hardwareGate.Authorization()
	hardwareEffects := 0
	hardwareDecision, hardwareErr := hardwareGate.Execute(func() error {
		hardwareEffects++
		return nil
	})
	hardwareLearning := 0
	_, hardwareLearnErr := hardwareGate.Learn(func() error {
		hardwareLearning++
		return nil
	})

	var effectHash string
	var effectDuration time.Duration
	allowedDecision, err = allowedGate.Execute(func() error {
		start := time.Now()
		payload := make([]byte, 2048)
		var sink [32]byte
		for i := 0; i < 90000; i++ {
			payload[i%len(payload)] ^= byte(i)
			sink = sha256.Sum256(payload)
			copy(payload[:32], sink[:])
		}
		effectDuration = time.Since(start)
		effectHash = hex.EncodeToString(sink[:])
		return nil
	})
	must(err)

	baseResult := map[string]any{
		"effect_sha256":            effectHash,
		"duration_ms":              float64(effectDuration.Microseconds()) / 1000,
		"source_v1_0_proof_sha256": sourceHash,
	}
	allowedResult, err := adaptive.BindPolicyAuthorityDecisionResult(baseResult, allowedAuthorization, allowedDecision)
	must(err)
	packet := metro.NewPacket(
		"policy-authority-v11-allowed",
		"liminal-policy-authority-v1.1",
		"Execute bounded CPU effect only under signed operation-policy authority.",
		metro.Action{Kind: "policy-authority.cpu.sha256", Inputs: map[string]any{
			"operation_class":    allowedAuthorization.OperationClass,
			"authorization_hash": allowedAuthorization.AuthorizationHash,
			"policy_hash":        allowedDecision.PolicyHash,
			"evidence_hash":      allowedDecision.EvidenceHash,
			"decision_hash":      allowedDecision.DecisionHash,
		}},
		[]string{"executor://policy-authority-local-cpu"},
	)
	packet.ContextRefs = []string{
		lifetrabridge.PolicyAuthorityManifestRefPrefix + allowedAuthorization.SignedManifestHash,
		lifetrabridge.PolicyAuthorityRotationRefPrefix + allowedAuthorization.RotationHash,
		lifetrabridge.PolicyAuthorizationRefPrefix + allowedAuthorization.AuthorizationHash,
		lifetrabridge.TrustPolicyRefPrefix + allowedDecision.PolicyHash,
		lifetrabridge.TrustEvidenceRefPrefix + allowedDecision.EvidenceHash,
		lifetrabridge.TrustDecisionRefPrefix + allowedDecision.DecisionHash,
	}
	route := metro.Route{
		Protocol:       metro.RouteProtocol,
		RouteID:        "route-" + packet.ActionID,
		ActionID:       packet.ActionID,
		RouterID:       "liminal-policy-authority-v1.1",
		DecisionMode:   "signed-policy-authority-gated",
		SelectedTarget: "executor://policy-authority-local-cpu",
		Candidates:     []metro.Candidate{{Target: "executor://policy-authority-local-cpu", Score: 1}},
		PolicyRef:      lifetrabridge.PolicyAuthorizationRefPrefix + allowedAuthorization.AuthorizationHash,
		DecidedAt:      metro.NowISO(),
	}
	allowedReceipt, err := metro.MakeSuccessReceipt(packet, route, allowedResult, lifetrabridge.PolicyAuthorizationRefPrefix+allowedAuthorization.AuthorizationHash)
	must(err)
	must(metro.Verify(packet, route, allowedResult, allowedReceipt))
	must(adaptive.ValidatePolicyAuthorityDecisionResultBinding(allowedResult, allowedAuthorization, allowedDecision))
	obs, err := lifetrabridge.ReceiptToObservation(allowedReceipt, "")
	must(err)
	obs, err = lifetrabridge.BindTrustProofRefs(obs, allowedDecision.PolicyHash, allowedDecision.EvidenceHash, allowedDecision.DecisionHash)
	must(err)
	obs, err = lifetrabridge.BindPolicyAuthorityProofRefs(obs, allowedAuthorization)
	must(err)

	_ = os.Remove(*learningPath)
	_, err = allowedGate.Learn(func() error {
		return appendLearning(*learningPath, map[string]any{
			"protocol":           "liminal.policy-authority-learning.v1.1",
			"authorization_hash": allowedAuthorization.AuthorizationHash,
			"decision_hash":      allowedDecision.DecisionHash,
			"receipt_id":         allowedReceipt.ReceiptID,
			"effect_sha256":      effectHash,
		})
	})
	must(err)

	tamperedManifest := currentSigned
	tamperedManifest.Manifest.Bindings[1].Policy = externalPortable
	tamperedManifestRejected := tamperedManifest.SelfVerify() != nil
	tamperedSignature := currentSigned
	if len(tamperedSignature.Signature) < 4 {
		panic("unexpectedly short signature")
	}
	tamperedSignature.Signature = tamperedSignature.Signature[:len(tamperedSignature.Signature)-4] + "AAAA"
	tamperedSignatureRejected := tamperedSignature.SelfVerify() != nil

	chainBytes, err := json.Marshal(struct {
		Root      policyauthority.TrustRoot         `json:"root"`
		Manifests []policyauthority.SignedManifest `json:"manifests"`
		Rotations []policyauthority.Rotation       `json:"rotations"`
	}{root, []policyauthority.SignedManifest{rootSigned, currentSigned}, []policyauthority.Rotation{rotation}})
	must(err)
	var restored struct {
		Root      policyauthority.TrustRoot         `json:"root"`
		Manifests []policyauthority.SignedManifest `json:"manifests"`
		Rotations []policyauthority.Rotation       `json:"rotations"`
	}
	must(json.Unmarshal(chainBytes, &restored))
	restarted, err := policyauthority.OpenResolver(restored.Root, restored.Manifests, restored.Rotations)
	must(err)
	_, restartedAuthorization, err := restarted.Resolve("bounded.cpu.sha256")
	must(err)

	weakManifest, err := policyauthority.NewManifest("rail-policy-authority", 2, []policyauthority.Binding{
		{OperationClass: "bounded.cpu.sha256", Policy: externalPortable},
		{OperationClass: "hardware.critical", Policy: externalPortable},
	})
	must(err)
	weakSigned, err := policyauthority.SignManifest(weakManifest, "rail-policy-next", nextKey)
	must(err)
	_, silentWeakeningErr := policyauthority.SignRotation(rootSigned, weakSigned, rootKey, false)
	explicitWeakening, err := policyauthority.SignRotation(rootSigned, weakSigned, rootKey, true)
	must(err)
	must(explicitWeakening.Verify(rootSigned, weakSigned))

	refsPresent := contains(obs.ProofRefs, lifetrabridge.PolicyAuthorityManifestRefPrefix+allowedAuthorization.SignedManifestHash) &&
		contains(obs.ProofRefs, lifetrabridge.PolicyAuthorityRotationRefPrefix+allowedAuthorization.RotationHash) &&
		contains(obs.ProofRefs, lifetrabridge.PolicyAuthorizationRefPrefix+allowedAuthorization.AuthorizationHash)
	flags := proofFlags{
		RootManifestVerified:              root.Validate() == nil && rootSigned.SelfVerify() == nil,
		OperationPolicyExactBound:         allowedAuthorization.PolicyHash == externalPortable.PolicyHash && hardwareAuthorization.PolicyHash == hardwareCritical.PolicyHash,
		WeakPolicySubstitutionRejected:    errors.Is(weakErr, adaptive.ErrPolicyAuthorityMismatch),
		WeakSubstitutionEffectNotCalled:   weakEffects == 0,
		WeakSubstitutionLearningNotCalled: weakLearning == 0,
		HardwareCriticalPolicyDenied:      !hardwareDecision.Allowed && errors.Is(hardwareErr, adaptive.ErrTrustPolicyDenied) && errors.Is(hardwareLearnErr, adaptive.ErrTrustPolicyDenied),
		HardwareDeniedEffectNotCalled:     hardwareEffects == 0,
		HardwareDeniedLearningNotCalled:   hardwareLearning == 0,
		TamperedManifestRejected:          tamperedManifestRejected,
		TamperedSignatureRejected:         tamperedSignatureRejected,
		RestartAuthorizationStable:        restartedAuthorization.AuthorizationHash == allowedAuthorization.AuthorizationHash,
		ExplicitRotationVerified:          rotation.Verify(rootSigned, currentSigned) == nil && allowedAuthorization.Generation == 2 && allowedAuthorization.RotationHash == rotation.RotationHash,
		SilentWeakeningRotationRejected:   silentWeakeningErr != nil,
		ExplicitWeakeningMarked:           len(explicitWeakening.WeakenedOperations) == 1 && explicitWeakening.WeakenedOperations[0] == "hardware.critical" && explicitWeakening.AllowWeakening,
		AllowedReceiptAuthorityBound:      adaptive.ValidatePolicyAuthorityDecisionResultBinding(allowedResult, allowedAuthorization, allowedDecision) == nil,
		LifetraAuthorityRefsPresent:       refsPresent,
		ExactlyOneLearningWrite:           nonEmptyLines(*learningPath) == 1,
		V10EvidenceReused:                 source.Evidence.EvidenceHash == allowedDecision.EvidenceHash && source.Evidence.EvidenceHash == hardwareDecision.EvidenceHash,
	}

	out := proof{
		Protocol:              "liminal.policy-authority-proof.v1.1",
		SourceV10ProofSHA256:  sourceHash,
		Evidence:              source.Evidence,
		TrustRoot:             root,
		RootSignedManifest:    rootSigned,
		CurrentSignedManifest: currentSigned,
		Rotation:              rotation,
		AllowedAuthorization:  allowedAuthorization,
		HardwareAuthorization: hardwareAuthorization,
		AllowedDecision:       allowedDecision,
		HardwareDecision:      hardwareDecision,
		AllowedResult:         allowedResult,
		AllowedReceipt:        allowedReceipt,
		AllowedObservation:    obs,
		WeakeningRotation:     explicitWeakening,
		LearningLog:           *learningPath,
		Flags:                 flags,
		Claim:                 "v1.1 binds operation classes to exact trust-policy hashes through a pinned Ed25519 policy-authority root and explicit signed manifest rotation. A caller-supplied weaker policy for hardware.critical is rejected before a TrustGate is constructed; the authority-selected hardware policy then independently denies the same software-bound evidence before effect or learning. Explicit signed rotation may deliberately weaken policy only when allow_weakening is set and the weakened operation set is committed into the rotation. This does not provide OS/kernel mandatory enforcement or hardware-rooted policy keys.",
	}
	b, err := json.MarshalIndent(out, "", "  ")
	must(err)
	must(os.WriteFile(*outPath, append(b, '\n'), 0o644))

	fmt.Printf("allow=%v hardware_deny=%v weak_substitution_rejected=%v weak_effects=%d weak_learning=%d hardware_effects=%d hardware_learning=%d\n",
		allowedDecision.Allowed, !hardwareDecision.Allowed, errors.Is(weakErr, adaptive.ErrPolicyAuthorityMismatch), weakEffects, weakLearning, hardwareEffects, hardwareLearning)
	fmt.Printf("manifest=%s rotation=%s authorization=%s policy=%s evidence=%s decision=%s\n",
		allowedAuthorization.SignedManifestHash, allowedAuthorization.RotationHash, allowedAuthorization.AuthorizationHash, allowedDecision.PolicyHash, allowedDecision.EvidenceHash, allowedDecision.DecisionHash)
	fmt.Printf("hardware_unmet=%v explicit_weakening=%v learning_lines=%d restart_stable=%v\n",
		hardwareDecision.Unmet, explicitWeakening.WeakenedOperations, nonEmptyLines(*learningPath), restartedAuthorization.AuthorizationHash == allowedAuthorization.AuthorizationHash)
	fmt.Printf("proof=%s\n", *outPath)
}

func deterministicKey(label string) ed25519.PrivateKey {
	sum := sha256.Sum256([]byte(label))
	return ed25519.NewKeyFromSeed(sum[:])
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

func contains(refs []string, want string) bool {
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
