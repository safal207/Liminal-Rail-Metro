package metro

import "testing"

func TestRouteRejectsDisallowedTarget(t *testing.T) {
	packet := NewPacket(
		"action-test-001",
		"research-agent",
		"bounded test",
		Action{Kind: "code.implement", Inputs: map[string]any{"task": "demo"}},
		[]string{"qa-agent"},
	)

	router := Router{
		ID: "router-test",
		Policy: map[string]string{
			"code.implement": "code-agent",
		},
	}

	if _, err := router.Route(packet); err == nil {
		t.Fatal("expected disallowed target to fail closed")
	}
}

func TestReceiptVerification(t *testing.T) {
	packet := NewPacket(
		"action-test-002",
		"research-agent",
		"bounded test",
		Action{Kind: "code.implement", Inputs: map[string]any{"task": "demo"}},
		[]string{"code-agent"},
	)
	router := Router{
		ID: "router-test",
		Policy: map[string]string{
			"code.implement": "code-agent",
		},
	}

	route, err := router.Route(packet)
	if err != nil {
		t.Fatal(err)
	}
	result := map[string]any{"status": "implemented", "artifact_ref": "memory://demo"}
	receipt, err := MakeSuccessReceipt(packet, route, result, "memory://demo")
	if err != nil {
		t.Fatal(err)
	}
	if err := Verify(packet, route, result, receipt); err != nil {
		t.Fatalf("verification failed: %v", err)
	}
}
