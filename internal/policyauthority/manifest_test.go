package policyauthority

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/json"
	"testing"

	"github.com/safal207/Liminal-Rail-Metro/internal/trustpolicy"
)

func deterministicKey(label string) ed25519.PrivateKey {
	sum := sha256.Sum256([]byte(label))
	return ed25519.NewKeyFromSeed(sum[:])
}

func policies(t *testing.T) (trustpolicy.Policy, trustpolicy.Policy, trustpolicy.Policy) {
	t.Helper()
	externalPortable, err := trustpolicy.NewPolicy("external-portable-required", trustpolicy.Requirements{
		ExternalIdentity:    true,
		PortablePublication: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	hardwareCritical, err := trustpolicy.NewPolicy("hardware-critical", trustpolicy.Requirements{
		ExternalIdentity:          true,
		PortablePublication:       true,
		HardwareBacked:            true,
		RemoteHardwareAttestation: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	externalOnly, err := trustpolicy.NewPolicy("external-only", trustpolicy.Requirements{ExternalIdentity: true})
	if err != nil {
		t.Fatal(err)
	}
	return externalPortable, hardwareCritical, externalOnly
}

func rootChain(t *testing.T) (TrustRoot, SignedManifest, ed25519.PrivateKey, trustpolicy.Policy, trustpolicy.Policy, trustpolicy.Policy) {
	t.Helper()
	externalPortable, hardwareCritical, externalOnly := policies(t)
	manifest, err := NewManifest("rail-policy-authority", 1, []Binding{
		{OperationClass: "hardware.critical", Policy: hardwareCritical},
		{OperationClass: "bounded.cpu.sha256", Policy: externalPortable},
	})
	if err != nil {
		t.Fatal(err)
	}
	key := deterministicKey("liminal-policy-authority-root")
	signed, err := SignManifest(manifest, "rail-policy-root", key)
	if err != nil {
		t.Fatal(err)
	}
	root, err := NewTrustRoot(signed)
	if err != nil {
		t.Fatal(err)
	}
	return root, signed, key, externalPortable, hardwareCritical, externalOnly
}

func TestManifestCanonicalAcrossBindingOrder(t *testing.T) {
	externalPortable, hardwareCritical, _ := policies(t)
	a, err := NewManifest("rail-policy-authority", 1, []Binding{
		{OperationClass: "hardware.critical", Policy: hardwareCritical},
		{OperationClass: "bounded.cpu.sha256", Policy: externalPortable},
	})
	if err != nil {
		t.Fatal(err)
	}
	b, err := NewManifest("rail-policy-authority", 1, []Binding{
		{OperationClass: "bounded.cpu.sha256", Policy: externalPortable},
		{OperationClass: "hardware.critical", Policy: hardwareCritical},
	})
	if err != nil {
		t.Fatal(err)
	}
	if a.ManifestHash != b.ManifestHash {
		t.Fatalf("canonical manifest hash mismatch: %s != %s", a.ManifestHash, b.ManifestHash)
	}
	if a.Bindings[0].OperationClass != "bounded.cpu.sha256" {
		t.Fatalf("bindings not canonical: %+v", a.Bindings)
	}
}

func TestResolverRejectsWeakPolicySubstitution(t *testing.T) {
	root, signed, _, _, hardwareCritical, externalOnly := rootChain(t)
	resolver, err := OpenResolver(root, []SignedManifest{signed}, nil)
	if err != nil {
		t.Fatal(err)
	}
	policy, auth, err := resolver.Resolve("hardware.critical")
	if err != nil {
		t.Fatal(err)
	}
	if policy.PolicyHash != hardwareCritical.PolicyHash || auth.PolicyHash != hardwareCritical.PolicyHash {
		t.Fatalf("wrong authority policy: policy=%s auth=%s want=%s", policy.PolicyHash, auth.PolicyHash, hardwareCritical.PolicyHash)
	}
	if err := VerifyRequestedPolicy(auth, externalOnly); err == nil {
		t.Fatal("weaker requested policy unexpectedly accepted")
	}
	if err := VerifyRequestedPolicy(auth, hardwareCritical); err != nil {
		t.Fatalf("exact authority policy rejected: %v", err)
	}
}

func TestSignedManifestTamperingRejected(t *testing.T) {
	_, signed, _, _, _, _ := rootChain(t)
	tampered := signed
	tampered.Manifest.Bindings[0].OperationClass = "bounded.cpu.changed"
	if err := tampered.SelfVerify(); err == nil {
		t.Fatal("tampered manifest unexpectedly verified")
	}

	tampered = signed
	tampered.Signature = tampered.Signature[:len(tampered.Signature)-2] + "AA"
	if err := tampered.SelfVerify(); err == nil {
		t.Fatal("tampered signature unexpectedly verified")
	}
}

func TestResolverRoundTripRestartStable(t *testing.T) {
	root, signed, _, _, _, _ := rootChain(t)
	resolver, err := OpenResolver(root, []SignedManifest{signed}, nil)
	if err != nil {
		t.Fatal(err)
	}
	_, before, err := resolver.Resolve("hardware.critical")
	if err != nil {
		t.Fatal(err)
	}

	blob, err := json.Marshal(struct {
		Root     TrustRoot      `json:"root"`
		Manifest SignedManifest `json:"manifest"`
	}{root, signed})
	if err != nil {
		t.Fatal(err)
	}
	var restored struct {
		Root     TrustRoot      `json:"root"`
		Manifest SignedManifest `json:"manifest"`
	}
	if err := json.Unmarshal(blob, &restored); err != nil {
		t.Fatal(err)
	}
	restarted, err := OpenResolver(restored.Root, []SignedManifest{restored.Manifest}, nil)
	if err != nil {
		t.Fatal(err)
	}
	_, after, err := restarted.Resolve("hardware.critical")
	if err != nil {
		t.Fatal(err)
	}
	if before.AuthorizationHash != after.AuthorizationHash {
		t.Fatalf("authorization changed after restart: %s != %s", before.AuthorizationHash, after.AuthorizationHash)
	}
}

func TestRotationRequiresExplicitWeakening(t *testing.T) {
	root, current, currentKey, externalPortable, hardwareCritical, _ := rootChain(t)
	_ = root

	nextKey := deterministicKey("liminal-policy-authority-next")
	nextManifest, err := NewManifest("rail-policy-authority", 2, []Binding{
		{OperationClass: "bounded.cpu.sha256", Policy: externalPortable},
		{OperationClass: "hardware.critical", Policy: hardwareCritical},
		{OperationClass: "portable.publish", Policy: externalPortable},
	})
	if err != nil {
		t.Fatal(err)
	}
	next, err := SignManifest(nextManifest, "rail-policy-next", nextKey)
	if err != nil {
		t.Fatal(err)
	}
	rotation, err := SignRotation(current, next, currentKey, false)
	if err != nil {
		t.Fatal(err)
	}
	if err := rotation.Verify(current, next); err != nil {
		t.Fatalf("valid rotation rejected: %v", err)
	}
	resolver, err := OpenResolver(root, []SignedManifest{current, next}, []Rotation{rotation})
	if err != nil {
		t.Fatal(err)
	}
	_, auth, err := resolver.Resolve("hardware.critical")
	if err != nil {
		t.Fatal(err)
	}
	if auth.RotationHash != rotation.RotationHash || auth.Generation != 2 {
		t.Fatalf("rotation not reflected in authorization: %+v", auth)
	}

	weakerManifest, err := NewManifest("rail-policy-authority", 2, []Binding{
		{OperationClass: "bounded.cpu.sha256", Policy: externalPortable},
		{OperationClass: "hardware.critical", Policy: externalPortable},
	})
	if err != nil {
		t.Fatal(err)
	}
	weaker, err := SignManifest(weakerManifest, "rail-policy-next", nextKey)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := SignRotation(current, weaker, currentKey, false); err == nil {
		t.Fatal("silent policy weakening unexpectedly accepted")
	}
	explicit, err := SignRotation(current, weaker, currentKey, true)
	if err != nil {
		t.Fatalf("explicitly authorized weakening should be signable: %v", err)
	}
	if len(explicit.WeakenedOperations) != 1 || explicit.WeakenedOperations[0] != "hardware.critical" {
		t.Fatalf("weakening marker mismatch: %+v", explicit.WeakenedOperations)
	}
	if err := explicit.Verify(current, weaker); err != nil {
		t.Fatalf("explicit weakening rotation rejected: %v", err)
	}
}
