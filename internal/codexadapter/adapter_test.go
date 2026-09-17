package codexadapter

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/safal207/Liminal-Rail-Metro/internal/decisionplane"
	"github.com/safal207/Liminal-Rail-Metro/internal/metro"
)

func ptr[T any](v T) *T { return &v }

func testAction() Action {
	return Action{Protocol: ActionProtocol, RequestID: "req-1", ActionID: "act-1", Kind: "hash_text", Text: ptr("abc"), SideEffect: ptr(false)}
}

type providerFunc func(context.Context, decisionplane.Request) (decisionplane.Decision, error)

func (f providerFunc) Decide(ctx context.Context, r decisionplane.Request) (decisionplane.Decision, error) {
	return f(ctx, r)
}

func goodDecision(r decisionplane.Request) decisionplane.Decision {
	return decisionplane.NewDecision(r, ProviderID, r.Choices[0].ID,
		[]decisionplane.Probability{{ChoiceID: r.Choices[0].ID, Probability: 1}}, 1)
}

func mustResponse(t *testing.T, a Action) Response {
	t.Helper()
	r, err := Run(context.Background(), a)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func TestRoundTripAndKnownSHA256(t *testing.T) {
	for _, state := range []map[string]string{nil, {}, {"phase": "proof"}} {
		a := testAction()
		a.State = state
		r := mustResponse(t, a)
		if !r.Evidence.Dispatched || r.Evidence.Receipt.Status != "SUCCEEDED" || r.Evidence.Result["sha256"] != "ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad" {
			t.Fatalf("unexpected execution evidence: %+v", r)
		}
		data, err := json.Marshal(r)
		if err != nil {
			t.Fatal(err)
		}
		var decoded Response
		if err := Decode(bytes.NewReader(data), &decoded, 4*MaxInputBytes); err != nil {
			t.Fatal(err)
		}
		if err := Verify(decoded); err != nil {
			t.Fatal(err)
		}
	}
}

func TestApprovalHasNoExecutionEvidence(t *testing.T) {
	for _, kind := range []string{"hash_text", "external_action"} {
		a := testAction()
		a.Kind = kind
		a.SideEffect = ptr(true)
		if kind == "external_action" {
			a.Text = nil
			a.Description = ptr("publish a release")
		}
		r := mustResponse(t, a)
		e := r.Evidence
		if e.Gate.Disposition != decisionplane.DispositionRequireApproval || e.Gate.Route != nil || e.Dispatched || e.Result != nil || e.Receipt != nil {
			t.Fatalf("approval bypass: %+v", e)
		}
		data, _ := json.Marshal(r)
		var decoded Response
		if err := Decode(bytes.NewReader(data), &decoded, 4*MaxInputBytes); err != nil {
			t.Fatal(err)
		}
		if err := Verify(decoded); err != nil {
			t.Fatal(err)
		}
	}
}

func TestInvalidActionsFailClosed(t *testing.T) {
	cases := map[string]func(*Action){
		"protocol":                     func(a *Action) { a.Protocol = "openai-invented-v1" },
		"action ID":                    func(a *Action) { a.ActionID = "" },
		"request ID":                   func(a *Action) { a.RequestID = "../../x" },
		"missing effect flag":          func(a *Action) { a.SideEffect = nil },
		"unknown action":               func(a *Action) { a.Kind = "shell" },
		"missing text":                 func(a *Action) { a.Text = nil },
		"oversize text":                func(a *Action) { a.Text = ptr(strings.Repeat("a", MaxTextBytes+1)) },
		"extra description":            func(a *Action) { a.Description = ptr("x") },
		"external effect downgrade":    func(a *Action) { a.Kind = "external_action"; a.Text = nil; a.Description = ptr("send email") },
		"missing external description": func(a *Action) { a.Kind = "external_action"; a.Text = nil; a.SideEffect = ptr(true) },
		"state limit":                  func(a *Action) { a.State = map[string]string{"x": strings.Repeat("a", 1025)} },
	}
	for name, change := range cases {
		t.Run(name, func(t *testing.T) {
			a := testAction()
			change(&a)
			calls := 0
			r, err := run(context.Background(), a, providerFunc(func(_ context.Context, r decisionplane.Request) (decisionplane.Decision, error) {
				calls++
				return goodDecision(r), nil
			}))
			if err == nil || calls != 0 || r.Protocol != "" {
				t.Fatalf("not rejected before provider: %v, %d, %+v", err, calls, r)
			}
		})
	}
}

func TestGoAPICannotCreateEvidenceFromInvalidUTF8(t *testing.T) {
	invalid := string([]byte{0xff})
	cases := map[string]func(*Action){
		"text": func(a *Action) { a.Text = ptr(invalid) },
		"description": func(a *Action) {
			a.Kind, a.Text, a.Description, a.SideEffect = "external_action", nil, ptr(invalid), ptr(true)
		},
		"state key":   func(a *Action) { a.State = map[string]string{invalid: "value"} },
		"state value": func(a *Action) { a.State = map[string]string{"key": invalid} },
	}
	for name, change := range cases {
		t.Run(name, func(t *testing.T) {
			a := testAction()
			change(&a)
			calls := 0
			response, err := run(context.Background(), a, providerFunc(func(_ context.Context, request decisionplane.Request) (decisionplane.Decision, error) {
				calls++
				return goodDecision(request), nil
			}))
			if err == nil || calls != 0 || response.Protocol != "" {
				t.Fatalf("invalid UTF-8 reached provider or produced evidence: calls=%d, response=%+v, err=%v", calls, response, err)
			}
			if response, err := Run(context.Background(), a); err == nil || response.Protocol != "" {
				t.Fatal("public Run accepted invalid UTF-8")
			}
		})
	}
}

func TestUntrustedDecisionFailsClosed(t *testing.T) {
	cases := map[string]func(*decisionplane.Decision){
		"packet hash":             func(d *decisionplane.Decision) { d.PacketHash = "stale" },
		"state hash":              func(d *decisionplane.Decision) { d.StateHash = "stale" },
		"choices hash":            func(d *decisionplane.Decision) { d.ChoicesHash = "stale" },
		"provider":                func(d *decisionplane.Decision) { d.ProviderID = "impostor" },
		"action ID":               func(d *decisionplane.Decision) { d.ActionID = "other" },
		"request ID":              func(d *decisionplane.Decision) { d.RequestID = "other" },
		"unknown choice":          func(d *decisionplane.Decision) { d.SelectedChoiceID = "shell" },
		"incomplete distribution": func(d *decisionplane.Decision) { d.Probabilities = nil },
		"bad mass":                func(d *decisionplane.Decision) { d.Probabilities[0].Probability = 0.5 },
	}
	for name, change := range cases {
		t.Run(name, func(t *testing.T) {
			r, err := run(context.Background(), testAction(), providerFunc(func(_ context.Context, r decisionplane.Request) (decisionplane.Decision, error) {
				d := goodDecision(r)
				change(&d)
				return d, nil
			}))
			if err == nil || r.Evidence.Dispatched || r.Evidence.Receipt != nil {
				t.Fatalf("decision accepted: %+v, %v", r, err)
			}
		})
	}
}

func TestProviderCannotMutateBoundRequest(t *testing.T) {
	a := testAction()
	a.State = map[string]string{"phase": "proof"}
	for _, mutate := range []func(*decisionplane.Request){
		func(r *decisionplane.Request) {
			r.State["phase"] = "deploy"
			r.StateHash, _ = decisionplane.HashState(r.State)
		},
		func(r *decisionplane.Request) {
			r.Choices[0].Target = "shell"
			r.ChoicesHash, _ = decisionplane.HashChoices(r.Choices)
		},
	} {
		_, err := run(context.Background(), a, providerFunc(func(_ context.Context, r decisionplane.Request) (decisionplane.Decision, error) {
			mutate(&r)
			return goodDecision(r), nil
		}))
		if err == nil {
			t.Fatal("provider rebound mutated context")
		}
	}
	if a.State["phase"] != "proof" {
		t.Fatal("caller state mutated")
	}
}

func TestSystem2AndProviderFailure(t *testing.T) {
	r, err := run(context.Background(), testAction(), providerFunc(func(_ context.Context, r decisionplane.Request) (decisionplane.Decision, error) {
		d := goodDecision(r)
		d.Confidence = 0.5
		return d, nil
	}))
	if err != nil || r.Evidence.Gate.Disposition != decisionplane.DispositionEscalateSystem2 || r.Evidence.Gate.Route != nil || r.Evidence.Dispatched || r.Evidence.Receipt != nil {
		t.Fatalf("escalation failed: %+v, %v", r, err)
	}
	_, err = run(context.Background(), testAction(), providerFunc(func(context.Context, decisionplane.Request) (decisionplane.Decision, error) {
		return decisionplane.Decision{}, errors.New("private provider detail")
	}))
	if err == nil || strings.Contains(err.Error(), "private provider detail") {
		t.Fatal("provider failure leaked or accepted")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := Run(ctx, testAction()); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation ignored: %v", err)
	}
}

func TestTamperedProofRejectedEvenAfterRehash(t *testing.T) {
	cases := map[string]func(*Response){
		"packet":          func(r *Response) { r.Evidence.Packet.Action.Inputs["text"] = "changed" },
		"state":           func(r *Response) { r.Evidence.Request.State = map[string]any{"phase": "changed"} },
		"choices":         func(r *Response) { r.Evidence.Request.Choices[0].Target = "shell" },
		"route":           func(r *Response) { r.Evidence.Gate.Route.SelectedTarget = "shell" },
		"gate":            func(r *Response) { r.Evidence.Gate.Disposition = "REQUIRE_APPROVAL" },
		"result":          func(r *Response) { r.Evidence.Result["sha256"] = "wrong" },
		"receipt binding": func(r *Response) { r.Evidence.Receipt.ActionID = "other" },
		"receipt status":  func(r *Response) { r.Evidence.Receipt.Status = "UNKNOWN" },
		"receipt missing": func(r *Response) { r.Evidence.Receipt = nil },
		"dispatch flag":   func(r *Response) { r.Evidence.Dispatched = false },
	}
	for name, change := range cases {
		t.Run(name, func(t *testing.T) {
			r := mustResponse(t, testAction())
			change(&r)
			if Verify(r) == nil {
				t.Fatal("tampered proof accepted")
			}
			r.EvidenceHash, _ = metro.HashJSON(r.Evidence)
			if Verify(r) == nil {
				t.Fatal("rehash bypassed binding checks")
			}
		})
	}
}

func TestConcurrentIndependentCalls(t *testing.T) {
	for i := 0; i < 16; i++ {
		t.Run("call", func(t *testing.T) { t.Parallel(); mustResponse(t, testAction()) })
	}
}
