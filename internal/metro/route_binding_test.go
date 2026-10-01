package metro

import "testing"

// Regression for Issue #14: a matching receipt cannot bind an unrelated route.
func TestVerifyRejectsRouteActionMismatch(t *testing.T) {
	packet := NewPacket("action-original", "codex", "Check route binding",
		Action{Kind: "qa", Inputs: map[string]any{"scope": "one"}}, []string{"qa-agent"})
	route, err := (Router{ID: "test", Policy: map[string]string{"qa": "qa-agent"}}).Route(packet)
	if err != nil {
		t.Fatal(err)
	}
	result := map[string]any{"passed": true}
	receipt, err := MakeSuccessReceipt(packet, route, result, "")
	if err != nil {
		t.Fatal(err)
	}
	if err := Verify(packet, route, result, receipt); err != nil {
		t.Fatalf("matching receipt rejected: %v", err)
	}
	for _, wrong := range []string{"action-other", ""} {
		route.ActionID = wrong
		if err := Verify(packet, route, result, receipt); err == nil {
			t.Fatalf("accepted route.action_id=%q for packet.action_id=%q", wrong, packet.ActionID)
		}
	}
}
