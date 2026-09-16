package adaptive

import (
	"errors"
	"testing"

	"github.com/safal207/Liminal-Rail-Metro/internal/trustpolicy"
)

const gateHashA = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
const gateHashB = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"

func gateEvidence(t *testing.T) trustpolicy.Evidence {
	t.Helper()
	e, err := trustpolicy.NewEvidence(trustpolicy.EvidenceInput{
		ExternalIdentityVerified:    true,
		PortablePublicationVerified: true,
		SourceProofHash:             gateHashA,
		DiscoveryHash:               gateHashB,
		PortableProofHash:           gateHashA,
	})
	if err != nil {
		t.Fatal(err)
	}
	return e
}

func TestTrustGateDeniedCallbacksAreNotCalled(t *testing.T) {
	p, err := trustpolicy.NewPolicy("hardware-critical", trustpolicy.Requirements{
		ExternalIdentity:          true,
		PortablePublication:       true,
		HardwareBacked:            true,
		RemoteHardwareAttestation: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	gate, err := NewTrustGate(p, gateEvidence(t))
	if err != nil {
		t.Fatal(err)
	}

	effects := 0
	d, err := gate.Execute(func() error {
		effects++
		return nil
	})
	if !errors.Is(err, ErrTrustPolicyDenied) || d.Allowed {
		t.Fatalf("expected denied execution, decision=%+v err=%v", d, err)
	}
	if effects != 0 {
		t.Fatalf("denied execution invoked side effect %d times", effects)
	}

	learns := 0
	d, err = gate.Learn(func() error {
		learns++
		return nil
	})
	if !errors.Is(err, ErrTrustPolicyDenied) || d.Allowed {
		t.Fatalf("expected denied learning, decision=%+v err=%v", d, err)
	}
	if learns != 0 {
		t.Fatalf("denied learning invoked callback %d times", learns)
	}
}

func TestTrustGateAllowedAndResultBinding(t *testing.T) {
	p, err := trustpolicy.NewPolicy("portable-external", trustpolicy.Requirements{ExternalIdentity: true, PortablePublication: true})
	if err != nil {
		t.Fatal(err)
	}
	gate, err := NewTrustGate(p, gateEvidence(t))
	if err != nil {
		t.Fatal(err)
	}

	called := 0
	d, err := gate.Execute(func() error {
		called++
		return nil
	})
	if err != nil || !d.Allowed || called != 1 {
		t.Fatalf("expected allowed execution, decision=%+v called=%d err=%v", d, called, err)
	}

	bound, err := BindTrustDecisionResult(map[string]any{"reward": 1.0}, d)
	if err != nil {
		t.Fatal(err)
	}
	if err := ValidateTrustDecisionResultBinding(bound, d); err != nil {
		t.Fatalf("binding should validate: %v", err)
	}
	bound["trust_policy_hash"] = gateHashB
	if err := ValidateTrustDecisionResultBinding(bound, d); err == nil {
		t.Fatal("tampered trust binding unexpectedly validated")
	}
}
