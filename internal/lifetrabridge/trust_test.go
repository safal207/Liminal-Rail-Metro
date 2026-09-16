package lifetrabridge

import "testing"

const trustHashA = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
const trustHashB = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
const trustHashC = "cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc"

func TestBindTrustProofRefs(t *testing.T) {
	obs := Observation{ProofRefs: []string{"metro-receipt://receipt-1"}}
	got, err := BindTrustProofRefs(obs, trustHashA, trustHashB, trustHashC)
	if err != nil {
		t.Fatal(err)
	}
	wants := []string{
		TrustPolicyRefPrefix + trustHashA,
		TrustEvidenceRefPrefix + trustHashB,
		TrustDecisionRefPrefix + trustHashC,
	}
	for _, want := range wants {
		found := false
		for _, ref := range got.ProofRefs {
			if ref == want {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("missing proof ref %s in %v", want, got.ProofRefs)
		}
	}
}

func TestBindTrustProofRefsRejectsMalformedHash(t *testing.T) {
	if _, err := BindTrustProofRefs(Observation{}, "bad", trustHashB, trustHashC); err == nil {
		t.Fatal("expected malformed trust hash rejection")
	}
}
