package moltbook

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/safal207/Liminal-Rail-Metro/internal/metro"
)

type fakeIdentityVerifier struct {
	identity VerifiedIdentity
	err      error
	calls    int
}

func (f *fakeIdentityVerifier) VerifyIdentity(token string) (VerifiedIdentity, error) {
	f.calls++
	if f.err != nil {
		return VerifiedIdentity{}, f.err
	}
	return f.identity, nil
}

type fakeEvidenceVerifier struct {
	result map[string]any
	err    error
	calls  int
}

func (f *fakeEvidenceVerifier) VerifyEvidence(evidenceRef string) (map[string]any, error) {
	f.calls++
	if f.err != nil {
		return nil, f.err
	}
	return f.result, nil
}

func newTestStation(t *testing.T) (*Station, *fakeIdentityVerifier, *fakeEvidenceVerifier) {
	t.Helper()

	identity := &fakeIdentityVerifier{
		identity: VerifiedIdentity{
			AgentID:  "agent-123",
			Verified: true,
		},
	}
	evidence := &fakeEvidenceVerifier{
		result: map[string]any{
			"verdict":         "VERIFIED",
			"evidence_sha256": "fixture-sha256",
		},
	}
	station, err := NewStation(identity, evidence)
	if err != nil {
		t.Fatal(err)
	}
	return station, identity, evidence
}

func validRequest() Request {
	return Request{
		IdentityToken:   "test-token",
		ActionID:        "molt-action-001",
		Intent:          IntentVerifyEvidence,
		Target:          TargetAgentProof,
		EvidenceRef:     "fixture://proof-001",
		ExternalEffects: false,
	}
}

func TestExecuteBindsVerifiedIdentityPacketAndVerdictToReceipt(t *testing.T) {
	station, identity, evidence := newTestStation(t)

	out, err := station.Execute(validRequest())
	if err != nil {
		t.Fatal(err)
	}

	if identity.calls != 1 {
		t.Fatalf("identity verification calls = %d, want 1", identity.calls)
	}
	if evidence.calls != 1 {
		t.Fatalf("evidence verification calls = %d, want 1", evidence.calls)
	}
	if out.Route.SelectedTarget != TargetAgentProof {
		t.Fatalf("selected target = %q, want %q", out.Route.SelectedTarget, TargetAgentProof)
	}
	if out.Packet.Constraints.SideEffect {
		t.Fatal("side effects must remain false")
	}
	if got := out.Packet.Action.Inputs["identity_ref"]; got != out.IdentityRef {
		t.Fatalf("packet identity_ref = %v, want %q", got, out.IdentityRef)
	}

	wantPacketHash, err := metro.HashJSON(out.Packet)
	if err != nil {
		t.Fatal(err)
	}
	if out.PacketHash != wantPacketHash {
		t.Fatalf("packet hash = %q, want %q", out.PacketHash, wantPacketHash)
	}
	if got := out.ReceiptResult["packet_hash"]; got != out.PacketHash {
		t.Fatalf("receipt packet_hash = %v, want %q", got, out.PacketHash)
	}
	if got := out.ReceiptResult["identity_ref"]; got != out.IdentityRef {
		t.Fatalf("receipt identity_ref = %v, want %q", got, out.IdentityRef)
	}
	if got := out.ReceiptResult["target"]; got != TargetAgentProof {
		t.Fatalf("receipt target = %v, want %q", got, TargetAgentProof)
	}
	if got := out.ReceiptResult["verdict"]; got != "VERIFIED" {
		t.Fatalf("receipt verdict = %v, want VERIFIED", got)
	}
	if got := out.ReceiptResult["evidence_sha256"]; got != "fixture-sha256" {
		t.Fatalf("receipt evidence hash = %v, want fixture-sha256", got)
	}
	if out.Receipt.ActionID != out.Packet.ActionID {
		t.Fatal("receipt is not bound to packet action_id")
	}
	if out.Receipt.ExecutorID != TargetAgentProof {
		t.Fatalf("executor = %q, want %q", out.Receipt.ExecutorID, TargetAgentProof)
	}
	if err := metro.Verify(out.Packet, out.Route, out.ReceiptResult, out.Receipt); err != nil {
		t.Fatalf("receipt verification failed: %v", err)
	}
}

func TestIdentityTokenDoesNotLeakIntoResult(t *testing.T) {
	station, _, _ := newTestStation(t)
	req := validRequest()

	out, err := station.Execute(req)
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(out)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), req.IdentityToken) {
		t.Fatal("identity token leaked into station result")
	}
}

func TestInvalidIdentityFailsBeforeEvidenceDispatch(t *testing.T) {
	station, identity, evidence := newTestStation(t)
	identity.identity.Verified = false

	_, err := station.Execute(validRequest())
	if err == nil || !strings.Contains(err.Error(), "not verified") {
		t.Fatalf("expected verified-identity rejection, got %v", err)
	}
	if evidence.calls != 0 {
		t.Fatalf("evidence verifier called %d times, want 0", evidence.calls)
	}
}

func TestIdentityVerifierErrorFailsBeforeEvidenceDispatch(t *testing.T) {
	station, identity, evidence := newTestStation(t)
	identity.err = errors.New("token rejected")

	_, err := station.Execute(validRequest())
	if err == nil || !strings.Contains(err.Error(), "identity verification failed") {
		t.Fatalf("expected identity verification error, got %v", err)
	}
	if evidence.calls != 0 {
		t.Fatalf("evidence verifier called %d times, want 0", evidence.calls)
	}
}

