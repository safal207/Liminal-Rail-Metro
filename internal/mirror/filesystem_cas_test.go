package mirror

import (
	"os"
	"path/filepath"
	"testing"
)

func TestFilesystemCASPutResolveAndLayout(t *testing.T) {
	store, err := NewFilesystemCAS(t.TempDir())
	if err != nil {
		t.Fatalf("new filesystem CAS: %v", err)
	}
	content := []byte(`{"proof":"filesystem"}`)
	ref, err := store.Put(content)
	if err != nil {
		t.Fatalf("put filesystem CAS object: %v", err)
	}

	digest, err := digestFromCASRef(ref)
	if err != nil {
		t.Fatalf("parse CAS ref: %v", err)
	}
	path := filepath.Join(store.Root(), "sha256", digest[:2], digest)
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("expected content-addressed object at %s: %v", path, err)
	}

	resolved, err := store.Resolve(ref)
	if err != nil {
		t.Fatalf("resolve filesystem CAS object: %v", err)
	}
	if string(resolved) != string(content) {
		t.Fatalf("resolved content mismatch: %s", resolved)
	}
}

func TestFilesystemCASPutIsIdempotent(t *testing.T) {
	store, err := NewFilesystemCAS(t.TempDir())
	if err != nil {
		t.Fatalf("new filesystem CAS: %v", err)
	}
	content := []byte(`{"same":true}`)
	first, err := store.Put(content)
	if err != nil {
		t.Fatalf("first put: %v", err)
	}
	second, err := store.Put(content)
	if err != nil {
		t.Fatalf("second put: %v", err)
	}
	if first != second {
		t.Fatalf("same content must produce same ref: %s != %s", first, second)
	}
}

func TestFilesystemCASRejectsCorruptedStoredObject(t *testing.T) {
	store, err := NewFilesystemCAS(t.TempDir())
	if err != nil {
		t.Fatalf("new filesystem CAS: %v", err)
	}
	content := []byte(`{"stable":true}`)
	ref, err := store.Put(content)
	if err != nil {
		t.Fatalf("put object: %v", err)
	}
	digest, err := digestFromCASRef(ref)
	if err != nil {
		t.Fatalf("parse ref: %v", err)
	}
	path := store.objectPath(digest)
	if err := os.WriteFile(path, []byte(`{"stable":false}`), 0o644); err != nil {
		t.Fatalf("corrupt object: %v", err)
	}

	if _, err := store.Resolve(ref); err == nil {
		t.Fatal("corrupted filesystem CAS object must fail digest verification")
	}
	if _, err := store.Put(content); err == nil {
		t.Fatal("Put must not silently overwrite a corrupted object at an existing digest path")
	}
}

func TestFilesystemCASStoresAndReplaysEvidenceBundle(t *testing.T) {
	claim, packet, route, result, receipt := verifiedFixture(t, "action-fs-cas-001")
	policy := testCodeAuthorityPolicy()
	envelope, err := BuildProofEnvelope(claim, packet, route, result, receipt, policy)
	if err != nil {
		t.Fatalf("build proof envelope: %v", err)
	}
	store, err := NewFilesystemCAS(t.TempDir())
	if err != nil {
		t.Fatalf("new filesystem CAS: %v", err)
	}

	bundle, replay, err := StoreEvidenceBundle(store, envelope, packet, route, result, receipt, policy)
	if err != nil {
		t.Fatalf("store evidence bundle: %v", err)
	}
	if replay.Status != ReplayStatusReproduced {
		t.Fatalf("expected pre-storage replay to reproduce, got %q", replay.Status)
	}

	replayed, err := VerifyEvidenceBundleFromCAS(bundle, store)
	if err != nil {
		t.Fatalf("verify bundle from filesystem CAS: %v", err)
	}
	if replayed.Status != ReplayStatusReproduced {
		t.Fatalf("expected CAS-only replay REPRODUCED, got %q", replayed.Status)
	}
}

func TestFilesystemCASRejectsInvalidRef(t *testing.T) {
	store, err := NewFilesystemCAS(t.TempDir())
	if err != nil {
		t.Fatalf("new filesystem CAS: %v", err)
	}
	if _, err := store.Resolve("cas://sha1/deadbeef"); err == nil {
		t.Fatal("unsupported CAS reference must fail closed")
	}
}
