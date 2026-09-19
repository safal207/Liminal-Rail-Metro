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

func TestPolicyAuthorityRejectsMislabeledHardwareActionBeforeGate(t *testing.T) {
	resolver, externalPortable, _ := policyAuthorityResolver(t)
	request := OperationRequest{
		DeclaredClass: "bounded.cpu.sha256",
		Action: metro.Action{Kind: "hardware.power.write", Inputs: map[string]any{"device": "rail-7"}},
		Target: "executor://hardware", SideEffect: true,
	}
	_, err := NewPolicyAuthorityGateWithRequestedPolicy(resolver, request, externalPortable, policyAuthorityEvidence(t))
	if !errors.Is(err, ErrOperationClassMismatch) {
		t.Fatalf("expected operation-class mismatch, got %v", err)
	}
}

func TestPolicyAuthorityRejectsWeakerRequestedPolicyBeforeGate(t *testing.T) {
	resolver, externalPortable, _ := policyAuthorityResolver(t)
	request := OperationRequest{DeclaredClass: "hardware.critical", Action: metro.Action{Kind: "hardware.power.write", Inputs: map[string]any{"device": "rail-7"}}, Target: "executor://hardware", SideEffect: true}
	_, err := NewPolicyAuthorityGateWithRequestedPolicy(resolver, request, externalPortable, policyAuthorityEvidence(t))
	if !errors.Is(err, ErrPolicyAuthorityMismatch) { t.Fatalf("expected policy authority mismatch, got %v", err) }
}

func TestPolicyAuthorityHardwarePolicyStillDeniesInsufficientEvidence(t *testing.T) {
	resolver, _, hardware := policyAuthorityResolver(t)
	request := OperationRequest{DeclaredClass: "hardware.critical", Action: metro.Action{Kind: "hardware.power.write", Inputs: map[string]any{"device": "rail-7"}}, Target: "executor://hardware", SideEffect: true}
	gate, err := NewPolicyAuthorityGateWithRequestedPolicy(resolver, request, hardware, policyAuthorityEvidence(t))
	if err != nil { t.Fatal(err) }
	calls := 0
	decision, err := gate.Execute(DispatchFunc(func(op BoundOperation) error { calls++; return nil }))
	if !errors.Is(err, ErrTrustPolicyDenied) || decision.Allowed { t.Fatalf("expected trust-policy denial, decision=%+v err=%v", decision, err) }
	if calls != 0 { t.Fatalf("denied policy invoked dispatcher %d times", calls) }
}

func TestPolicyAuthorityDispatcherReceivesExactBoundDescriptor(t *testing.T) {
	resolver, externalPortable, _ := policyAuthorityResolver(t)
	request := OperationRequest{DeclaredClass: "bounded.cpu.sha256", Action: metro.Action{Kind: "cpu.sha256", Inputs: map[string]any{"rounds": 90000}}, Target: "executor://cpu"}
	gate, err := NewPolicyAuthorityGateWithRequestedPolicy(resolver, request, externalPortable, policyAuthorityEvidence(t))
	if err != nil { t.Fatal(err) }
	called := 0
	decision, err := gate.Execute(DispatchFunc(func(op BoundOperation) error {
		called++
		if err := op.Validate(); err != nil { t.Fatal(err) }
		if op.Action.Kind != "cpu.sha256" || op.Target != "executor://cpu" || op.OperationClass != "bounded.cpu.sha256" { t.Fatalf("wrong bound operation: %+v", op) }
		return nil
	}))
	if err != nil || !decision.Allowed || called != 1 { t.Fatalf("bound dispatch failed: decision=%+v called=%d err=%v", decision, called, err) }
}

func TestPolicyAuthorityAllowedResultBindingUsesVerifiedGate(t *testing.T) {
	resolver, externalPortable, _ := policyAuthorityResolver(t)
	request := OperationRequest{DeclaredClass: "bounded.cpu.sha256", Action: metro.Action{Kind: "cpu.sha256", Inputs: map[string]any{"rounds": 1}}, Target: "executor://cpu"}
	gate, err := NewPolicyAuthorityGateWithRequestedPolicy(resolver, request, externalPortable, policyAuthorityEvidence(t))
	if err != nil { t.Fatal(err) }
	decision, err := gate.RequireAllowed()
	if err != nil { t.Fatal(err) }
	bound, err := gate.BindDecisionResult(map[string]any{"reward": 1.0}, decision)
	if err != nil { t.Fatal(err) }
	if err := gate.ValidateDecisionResultBinding(bound, decision); err != nil { t.Fatalf("binding rejected: %v", err) }
	bound["policy_authorization_hash"] = policyAuthorityHashB
	if err := gate.ValidateDecisionResultBinding(bound, decision); err == nil { t.Fatal("tampered policy authority binding unexpectedly validated") }
}
