package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/safal207/Liminal-Rail-Metro/internal/mirror"
)

func TestBuildFixtureIsDeterministicAndReplayable(t *testing.T) {
	firstDir := filepath.Join(t.TempDir(), "first")
	secondDir := filepath.Join(t.TempDir(), "second")

	first, firstReplay, err := buildFixture(firstDir)
	if err != nil {
		t.Fatalf("build first fixture: %v", err)
	}
	second, secondReplay, err := buildFixture(secondDir)
	if err != nil {
		t.Fatalf("build second fixture: %v", err)
	}
	if firstReplay.Status != mirror.ReplayStatusReproduced || secondReplay.Status != mirror.ReplayStatusReproduced {
		t.Fatalf("expected reproduced fixtures, got %q and %q", firstReplay.Status, secondReplay.Status)
	}
	if first.BundleHash != second.BundleHash {
		t.Fatalf("deterministic fixture root changed: %s != %s", first.BundleHash, second.BundleHash)
	}

	firstBytes, err := os.ReadFile(filepath.Join(firstDir, "evidence-bundle.json"))
	if err != nil {
		t.Fatalf("read first bundle: %v", err)
	}
	secondBytes, err := os.ReadFile(filepath.Join(secondDir, "evidence-bundle.json"))
	if err != nil {
		t.Fatalf("read second bundle: %v", err)
	}
	if !bytes.Equal(firstBytes, secondBytes) {
		t.Fatal("deterministic fixture bundle bytes changed between builds")
	}

	store, err := mirror.NewFilesystemCAS(filepath.Join(firstDir, ".cas"))
	if err != nil {
		t.Fatalf("open first fixture CAS: %v", err)
	}
	replayed, err := mirror.VerifyEvidenceBundleFromCAS(first, store)
	if err != nil {
		t.Fatalf("verify fixture from CAS: %v", err)
	}
	if replayed.Status != mirror.ReplayStatusReproduced {
		t.Fatalf("expected REPRODUCED, got %q", replayed.Status)
	}
}

func TestRunFixtureWritesDownloadablePackage(t *testing.T) {
	outDir := filepath.Join(t.TempDir(), "proof-package")
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	code := run([]string{"fixture", "-out", outDir}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("fixture exit=%d stderr=%s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "replay=REPRODUCED") {
		t.Fatalf("expected reproduced fixture receipt, got %s", stdout.String())
	}

	for _, path := range []string{
		"evidence-bundle.json",
		"README.md",
		"verify.sh",
	} {
		if _, err := os.Stat(filepath.Join(outDir, path)); err != nil {
			t.Fatalf("missing fixture file %s: %v", path, err)
		}
	}

	bundleBytes, err := os.ReadFile(filepath.Join(outDir, "evidence-bundle.json"))
	if err != nil {
		t.Fatalf("read generated bundle: %v", err)
	}
	var bundle mirror.EvidenceBundle
	if err := json.Unmarshal(bundleBytes, &bundle); err != nil {
		t.Fatalf("decode generated bundle: %v", err)
	}
	if len(bundle.Artifacts) != 7 {
		t.Fatalf("expected 7 content-addressed artifacts, got %d", len(bundle.Artifacts))
	}
	for _, artifact := range bundle.Artifacts {
		digest := artifact.Digest
		objectPath := filepath.Join(outDir, ".cas", "sha256", digest[:2], digest)
		if _, err := os.Stat(objectPath); err != nil {
			t.Fatalf("missing CAS artifact %s: %v", artifact.Kind, err)
		}
	}
}

func TestRunFixtureRequiresOutputDirectory(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	code := run([]string{"fixture"}, &stdout, &stderr)
	if code != 2 {
		t.Fatalf("expected usage exit 2, got %d", code)
	}
	if !strings.Contains(stderr.String(), "fixture requires -out") {
		t.Fatalf("expected missing-output message, got %s", stderr.String())
	}
}
