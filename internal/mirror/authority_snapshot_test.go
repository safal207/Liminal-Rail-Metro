package mirror

import "testing"

func TestAuthorityPolicyHashStableAcrossRuleAndExecutorOrder(t *testing.T) {
	left := AuthorityPolicy{
		Protocol:  AuthorityPolicyProtocol,
		ID:        "authority-v1",
		PolicyRef: "policy://liminal-rail/authority/v1",
		ExecutorsByAction: map[string][]string{
			"payment.capture": {"payments-backup", "payments-agent", "payments-agent"},
			"code.implement":  {"code-agent"},
		},
	}
	right := AuthorityPolicy{
		ID:        "authority-v1",
		PolicyRef: "policy://liminal-rail/authority/v1",
		ExecutorsByAction: map[string][]string{
			"code.implement":  {"code-agent"},
			"payment.capture": {"payments-agent", "payments-backup"},
		},
	}

	leftSnapshot, err := left.Snapshot()
	if err != nil {
		t.Fatalf("left snapshot: %v", err)
	}
	rightSnapshot, err := right.Snapshot()
	if err != nil {
		t.Fatalf("right snapshot: %v", err)
	}
	if leftSnapshot.PolicyHash != rightSnapshot.PolicyHash {
		t.Fatalf("semantically equivalent policies must hash identically: %s != %s", leftSnapshot.PolicyHash, rightSnapshot.PolicyHash)
	}
}

func TestAuthorityPolicyHashChangesWhenAuthorityChanges(t *testing.T) {
	base := AuthorityPolicy{
		ID:        "authority-v1",
		PolicyRef: "policy://liminal-rail/authority/v1",
		ExecutorsByAction: map[string][]string{
			"payment.capture": {"payments-agent"},
		},
	}
	changed := AuthorityPolicy{
		ID:        "authority-v1",
		PolicyRef: "policy://liminal-rail/authority/v1",
		ExecutorsByAction: map[string][]string{
			"payment.capture": {"code-agent"},
		},
	}

	baseSnapshot, err := base.Snapshot()
	if err != nil {
		t.Fatalf("base snapshot: %v", err)
	}
	changedSnapshot, err := changed.Snapshot()
	if err != nil {
		t.Fatalf("changed snapshot: %v", err)
	}
	if baseSnapshot.PolicyHash == changedSnapshot.PolicyHash {
		t.Fatal("changing executor authority must change policy hash")
	}
}

func TestAuthorityPolicySnapshotRequiresDurableRef(t *testing.T) {
	policy := AuthorityPolicy{
		ID: "authority-v1",
		ExecutorsByAction: map[string][]string{
			"code.implement": {"code-agent"},
		},
	}

	if _, err := policy.Snapshot(); err == nil {
		t.Fatal("policy snapshot without policy_ref must fail closed")
	}
}

func TestAuthorityPolicyRejectsUnexpectedProtocolForSnapshot(t *testing.T) {
	policy := AuthorityPolicy{
		Protocol:  "mirror.authority-policy.v9",
		ID:        "authority-v1",
		PolicyRef: "policy://liminal-rail/authority/v1",
		ExecutorsByAction: map[string][]string{
			"code.implement": {"code-agent"},
		},
	}

	if _, err := policy.Snapshot(); err == nil {
		t.Fatal("unknown authority protocol must not produce immutable provenance")
	}
}

func TestAuthorizeProofCarriesPolicyHashAndRef(t *testing.T) {
	policy := AuthorityPolicy{
		ID:        "authority-v1",
		PolicyRef: "policy://liminal-rail/authority/v1",
		ExecutorsByAction: map[string][]string{
			"code.implement": {"code-agent"},
		},
	}

	proof, err := policy.AuthorizeProof("code.implement", "code-agent")
	if err != nil {
		t.Fatalf("authorize proof: %v", err)
	}
	if proof.PolicyRef != policy.PolicyRef || len(proof.PolicyHash) != 64 {
		t.Fatalf("expected durable policy ref + sha256 hash, got ref=%q hash=%q", proof.PolicyRef, proof.PolicyHash)
	}
	if proof.ActionKind != "code.implement" || proof.ExecutorID != "code-agent" {
		t.Fatal("authority proof lost action/executor binding")
	}
}
