package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/safal207/Liminal-Rail-Metro/internal/lifetrabridge"
	"github.com/safal207/Liminal-Rail-Metro/internal/lifetrastation"
	"github.com/safal207/Liminal-Rail-Metro/internal/metro"
)

func main() {
	lifetraDir := flag.String("lifetra-dir", envOr("LIFETRA_REPO", "../Lifetra"), "path to a Lifetra checkout containing the metro_station example")
	flag.Parse()

	initialPacket := metro.NewPacket(
		"action-001",
		"research-agent",
		"Implement one bounded artifact for independent QA verification.",
		metro.Action{
			Kind: "code.implement",
			Inputs: map[string]any{
				"spec_ref": "spec://rail-loop-v0.2",
			},
		},
		[]string{"code-agent"},
	)

	codeRouter := metro.Router{
		ID: "router-code",
		Policy: map[string]string{
			"code.implement": "code-agent",
		},
	}
	initialRoute, err := codeRouter.Route(initialPacket)
	must(err)

	result := map[string]any{
		"artifact_ref": "artifact://build-001",
		"status":       "ready-for-qa",
	}
	receipt, err := metro.MakeSuccessReceipt(initialPacket, initialRoute, result, "artifact://build-001")
	must(err)
	must(metro.Verify(initialPacket, initialRoute, result, receipt))

	observation, err := lifetrabridge.ReceiptToObservation(receipt, "")
	must(err)
	observedAt, err := time.Parse(time.RFC3339Nano, observation.ObservedAt)
	must(err)

	observedOrientation := lifetrabridge.Orientation{
		Growth:     0.6,
		Stability:  0.5,
		Truth:      0.8,
		Connection: 0.5,
	}
	stationRequest := lifetrabridge.StationRequest{
		Protocol:             lifetrabridge.StationRequestProtocol,
		Observation:          observation,
		ObservedEpochSeconds: observedAt.Unix(),
		DecidedAt:            metro.NowISO(),
		IntendedOrientation: lifetrabridge.Orientation{
			Growth:     0.8,
			Stability:  0.5,
			Truth:      0.8,
			Connection: 0.5,
		},
		ObservedOrientation: &observedOrientation,
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
			ActionID: "action-002",
			Goal:     "Independently verify the produced artifact.",
			Kind:     "qa.verify",
			Inputs: map[string]any{
				"artifact_ref": receipt.ResultRef,
			},
			AllowedTargets: []string{"qa-agent"},
			TimeoutMS:      5000,
			SideEffect:     false,
		},
	}

	station := lifetrastation.Process{
		Command: "cargo",
		Args:    []string{"run", "--quiet", "--example", "metro_station"},
		Dir:     *lifetraDir,
		Timeout: 2 * time.Minute,
	}
	decision, err := station.Evaluate(context.Background(), stationRequest)
	must(err)
	if decision.Verdict != lifetrabridge.VerdictAllow {
		must(fmt.Errorf("Lifetra station returned %s; demo requires ALLOW", decision.Verdict))
	}

	nextPacket, err := lifetrabridge.DecisionToPacket(decision)
	must(err)
	qaRouter := metro.Router{
		ID: "router-qa",
		Policy: map[string]string{
			"qa.verify": "qa-agent",
		},
	}
	nextRoute, err := qaRouter.Route(nextPacket)
	must(err)
	if nextRoute.SelectedTarget != "qa-agent" {
		must(fmt.Errorf("unexpected final route target %q", nextRoute.SelectedTarget))
	}

	journey := map[string]any{
		"initial_packet":      initialPacket,
		"initial_route":       initialRoute,
		"metro_receipt":       receipt,
		"lifetra_observation": observation,
		"station_request":     stationRequest,
		"lifetra_decision":    decision,
		"next_metro_packet":   nextPacket,
		"next_metro_route":    nextRoute,
		"proof": map[string]any{
			"cross_runtime_loop": "PASS",
			"path":               "Go -> Rust -> Go",
			"final_target":       nextRoute.SelectedTarget,
		},
	}
	encoded, err := json.MarshalIndent(journey, "", "  ")
	must(err)
	fmt.Println(string(encoded))
}

func envOr(name, fallback string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}
	return fallback
}

func must(err error) {
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
