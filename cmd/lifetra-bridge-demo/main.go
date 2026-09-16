package main

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/safal207/Liminal-Rail-Metro/internal/lifetrabridge"
	"github.com/safal207/Liminal-Rail-Metro/internal/metro"
)

func main() {
	receipt := metro.Receipt{
		Protocol:      metro.ReceiptProtocol,
		ReceiptID:     "receipt-action-001",
		ActionID:      "action-001",
		RouteID:       "route-action-001",
		ExecutorID:    "code-agent",
		Status:        "SUCCEEDED",
		HashAlgorithm: "sha256",
		InputHash:     "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		ResultHash:    "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
		ResultRef:     "artifact://build-001",
		StartedAt:     "2026-09-16T13:00:00Z",
		CompletedAt:   "2026-09-16T13:00:01Z",
	}

	observation, err := lifetrabridge.ReceiptToObservation(receipt, "lifetra-bead://bead-001")
	must(err)

	decision := lifetrabridge.Decision{
		Protocol:            lifetrabridge.DecisionProtocol,
		DecisionID:          "decision-001",
		SourceObservationID: observation.ObservationID,
		SourceReceiptRef:    observation.ProofRefs[0],
		CausedByActionID:    observation.ActionID,
		SourceBeadRef:       "lifetra-bead://bead-002",
		Verdict:             lifetrabridge.VerdictAllow,
		AuthorityProofRef:   "lifetra-authority://ticket-001",
		NextAction: &lifetrabridge.NextAction{
			ActionID:       "action-002",
			Goal:           "Verify the produced artifact.",
			Kind:           "qa.verify",
			Inputs:         map[string]any{"artifact_ref": receipt.ResultRef},
			AllowedTargets: []string{"qa-agent"},
			TimeoutMS:      5000,
		},
		DecidedAt: metro.NowISO(),
	}

	packet, err := lifetrabridge.DecisionToPacket(decision)
	must(err)

	out := map[string]any{
		"metro_receipt":       receipt,
		"lifetra_observation": observation,
		"lifetra_decision":    decision,
		"next_metro_packet":   packet,
	}
	b, err := json.MarshalIndent(out, "", "  ")
	must(err)
	fmt.Println(string(b))
}

func must(err error) {
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
