//go:build linux

package policyauthority

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

// A stale generation-2 writer must re-read the generation-3 head after
// waiting for another process's sidecar lock. This fails on the unlocked
// implementation because generation 2 completes while the lock is held.
func TestChainHeadLockedWriterRejectsStaleGeneration(t *testing.T) {
	_, first, firstKey, _, _, _ := rootChain(t)
	firstChain := []SignedManifest{first}
	firstHead, err := chainHeadFor(firstChain, nil)
	if err != nil {
		t.Fatal(err)
	}
	makeNext := func(generation uint64, keyLabel string) SignedManifest {
		t.Helper()
		manifest, err := NewManifest(first.Manifest.AuthorityID, generation, first.Manifest.Bindings)
		if err != nil {
			t.Fatal(err)
		}
		signed, err := SignManifest(manifest, "rail-policy-next", deterministicKey(keyLabel))
		if err != nil {
			t.Fatal(err)
		}
		return signed
	}
	secondKey := deterministicKey("policy-head-lock-second")
	secondManifest, err := NewManifest(first.Manifest.AuthorityID, 2, first.Manifest.Bindings)
	if err != nil {
		t.Fatal(err)
	}
	second, err := SignManifest(secondManifest, "rail-policy-second", secondKey)
	if err != nil {
		t.Fatal(err)
	}
	secondRotation, err := SignRotation(first, second, firstKey, false)
	if err != nil {
		t.Fatal(err)
	}
	third := makeNext(3, "policy-head-lock-third")
	thirdRotation, err := SignRotation(second, third, secondKey, false)
	if err != nil {
		t.Fatal(err)
	}
	secondChain := []SignedManifest{first, second}
	secondRotations := []Rotation{secondRotation}
	secondHead, err := chainHeadFor(secondChain, secondRotations)
	if err != nil {
		t.Fatal(err)
	}
	thirdChain := []SignedManifest{first, second, third}
	thirdRotations := []Rotation{secondRotation, thirdRotation}
	thirdHead, err := chainHeadFor(thirdChain, thirdRotations)
	if err != nil {
		t.Fatal(err)
	}

	path := filepath.Join(t.TempDir(), "head.json")
	if err := writeChainHeadAtomic(path, firstHead); err != nil {
		t.Fatal(err)
	}
	// This external lock uses only OS primitives, so the race test can also
	// be run against the pre-fix acceptChainHead implementation.
	lock, err := os.OpenFile(path+".lock", os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Close()
	if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX); err != nil {
		t.Fatal(err)
	}
	locked := true
	defer func() {
		if locked {
			_ = syscall.Flock(int(lock.Fd()), syscall.LOCK_UN)
		}
	}()
	input := chainHeadChildInput{
		Path: path, Candidate: secondHead, Manifests: secondChain, Rotations: secondRotations,
		Ready: filepath.Join(t.TempDir(), "writer-ready"), Want: "rollback",
	}
	child := startChainHeadChild(t, input)
	waitForChainHeadChildReady(t, child, input.Ready)
	select {
	case err := <-child.done:
		t.Fatalf("stale writer completed while another process held head lock: %v\n%s", err, child.output.String())
	case <-time.After(300 * time.Millisecond):
	}
	// H3 is written by the current lock owner, exactly as a cooperating
	// writer does before releasing its lock.
	if err := writeChainHeadAtomic(path, thirdHead); err != nil {
		t.Fatal(err)
	}
	if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_UN); err != nil {
		t.Fatal(err)
	}
	locked = false
	waitForChainHeadChildDone(t, child)
	accepted, err := readChainHead(path)
	if err != nil {
		t.Fatal(err)
	}
	if accepted.HeadHash != thirdHead.HeadHash {
		t.Fatal("stale writer replaced generation 3")
	}

	// A fresh process can reopen the persistent sidecar and accept the
	// already committed head after the previous owner exits.
	restart := chainHeadChildInput{
		Path: path, Candidate: thirdHead, Manifests: thirdChain, Rotations: thirdRotations,
		Ready: filepath.Join(t.TempDir(), "restart-ready"), Want: "accept",
	}
	restarted := startChainHeadChild(t, restart)
	waitForChainHeadChildDone(t, restarted)
	if _, err := os.Stat(restart.Ready); err != nil {
		t.Fatalf("restart process did not enter the acceptance path: %v", err)
	}
}
