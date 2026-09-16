package decisionplane

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestRemoteProviderRoundTripPreservesDecisionBinding(t *testing.T) {
	packet := decisionTestPacket(false)
	request := mustDecisionRequest(t, packet)

	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, httpRequest *http.Request) {
		if httpRequest.Method != http.MethodPost {
			t.Errorf("unexpected method %q", httpRequest.Method)
		}
		if got := httpRequest.Header.Get("Authorization"); got != "Bearer secret-token" {
			t.Errorf("unexpected authorization header %q", got)
		}
		if got := httpRequest.Header.Get("X-Metro-Test"); got != "remote-v0.7" {
			t.Errorf("unexpected custom header %q", got)
		}

		decoder := json.NewDecoder(httpRequest.Body)
		decoder.DisallowUnknownFields()
		var received Request
		if err := decoder.Decode(&received); err != nil {
			t.Errorf("decode request: %v", err)
			writer.WriteHeader(http.StatusBadRequest)
			return
		}
		decision := NewDecision(received, "remote-proof", "code", []Probability{
			{ChoiceID: "research", Probability: 0.005},
			{ChoiceID: "code", Probability: 0.99},
			{ChoiceID: "qa", Probability: 0.005},
		}, 0.99)
		writer.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(writer).Encode(decision); err != nil {
			t.Errorf("encode decision: %v", err)
		}
	}))
	defer server.Close()

	provider, err := NewRemoteProvider(RemoteProviderConfig{
		Endpoint:    server.URL,
		ProviderID:  "remote-proof",
		BearerToken: "secret-token",
		Headers: map[string]string{
			"X-Metro-Test": "remote-v0.7",
		},
		Timeout: time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}

	decision, err := provider.Decide(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	result, err := ApplyDecision(packet, request, decision, DefaultGatePolicy())
	if err != nil {
		t.Fatal(err)
	}
	if result.Disposition != DispositionAutoRoute || result.Route == nil || result.Route.SelectedTarget != "code-agent" {
		t.Fatalf("unexpected remote gate result %#v", result)
	}
}

func TestRemoteProviderRejectsProviderIdentityMismatch(t *testing.T) {
	packet := decisionTestPacket(false)
	request := mustDecisionRequest(t, packet)
	server := decisionServer(t, func(request Request) Decision {
		return NewDecision(request, "wrong-provider", "code", []Probability{
			{ChoiceID: "research", Probability: 0.005},
			{ChoiceID: "code", Probability: 0.99},
			{ChoiceID: "qa", Probability: 0.005},
		}, 0.99)
	})
	defer server.Close()

	provider := mustRemoteProvider(t, server.URL, "expected-provider")
	if _, err := provider.Decide(context.Background(), request); err == nil || !strings.Contains(err.Error(), "provider_id mismatch") {
		t.Fatalf("expected provider identity mismatch, got %v", err)
	}
}

func TestRemoteProviderRejectsStaleBindingBeforePolicyGate(t *testing.T) {
	packet := decisionTestPacket(false)
	request := mustDecisionRequest(t, packet)
	server := decisionServer(t, func(request Request) Decision {
		decision := NewDecision(request, "remote-proof", "code", []Probability{
			{ChoiceID: "research", Probability: 0.005},
			{ChoiceID: "code", Probability: 0.99},
			{ChoiceID: "qa", Probability: 0.005},
		}, 0.99)
		decision.StateHash = strings.Repeat("0", 64)
		return decision
	})
	defer server.Close()

	provider := mustRemoteProvider(t, server.URL, "remote-proof")
	if _, err := provider.Decide(context.Background(), request); err == nil || !strings.Contains(err.Error(), "state_hash mismatch") {
		t.Fatalf("expected stale binding rejection, got %v", err)
	}
}

func TestRemoteProviderTimeout(t *testing.T) {
	packet := decisionTestPacket(false)
	request := mustDecisionRequest(t, packet)
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		time.Sleep(75 * time.Millisecond)
		writer.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	provider, err := NewRemoteProvider(RemoteProviderConfig{
		Endpoint:   server.URL,
		ProviderID: "remote-proof",
		Timeout:    10 * time.Millisecond,
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = provider.Decide(context.Background(), request)
	if err == nil {
		t.Fatal("expected remote timeout")
	}
}

func TestRemoteProviderReturnsTypedHTTPError(t *testing.T) {
	packet := decisionTestPacket(false)
	request := mustDecisionRequest(t, packet)
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.WriteHeader(http.StatusServiceUnavailable)
		_, _ = writer.Write([]byte("temporarily unavailable"))
	}))
	defer server.Close()

	provider := mustRemoteProvider(t, server.URL, "remote-proof")
	_, err := provider.Decide(context.Background(), request)
	var httpErr *RemoteHTTPError
	if !errors.As(err, &httpErr) {
		t.Fatalf("expected RemoteHTTPError, got %T: %v", err, err)
	}
	if httpErr.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("unexpected status code %d", httpErr.StatusCode)
	}
}

