package mirror

import (
	"errors"
	"testing"
)

func TestStoreAndVerifyEvidenceBundleFromCAS(t *testing.T) {
	claim, packet, route, result, receipt := verifiedFixture(t, "action-cas-001")
	policy := testCodeAuthorityPolicy()
	envelope, err := BuildProofEnvelope(claim, packet, route, result, receipt, policy)
	if err != nil {
		t.Fatalf("build proof envelope: %v", err)
	}

	store := NewMemoryCAS()
	bundle, replay, err := StoreEvidenceBundle(store, envelope, packet, route, result, receipt, policy)
	if err != nil {
		t.Fatalf("store evidence bundle: %v", err)
	}
	if replay.Status != ReplayStatusReproduced {
		t.Fatalf("expected reproduced replay before storage, got %q", replay.Status)
	}

	replayed, err := VerifyEvidenceBundleFromCAS(bundle, store)
	if err != nil {
		t.Fatalf("verify bundle from CAS: %v", err)
	}
	if replayed.Status != ReplayStatusReproduced {
		t.Fatalf("expected REPRODUCED from CAS, got %q", replayed.Status)
	}
}

func TestVerifyEvidenceBundleFromCASRejectsMissingObject(t *testing.T) {
	claim, packet, route, result, receipt := verifiedFixture(t, "action-cas-002")
	policy := testCodeAuthorityPolicy()
	envelope, err := BuildProofEnvelope(claim, packet, route, result, receipt, policy)
	if err != nil {
		t.Fatalf("build proof envelope: %v", err)
	}
	store := NewMemoryCAS()
	bundle, _, err := StoreEvidenceBundle(store, envelope, packet, route, result, receipt, policy)
	if err != nil {
		t.Fatalf("store evidence bundle: %v", err)
	}

	missing := NewMemoryCAS()
	if _, err := VerifyEvidenceBundleFromCAS(bundle, missing); err == nil {
		t.Fatal("missing CAS objects must fail closed")
	}
}

func TestVerifyEvidenceBundleFromCASRejectsResolverDigestMismatch(t *testing.T) {
	claim, packet, route, result, receipt := verifiedFixture(t, "action-cas-003")
	policy := testCodeAuthorityPolicy()
	envelope, err := BuildProofEnvelope(claim, packet, route, result, receipt, policy)
	if err != nil {
		t.Fatalf("build proof envelope: %v", err)
	}
	store := NewMemoryCAS()
	bundle, _, err := StoreEvidenceBundle(store, envelope, packet, route, result, receipt, policy)
	if err != nil {
		t.Fatalf("store evidence bundle: %v", err)
	}

	resolver := corruptResolver{base: store}
	if _, err := VerifyEvidenceBundleFromCAS(bundle, resolver); err == nil {
		t.Fatal("resolver returning bytes that do not match ref digest must fail closed")
	}
}

func TestMemoryCASPutCopiesInput(t *testing.T) {
	store := NewMemoryCAS()
	content := []byte(`{"proof":"stable"}`)
	ref, err := store.Put(content)
	if err != nil {
		t.Fatalf("put CAS object: %v", err)
	}
	content[2] = 'X'

	resolved, err := store.Resolve(ref)
	if err != nil {
		t.Fatalf("resolve CAS object: %v", err)
	}
	if string(resolved) != `{"proof":"stable"}` {
		t.Fatalf("stored CAS content changed with caller buffer: %s", resolved)
	}
}

func TestMemoryCASRejectsInvalidRef(t *testing.T) {
	store := NewMemoryCAS()
	if _, err := store.Resolve("memory://not-cas"); err == nil {
		t.Fatal("unsupported CAS ref must be rejected")
	}
}

type corruptResolver struct {
	base CASResolver
}

func (r corruptResolver) Resolve(ref string) ([]byte, error) {
	content, err := r.base.Resolve(ref)
	if err != nil {
		return nil, err
	}
	if len(content) == 0 {
		return nil, errors.New("empty object")
	}
	corrupt := append([]byte(nil), content...)
	corrupt[len(corrupt)-1] ^= 1
	return corrupt, nil
}
