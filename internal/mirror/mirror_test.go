package mirror

import "testing"

func TestGateRejectsNoEvidence(t *testing.T) {
	claim := Claim{
		ID:        "claim-001",
		ActionID:  "action-001",
		ValueHash: mustHash(t, map[string]any{"status": "done"}),
	}

	decision := (Gate{}).Evaluate(claim, nil)
	if decision.Commit {
		t.Fatal("expected claim without evidence to fail closed")
	}
}

func TestGateRejectsHundredMatchingMirrorsWithoutExternalProof(t *testing.T) {
	valueHash := mustHash(t, map[string]any{"status": "done"})
	claim := Claim{
		ID:        "claim-002",
		ActionID:  "action-002",
		ValueHash: valueHash,
	}

	evidence := make([]Evidence, 0, 100)
	for i := 0; i < 100; i++ {
		evidence = append(evidence, Evidence{
			ClaimID:    claim.ID,
			ActionID:   claim.ActionID,
			Source:     SourceMirrorSystem,
			ValueHash:  valueHash,
			Verified:   true,
			Provenance: []string{"mirror://system"},
		})
	}

	decision := (Gate{}).Evaluate(claim, evidence)
	if decision.Commit {
		t.Fatal("100 matching mirrors must not satisfy the external-proof boundary")
	}
	if decision.VerifiedExternal != 0 {
		t.Fatalf("expected zero verified external proofs, got %d", decision.VerifiedExternal)
	}
}

func TestGateAcceptsVerifiedExternalProofWithProvenance(t *testing.T) {
	valueHash := mustHash(t, map[string]any{"status": "done"})
	claim := Claim{
		ID:        "claim-003",
		ActionID:  "action-003",
		ValueHash: valueHash,
	}

	evidence := []Evidence{{
		ClaimID:    claim.ID,
		ActionID:   claim.ActionID,
		Source:     SourceExternal,
		ValueHash:  valueHash,
		Verified:   true,
		Provenance: []string{"receipt://receipt-action-003"},
	}}

	decision := (Gate{}).Evaluate(claim, evidence)
	if !decision.Commit {
		t.Fatalf("expected verified external proof to authorize commit: %s", decision.Reason)
	}
	if decision.VerifiedExternal != 1 {
		t.Fatalf("expected one verified external proof, got %d", decision.VerifiedExternal)
	}
}

func TestGateRejectsUnverifiedOrUnprovenancedExternalEvidence(t *testing.T) {
	valueHash := mustHash(t, map[string]any{"status": "done"})
	claim := Claim{
		ID:        "claim-004",
		ActionID:  "action-004",
		ValueHash: valueHash,
	}

	cases := []struct {
		name     string
		evidence Evidence
	}{
		{
			name: "unverified",
			evidence: Evidence{
				ClaimID:    claim.ID,
				ActionID:   claim.ActionID,
				Source:     SourceExternal,
				ValueHash:  valueHash,
				Verified:   false,
				Provenance: []string{"receipt://candidate"},
			},
		},
		{
			name: "missing provenance",
			evidence: Evidence{
				ClaimID:   claim.ID,
				ActionID:  claim.ActionID,
				Source:    SourceExternal,
				ValueHash: valueHash,
				Verified:  true,
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			decision := (Gate{}).Evaluate(claim, []Evidence{tc.evidence})
			if decision.Commit {
				t.Fatal("expected incomplete external evidence to fail closed")
			}
		})
	}
}

func TestGateRejectsExternalProofBoundToDifferentClaim(t *testing.T) {
	valueHash := mustHash(t, map[string]any{"status": "done"})
	claim := Claim{
		ID:        "claim-005",
		ActionID:  "action-005",
		ValueHash: valueHash,
	}

	evidence := []Evidence{{
		ClaimID:    "claim-other",
		ActionID:   claim.ActionID,
		Source:     SourceExternal,
		ValueHash:  valueHash,
		Verified:   true,
		Provenance: []string{"receipt://other-claim"},
	}}

	decision := (Gate{}).Evaluate(claim, evidence)
	if decision.Commit {
		t.Fatal("proof bound to another claim must not authorize commit")
	}
}

func TestGateFreezesIrreversibleClaimOnDivergence(t *testing.T) {
	claimedHash := mustHash(t, map[string]any{"status": "paid", "amount": 100})
	conflictingHash := mustHash(t, map[string]any{"status": "paid", "amount": 200})
	claim := Claim{
		ID:           "claim-006",
		ActionID:     "action-006",
		ValueHash:    claimedHash,
		Irreversible: true,
	}

	evidence := []Evidence{
		{
			ClaimID:    claim.ID,
			ActionID:   claim.ActionID,
			Source:     SourceExternal,
			ValueHash:  claimedHash,
			Verified:   true,
			Provenance: []string{"receipt://payment-provider"},
		},
		{
			ClaimID:    claim.ID,
			ActionID:   claim.ActionID,
			Source:     SourceMirrorDomain,
			ValueHash:  conflictingHash,
			Verified:   true,
			Provenance: []string{"mirror://payments"},
		},
	}

	decision := (Gate{}).Evaluate(claim, evidence)
	if decision.Commit {
		t.Fatal("irreversible effect must freeze when bound evidence diverges")
	}
	if !decision.Divergent {
		t.Fatal("expected divergence to be reported")
	}
	if decision.VerifiedExternal != 1 {
		t.Fatalf("expected matching external proof to remain visible, got %d", decision.VerifiedExternal)
	}
}

func mustHash(t *testing.T, value any) string {
	t.Helper()
	hash, err := HashValue(value)
	if err != nil {
		t.Fatalf("hash value: %v", err)
	}
	return hash
}
