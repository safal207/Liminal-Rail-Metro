package lifetrabridge

import (
	"crypto/ed25519"
	"crypto/sha256"
	"path/filepath"
	"testing"

	"github.com/safal207/Liminal-Rail-Metro/internal/policyauthority"
	"github.com/safal207/Liminal-Rail-Metro/internal/trustpolicy"
)

func TestBindPolicyAuthorityProofRefs(t *testing.T) {
	policy, err := trustpolicy.NewPolicy("external-portable-required", trustpolicy.Requirements{ExternalIdentity: true, PortablePublication: true})
	if err != nil {
		t.Fatal(err)
	}
	manifest, err := policyauthority.NewManifest("rail-policy-authority", 1, []policyauthority.Binding{{
		OperationClass: "bounded.cpu.sha256", ActionKind: "cpu.sha256", Target: "executor://cpu", Policy: policy,
	}})
	if err != nil {
		t.Fatal(err)
	}
	seed := sha256.Sum256([]byte("lifetra-policy-authority-test"))
	key := ed25519.NewKeyFromSeed(seed[:])
	signed, err := policyauthority.SignManifest(manifest, "rail-policy-root", key)
	if err != nil {
		t.Fatal(err)
	}
	root, err := policyauthority.NewTrustRoot(signed)
	if err != nil {
		t.Fatal(err)
	}
	resolver, err := policyauthority.OpenDurableResolver(filepath.Join(t.TempDir(), "head.json"), root, []policyauthority.SignedManifest{signed}, nil)
	if err != nil {
		t.Fatal(err)
	}
	_, _, authorization, err := resolver.ResolveOperation("cpu.sha256", map[string]any{"rounds": 1}, "executor://cpu", false)
	if err != nil {
		t.Fatal(err)
	}

	obs := Observation{ProofRefs: []string{"metro-receipt://receipt-1"}}
	bound, err := BindPolicyAuthorityProofRefs(obs, authorization)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		PolicyAuthorityManifestRefPrefix + authorization.SignedManifestHash,
		PolicyAuthorityHeadRefPrefix + authorization.ChainHeadHash,
		PolicyAuthorizationRefPrefix + authorization.AuthorizationHash,
	} {
		if !containsPolicyAuthorityRef(bound.ProofRefs, want) {
			t.Fatalf("missing proof ref %s in %v", want, bound.ProofRefs)
		}
	}
}

func containsPolicyAuthorityRef(refs []string, want string) bool {
	for _, ref := range refs {
		if ref == want {
			return true
		}
	}
	return false
}
