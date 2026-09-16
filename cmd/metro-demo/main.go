package main

import (
	"encoding/json"
	"fmt"
	"log"

	"github.com/safal207/Liminal-Rail-Metro/internal/metro"
)

func main() {
	packet := metro.NewPacket(
		"action-demo-001",
		"research-agent",
		"Implement one bounded route adapter.",
		metro.Action{
			Kind: "code.implement",
			Inputs: map[string]any{
				"research_summary": "Fast router candidate identified",
				"task":             "implement bounded route adapter",
			},
		},
		[]string{"code-agent", "qa-agent"},
	)
	packet.ContextRefs = []string{"evidence://demo/research-note"}
	packet.Constraints = metro.Constraints{TimeoutMS: 5000, SideEffect: false}

	router := metro.Router{
		ID: "metro-router-local",
		Policy: map[string]string{
			"code.implement": "code-agent",
			"qa.verify":      "qa-agent",
		},
	}

	route, err := router.Route(packet)
	if err != nil {
		log.Fatal(err)
	}

	result := map[string]any{
		"artifact_ref": "memory://demo/code-adapter.go",
		"status":       "implemented",
	}
	receipt, err := metro.MakeSuccessReceipt(packet, route, result, result["artifact_ref"].(string))
	if err != nil {
		log.Fatal(err)
	}
	if err := metro.Verify(packet, route, result, receipt); err != nil {
		log.Fatal(err)
	}

	out := map[string]any{
		"packet":  packet,
		"route":   route,
		"result":  result,
		"receipt": receipt,
		"verification": map[string]any{
			"status": "PASS",
		},
	}
	b, err := json.MarshalIndent(out, "", "  ")
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println(string(b))
}