func TestRemoteProviderRejectsUnknownResponseFields(t *testing.T) {
	packet := decisionTestPacket(false)
	request := mustDecisionRequest(t, packet)
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, httpRequest *http.Request) {
		decision := NewDecision(request, "remote-proof", "code", []Probability{
			{ChoiceID: "research", Probability: 0.005},
			{ChoiceID: "code", Probability: 0.99},
			{ChoiceID: "qa", Probability: 0.005},
		}, 0.99)
		payload, err := json.Marshal(decision)
		if err != nil {
			t.Fatal(err)
		}
		payload = append(payload[:len(payload)-1], []byte(`,"unexpected":"field"}`)...)
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write(payload)
	}))
	defer server.Close()

	provider := mustRemoteProvider(t, server.URL, "remote-proof")
	if _, err := provider.Decide(context.Background(), request); err == nil || !strings.Contains(err.Error(), "unknown field") {
		t.Fatalf("expected strict JSON rejection, got %v", err)
	}
}

func TestRemoteProviderRejectsOversizedResponse(t *testing.T) {
	packet := decisionTestPacket(false)
	request := mustDecisionRequest(t, packet)
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		_, _ = writer.Write([]byte(strings.Repeat("x", 65)))
	}))
	defer server.Close()

	provider, err := NewRemoteProvider(RemoteProviderConfig{
		Endpoint:         server.URL,
		ProviderID:       "remote-proof",
		MaxResponseBytes: 64,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := provider.Decide(context.Background(), request); err == nil || !strings.Contains(err.Error(), "exceeds 64 bytes") {
		t.Fatalf("expected response size rejection, got %v", err)
	}
}

func TestNewRemoteProviderRejectsUnsafeConfiguration(t *testing.T) {
	for name, config := range map[string]RemoteProviderConfig{
		"unsupported scheme": {Endpoint: "file:///tmp/provider", ProviderID: "remote-proof"},
		"missing provider":   {Endpoint: "https://example.com"},
		"reserved auth": {
			Endpoint:   "https://example.com",
			ProviderID: "remote-proof",
			Headers:    map[string]string{"Authorization": "Basic abc"},
		},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := NewRemoteProvider(config); err == nil {
				t.Fatalf("expected config %q to fail", name)
			}
		})
	}
}

func decisionServer(t *testing.T, build func(Request) Decision) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, httpRequest *http.Request) {
		var request Request
		decoder := json.NewDecoder(httpRequest.Body)
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&request); err != nil {
			writer.WriteHeader(http.StatusBadRequest)
			_, _ = fmt.Fprint(writer, err.Error())
			return
		}
		writer.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(writer).Encode(build(request)); err != nil {
			t.Errorf("encode remote response: %v", err)
		}
	}))
}

func mustRemoteProvider(t *testing.T, endpoint, providerID string) *RemoteProvider {
	t.Helper()
	provider, err := NewRemoteProvider(RemoteProviderConfig{
		Endpoint:   endpoint,
		ProviderID: providerID,
		Timeout:    time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	return provider
}
