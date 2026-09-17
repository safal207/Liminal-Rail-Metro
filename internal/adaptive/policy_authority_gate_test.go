package adaptive

import (
	"crypto/ed25519"
	"crypto/sha256"
	"errors"
	"testing"

	"github.com/safal207/Liminal-Rail-Metro/internal/policyauthority"
	"github.com/safal207/Liminal-Rail-Metro/internal/trustpolicy"
)

const policyAuthorityHashA = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
const policyAuthorityHashB = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"

func policyAuthorityResolver(t *testing.T) (*policyauthority.Resolver, trustpolicy.Policy, trustpolicy.Policy) {
	t.Helper()
	externalPortable, err := trustpolicy.NewPolicy("external-portable-required", trustpolicy.Requirements{
		ExternalIdentity:    true,
		PortablePublication: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	hardware, err := trustpolicy.NewPolicy("hardware-critical", trustpolicy.Requirements{
		ExternalIdentity:          true,
		PortablePublication:       true,
		HardwareBacked:            true,
		RemoteHardwareAttestation: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	manifest, err := policyauthority.NewManifest("rail-policy-authority", 1, []policyauthority.Binding{
		{OperationClass: "bounded.cpu.sha256", Policy: externalPortable},
		{OperationClass: "hardware.critical", Policy: hardware},
	})
	if err != nil {
		t.Fatal(err)
	}
	seed := sha256.Sum256([]byte("adaptive-policy-authority-test"))
	key := ed25519.NewKeyFromSeed(seed[:])
	signed, err := policyauthority.SignManifest(manifest, "rail-policy-root", key)
	if err != nil {
		t.Fatal(err)
	}
	root, err := policyauthority.NewTrustRoot(signed)
	if err != nil {
		t.Fatal(err)
	}
	resolver, err := policyauthority.OpenResolver(root, []policyauthority.SignedManifest{signed}, nil)
	if err != nil {
		t.Fatal(err)
	}
	return resolver, externalPortable, hardware
}

func policyAuthorityEvidence(t *testing.T) trustpolicy.Evidence {
	t.Helper()
	e, err := trustpolicy.NewEvidence(trustpolicy.EvidenceInput{
		ExternalIdentityVerified:    true,
		PortablePublicationVerified: true,
		SourceProofHash:             policyAuthorityHashA,
		DiscoveryHash:               policyAuthorityHashB,
		PortableProofHash:           policyAuthorityHashA,
	})
	if err != nil {
		t.Fatal(err)
	}
	return e
}

func TestPolicyAuthorityRejectsWeakerRequestedPolicyBeforeGate(t *testing.T) {
	resolver, _, _ := policyAuthorityResolver(t)
	weaker, err := trustpolicy.NewPolicy("external-only", trustpolicy.Requirements{ExternalIdentity: true})
	if err != nil {
		t.Fatal(err)
	}
	_, err = NewPolicyAuthorityGateWithRequestedPolicy(resolver, "hardware.critical", weaker, policyAuthorityEvidence(t))
	if !errors.Is(err, ErrPolicyAuthorityMismatch) {
		t.Fatalf("expected policy authority mismatch, got %v", err)
	}
}

func TestPolicyAuthorityHardwarePolicyStillDeniesInsufficientEvidence(t *testing.T) {
	resolver, _, hardware := policyAuthorityResolver(t)
	gate, err := NewPolicyAuthorityGateWithRequestedPolicy(resolver, "hardware.critical", hardware, policyAuthorityEvidence(t))
	if err != nil {
		t.Fatal(err)
	}
	calls := 0
	decision, err := gate.Execute(func() error {
		calls++
		return nil
	})
	if !errors.Is(err, ErrTrustPolicyDenied) || decision.Allowed {
		t.Fatalf("expected trust-policy denial, decision=%+v err=%v", decision, err)
	}
	if calls != 0 {
		t.Fatalf("denied policy invoked effect %d times", calls)
	}
}

func TestPolicyAuthorityAllowedResultBinding(t *testing.T) {
	resolver, externalPortable, _ := policyAuthorityResolver(t)
	gate, err := NewPolicyAuthorityGateWithRequestedPolicy(resolver, "bounded.cpu.sha256", externalPortable, policyAuthorityEvidence(t))
	if err != nil {
		t.Fatal(err)
	}
	decision, err := gate.RequireAllowed()
	if err != nil {
		t.Fatal(err)
	}
	bound, err := BindPolicyAuthorityDecisionResult(map[string]any{"reward": 1.0}, gate.Authorization(), decision)
	if err != nil {
		t.Fatal(err)
	}
	if err := ValidatePolicyAuthorityDecisionResultBinding(bound, gate.Authorization(), decision); err != nil {
		t.Fatalf("binding rejected: %v", err)
	}
	bound["policy_authorization_hash"] = policyAuthorityHashB
	if err := ValidatePolicyAuthorityDecisionResultBinding(bound, gate.Authorization(), decision); err == nil {
		t.Fatal("tampered policy authority binding unexpectedly validated")
	}
}
