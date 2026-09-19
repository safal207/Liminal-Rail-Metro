package adaptive

import (
	"crypto/ed25519"
	"crypto/sha256"
	"errors"
	"path/filepath"
	"testing"

	"github.com/safal207/Liminal-Rail-Metro/internal/metro"
	"github.com/safal207/Liminal-Rail-Metro/internal/policyauthority"
	"github.com/safal207/Liminal-Rail-Metro/internal/trustpolicy"
)

const policyAuthorityHashA = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
const policyAuthorityHashB = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"

func policyAuthorityResolver(t *testing.T) (*policyauthority.Resolver, trustpolicy.Policy, trustpolicy.Policy) {
	t.Helper()
	externalPortable, err := trustpolicy.NewPolicy("external-portable-required", trustpolicy.Requirements{ExternalIdentity: true, PortablePublication: true})
	if err != nil { t.Fatal(err) }
	hardware, err := trustpolicy.NewPolicy("hardware-critical", trustpolicy.Requirements{ExternalIdentity: true, PortablePublication: true, HardwareBacked: true, RemoteHardwareAttestation: true})
	if err != nil { t.Fatal(err) }
	manifest, err := policyauthority.NewManifest("rail-policy-authority", 1, []policyauthority.Binding{
		{OperationClass: "bounded.cpu.sha256", ActionKind: "cpu.sha256", Target: "executor://cpu", Policy: externalPortable},
		{OperationClass: "hardware.critical", ActionKind: "hardware.power.write", Target: "executor://hardware", SideEffect: true, Policy: hardware},
	})
	if err != nil { t.Fatal(err) }
	seed := sha256.Sum256([]byte("adaptive-policy-authority-test"))
	key := ed25519.NewKeyFromSeed(seed[:])
	signed, err := policyauthority.SignManifest(manifest, "rail-policy-root", key)
	if err != nil { t.Fatal(err) }
	root, err := policyauthority.NewTrustRoot(signed)
	if err != nil { t.Fatal(err) }
	resolver, err := policyauthority.OpenDurableResolver(filepath.Join(t.TempDir(), "head.json"), root, []policyauthority.SignedManifest{signed}, nil)
	if err != nil { t.Fatal(err) }
	return resolver, externalPortable, hardware
}

func policyAuthorityEvidence(t *testing.T) trustpolicy.Evidence {
	t.Helper()
	e, err := trustpolicy.NewEvidence(trustpolicy.EvidenceInput{
		ExternalIdentityVerified: true, PortablePublicationVerified: true,
		SourceProofHash: policyAuthorityHashA, DiscoveryHash: policyAuthorityHashB, PortableProofHash: policyAuthorityHashA,
	})
	if err != nil { t.Fatal(err) }
	return e
}

func policyAuthorityRuntime(t *testing.T, resolver *policyauthority.Resolver, cpuCalls, hardwareCalls *int, learner OperationLearner) *PolicyAuthorityRuntime {
	t.Helper()
	runtime, err := NewPolicyAuthorityRuntime(resolver, []HandlerRegistration{
		{ActionKind: "cpu.sha256", Target: "executor://cpu", Handler: func(op BoundOperation) error { *cpuCalls++; return nil }},
		{ActionKind: "hardware.power.write", Target: "executor://hardware", SideEffect: true, Handler: func(op BoundOperation) error { *hardwareCalls++; return nil }},
	}, learner)
	if err != nil { t.Fatal(err) }
	return runtime
}

func TestPolicyAuthorityRejectsMislabeledHardwareActionBeforeGate(t *testing.T) {
	resolver, externalPortable, _ := policyAuthorityResolver(t)
	cpuCalls, hardwareCalls := 0, 0
	runtime := policyAuthorityRuntime(t, resolver, &cpuCalls, &hardwareCalls, LearnFunc(func(BoundOperation) error { return nil }))
	request := OperationRequest{
		DeclaredClass: "bounded.cpu.sha256",
		Action: metro.Action{Kind: "hardware.power.write", Inputs: map[string]any{"device": "rail-7"}},
		Target: "executor://hardware", SideEffect: true,
	}
	_, err := runtime.NewGateWithRequestedPolicy(request, externalPortable, policyAuthorityEvidence(t))
	if !errors.Is(err, ErrOperationClassMismatch) {
		t.Fatalf("expected operation-class mismatch, got %v", err)
	}
	if cpuCalls != 0 || hardwareCalls != 0 { t.Fatal("rejected gate dispatched an operation") }
}

