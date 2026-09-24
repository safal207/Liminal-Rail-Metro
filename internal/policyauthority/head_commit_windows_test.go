package policyauthority

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/sys/windows"
)

func testWindowsChainHead(t *testing.T, generation uint64) ChainHead {
	t.Helper()
	h := ChainHead{
		Protocol:           ChainHeadProtocol,
		AuthorityID:        "windows-head-test",
		Generation:         generation,
		SignedManifestHash: strings.Repeat("a", 64),
	}
	var err error
	h.HeadHash, err = hashJSON(h.material())
	if err != nil {
		t.Fatal(err)
	}
	return h
}

func TestWindowsChainHeadWriteThroughReplace(t *testing.T) {
	stateDir := t.TempDir()
	path := filepath.Join(stateDir, "accepted-head.json")
	first := testWindowsChainHead(t, 1)
	second := testWindowsChainHead(t, 2)
	for _, head := range []ChainHead{first, second} {
		if err := writeChainHeadAtomic(path, head); err != nil {
			t.Fatal(err)
		}
		got, err := readChainHead(path)
		if err != nil {
			t.Fatal(err)
		}
		if got != head {
			t.Fatalf("read chain head %+v, want %+v", got, head)
		}
	}
	assertNoWindowsHeadTempFiles(t, stateDir)
}

func TestWindowsChainHeadLongPath(t *testing.T) {
	stateDir := filepath.Join(t.TempDir(), strings.Repeat("nested", 20), strings.Repeat("folder", 20))
	if err := os.MkdirAll(stateDir, 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(stateDir, "accepted-head.json")
	if len(path) < 260 {
		t.Fatalf("test path is not long enough: %d", len(path))
	}
	encoded, err := moveFileExPath(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := windows.UTF16PtrToString(encoded); !strings.HasPrefix(got, `\\?\`) {
		t.Fatalf("long MoveFileEx path lacks extended-length prefix: %q", got)
	}
	for _, generation := range []uint64{1, 2} {
		head := testWindowsChainHead(t, generation)
		if err := writeChainHeadAtomic(path, head); err != nil {
			t.Fatal(err)
		}
		got, err := readChainHead(path)
		if err != nil || got != head {
			t.Fatalf("long-path head mismatch: got=%+v err=%v", got, err)
		}
	}
	assertNoWindowsHeadTempFiles(t, stateDir)
}

func TestWindowsChainHeadMoveFailureKeepsDestination(t *testing.T) {
	stateDir := t.TempDir()
	destination := filepath.Join(stateDir, "accepted-head.json")
	if err := os.Mkdir(destination, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := writeChainHeadAtomic(destination, testWindowsChainHead(t, 1)); err == nil {
		t.Fatal("write-through move into a directory unexpectedly succeeded")
	}
	info, err := os.Stat(destination)
	if err != nil || !info.IsDir() {
		t.Fatalf("destination changed after failed move: info=%v err=%v", info, err)
	}
	assertNoWindowsHeadTempFiles(t, stateDir)
}

func assertNoWindowsHeadTempFiles(t *testing.T, dir string) {
	t.Helper()
	matches, err := filepath.Glob(filepath.Join(dir, ".policy-authority-head-*"))
	if err != nil {
		t.Fatal(err)
	}
	if len(matches) != 0 {
		t.Fatalf("temporary chain-head files remain: %v", matches)
	}
}
