package moltbook

import (
	"testing"

	"github.com/safal207/Liminal-Rail-Metro/internal/decisionplane"
)

const stationTargetFixture = "local-control-fixture"

type stationTargetIdentity struct{}

func (stationTargetIdentity) VerifyIdentity(string) (VerifiedIdentity, error) {
	return VerifiedIdentity{AgentID: "target-test-agent", Verified: true}, nil
}

type stationTargetEvidence struct{}

func (stationTargetEvidence) VerifyEvidence(string) (map[string]any, error) {
	return map[string]any{
		"verdict":         "VERIFIED",
		"evidence_sha256": "target-test-evidence",
	}, nil
}

func TestNewStationForTargetBindsRouteAndReceiptExecutor(t *testing.T) {
	authority, err := NewDecisionPlaneAuthority(decisionplane.StaticProvider{
		ID: "target-test-authority",
		Scores: map[string]float64{
			"moltbook-target": 1,
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	station, err := NewStationForTarget(
		stationTargetIdentity{},
		stationTargetEvidence{},
		authority,
		stationTargetFixture,
	)
	if err != nil {
		t.Fatal(err)
	}

	out, err := station.Execute(Request{
		IdentityToken:   "target-test-token",
		ActionID:        "target-test-action",
		Intent:          IntentVerifyEvidence,
		Target:          stationTargetFixture,
		EvidenceRef:     "fixture://target-test",
		ExternalEffects: false,
	})
	if err != nil {
		t.Fatal(err)
	}

	if len(out.Packet.AllowedTargets) != 1 || out.Packet.AllowedTargets[0] != stationTargetFixture {
		t.Fatalf("allowed targets = %#v, want only %q", out.Packet.AllowedTargets, stationTargetFixture)
	}
	if out.Authority.Target != stationTargetFixture {
		t.Fatalf("authority target = %q, want %q", out.Authority.Target, stationTargetFixture)
	}
	if out.Route.SelectedTarget != stationTargetFixture {
		t.Fatalf("route target = %q, want %q", out.Route.SelectedTarget, stationTargetFixture)
	}
	if out.Receipt.ExecutorID != stationTargetFixture {
		t.Fatalf("receipt executor = %q, want %q", out.Receipt.ExecutorID, stationTargetFixture)
	}
	if got := out.ReceiptResult["target"]; got != stationTargetFixture {
		t.Fatalf("receipt result target = %v, want %q", got, stationTargetFixture)
	}
	if err := VerifyResult(out); err != nil {
		t.Fatalf("custom-target station result did not verify: %v", err)
	}
}

func TestDefaultStationRemainsPinnedToAgentProof(t *testing.T) {
	authority, err := NewDecisionPlaneAuthority(decisionplane.StaticProvider{
		ID: "default-target-test-authority",
		Scores: map[string]float64{
			"moltbook-target": 1,
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	station, err := NewStation(stationTargetIdentity{}, stationTargetEvidence{}, authority)
	if err != nil {
		t.Fatal(err)
	}

	_, err = station.Execute(Request{
		IdentityToken:   "target-test-token",
		ActionID:        "target-test-default-action",
		Intent:          IntentVerifyEvidence,
		Target:          stationTargetFixture,
		EvidenceRef:     "fixture://target-test",
		ExternalEffects: false,
	})
	if err == nil {
		t.Fatal("default Station accepted non-agentproof target")
	}
}

func TestNewStationForTargetRejectsEmptyTarget(t *testing.T) {
	authority, err := NewDecisionPlaneAuthority(decisionplane.StaticProvider{
		ID: "empty-target-test-authority",
		Scores: map[string]float64{
			"moltbook-target": 1,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := NewStationForTarget(stationTargetIdentity{}, stationTargetEvidence{}, authority, ""); err == nil {
		t.Fatal("expected empty Station target to fail")
	}
}
