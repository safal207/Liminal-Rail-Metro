package moltbook

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/safal207/Liminal-Rail-Metro/internal/decisionplane"
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

type providerFunc func(context.Context, decisionplane.Request) (decisionplane.Decision, error)

func (f providerFunc) Decide(ctx context.Context, request decisionplane.Request) (decisionplane.Decision, error) {
	return f(ctx, request)
}

type fakeAuthorityGate struct {
	disposition       string
	packetHashOverride string
	actionIDOverride   string
	targetOverride     string
	err                error
	calls              int
}

func (f *fakeAuthorityGate) Authorize(_ context.Context, packet metro.Packet, target string) (AuthorityDecision, error) {
	f.calls++
	if f.err != nil {
		return AuthorityDecision{}, f.err
	}
	packetHash, err := metro.HashJSON(packet)
	if err != nil {
		return AuthorityDecision{}, err
	}
	if f.packetHashOverride != "" {
		packetHash = f.packetHashOverride
	}
	actionID := packet.ActionID
	if f.actionIDOverride != "" {
		actionID = f.actionIDOverride
	}
	authorityTarget := target
	if f.targetOverride != "" {
		authorityTarget = f.targetOverride
	}
	disposition := f.disposition
	if disposition == "" {
		disposition = DispositionAutoRoute
	}
	route := metro.Route{
		Protocol:       metro.RouteProtocol,
		RouteID:        "route-" + packet.ActionID,
		ActionID:       packet.ActionID,
		RouterID:       "fake-authority",
		DecisionMode:   "authority-test",
		SelectedTarget: target,
		Confidence:     1,
		PolicyRef:      "policy://moltbook/test",
		DecidedAt:      metro.NowISO(),
	}
	return AuthorityDecision{
		Disposition: disposition,
		ActionID:    actionID,
		PacketHash:  packetHash,
		Target:      authorityTarget,
		ProviderID:  "fake-authority",
		ReasonCode:  "test",
		Route:       &route,
	}, nil
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
	authority, err := NewDecisionPlaneAuthority(decisionplane.StaticProvider{
		ID: "moltbook-static-proof",
		Scores: map[string]float64{
			"moltbook-target": 1,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	station, err := NewStation(identity, evidence, authority)
	if err != nil {
		t.Fatal(err)
	}
	return station, identity, evidence
}

func newStationWithAuthority(t *testing.T, authority AuthorityGate) (*Station, *fakeEvidenceVerifier) {
	t.Helper()
	identity := &fakeIdentityVerifier{
		identity: VerifiedIdentity{AgentID: "agent-123", Verified: true},
	}
	evidence := &fakeEvidenceVerifier{
		result: map[string]any{
			"verdict":         "VERIFIED",
			"evidence_sha256": "fixture-sha256",
		},
	}
	station, err := NewStation(identity, evidence, authority)
	if err != nil {
		t.Fatal(err)
	}
	return station, evidence
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

func TestExecuteBindsIdentityAuthorityPacketAndVerdictToReceipt(t *testing.T) {
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
	if out.Authority.Disposition != DispositionAutoRoute {
		t.Fatalf("authority disposition = %q, want %q", out.Authority.Disposition, DispositionAutoRoute)
	}
	if out.Authority.PacketHash != out.PacketHash {
		t.Fatalf("authority packet hash = %q, want %q", out.Authority.PacketHash, out.PacketHash)
	}
	if out.Authority.ActionID != out.Packet.ActionID {
		t.Fatal("authority is not bound to packet action_id")
	}
	if out.Authority.Target != TargetAgentProof {
		t.Fatalf("authority target = %q, want %q", out.Authority.Target, TargetAgentProof)
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
	if got := out.ReceiptResult["authority_hash"]; got != out.AuthorityHash {
		t.Fatalf("receipt authority_hash = %v, want %q", got, out.AuthorityHash)
	}
	if got := out.ReceiptResult["authority_result"]; got != DispositionAutoRoute {
		t.Fatalf("receipt authority result = %v, want %q", got, DispositionAutoRoute)
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
	if err := VerifyResult(out); err != nil {
		t.Fatalf("station result verification failed: %v", err)
	}
}

func TestExplicitAllowAuthorityCanDispatch(t *testing.T) {
	authority := &fakeAuthorityGate{disposition: DispositionAllow}
	station, evidence := newStationWithAuthority(t, authority)

	out, err := station.Execute(validRequest())
	if err != nil {
		t.Fatal(err)
	}
	if out.Authority.Disposition != DispositionAllow {
		t.Fatalf("authority disposition = %q, want ALLOW", out.Authority.Disposition)
	}
	if evidence.calls != 1 {
		t.Fatalf("evidence verifier called %d times, want 1", evidence.calls)
	}
	if err := VerifyResult(out); err != nil {
		t.Fatal(err)
	}
}

func TestNonPermittingAuthorityNeverDispatchesEvidence(t *testing.T) {
	for _, disposition := range []string{"DENY", "UNKNOWN", decisionplane.DispositionEscalateSystem2, decisionplane.DispositionRequireApproval} {
		t.Run(disposition, func(t *testing.T) {
			authority := &fakeAuthorityGate{disposition: disposition}
			station, evidence := newStationWithAuthority(t, authority)

			_, err := station.Execute(validRequest())
			if err == nil || !strings.Contains(err.Error(), "does not permit dispatch") {
				t.Fatalf("expected authority rejection, got %v", err)
			}
			if evidence.calls != 0 {
				t.Fatalf("evidence verifier called %d times, want 0", evidence.calls)
			}
		})
	}
}

func TestLowConfidenceDecisionPlaneAuthorityNeverDispatchesEvidence(t *testing.T) {
	provider := providerFunc(func(_ context.Context, request decisionplane.Request) (decisionplane.Decision, error) {
		return decisionplane.NewDecision(
			request,
			"low-confidence-proof",
			"moltbook-target",
			[]decisionplane.Probability{{ChoiceID: "moltbook-target", Probability: 1}},
			0.50,
		), nil
	})
	authority, err := NewDecisionPlaneAuthority(provider)
	if err != nil {
		t.Fatal(err)
	}
	station, evidence := newStationWithAuthority(t, authority)

	_, err = station.Execute(validRequest())
	if err == nil || !strings.Contains(err.Error(), "does not permit dispatch") {
		t.Fatalf("expected low-confidence authority rejection, got %v", err)
	}
	if evidence.calls != 0 {
		t.Fatalf("evidence verifier called %d times, want 0", evidence.calls)
	}
}

func TestMismatchedDecisionPlaneBindingsNeverDispatchEvidence(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*decisionplane.Decision)
		want   string
	}{
		{
			name: "packet hash",
			mutate: func(decision *decisionplane.Decision) {
				decision.PacketHash = strings.Repeat("0", 64)
			},
			want: "packet_hash mismatch",
		},
		{
			name: "action id",
			mutate: func(decision *decisionplane.Decision) {
				decision.ActionID = "other-action"
			},
			want: "action_id is not bound",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			provider := providerFunc(func(_ context.Context, request decisionplane.Request) (decisionplane.Decision, error) {
				decision := decisionplane.NewDecision(
					request,
					"tampered-proof",
					"moltbook-target",
					[]decisionplane.Probability{{ChoiceID: "moltbook-target", Probability: 1}},
					1,
				)
				tt.mutate(&decision)
				return decision, nil
			})
			authority, err := NewDecisionPlaneAuthority(provider)
			if err != nil {
				t.Fatal(err)
			}
			station, evidence := newStationWithAuthority(t, authority)

			_, err = station.Execute(validRequest())
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("expected %q rejection, got %v", tt.want, err)
			}
			if evidence.calls != 0 {
				t.Fatalf("evidence verifier called %d times, want 0", evidence.calls)
			}
		})
	}
}

func TestMismatchedAuthorityTargetNeverDispatchesEvidence(t *testing.T) {
	authority := &fakeAuthorityGate{targetOverride: "other-target"}
	station, evidence := newStationWithAuthority(t, authority)

	_, err := station.Execute(validRequest())
	if err == nil || !strings.Contains(err.Error(), "authority target mismatch") {
		t.Fatalf("expected authority target rejection, got %v", err)
	}
	if evidence.calls != 0 {
		t.Fatalf("evidence verifier called %d times, want 0", evidence.calls)
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

func TestIdentityPacketMismatchBreaksStationVerification(t *testing.T) {
	station, _, _ := newTestStation(t)
	out, err := station.Execute(validRequest())
	if err != nil {
		t.Fatal(err)
	}

	inputs := make(map[string]any, len(out.Packet.Action.Inputs))
	for k, v := range out.Packet.Action.Inputs {
		inputs[k] = v
	}
	inputs["identity_ref"] = "tampered-identity"
	out.Packet.Action.Inputs = inputs

	if err := VerifyResult(out); err == nil {
		t.Fatal("expected identity/packet tampering to break station verification")
	}
}

func TestFullPacketTamperFailsStationVerification(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*Result)
	}{
		{
			name: "source agent",
			mutate: func(out *Result) {
				out.Packet.SourceAgent = "moltbook:tampered"
			},
		},
		{
			name: "side effect constraint",
			mutate: func(out *Result) {
				out.Packet.Constraints.SideEffect = true
			},
		},
		{
			name: "allowed targets",
			mutate: func(out *Result) {
				out.Packet.AllowedTargets = []string{TargetAgentProof, "unexpected-target"}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			station, _, _ := newTestStation(t)
			out, err := station.Execute(validRequest())
			if err != nil {
				t.Fatal(err)
			}

			tt.mutate(&out)
			if err := VerifyResult(out); err == nil || !strings.Contains(err.Error(), "packet hash mismatch") {
				t.Fatalf("expected packet hash mismatch, got %v", err)
			}
		})
	}
}

func TestAuthorityTamperFailsStationVerification(t *testing.T) {
	station, _, _ := newTestStation(t)
	out, err := station.Execute(validRequest())
	if err != nil {
		t.Fatal(err)
	}

	out.Authority.ProviderID = "tampered-provider"
	if err := VerifyResult(out); err == nil || !strings.Contains(err.Error(), "authority hash mismatch") {
		t.Fatalf("expected authority hash mismatch, got %v", err)
	}
}

func TestPacketHashBindingBreaksOnReceiptTamper(t *testing.T) {
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
	out.ReceiptResult = tampered

	if err := VerifyResult(out); err == nil {
		t.Fatal("expected receipt packet-hash tampering to break station verification")
	}
}

func TestMissingReceiptBindingFailsVerification(t *testing.T) {
	station, _, _ := newTestStation(t)
	out, err := station.Execute(validRequest())
	if err != nil {
		t.Fatal(err)
	}

	out.Receipt.InputHash = ""

	if err := VerifyResult(out); err == nil {
		t.Fatal("expected missing input hash to fail station verification")
	}
}
