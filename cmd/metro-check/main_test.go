package main

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestCLIRejectsMissingCommand(t *testing.T) {
	var out, errs bytes.Buffer
	if run(nil, &out, &errs) != 1 || out.Len() != 0 {
		t.Fatal("missing command accepted")
	}
}
func TestPathsCannotEscapeViaSymlinkOrLiveInsideCandidate(t *testing.T) {
	root := t.TempDir()
	if _, err := externalPath(root, filepath.Join(root, "proof.json")); err == nil {
		t.Fatal("inside proof accepted")
	}
	outside := filepath.Join(t.TempDir(), "symlink.json")
	if err := os.Symlink(filepath.Join(root, "inside.json"), outside); err != nil {
		t.Fatal(err)
	}
	if _, err := externalPath(root, outside); err == nil {
		t.Fatal("symlink accepted")
	}
}