func TestPolicyAuthorityCallerCannotSupplyExecuteCallback(t *testing.T) {
	resolver, externalPortable, _ := policyAuthorityResolver(t)
	cpuCalls, hardwareCalls := 0, 0
	runtime := policyAuthorityRuntime(t, resolver, &cpuCalls, &hardwareCalls, LearnFunc(func(BoundOperation) error { return nil }))
	request := OperationRequest{DeclaredClass: "bounded.cpu.sha256", Action: metro.Action{Kind: "cpu.sha256", Inputs: map[string]any{"rounds": 1}}, Target: "executor://cpu"}
	gate, err := runtime.NewGateWithRequestedPolicy(request, externalPortable, policyAuthorityEvidence(t))
	if err != nil { t.Fatal(err) }
	decision, err := gate.Execute()
	if err != nil || !decision.Allowed { t.Fatalf("trusted dispatch failed: decision=%+v err=%v", decision, err) }
	if cpuCalls != 1 || hardwareCalls != 0 { t.Fatalf("wrong trusted handler calls cpu=%d hardware=%d", cpuCalls, hardwareCalls) }
}

func TestPolicyAuthorityRejectsWeakerRequestedPolicyBeforeGate(t *testing.T) {
	resolver, externalPortable, _ := policyAuthorityResolver(t)
	cpuCalls, hardwareCalls := 0, 0
	runtime := policyAuthorityRuntime(t, resolver, &cpuCalls, &hardwareCalls, nil)
	request := OperationRequest{DeclaredClass: "hardware.critical", Action: metro.Action{Kind: "hardware.power.write", Inputs: map[string]any{"device": "rail-7"}}, Target: "executor://hardware", SideEffect: true}
	_, err := runtime.NewGateWithRequestedPolicy(request, externalPortable, policyAuthorityEvidence(t))
	if !errors.Is(err, ErrPolicyAuthorityMismatch) { t.Fatalf("expected policy authority mismatch, got %v", err) }
}

func TestPolicyAuthorityHardwarePolicyStillDeniesInsufficientEvidence(t *testing.T) {
	resolver, _, hardware := policyAuthorityResolver(t)
	cpuCalls, hardwareCalls, learningCalls := 0, 0, 0
	runtime := policyAuthorityRuntime(t, resolver, &cpuCalls, &hardwareCalls, LearnFunc(func(BoundOperation) error { learningCalls++; return nil }))
	request := OperationRequest{DeclaredClass: "hardware.critical", Action: metro.Action{Kind: "hardware.power.write", Inputs: map[string]any{"device": "rail-7"}}, Target: "executor://hardware", SideEffect: true}
	gate, err := runtime.NewGateWithRequestedPolicy(request, hardware, policyAuthorityEvidence(t))
	if err != nil { t.Fatal(err) }
	decision, err := gate.Execute()
	if !errors.Is(err, ErrTrustPolicyDenied) || decision.Allowed { t.Fatalf("expected trust-policy denial, decision=%+v err=%v", decision, err) }
	if _, err := gate.Learn(); !errors.Is(err, ErrTrustPolicyDenied) { t.Fatalf("expected learning denial, got %v", err) }
	if hardwareCalls != 0 || learningCalls != 0 { t.Fatalf("denied gate invoked trusted callbacks hardware=%d learning=%d", hardwareCalls, learningCalls) }
}

func TestPolicyAuthorityAllowedResultBindingUsesVerifiedGate(t *testing.T) {
	resolver, externalPortable, _ := policyAuthorityResolver(t)
	cpuCalls, hardwareCalls := 0, 0
	runtime := policyAuthorityRuntime(t, resolver, &cpuCalls, &hardwareCalls, nil)
	request := OperationRequest{DeclaredClass: "bounded.cpu.sha256", Action: metro.Action{Kind: "cpu.sha256", Inputs: map[string]any{"rounds": 1}}, Target: "executor://cpu"}
	gate, err := runtime.NewGateWithRequestedPolicy(request, externalPortable, policyAuthorityEvidence(t))
	if err != nil { t.Fatal(err) }
	decision, err := gate.RequireAllowed()
	if err != nil { t.Fatal(err) }
	bound, err := gate.BindDecisionResult(map[string]any{"reward": 1.0}, decision)
	if err != nil { t.Fatal(err) }
	if err := gate.ValidateDecisionResultBinding(bound, decision); err != nil { t.Fatalf("binding rejected: %v", err) }
	bound["policy_authorization_hash"] = policyAuthorityHashB
	if err := gate.ValidateDecisionResultBinding(bound, decision); err == nil { t.Fatal("tampered policy authority binding unexpectedly validated") }
}
