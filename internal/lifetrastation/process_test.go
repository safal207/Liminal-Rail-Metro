package lifetrastation

import (
	"context"
	"encoding/json"
	"os"
	"testing"
	"time"

	"github.com/safal207/Liminal-Rail-Metro/internal/lifetrabridge"
)

func TestProcessStationAllowRoundTrip(t *testing.T) {
	station := Process{
		Command: os.Args[0],
		Args:    []string{"-test.run=TestStationHelperProcess"},
		Env: []string{
			"GO_WANT_STATION_HELPER=1",
			"STATION_HELPER_MODE=allow",
		},
		Timeout: 5 * time.Second,
	}

	decision, err := station.Evaluate(context.Background(), testRequest())
	if err != nil {
		t.Fatal(err)
	}
	if decision.Verdict != lifetrabridge.VerdictAllow {
		t.Fatalf("expected ALLOW, got %q", decision.Verdict)
	}
	if decision.NextAction == nil || decision.NextAction.ActionID != "action-002" {
		t.Fatal("allowed decision did not preserve the next action")
	}
}

func TestProcessStationRejectsMismatchedObservationBinding(t *testing.T) {
	station := Process{
		Command: os.Args[0],
		Args:    []string{"-test.run=TestStationHelperProcess"},
		Env: []string{
			"GO_WANT_STATION_HELPER=1",
			"STATION_HELPER_MODE=mismatch",
		},
		Timeout: 5 * time.Second,
	}

	if _, err := station.Evaluate(context.Background(), testRequest()); err == nil {
		t.Fatal("expected mismatched station decision to fail closed")
	}
}

func TestStationHelperProcess(t *testing.T) {
	if os.Getenv("GO_WANT_STATION_HELPER") != "1" {
		return
	}

	var request lifetrabridge.StationRequest
	if err := json.NewDecoder(os.Stdin).Decode(&request); err != nil {
		os.Exit(2)
	}

	sourceObservationID := request.Observation.ObservationID
	if os.Getenv("STATION_HELPER_MODE") == "mismatch" {
		sourceObservationID = "observation-wrong"
	}

	decision := lifetrabridge.Decision{
		Protocol:            lifetrabridge.DecisionProtocol,
		DecisionID:          "decision-001",
		SourceObservationID: sourceObservationID,
		SourceReceiptRef:    "metro-receipt://" + request.Observation.ReceiptID,
		CausedByActionID:    request.Observation.ActionID,
		SourceBeadRef:       "lifetra-bead://bead-001",
		Verdict:             lifetrabridge.VerdictAllow,
		AuthorityProofRef:   "lifetra-authority://decision-001",
		NextAction:          &request.NextAction,
		DecidedAt:           request.DecidedAt,
	}
	if err := json.NewEncoder(os.Stdout).Encode(decision); err != nil {
		os.Exit(3)
	}
	os.Exit(0)
}

func testRequest() lifetrabridge.StationRequest {
	observed := lifetrabridge.Orientation{
		Growth:     0.6,
		Stability:  0.5,
		Truth:      0.8,
		Connection: 0.5,
	}
	return lifetrabridge.StationRequest{
		Protocol: lifetrabridge.StationRequestProtocol,
		Observation: lifetrabridge.Observation{
			Protocol:      lifetrabridge.ObservationProtocol,
			ObservationID: "observation-action-001",
			ActionID:      "action-001",
			ReceiptID:     "receipt-action-001",
			ReceiptStatus: "SUCCEEDED",
			ReceiptHash:   "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
			HashAlgorithm: "sha256",
			ProofRefs:     []string{"metro-receipt://receipt-action-001"},
			ObservedAt:    "2026-09-16T13:30:00Z",
		},
		ObservedEpochSeconds: 1789565400,
		DecidedAt:            "2026-09-16T13:30:01Z",
		IntendedOrientation: lifetrabridge.Orientation{
			Growth:     0.8,
			Stability:  0.5,
			Truth:      0.8,
			Connection: 0.5,
		},
		ObservedOrientation: &observed,
		Correction: lifetrabridge.CorrectionConfig{
			Gain:             0.5,
			EngageThreshold:  0.1,
			ReleaseThreshold: 0.05,
			MaxStep:          0.1,
		},
		Safety: lifetrabridge.SafetyConfig{
			Autonomy:                "bounded_automatic",
			MinProofRefs:            1,
			QuorumRequired:          0,
			RequireHumanApproval:    false,
			MaxAutonomousAdjustment: 0.1,
			HardMaxAdjustment:       0.25,
		},
		AuthorityContext: lifetrabridge.AuthorityContext{
			HumanApproval:  "unknown",
			QuorumApprovals: 0,
		},
		NextAction: lifetrabridge.NextAction{
			ActionID:       "action-002",
			Goal:           "Verify corrected implementation",
			Kind:           "qa.verify",
			Inputs:         map[string]any{"artifact_ref": "artifact://build-001"},
			AllowedTargets: []string{"qa-agent"},
			TimeoutMS:      5000,
			SideEffect:     false,
		},
	}
}
