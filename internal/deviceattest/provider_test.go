package deviceattest

import (
	"crypto/ed25519"
	"crypto/sha256"
	"strings"
	"testing"
)

func deterministicProviderKey(label string) ed25519.PrivateKey {
	seed := sha256.Sum256([]byte("liminal-provider-test:" + label))
	return ed25519.NewKeyFromSeed(seed[:])
}

func TestSoftwareProviderAdvertisesWeakAssuranceHonestly(t *testing.T) {
	p, err := NewSoftwareProvider(deterministicProviderKey("one"))
	if err != nil {
		t.Fatal(err)
	}
	d := p.Descriptor()
	if d.ProviderID == "" || d.AssuranceLevel != AssuranceSoftwareBound {
		t.Fatalf("unexpected descriptor: %#v", d)
	}
	if d.HardwareBacked || d.RemoteVerifiable {
		t.Fatalf("software provider must not claim hardware/remote assurance: %#v", d)
	}
}

func TestSoftwareProviderRoundTripAndReplayRejection(t *testing.T) {
	p, err := NewSoftwareProvider(deterministicProviderKey("roundtrip"))
	if err != nil {
		t.Fatal(err)
	}
	anchor, evidence, err := p.Enroll()
	if err != nil {
		t.Fatal(err)
	}
	if evidence.Descriptor != p.Descriptor() || anchor.Descriptor != p.Descriptor() {
		t.Fatal("provider descriptor was not preserved in envelopes")
	}
	binding := ProviderBinding{
		Nonce:                "nonce-one",
		SignedAuthorityHash:  strings.Repeat("a", 64),
		AuthorityIssuerKeyID: strings.Repeat("b", 64),
		JournalSequence:      0,
		Workload:             "sha256-2k",
		ContextKey:           "ctx-provider-test",
	}
	att, err := p.Attest(binding)
	if err != nil {
		t.Fatal(err)
	}
	if err := p.Verify(att, anchor, binding); err != nil {
		t.Fatal(err)
	}
	stale := binding
	stale.Nonce = "nonce-two"
	if err := p.Verify(att, anchor, stale); err == nil {
		t.Fatal("expected stale nonce to be rejected")
	}
}

func TestSoftwareProviderRejectsAnchorFromDifferentKey(t *testing.T) {
	p1, err := NewSoftwareProvider(deterministicProviderKey("one"))
	if err != nil {
		t.Fatal(err)
	}
	p2, err := NewSoftwareProvider(deterministicProviderKey("two"))
	if err != nil {
		t.Fatal(err)
	}
	anchor, _, err := p1.Enroll()
	if err != nil {
		t.Fatal(err)
	}
	if err := p2.ValidateAnchor(anchor); err == nil {
		t.Fatal("expected different software provider key to reject anchor")
	}
}

func TestSoftwareProviderRejectsTamperedEnvelope(t *testing.T) {
	p, err := NewSoftwareProvider(deterministicProviderKey("tamper"))
	if err != nil {
		t.Fatal(err)
	}
	anchor, _, err := p.Enroll()
	if err != nil {
		t.Fatal(err)
	}
	binding := ProviderBinding{Nonce: "nonce", SignedAuthorityHash: strings.Repeat("c", 64), AuthorityIssuerKeyID: strings.Repeat("d", 64), Workload: "w", ContextKey: "ctx"}
	att, err := p.Attest(binding)
	if err != nil {
		t.Fatal(err)
	}
	att.AttestationHash = strings.Repeat("0", 64)
	if err := p.Verify(att, anchor, binding); err == nil {
		t.Fatal("expected tampered provider envelope to be rejected")
	}
}