func TestTargetConfusionFailsClosedBeforeIdentityVerification(t *testing.T) {
	station, identity, evidence := newTestStation(t)
	req := validRequest()
	req.Target = "github-writer"

	_, err := station.Execute(req)
	if err == nil || !strings.Contains(err.Error(), "not allowlisted") {
		t.Fatalf("expected target rejection, got %v", err)
	}
	if identity.calls != 0 {
		t.Fatalf("identity verifier called %d times, want 0", identity.calls)
	}
	if evidence.calls != 0 {
		t.Fatalf("evidence verifier called %d times, want 0", evidence.calls)
	}
}

func TestUnknownIntentFailsClosed(t *testing.T) {
	station, identity, evidence := newTestStation(t)
	req := validRequest()
	req.Intent = "POST_TO_FEED"

	_, err := station.Execute(req)
	if err == nil || !strings.Contains(err.Error(), "unsupported intent") {
		t.Fatalf("expected intent rejection, got %v", err)
	}
	if identity.calls != 0 || evidence.calls != 0 {
		t.Fatalf("unexpected verifier calls: identity=%d evidence=%d", identity.calls, evidence.calls)
	}
}

func TestExternalEffectsFailClosed(t *testing.T) {
	station, identity, evidence := newTestStation(t)
	req := validRequest()
	req.ExternalEffects = true

	_, err := station.Execute(req)
	if err == nil || !strings.Contains(err.Error(), "external effects are forbidden") {
		t.Fatalf("expected external-effect rejection, got %v", err)
	}
	if identity.calls != 0 || evidence.calls != 0 {
		t.Fatalf("unexpected verifier calls: identity=%d evidence=%d", identity.calls, evidence.calls)
	}
}

func TestDuplicateActionDoesNotRedispatchEvidence(t *testing.T) {
	station, _, evidence := newTestStation(t)
	req := validRequest()

	if _, err := station.Execute(req); err != nil {
		t.Fatal(err)
	}
	if _, err := station.Execute(req); err == nil || !strings.Contains(err.Error(), "already been consumed") {
		t.Fatalf("expected duplicate action rejection, got %v", err)
	}
	if evidence.calls != 1 {
		t.Fatalf("evidence verifier called %d times, want exactly 1", evidence.calls)
	}
}

func TestExecutionErrorDoesNotBlindlyReplayConsumedAction(t *testing.T) {
	station, _, evidence := newTestStation(t)
	evidence.err = errors.New("ambiguous verifier failure")
	req := validRequest()

	if _, err := station.Execute(req); err == nil {
		t.Fatal("expected verifier failure")
	}
	evidence.err = nil

	if _, err := station.Execute(req); err == nil || !strings.Contains(err.Error(), "already been consumed") {
		t.Fatalf("expected consumed action to block blind retry, got %v", err)
	}
	if evidence.calls != 1 {
		t.Fatalf("evidence verifier called %d times, want exactly 1", evidence.calls)
	}
}

func TestVerificationMustExposeVerdictAndEvidenceHash(t *testing.T) {
	tests := []struct {
		name   string
		result map[string]any
		want   string
	}{
		{
			name: "missing verdict",
			result: map[string]any{
				"evidence_sha256": "fixture-sha256",
			},
			want: "verdict is missing",
		},
		{
			name: "missing evidence hash",
			result: map[string]any{
				"verdict": "VERIFIED",
			},
			want: "evidence_sha256 is missing",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			station, _, evidence := newTestStation(t)
			evidence.result = tt.result

			_, err := station.Execute(validRequest())
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("expected %q rejection, got %v", tt.want, err)
			}
		})
	}
}

func TestIdentityPacketMismatchBreaksReceiptVerification(t *testing.T) {
	station, _, _ := newTestStation(t)
	out, err := station.Execute(validRequest())
	if err != nil {
		t.Fatal(err)
	}

	tampered := out.Packet
	inputs := make(map[string]any, len(out.Packet.Action.Inputs))
	for k, v := range out.Packet.Action.Inputs {
		inputs[k] = v
	}
	inputs["identity_ref"] = "tampered-identity"
	tampered.Action.Inputs = inputs

	if err := metro.Verify(tampered, out.Route, out.ReceiptResult, out.Receipt); err == nil {
		t.Fatal("expected identity/packet tampering to break receipt verification")
	}
}

func TestPacketHashBindingBreaksOnTamper(t *testing.T) {
	station, _, _ := newTestStation(t)
	out, err := station.Execute(validRequest())
	if err != nil {
		t.Fatal(err)
	}

	tampered := make(map[string]any, len(out.ReceiptResult))
	for k, v := range out.ReceiptResult {
		tampered[k] = v
	}
	tampered["packet_hash"] = "tampered-packet-hash"

	if err := metro.Verify(out.Packet, out.Route, tampered, out.Receipt); err == nil {
		t.Fatal("expected packet-hash tampering to break receipt verification")
	}
}

func TestMissingReceiptBindingFailsVerification(t *testing.T) {
	station, _, _ := newTestStation(t)
	out, err := station.Execute(validRequest())
	if err != nil {
		t.Fatal(err)
	}

	receipt := out.Receipt
	receipt.InputHash = ""

	if err := metro.Verify(out.Packet, out.Route, out.ReceiptResult, receipt); err == nil {
		t.Fatal("expected missing input hash to fail receipt verification")
	}
}
