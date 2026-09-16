package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/safal207/Liminal-Rail-Metro/internal/metro"
	"github.com/safal207/Liminal-Rail-Metro/internal/mirror"
)

const fixtureTimestamp = "2026-09-16T00:00:00Z"

func runFixture(args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("fixture", flag.ContinueOnError)
	flags.SetOutput(stderr)
	outDir := flags.String("out", "", "output directory for evidence-bundle.json and .cas")
	if err := flags.Parse(args); err != nil {
		return 2
	}
	if flags.NArg() != 0 {
		fmt.Fprintln(stderr, "fixture does not accept positional arguments")
		return 2
	}
	if *outDir == "" {
		fmt.Fprintln(stderr, "fixture requires -out")
		return 2
	}

	bundle, replay, err := buildFixture(*outDir)
	if err != nil {
		fmt.Fprintf(stderr, "generate proof fixture: %v\n", err)
		return 1
	}
	fmt.Fprintf(stdout, "bundle_hash=%s\n", bundle.BundleHash)
	fmt.Fprintf(stdout, "artifacts=%d\n", len(bundle.Artifacts))
	fmt.Fprintf(stdout, "replay=%s\n", replay.Status)
	fmt.Fprintf(stdout, "bundle=%s\n", filepath.Join(*outDir, "evidence-bundle.json"))
	fmt.Fprintf(stdout, "cas=%s\n", filepath.Join(*outDir, ".cas"))
	return 0
}

func buildFixture(outDir string) (mirror.EvidenceBundle, mirror.ReplayReport, error) {
	if outDir == "" {
		return mirror.EvidenceBundle{}, mirror.ReplayReport{}, fmt.Errorf("fixture output directory is required")
	}
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return mirror.EvidenceBundle{}, mirror.ReplayReport{}, fmt.Errorf("create fixture output directory: %w", err)
	}

	packet := metro.Packet{
		Protocol:    metro.PacketProtocol,
		ActionID:    "action-mirror-proof-fixture-001",
		SourceAgent: "fixture-agent",
		CreatedAt:   fixtureTimestamp,
		Goal:        "produce independently replayable mirror-boundary proof",
		Action: metro.Action{
			Kind: "code.implement",
			Inputs: map[string]any{
				"task":    "mirror-boundary-proof-fixture",
				"version": 1,
			},
		},
		AllowedTargets: []string{"code-agent"},
		Constraints: metro.Constraints{
			SideEffect: false,
		},
	}
	route := metro.Route{
		Protocol:       metro.RouteProtocol,
		RouteID:        "route-mirror-proof-fixture-001",
		ActionID:       packet.ActionID,
		RouterID:       "fixture-router",
		DecisionMode:   "deterministic",
		SelectedTarget: "code-agent",
		Candidates:     []metro.Candidate{{Target: "code-agent", Score: 1}},
		Confidence:     1,
		PolicyRef:      "policy://liminal-rail/fixture-route/v1",
		DecidedAt:      fixtureTimestamp,
	}
	result := map[string]any{
		"artifact_ref": "artifact://mirror-proof-fixture/v1",
		"status":       "implemented",
	}
	inputHash, err := metro.HashJSON(packet.Action.Inputs)
	if err != nil {
		return mirror.EvidenceBundle{}, mirror.ReplayReport{}, fmt.Errorf("hash fixture inputs: %w", err)
	}
	resultHash, err := metro.HashJSON(result)
	if err != nil {
		return mirror.EvidenceBundle{}, mirror.ReplayReport{}, fmt.Errorf("hash fixture result: %w", err)
	}
	receipt := metro.Receipt{
		Protocol:      metro.ReceiptProtocol,
		ReceiptID:     "receipt-mirror-proof-fixture-001",
		ActionID:      packet.ActionID,
		RouteID:       route.RouteID,
		ExecutorID:    route.SelectedTarget,
		Status:        "SUCCEEDED",
		HashAlgorithm: "sha256",
		InputHash:     inputHash,
		ResultHash:    resultHash,
		ResultRef:     "artifact://mirror-proof-fixture/v1",
		StartedAt:     fixtureTimestamp,
		CompletedAt:   fixtureTimestamp,
	}
	claim := mirror.Claim{
		ID:        "claim-mirror-proof-fixture-001",
		ActionID:  packet.ActionID,
		ValueHash: resultHash,
	}
	authority := mirror.AuthorityPolicy{
		Protocol:  mirror.AuthorityPolicyProtocol,
		ID:        "fixture-authority-v1",
		PolicyRef: "policy://liminal-rail/mirror-proof-fixture/v1",
		ExecutorsByAction: map[string][]string{
			"code.implement": {"code-agent"},
		},
	}

	envelope, err := mirror.BuildProofEnvelope(claim, packet, route, result, receipt, authority)
	if err != nil {
		return mirror.EvidenceBundle{}, mirror.ReplayReport{}, fmt.Errorf("build fixture proof envelope: %w", err)
	}
	store, err := mirror.NewFilesystemCAS(filepath.Join(outDir, ".cas"))
	if err != nil {
		return mirror.EvidenceBundle{}, mirror.ReplayReport{}, fmt.Errorf("create fixture CAS: %w", err)
	}
	bundle, replay, err := mirror.StoreEvidenceBundle(store, envelope, packet, route, result, receipt, authority)
	if err != nil {
		return mirror.EvidenceBundle{}, replay, fmt.Errorf("store fixture evidence bundle: %w", err)
	}
	if _, err := mirror.VerifyEvidenceBundleFromCAS(bundle, store); err != nil {
		return mirror.EvidenceBundle{}, replay, fmt.Errorf("self-verify fixture from CAS: %w", err)
	}

	bundleBytes, err := json.MarshalIndent(bundle, "", "  ")
	if err != nil {
		return mirror.EvidenceBundle{}, replay, fmt.Errorf("marshal fixture bundle: %w", err)
	}
	bundleBytes = append(bundleBytes, '\n')
	if err := os.WriteFile(filepath.Join(outDir, "evidence-bundle.json"), bundleBytes, 0o644); err != nil {
		return mirror.EvidenceBundle{}, replay, fmt.Errorf("write fixture bundle: %w", err)
	}

	verifyScript := `#!/usr/bin/env bash
set -euo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
chmod +x "$ROOT/liminal-proof"
exec "$ROOT/liminal-proof" verify -bundle "$ROOT/evidence-bundle.json" -cas "$ROOT/.cas"
`
	if err := os.WriteFile(filepath.Join(outDir, "verify.sh"), []byte(verifyScript), 0o755); err != nil {
		return mirror.EvidenceBundle{}, replay, fmt.Errorf("write fixture verify script: %w", err)
	}

	readme := fmt.Sprintf(`# Liminal Rail Mirror Proof Fixture

This package contains one deterministic `+"`mirror.evidence-bundle.v0.1`"+` proof and its seven content-addressed artifacts.

Bundle root:

`+"```text\n%s\n```"+`

Verify on Linux with the packaged binary:

`+"```bash\nbash verify.sh\n```"+`

Expected terminal receipt:

`+"```text\nVERDICT: REPRODUCED\n```"+`

The receipt means the packaged artifacts reproduce the Metro receipt, authority, policy hash, and Mirror proof bindings. It is not a cryptographic signature or remote-attestation claim.
`, bundle.BundleHash)
	if err := os.WriteFile(filepath.Join(outDir, "README.md"), []byte(readme), 0o644); err != nil {
		return mirror.EvidenceBundle{}, replay, fmt.Errorf("write fixture README: %w", err)
	}

	return bundle, replay, nil
}
