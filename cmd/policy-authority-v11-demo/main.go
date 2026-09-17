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
}

type savedChain struct {
	Root      policyauthority.TrustRoot        `json:"root"`
	Manifests []policyauthority.SignedManifest `json:"manifests"`
	Rotations []policyauthority.Rotation       `json:"rotations"`
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
		ExternalIdentity: true, PortablePublication: true,
	})
	must(err)
	hardwareCritical, err := trustpolicy.NewPolicy("hardware-critical", trustpolicy.Requirements{
		ExternalIdentity: true, PortablePublication: true, HardwareBacked: true, RemoteHardwareAttestation: true,
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
	allowedAuth := allowedGate.Authorization()
	allowedDecision, err := allowedGate.RequireAllowed()
	must(err)

	_, weakErr := adaptive.NewPolicyAuthorityGateWithRequestedPolicy(resolver, "hardware.critical", externalPortable, source.Evidence)
	if weakErr == nil {
		panic("weaker requested policy unexpectedly accepted")
	}
	weakEffects, weakLearning := 0, 0 // gate never constructed; callbacks cannot run

	hardwareGate, err := adaptive.NewPolicyAuthorityGateWithRequestedPolicy(resolver, "hardware.critical", hardwareCritical, source.Evidence)
	must(err)
	hardwareAuth := hardwareGate.Authorization()
	hardwareEffects, hardwareLearning := 0, 0
	hardwareDecision, hardwareErr := hardwareGate.Execute(func() error { hardwareEffects++; return nil })
	_, hardwareLearnErr := hardwareGate.Learn(func() error { hardwareLearning++; return nil })

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
		effectDuration, effectHash = time.Since(start), hex.EncodeToString(sink[:])
		return nil
	})
	must(err)

	allowedResult, err := adaptive.BindPolicyAuthorityDecisionResult(map[string]any{
		"effect_sha256": effectHash, "duration_ms": float64(effectDuration.Microseconds()) / 1000, "source_v1_0_proof_sha256": sourceHash,
	}, allowedAuth, allowedDecision)
	must(err)
	packet := metro.NewPacket("policy-authority-v11-allowed", "liminal-policy-authority-v1.1",
		"Execute bounded CPU effect only under signed operation-policy authority.",
		metro.Action{Kind: "policy-authority.cpu.sha256", Inputs: map[string]any{
			"operation_class": allowedAuth.OperationClass, "authorization_hash": allowedAuth.AuthorizationHash,
			"policy_hash": allowedDecision.PolicyHash, "evidence_hash": allowedDecision.EvidenceHash, "decision_hash": allowedDecision.DecisionHash,
		}}, []string{"executor://policy-authority-local-cpu"})
	packet.ContextRefs = []string{
		lifetrabridge.PolicyAuthorityManifestRefPrefix + allowedAuth.SignedManifestHash,
		lifetrabridge.PolicyAuthorityRotationRefPrefix + allowedAuth.RotationHash,
		lifetrabridge.PolicyAuthorizationRefPrefix + allowedAuth.AuthorizationHash,
		lifetrabridge.TrustPolicyRefPrefix + allowedDecision.PolicyHash,
		lifetrabridge.TrustEvidenceRefPrefix + allowedDecision.EvidenceHash,
		lifetrabridge.TrustDecisionRefPrefix + allowedDecision.DecisionHash,
	}
	route := metro.Route{Protocol: metro.RouteProtocol, RouteID: "route-" + packet.ActionID, ActionID: packet.ActionID,
		RouterID: "liminal-policy-authority-v1.1", DecisionMode: "signed-policy-authority-gated",
		SelectedTarget: "executor://policy-authority-local-cpu", Candidates: []metro.Candidate{{Target: "executor://policy-authority-local-cpu", Score: 1}},
		PolicyRef: lifetrabridge.PolicyAuthorizationRefPrefix + allowedAuth.AuthorizationHash, DecidedAt: metro.NowISO()}
	allowedReceipt, err := metro.MakeSuccessReceipt(packet, route, allowedResult, lifetrabridge.PolicyAuthorizationRefPrefix+allowedAuth.AuthorizationHash)
	must(err)
	must(metro.Verify(packet, route, allowedResult, allowedReceipt))
	must(adaptive.ValidatePolicyAuthorityDecisionResultBinding(allowedResult, allowedAuth, allowedDecision))
	obs, err := lifetrabridge.ReceiptToObservation(allowedReceipt, "")
	must(err)
	obs, err = lifetrabridge.BindTrustProofRefs(obs, allowedDecision.PolicyHash, allowedDecision.EvidenceHash, allowedDecision.DecisionHash)
	must(err)
	obs, err = lifetrabridge.BindPolicyAuthorityProofRefs(obs, allowedAuth)
	must(err)

	_ = os.Remove(*learningPath)
	_, err = allowedGate.Learn(func() error {
		return appendLearning(*learningPath, map[string]any{"protocol": "liminal.policy-authority-learning.v1.1",
			"authorization_hash": allowedAuth.AuthorizationHash, "decision_hash": allowedDecision.DecisionHash,
			"receipt_id": allowedReceipt.ReceiptID, "effect_sha256": effectHash})
	})
	must(err)

	tamperedManifest := currentSigned
	tamperedManifest.Manifest.ManifestHash = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	tamperedManifestRejected := tamperedManifest.SelfVerify() != nil
	tamperedSignature := currentSigned
	tamperedSignature.Signature = tamperedSignature.Signature[:len(tamperedSignature.Signature)-4] + "AAAA"
	tamperedSignatureRejected := tamperedSignature.SelfVerify() != nil

	chainBytes, err := json.Marshal(savedChain{Root: root,
		Manifests: []policyauthority.SignedManifest{rootSigned, currentSigned}, Rotations: []policyauthority.Rotation{rotation}})
	must(err)
	var restored savedChain
	must(json.Unmarshal(chainBytes, &restored))
	restarted, err := policyauthority.OpenResolver(restored.Root, restored.Manifests, restored.Rotations)
	must(err)
	_, restartedAuth, err := restarted.Resolve("bounded.cpu.sha256")
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

	refsPresent := contains(obs.ProofRefs, lifetrabridge.PolicyAuthorityManifestRefPrefix+allowedAuth.SignedManifestHash) &&
		contains(obs.ProofRefs, lifetrabridge.PolicyAuthorityRotationRefPrefix+allowedAuth.RotationHash) &&
		contains(obs.ProofRefs, lifetrabridge.PolicyAuthorizationRefPrefix+allowedAuth.AuthorizationHash)
	flags := map[string]bool{
		"root_manifest_verified":                root.Validate() == nil && rootSigned.SelfVerify() == nil,
		"operation_policy_exact_bound":          allowedAuth.PolicyHash == externalPortable.PolicyHash && hardwareAuth.PolicyHash == hardwareCritical.PolicyHash,
		"weak_policy_substitution_rejected":     errors.Is(weakErr, adaptive.ErrPolicyAuthorityMismatch),
		"weak_substitution_effect_not_called":   weakEffects == 0,
		"weak_substitution_learning_not_called": weakLearning == 0,
		"hardware_critical_policy_denied":       !hardwareDecision.Allowed && errors.Is(hardwareErr, adaptive.ErrTrustPolicyDenied) && errors.Is(hardwareLearnErr, adaptive.ErrTrustPolicyDenied),
		"hardware_denied_effect_not_called":     hardwareEffects == 0,
		"hardware_denied_learning_not_called":   hardwareLearning == 0,
		"tampered_manifest_rejected":            tamperedManifestRejected,
		"tampered_signature_rejected":           tamperedSignatureRejected,
		"restart_authorization_stable":          restartedAuth.AuthorizationHash == allowedAuth.AuthorizationHash,
		"explicit_rotation_verified":            rotation.Verify(rootSigned, currentSigned) == nil && allowedAuth.Generation == 2 && allowedAuth.RotationHash == rotation.RotationHash,
		"silent_weakening_rotation_rejected":    silentWeakeningErr != nil,
		"explicit_weakening_marked":             len(explicitWeakening.WeakenedOperations) == 1 && explicitWeakening.WeakenedOperations[0] == "hardware.critical" && explicitWeakening.AllowWeakening,
		"allowed_receipt_authority_bound":       adaptive.ValidatePolicyAuthorityDecisionResultBinding(allowedResult, allowedAuth, allowedDecision) == nil,
		"lifetra_authority_refs_present":        refsPresent,
		"exactly_one_learning_write":            nonEmptyLines(*learningPath) == 1,
		"v1_0_evidence_reused":                  source.Evidence.EvidenceHash == allowedDecision.EvidenceHash && source.Evidence.EvidenceHash == hardwareDecision.EvidenceHash,
	}
	out := map[string]any{
		"protocol": "liminal.policy-authority-proof.v1.1", "source_v1_0_proof_sha256": sourceHash, "evidence": source.Evidence,
		"trust_root": root, "root_signed_manifest": rootSigned, "current_signed_manifest": currentSigned, "rotation": rotation,
		"allowed_authorization": allowedAuth, "hardware_authorization": hardwareAuth, "allowed_decision": allowedDecision,
		"hardware_decision": hardwareDecision, "allowed_result": allowedResult, "allowed_receipt": allowedReceipt,
		"allowed_observation": obs, "explicit_weakening_rotation": explicitWeakening, "learning_log": *learningPath, "flags": flags,
		"claim": "v1.1 binds operation classes to exact trust-policy hashes through a pinned Ed25519 policy-authority root and explicit signed manifest rotation. A weaker caller-selected policy is rejected before TrustGate construction; the authority-selected hardware policy independently denies insufficient evidence before effect or learning. Intentional weakening requires an explicit signed rotation naming the weakened operations. This does not provide OS/kernel mandatory enforcement or hardware-rooted policy keys.",
	}
	b, err := json.MarshalIndent(out, "", "  ")
	must(err)
	must(os.WriteFile(*outPath, append(b, '\n'), 0o644))
	fmt.Printf("allow=%v hardware_deny=%v weak_substitution_rejected=%v weak_effects=%d weak_learning=%d hardware_effects=%d hardware_learning=%d\n",
		allowedDecision.Allowed, !hardwareDecision.Allowed, errors.Is(weakErr, adaptive.ErrPolicyAuthorityMismatch), weakEffects, weakLearning, hardwareEffects, hardwareLearning)
	fmt.Printf("manifest=%s rotation=%s authorization=%s policy=%s evidence=%s decision=%s\n",
		allowedAuth.SignedManifestHash, allowedAuth.RotationHash, allowedAuth.AuthorizationHash, allowedDecision.PolicyHash, allowedDecision.EvidenceHash, allowedDecision.DecisionHash)
	fmt.Printf("hardware_unmet=%v explicit_weakening=%v learning_lines=%d restart_stable=%v\n",
		hardwareDecision.Unmet, explicitWeakening.WeakenedOperations, nonEmptyLines(*learningPath), restartedAuth.AuthorizationHash == allowedAuth.AuthorizationHash)
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
	count, inLine := 0, false
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
