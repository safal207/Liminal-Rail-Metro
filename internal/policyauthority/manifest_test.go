package policyauthority

import (
	"crypto/ed25519"
	"crypto/sha256"
	"errors"
	"path/filepath"
	"testing"

	"github.com/safal207/Liminal-Rail-Metro/internal/trustpolicy"
)

func deterministicKey(label string) ed25519.PrivateKey {
	sum := sha256.Sum256([]byte(label))
	return ed25519.NewKeyFromSeed(sum[:])
}

func policies(t *testing.T) (trustpolicy.Policy, trustpolicy.Policy, trustpolicy.Policy) {
	t.Helper()
	externalPortable, err := trustpolicy.NewPolicy("external-portable-required", trustpolicy.Requirements{ExternalIdentity: true, PortablePublication: true})
	if err != nil { t.Fatal(err) }
	hardwareCritical, err := trustpolicy.NewPolicy("hardware-critical", trustpolicy.Requirements{ExternalIdentity: true, PortablePublication: true, HardwareBacked: true, RemoteHardwareAttestation: true})
	if err != nil { t.Fatal(err) }
	externalOnly, err := trustpolicy.NewPolicy("external-only", trustpolicy.Requirements{ExternalIdentity: true})
	if err != nil { t.Fatal(err) }
	return externalPortable, hardwareCritical, externalOnly
}

func rootChain(t *testing.T) (TrustRoot, SignedManifest, ed25519.PrivateKey, trustpolicy.Policy, trustpolicy.Policy, trustpolicy.Policy) {
	t.Helper()
	externalPortable, hardwareCritical, externalOnly := policies(t)
	manifest, err := NewManifest("rail-policy-authority", 1, []Binding{
		{OperationClass: "hardware.critical", ActionKind: "hardware.power.write", Target: "executor://hardware", SideEffect: true, Policy: hardwareCritical},
		{OperationClass: "bounded.cpu.sha256", ActionKind: "cpu.sha256", Target: "executor://cpu", SideEffect: false, Policy: externalPortable},
	})
	if err != nil { t.Fatal(err) }
	key := deterministicKey("liminal-policy-authority-root")
	signed, err := SignManifest(manifest, "rail-policy-root", key)
	if err != nil { t.Fatal(err) }
	root, err := NewTrustRoot(signed)
	if err != nil { t.Fatal(err) }
	return root, signed, key, externalPortable, hardwareCritical, externalOnly
}

func TestManifestCanonicalAcrossBindingOrder(t *testing.T) {
	externalPortable, hardwareCritical, _ := policies(t)
	a, err := NewManifest("rail-policy-authority", 1, []Binding{
		{OperationClass: "hardware.critical", ActionKind: "hardware.power.write", Target: "executor://hardware", SideEffect: true, Policy: hardwareCritical},
		{OperationClass: "bounded.cpu.sha256", ActionKind: "cpu.sha256", Target: "executor://cpu", Policy: externalPortable},
	})
	if err != nil { t.Fatal(err) }
	b, err := NewManifest("rail-policy-authority", 1, []Binding{
		{OperationClass: "bounded.cpu.sha256", ActionKind: "cpu.sha256", Target: "executor://cpu", Policy: externalPortable},
		{OperationClass: "hardware.critical", ActionKind: "hardware.power.write", Target: "executor://hardware", SideEffect: true, Policy: hardwareCritical},
	})
	if err != nil { t.Fatal(err) }
	if a.ManifestHash != b.ManifestHash { t.Fatalf("canonical manifest hash mismatch: %s != %s", a.ManifestHash, b.ManifestHash) }
}

func TestManifestRejectsAmbiguousExecutionContract(t *testing.T) {
	externalPortable, hardwareCritical, _ := policies(t)
	_, err := NewManifest("rail-policy-authority", 1, []Binding{
		{OperationClass: "a", ActionKind: "same", Target: "executor://same", Policy: externalPortable},
		{OperationClass: "b", ActionKind: "same", Target: "executor://same", Policy: hardwareCritical},
	})
	if err == nil { t.Fatal("ambiguous execution contract unexpectedly accepted") }
}

func TestResolverDerivesClassFromExecutionDescriptor(t *testing.T) {
	root, signed, _, _, hardware, _ := rootChain(t)
	state := filepath.Join(t.TempDir(), "head.json")
	resolver, err := OpenDurableResolver(state, root, []SignedManifest{signed}, nil)
	if err != nil { t.Fatal(err) }
	policy, descriptor, auth, err := resolver.ResolveOperation("hardware.power.write", map[string]any{"rail": 7}, "executor://hardware", true)
	if err != nil { t.Fatal(err) }
	if descriptor.OperationClass != "hardware.critical" || policy.PolicyHash != hardware.PolicyHash || auth.OperationClass != "hardware.critical" {
		t.Fatalf("execution descriptor resolved to wrong class: descriptor=%+v auth=%+v", descriptor, auth)
	}
	if _, _, _, err := resolver.ResolveOperation("hardware.power.write", map[string]any{"rail": 7}, "executor://cpu", false); err == nil {
		t.Fatal("hardware action mislabeled as bounded execution contract unexpectedly resolved")
	}
}

