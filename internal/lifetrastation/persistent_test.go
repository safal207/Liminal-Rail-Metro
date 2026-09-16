package lifetrastation

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/safal207/Liminal-Rail-Metro/internal/lifetrabridge"
)

func TestPersistentStationHelper(t *testing.T) {
	if os.Getenv("GO_WANT_LIFETRA_STATION_HELPER") != "1" {
		return
	}

	scanner := bufio.NewScanner(os.Stdin)
	for scanner.Scan() {
		var request lifetrabridge.StationRequest
		if err := json.Unmarshal(scanner.Bytes(), &request); err != nil {
			fmt.Printf("{\"protocol\":%q,\"error\":%q}\n", StationErrorProtocol, err.Error())
			continue
		}
		if request.Observation.ActionID == "action-error" {
			fmt.Printf("{\"protocol\":%q,\"error\":%q}\n", StationErrorProtocol, "synthetic station error")
			continue
		}

		causedBy := request.Observation.ActionID
		if request.Observation.ActionID == "action-mismatch" {
			causedBy = "wrong-action"
		}
		decision := lifetrabridge.Decision{
			Protocol:            lifetrabridge.DecisionProtocol,
			DecisionID:          "decision-" + request.Observation.ObservationID,
			SourceObservationID: request.Observation.ObservationID,
			SourceReceiptRef:    "metro-receipt://" + request.Observation.ReceiptID,
			CausedByActionID:    causedBy,
			SourceBeadRef:       "lifetra-bead://" + request.Observation.ObservationID,
			Verdict:             lifetrabridge.VerdictAllow,
			AuthorityProofRef:   "lifetra-authority://" + request.Observation.ObservationID,
			NextAction:          &request.NextAction,
			DecidedAt:           request.DecidedAt,
		}
		payload, _ := json.Marshal(decision)
		fmt.Println(string(payload))
	}
	os.Exit(0)
}

func helperPersistentProcess(t *testing.T) *PersistentProcess {
	t.Helper()
	station := &PersistentProcess{
		Command: os.Args[0],
		Args:    []string{"-test.run=TestPersistentStationHelper"},
		Env:     []string{"GO_WANT_LIFETRA_STATION_HELPER=1"},
		Timeout: 2 * time.Second,
	}
	if err := station.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := station.Close(); err != nil {
			t.Errorf("close persistent station: %v", err)
		}
	})
	return station
}

func persistentRequest(actionID, nextActionID string) lifetrabridge.StationRequest {
	observed := lifetrabridge.Orientation{Growth: 0.6, Stability: 0.5, Truth: 0.8, Connection: 0.5}
	return lifetrabridge.StationRequest{
		Protocol: lifetrabridge.StationRequestProtocol,
		Observation: lifetrabridge.Observation{
			Protocol:      lifetrabridge.ObservationProtocol,
			ObservationID: "observation-" + actionID,
			ActionID:      actionID,
			ReceiptID:     "receipt-" + actionID,
			ReceiptStatus: "SUCCEEDED",
			ReceiptHash:   "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
			HashAlgorithm: "sha256",
			ProofRefs:     []string{"metro-receipt://receipt-" + actionID},
			ObservedAt:    "2026-09-16T19:00:00Z",
		},
		ObservedEpochSeconds: 1_789_585_200,
		DecidedAt:            "2026-09-16T19:00:01Z",
		IntendedOrientation:  lifetrabridge.Orientation{Growth: 0.8, Stability: 0.5, Truth: 0.8, Connection: 0.5},
		ObservedOrientation:  &observed,
		Correction: lifetrabridge.CorrectionConfig{
			Gain: 0.5, EngageThreshold: 0.1, ReleaseThreshold: 0.05, MaxStep: 0.1,
		},
		Safety: lifetrabridge.SafetyConfig{
			Autonomy: "bounded_automatic", MinProofRefs: 1, MaxAutonomousAdjustment: 0.1, HardMaxAdjustment: 0.25,
		},
		AuthorityContext: lifetrabridge.AuthorityContext{HumanApproval: "unknown"},
		NextAction: lifetrabridge.NextAction{
			ActionID:       nextActionID,
			Goal:           "Verify the next artifact.",
			Kind:           "qa.verify",
			Inputs:         map[string]any{"artifact_ref": "artifact://test"},
			AllowedTargets: []string{"qa-agent"},
			TimeoutMS:      5000,
		},
	}
}

func TestPersistentProcessReusesOnePIDAcrossRequests(t *testing.T) {
	station := helperPersistentProcess(t)
	pid := station.PID()
	if pid == 0 {
		t.Fatal("expected started station PID")
	}

	for _, pair := range [][2]string{{"action-001", "action-002"}, {"action-003", "action-004"}} {
		decision, err := station.Evaluate(context.Background(), persistentRequest(pair[0], pair[1]))
		if err != nil {
			t.Fatal(err)
		}
		if decision.Verdict != lifetrabridge.VerdictAllow {
			t.Fatalf("expected ALLOW, got %q", decision.Verdict)
		}
		if station.PID() != pid {
			t.Fatal("persistent station PID changed between requests")
		}
	}
}

func TestPersistentProcessErrorLineDoesNotKillStation(t *testing.T) {
	station := helperPersistentProcess(t)
	pid := station.PID()

	_, err := station.Evaluate(context.Background(), persistentRequest("action-error", "action-after-error"))
	if err == nil || err.Error() != "synthetic station error" {
		t.Fatalf("expected typed station error, got %v", err)
	}

	decision, err := station.Evaluate(context.Background(), persistentRequest("action-005", "action-006"))
	if err != nil {
		t.Fatal(err)
	}
	if decision.Verdict != lifetrabridge.VerdictAllow || station.PID() != pid {
		t.Fatal("station did not recover on the same process after an error line")
	}
}

func TestPersistentProcessStillRejectsBindingMismatch(t *testing.T) {
	station := helperPersistentProcess(t)
	_, err := station.Evaluate(context.Background(), persistentRequest("action-mismatch", "action-next"))
	if err == nil {
		t.Fatal("expected binding mismatch to fail closed")
	}
}
