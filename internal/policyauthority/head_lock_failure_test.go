package policyauthority

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestChainHeadLockOpenFailureIsClosed(t *testing.T) {
	_, signed, _, _, _, _ := rootChain(t)
	candidate, err := chainHeadFor([]SignedManifest{signed}, nil)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "head.json")
	if err := os.Mkdir(path+".lock", 0o700); err != nil {
		t.Fatal(err)
	}
	if err := acceptChainHead(path, candidate, []SignedManifest{signed}, nil); err == nil {
		t.Fatal("head accepted without acquiring lock")
	}
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("head changed despite lock failure: stat error %v", err)
	}
}
