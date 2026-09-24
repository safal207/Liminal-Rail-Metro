package policyauthority

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

type chainHeadChildInput struct {
	Path      string           `json:"path"`
	Candidate ChainHead        `json:"candidate"`
	Manifests []SignedManifest `json:"manifests"`
	Rotations []Rotation       `json:"rotations"`
	Ready     string           `json:"ready"`
	Acquired  string           `json:"acquired"`
	Want      string           `json:"want"`
}

// This test is entered only by a child process started by the parent tests.
func TestChainHeadLockChild(t *testing.T) {
	inputPath := os.Getenv("POLICY_HEAD_CHILD_INPUT")
	if inputPath == "" {
		return
	}
	b, err := os.ReadFile(inputPath)
	if err != nil {
		t.Fatal(err)
	}
	var input chainHeadChildInput
	if err := json.Unmarshal(b, &input); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(input.Ready, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if input.Want == "lock" {
		lock, err := openChainHeadLock(input.Path)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(input.Acquired, nil, 0o600); err != nil {
			t.Fatal(err)
		}
		if err := closeChainHeadLock(lock); err != nil {
			t.Fatal(err)
		}
		return
	}
	err = acceptChainHead(input.Path, input.Candidate, input.Manifests, input.Rotations)
	switch input.Want {
	case "rollback":
		if !errors.Is(err, ErrChainRollback) {
			t.Fatalf("expected rollback after lock release, got %v", err)
		}
	case "accept":
		if err != nil {
			t.Fatal(err)
		}
	default:
		t.Fatalf("unknown child expectation %q", input.Want)
	}
}

type chainHeadChild struct {
	cancel context.CancelFunc
	done   chan error
	output *bytes.Buffer
}

func startChainHeadChild(t *testing.T, input chainHeadChildInput) *chainHeadChild {
	t.Helper()
	data, err := json.Marshal(input)
	if err != nil {
		t.Fatal(err)
	}
	inputPath := filepath.Join(t.TempDir(), "child-input.json")
	if err := os.WriteFile(inputPath, data, 0o600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestChainHeadLockChild$")
	cmd.Env = append(os.Environ(), "POLICY_HEAD_CHILD_INPUT="+inputPath)
	output := new(bytes.Buffer)
	cmd.Stdout, cmd.Stderr = output, output
	if err := cmd.Start(); err != nil {
		cancel()
		t.Fatal(err)
	}
	child := &chainHeadChild{cancel: cancel, done: make(chan error, 1), output: output}
	go func() { child.done <- cmd.Wait() }()
	t.Cleanup(cancel)
	return child
}

func waitForChainHeadChildReady(t *testing.T, child *chainHeadChild, ready string) {
	t.Helper()
	deadline := time.NewTimer(5 * time.Second)
	defer deadline.Stop()
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for {
		if _, err := os.Stat(ready); err == nil {
			return
		} else if !errors.Is(err, os.ErrNotExist) {
			t.Fatal(err)
		}
		select {
		case err := <-child.done:
			t.Fatalf("child exited before ready: %v\n%s", err, child.output.String())
		case <-deadline.C:
			t.Fatal("child did not become ready")
		case <-ticker.C:
		}
	}
}

func waitForChainHeadChildDone(t *testing.T, child *chainHeadChild) {
	t.Helper()
	select {
	case err := <-child.done:
		child.cancel()
		if err != nil {
			t.Fatalf("child failed: %v\n%s", err, child.output.String())
		}
	case <-time.After(10 * time.Second):
		t.Fatal("child did not finish after lock release")
	}
}

func TestChainHeadLockBlocksOtherProcess(t *testing.T) {
	path := filepath.Join(t.TempDir(), "head.json")
	lock, err := openChainHeadLock(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if lock != nil {
			_ = closeChainHeadLock(lock)
		}
	}()
	input := chainHeadChildInput{
		Path: path, Ready: filepath.Join(t.TempDir(), "ready"),
		Acquired: filepath.Join(t.TempDir(), "acquired"), Want: "lock",
	}
	child := startChainHeadChild(t, input)
	waitForChainHeadChildReady(t, child, input.Ready)
	select {
	case err := <-child.done:
		t.Fatalf("second process completed while head lock was held: %v\n%s", err, child.output.String())
	case <-time.After(300 * time.Millisecond):
	}
	if _, err := os.Stat(input.Acquired); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("second process acquired held lock: stat error %v", err)
	}
	if err := closeChainHeadLock(lock); err != nil {
		t.Fatal(err)
	}
	lock = nil
	waitForChainHeadChildDone(t, child)
	if _, err := os.Stat(input.Acquired); err != nil {
		t.Fatalf("second process did not acquire released lock: %v", err)
	}
}

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