func TestSignedManifestTamperingRejected(t *testing.T) {
	_, signed, _, _, _, _ := rootChain(t)
	tampered := signed
	tampered.Manifest.ManifestHash = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	if err := tampered.SelfVerify(); err == nil { t.Fatal("tampered manifest unexpectedly verified") }
	tampered = signed
	tampered.Signature = tampered.Signature[:len(tampered.Signature)-2] + "AA"
	if err := tampered.SelfVerify(); err == nil { t.Fatal("tampered signature unexpectedly verified") }
}

func TestDurableResolverRejectsRollbackAndFork(t *testing.T) {
	root, current, currentKey, externalPortable, _, _ := rootChain(t)
	stronger, err := trustpolicy.NewPolicy("hardware-critical-v2", trustpolicy.Requirements{ExternalIdentity: true, PortablePublication: true, HardwareBacked: true, RemoteHardwareAttestation: true})
	if err != nil { t.Fatal(err) }
	nextManifest, err := NewManifest("rail-policy-authority", 2, []Binding{
		{OperationClass: "bounded.cpu.sha256", ActionKind: "cpu.sha256", Target: "executor://cpu", Policy: externalPortable},
		{OperationClass: "hardware.critical", ActionKind: "hardware.power.write", Target: "executor://hardware", SideEffect: true, Policy: stronger},
	})
	if err != nil { t.Fatal(err) }
	nextKey := deterministicKey("next")
	next, err := SignManifest(nextManifest, "rail-policy-next", nextKey)
	if err != nil { t.Fatal(err) }
	rotation, err := SignRotation(current, next, currentKey, false)
	if err != nil { t.Fatal(err) }
	state := filepath.Join(t.TempDir(), "accepted-head.json")
	accepted, err := OpenDurableResolver(state, root, []SignedManifest{current, next}, []Rotation{rotation})
	if err != nil { t.Fatal(err) }
	acceptedHead := accepted.Head()
	if _, err := OpenDurableResolver(state, root, []SignedManifest{current}, nil); !errors.Is(err, ErrChainRollback) {
		t.Fatalf("expected rollback rejection, got %v", err)
	}

	forkManifest, err := NewManifest("rail-policy-authority", 2, []Binding{
		{OperationClass: "bounded.cpu.sha256", ActionKind: "cpu.sha256.v2", Target: "executor://cpu", Policy: externalPortable},
		{OperationClass: "hardware.critical", ActionKind: "hardware.power.write", Target: "executor://hardware", SideEffect: true, Policy: stronger},
	})
	if err != nil { t.Fatal(err) }
	fork, err := SignManifest(forkManifest, "rail-policy-next", nextKey)
	if err != nil { t.Fatal(err) }
	forkRotation, err := SignRotation(current, fork, currentKey, true)
	if err != nil { t.Fatal(err) }
	if _, err := OpenDurableResolver(state, root, []SignedManifest{current, fork}, []Rotation{forkRotation}); !errors.Is(err, ErrChainFork) {
		t.Fatalf("expected fork rejection, got %v", err)
	}
	restarted, err := OpenDurableResolver(state, root, []SignedManifest{current, next}, []Rotation{rotation})
	if err != nil { t.Fatal(err) }
	if restarted.Head().HeadHash != acceptedHead.HeadHash { t.Fatal("accepted head changed after exact restart") }
}

func TestRotationRequiresExplicitWeakening(t *testing.T) {
	_, current, currentKey, externalPortable, hardwareCritical, _ := rootChain(t)
	nextKey := deterministicKey("liminal-policy-authority-next")
	nextManifest, err := NewManifest("rail-policy-authority", 2, []Binding{
		{OperationClass: "bounded.cpu.sha256", ActionKind: "cpu.sha256", Target: "executor://cpu", Policy: externalPortable},
		{OperationClass: "hardware.critical", ActionKind: "hardware.power.write", Target: "executor://hardware", SideEffect: true, Policy: hardwareCritical},
		{OperationClass: "portable.publish", ActionKind: "portable.publish", Target: "executor://publisher", SideEffect: true, Policy: externalPortable},
	})
	if err != nil { t.Fatal(err) }
	next, err := SignManifest(nextManifest, "rail-policy-next", nextKey)
	if err != nil { t.Fatal(err) }
	rotation, err := SignRotation(current, next, currentKey, false)
	if err != nil { t.Fatal(err) }
	if err := rotation.Verify(current, next); err != nil { t.Fatal(err) }

	weakerManifest, err := NewManifest("rail-policy-authority", 2, []Binding{
		{OperationClass: "bounded.cpu.sha256", ActionKind: "cpu.sha256", Target: "executor://cpu", Policy: externalPortable},
		{OperationClass: "hardware.critical", ActionKind: "hardware.power.write", Target: "executor://hardware", SideEffect: true, Policy: externalPortable},
	})
	if err != nil { t.Fatal(err) }
	weaker, err := SignManifest(weakerManifest, "rail-policy-next", nextKey)
	if err != nil { t.Fatal(err) }
	if _, err := SignRotation(current, weaker, currentKey, false); err == nil { t.Fatal("silent weakening unexpectedly accepted") }
	explicit, err := SignRotation(current, weaker, currentKey, true)
	if err != nil { t.Fatal(err) }
	if len(explicit.WeakenedOperations) != 1 || explicit.WeakenedOperations[0] != "hardware.critical" { t.Fatalf("weakening marker mismatch: %+v", explicit.WeakenedOperations) }
}
