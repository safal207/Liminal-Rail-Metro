package deviceattest

import (
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"math/big"
	"testing"
)

func TestAudienceMatches(t *testing.T) {
	if !audienceMatches("liminal", "liminal") {
		t.Fatal("string audience did not match")
	}
	if !audienceMatches([]interface{}{"other", "liminal"}, "liminal") {
		t.Fatal("array audience did not match")
	}
	if audienceMatches("other", "liminal") {
		t.Fatal("unexpected audience match")
	}
}

func TestRSAKeyForID(t *testing.T) {
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	n := base64.RawURLEncoding.EncodeToString(privateKey.PublicKey.N.Bytes())
	e := big.NewInt(int64(privateKey.PublicKey.E)).Bytes()
	set := jsonWebKeySet{Keys: []jsonWebKey{{KeyType: "RSA", KeyID: "kid-1", Algorithm: "RS256", N: n, E: base64.RawURLEncoding.EncodeToString(e)}}}
	publicKey, err := rsaKeyForID(set, "kid-1")
	if err != nil {
		t.Fatal(err)
	}
	if publicKey.N.Cmp(privateKey.PublicKey.N) != 0 || publicKey.E != privateKey.PublicKey.E {
		t.Fatal("reconstructed RSA key does not match")
	}
}

func TestDiscoveryReportDetectsTamper(t *testing.T) {
	fallback := ProviderDescriptor{Protocol: ProviderDescriptorProtocol, ProviderID: "software", RootType: "software-observed", AssuranceLevel: AssuranceSoftwareBound}
	report := DiscoveryReport{
		Protocol:               DiscoveryProtocol,
		Probes:                 []DiscoveryProbe{{Kind: "tpm2", Source: "/dev/tpm0", Reason: "absent"}},
		SelectedProviderID:     fallback.ProviderID,
		SelectedAssuranceLevel: fallback.AssuranceLevel,
		SelectionReason:        "fallback",
	}
	material := discoveryHashMaterial{Protocol: report.Protocol, Probes: report.Probes, SelectedProviderID: report.SelectedProviderID, SelectedAssuranceLevel: report.SelectedAssuranceLevel, SelectionReason: report.SelectionReason}
	h, err := hashJSON(material)
	if err != nil {
		t.Fatal(err)
	}
	report.ReportHash = h
	if err := report.Validate(); err != nil {
		t.Fatal(err)
	}
	report.SelectionReason = "tampered"
	if err := report.Validate(); err == nil {
		t.Fatal("tampered discovery report should fail validation")
	}
}
