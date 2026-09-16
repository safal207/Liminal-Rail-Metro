package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/safal207/Liminal-Rail-Metro/internal/metro"
	"github.com/safal207/Liminal-Rail-Metro/internal/mirror"
)

func TestRunVerifyReproducesBundleFromFilesystemCAS(t *testing.T) {
	bundlePath, casRoot := writeCLIFixture(t)

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	code := run([]string{"verify", "-bundle", bundlePath, "-cas", casRoot}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("expected exit 0, got %d stderr=%s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "VERDICT: REPRODUCED") {
		t.Fatalf("expected reproduced verdict, got stdout=%s", stdout.String())
	}
	if !strings.Contains(stdout.String(), "7/7 artifacts resolved") {
		t.Fatalf("expected artifact receipt summary, got stdout=%s", stdout.String())
	}
}

func TestRunVerifyRejectsWhenCASObjectsAreMissing(t *testing.T) {
	bundlePath, _ := writeCLIFixture(t)
	emptyCAS := t.TempDir()

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	code := run([]string{"verify", "-bundle", bundlePath, "-cas", emptyCAS}, &stdout, &stderr)
	if code != 1 {
		t.Fatalf("expected exit 1, got %d stdout=%s stderr=%s", code, stdout.String(), stderr.String())
	}
	if !strings.Contains(stderr.String(), "VERDICT: REJECTED") {
		t.Fatalf("expected rejected verdict, got stderr=%s", stderr.String())
	}
}

func TestRunRequiresVerifyInputs(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	code := run([]string{"verify"}, &stdout, &stderr)
	if code != 2 {
		t.Fatalf("expected usage exit 2, got %d", code)
	}
	if !strings.Contains(stderr.String(), "requires both -bundle and -cas") {
		t.Fatalf("expected missing-input message, got %s", stderr.String())
	}
}

func writeCLIFixture(t *testing.T) (string, string) {
	t.Helper()

	packet := metro.NewPacket(
		"action-cli-001",
		"research-agent",
		"verify content-addressed proof from CLI",
		metro.Action{Kind: "code.implement", Inputs: map[string]any{"task": "cli proof"}},
		[]string{"code-agent"},
	)
	router := metro.Router{
		ID: "router-cli",
		Policy: map[string]string{
			"code.implement": "code-agent",
		},
	}
	route, err := router.Route(packet)
	if err != nil {
		t.Fatalf("route packet: %v", err)
	}
	result := map[string]any{
		"status":       "implemented",
		"artifact_ref": "artifact://cli-proof",
	}
	receipt, err := metro.MakeSuccessReceipt(packet, route, result, "artifact://cli-proof")
	if err != nil {
		t.Fatalf("make receipt: %v", err)
	}
	valueHash, err := mirror.HashValue(result)
	if err != nil {
		t.Fatalf("hash result: %v", err)
	}
	claim := mirror.Claim{
		ID:        "claim-cli-001",
		ActionID:  packet.ActionID,
		ValueHash: valueHash,
	}
	policy := mirror.AuthorityPolicy{
		Protocol:  mirror.AuthorityPolicyProtocol,
		ID:        "cli-authority-v1",
		PolicyRef: "policy://liminal-rail/cli/v1",
		ExecutorsByAction: map[string][]string{
			"code.implement": {"code-agent"},
		},
	}
	envelope, err := mirror.BuildProofEnvelope(claim, packet, route, result, receipt, policy)
	if err != nil {
		t.Fatalf("build proof envelope: %v", err)
	}

	casRoot := filepath.Join(t.TempDir(), "cas")
	store, err := mirror.NewFilesystemCAS(casRoot)
	if err != nil {
		t.Fatalf("new filesystem CAS: %v", err)
	}
	bundle, replay, err := mirror.StoreEvidenceBundle(store, envelope, packet, route, result, receipt, policy)
	if err != nil {
		t.Fatalf("store evidence bundle: %v", err)
	}
	if replay.Status != mirror.ReplayStatusReproduced {
		t.Fatalf("expected reproduced fixture, got %q", replay.Status)
	}

	bundleBytes, err := json.MarshalIndent(bundle, "", "  ")
	if err != nil {
		t.Fatalf("marshal bundle: %v", err)
	}
	bundlePath := filepath.Join(t.TempDir(), "evidence-bundle.json")
	if err := os.WriteFile(bundlePath, bundleBytes, 0o644); err != nil {
		t.Fatalf("write bundle: %v", err)
	}
	return bundlePath, casRoot
}
