package trustpolicy

import "testing"

const testHash = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
const otherHash = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"

func portableEvidence(t *testing.T) Evidence {
	t.Helper()
	e, err := NewEvidence(EvidenceInput{
		ExternalIdentityVerified:    true,
		PortablePublicationVerified: true,
		HardwareBacked:              false,
		RemoteHardwareAttestation:   false,
		SourceProofHash:              testHash,
		DiscoveryHash:                otherHash,
		PortableProofHash:            testHash,
	})
	if err != nil {
		t.Fatal(err)
	}
	return e
}

func TestPortableExternalPolicyAllows(t *testing.T) {
	p, err := NewPolicy("portable-external", Requirements{ExternalIdentity: true, PortablePublication: true})
	if err != nil {
		t.Fatal(err)
	}
	d, err := Evaluate(p, portableEvidence(t))
	if err != nil {
		t.Fatal(err)
	}
	if !d.Allowed || len(d.Unmet) != 0 {
		t.Fatalf("expected ALLOW, got %+v", d)
	}
	if err := VerifyDecision(d, p, portableEvidence(t)); err != nil {
		t.Fatalf("verify decision: %v", err)
	}
}

func TestHardwareCriticalPolicyFailsClosed(t *testing.T) {
	p, err := NewPolicy("hardware-critical", Requirements{
		ExternalIdentity:           true,
		PortablePublication:       true,
		HardwareBacked:            true,
		RemoteHardwareAttestation: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	d, err := Evaluate(p, portableEvidence(t))
	if err != nil {
		t.Fatal(err)
	}
	if d.Allowed {
		t.Fatal("hardware-critical policy unexpectedly allowed software-bound evidence")
	}
	want := []string{"hardware_backed", "remote_hardware_attestation"}
	if len(d.Unmet) != len(want) {
		t.Fatalf("unexpected unmet: %v", d.Unmet)
	}
	for i := range want {
		if d.Unmet[i] != want[i] {
			t.Fatalf("unexpected unmet order/content: %v", d.Unmet)
		}
	}
}

func TestRemoteHardwareRequirementMustRequireHardware(t *testing.T) {
	if _, err := NewPolicy("invalid", Requirements{RemoteHardwareAttestation: true}); err == nil {
		t.Fatal("expected invalid policy to fail")
	}
}

func TestTamperingIsDetected(t *testing.T) {
	p, err := NewPolicy("portable-external", Requirements{ExternalIdentity: true, PortablePublication: true})
	if err != nil {
		t.Fatal(err)
	}
	e := portableEvidence(t)
	d, err := Evaluate(p, e)
	if err != nil {
		t.Fatal(err)
	}

	tamperedPolicy := p
	tamperedPolicy.Requirements.HardwareBacked = true
	if err := tamperedPolicy.Validate(); err == nil {
		t.Fatal("tampered policy hash unexpectedly validated")
	}

	tamperedEvidence := e
	tamperedEvidence.HardwareBacked = true
	if err := tamperedEvidence.Validate(); err == nil {
		t.Fatal("tampered evidence hash unexpectedly validated")
	}

	tamperedDecision := d
	tamperedDecision.Allowed = false
	if err := tamperedDecision.Validate(); err == nil {
		t.Fatal("tampered decision unexpectedly validated")
	}
}
