package deviceattest

import (
	"crypto/ed25519"
	"crypto/sha256"
	"strings"
	"testing"
)

func testKey(label string) ed25519.PrivateKey {
	s := sha256.Sum256([]byte(label))
	return ed25519.NewKeyFromSeed(s[:])
}

func TestSenseStableWithinRuntime(t *testing.T) {
	a, err := Sense()
	if err != nil {
		t.Fatal(err)
	}
	b, err := Sense()
	if err != nil {
		t.Fatal(err)
	}
	if a.DeviceFingerprint != b.DeviceFingerprint || a.RuntimeFingerprint != b.RuntimeFingerprint {
		t.Fatal("fingerprint changed in same runtime")
	}
}

func TestAttestationBindsNonceAuthorityJournalAndRuntime(t *testing.T) {
	e, err := Sense()
	if err != nil {
		t.Fatal(err)
	}
	priv := testKey("device")
	pub := priv.Public().(ed25519.PublicKey)
	anchor, err := NewTrustAnchor(e, pub)
	if err != nil {
		t.Fatal(err)
	}
	authority := strings.Repeat("a", 64)
	issuer := strings.Repeat("b", 64)
	head := strings.Repeat("c", 64)
	a, err := Sign(e, priv, "nonce-1", authority, issuer, 7, head, "sha256-2k", "ctx-1")
	if err != nil {
		t.Fatal(err)
	}
	if err := VerifyBound(a, anchor, "nonce-1", e, authority, issuer, 7, head, "sha256-2k", "ctx-1"); err != nil {
		t.Fatal(err)
	}
	if err := VerifyBound(a, anchor, "nonce-2", e, authority, issuer, 7, head, "sha256-2k", "ctx-1"); err == nil {
		t.Fatal("expected replay nonce rejection")
	}
}

func TestDifferentDeviceKeyRejected(t *testing.T) {
	e, err := Sense()
	if err != nil {
		t.Fatal(err)
	}
	priv := testKey("device")
	other := testKey("other")
	anchor, err := NewTrustAnchor(e, priv.Public().(ed25519.PublicKey))
	if err != nil {
		t.Fatal(err)
	}
	a, err := Sign(e, other, "nonce", strings.Repeat("a", 64), strings.Repeat("b", 64), 0, "", "w", "ctx")
	if err != nil {
		t.Fatal(err)
	}
	if err := VerifyBound(a, anchor, "nonce", e, strings.Repeat("a", 64), strings.Repeat("b", 64), 0, "", "w", "ctx"); err == nil {
		t.Fatal("expected unpinned key rejection")
	}
}

func TestTamperFailsSignature(t *testing.T) {
	e, err := Sense()
	if err != nil {
		t.Fatal(err)
	}
	priv := testKey("device")
	a, err := Sign(e, priv, "nonce", strings.Repeat("a", 64), strings.Repeat("b", 64), 0, "", "w", "ctx")
	if err != nil {
		t.Fatal(err)
	}
	a.RuntimeFingerprint = strings.Repeat("0", 64)
	if err := a.SelfVerify(); err == nil {
		t.Fatal("expected tamper rejection")
	}
}
