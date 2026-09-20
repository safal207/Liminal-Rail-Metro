package moltbook

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestMoltbookIdentityVerificationResultCarriesPointInTimeProvenance(t *testing.T) {
	const exactAgentID = "Agent-e\u0301-CaseSensitive"
	fixed := time.Date(2026, 9, 20, 19, 45, 0, 123456789, time.UTC)

	client := &http.Client{
		Timeout: time.Second,
		Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			return jsonResponse(req, http.StatusOK, "{\"success\":true,\"valid\":true,\"agent\":{\"id\":\"Agent-e\\u0301-CaseSensitive\"}}"), nil
		}),
	}
	verifier, err := NewMoltbookIdentityVerifier("moltdev_test", client)
	if err != nil {
		t.Fatal(err)
	}
	verifier.now = func() time.Time { return fixed }

	result, err := verifier.VerifyIdentityResultContext(context.Background(), "identity-token")
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != IdentityStatusVerified {
		t.Fatalf("status = %q, want %q", result.Status, IdentityStatusVerified)
	}
	if result.Provider != "moltbook" {
		t.Fatalf("provider = %q, want moltbook", result.Provider)
	}
	if result.AgentID != exactAgentID {
		t.Fatalf("agent id = %q, want exact %q", result.AgentID, exactAgentID)
	}
	if result.VerifiedAt != fixed.Format(time.RFC3339Nano) {
		t.Fatalf("verified_at = %q, want %q", result.VerifiedAt, fixed.Format(time.RFC3339Nano))
	}
	if result.VerificationSource != moltbookVerifyIdentityURL {
		t.Fatalf("verification source = %q", result.VerificationSource)
	}
}

func TestMoltbookIdentityVerificationClassifiesInvalidVsUnknown(t *testing.T) {
	tests := []struct {
		name       string
		statusCode int
		body       string
		token      string
		wantStatus IdentityVerificationStatus
	}{
		{
			name:       "explicit invalid",
			statusCode: http.StatusOK,
			body:       "{\"success\":true,\"valid\":false,\"agent\":{\"id\":\"agent-123\"}}",
			token:      "identity-token",
			wantStatus: IdentityStatusInvalid,
		},
		{
			name:       "provider unsuccessful is unknown",
			statusCode: http.StatusOK,
			body:       "{\"success\":false,\"valid\":true,\"agent\":{\"id\":\"agent-123\"}}",
			token:      "identity-token",
			wantStatus: IdentityStatusUnknownOrHold,
		},
		{
			name:       "empty token",
			statusCode: http.StatusOK,
			body:       "{\"success\":true,\"valid\":true,\"agent\":{\"id\":\"agent-123\"}}",
			token:      "   ",
			wantStatus: IdentityStatusInvalid,
		},
		{
			name:       "provider 500",
			statusCode: http.StatusInternalServerError,
			body:       "{\"error\":\"unavailable\"}",
			token:      "identity-token",
			wantStatus: IdentityStatusUnknownOrHold,
		},
		{
			name:       "provider 401 is not inferred invalid",
			statusCode: http.StatusUnauthorized,
			body:       "{\"error\":\"unauthorized\"}",
			token:      "identity-token",
			wantStatus: IdentityStatusUnknownOrHold,
		},
		{
			name:       "malformed response",
			statusCode: http.StatusOK,
			body:       "{not-json",
			token:      "identity-token",
			wantStatus: IdentityStatusUnknownOrHold,
		},
		{
			name:       "missing agent id",
			statusCode: http.StatusOK,
			body:       "{\"success\":true,\"valid\":true,\"agent\":{\"id\":\"\"}}",
			token:      "identity-token",
			wantStatus: IdentityStatusUnknownOrHold,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := &http.Client{
				Timeout: time.Second,
				Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
					return jsonResponse(req, tt.statusCode, tt.body), nil
				}),
			}
			verifier, err := NewMoltbookIdentityVerifier("moltdev_test", client)
			if err != nil {
				t.Fatal(err)
			}

			result, err := verifier.VerifyIdentityResultContext(context.Background(), tt.token)
			if err == nil {
				t.Fatalf("expected classified failure, got result %#v", result)
			}
			got, ok := IdentityVerificationStatusOf(err)
			if !ok {
				t.Fatalf("error is not classified: %T %v", err, err)
			}
			if got != tt.wantStatus {
				t.Fatalf("classified status = %q, want %q", got, tt.wantStatus)
			}
			if tt.wantStatus == IdentityStatusInvalid && strings.TrimSpace(tt.token) != "" && result.Status != IdentityStatusInvalid {
				t.Fatalf("result status = %q, want INVALID", result.Status)
			}
			if tt.wantStatus == IdentityStatusUnknownOrHold && result.Status != IdentityStatusUnknownOrHold {
				t.Fatalf("result status = %q, want UNKNOWN/HOLD", result.Status)
			}
		})
	}
}

func TestMoltbookAgentIDIsRejectedNotRepaired(t *testing.T) {
	tests := []struct {
		name string
		id   string
		want string
	}{
		{name: "leading whitespace", id: " agent-123", want: "surrounding whitespace"},
		{name: "trailing whitespace", id: "agent-123 ", want: "surrounding whitespace"},
		{name: "control character", id: "agent-\n123", want: "control character"},
		{name: "oversized", id: strings.Repeat("a", moltbookAgentIDMaxBytes+1), want: "exceeds limit"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			encoded, err := json.Marshal(tt.id)
			if err != nil {
				t.Fatal(err)
			}
			payload := "{\"success\":true,\"valid\":true,\"agent\":{\"id\":" + string(encoded) + "}}"

			client := &http.Client{
				Timeout: time.Second,
				Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
					return jsonResponse(req, http.StatusOK, payload), nil
				}),
			}
			verifier, err := NewMoltbookIdentityVerifier("moltdev_test", client)
			if err != nil {
				t.Fatal(err)
			}

			result, err := verifier.VerifyIdentityResultContext(context.Background(), "identity-token")
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("expected %q rejection, got result=%#v err=%v", tt.want, result, err)
			}
			status, ok := IdentityVerificationStatusOf(err)
			if !ok || status != IdentityStatusUnknownOrHold {
				t.Fatalf("classification = %q ok=%v, want UNKNOWN/HOLD", status, ok)
			}
			if result.AgentID != "" {
				t.Fatalf("rejected id leaked as normalized id %q", result.AgentID)
			}
		})
	}
}

func TestUnknownIdentityVerificationNeverReachesAuthorityOrEvidence(t *testing.T) {
	client := &http.Client{
		Timeout: time.Second,
		Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			return jsonResponse(req, http.StatusInternalServerError, "{\"error\":\"provider unavailable\"}"), nil
		}),
	}
	verifier, err := NewMoltbookIdentityVerifier("moltdev_test", client)
	if err != nil {
		t.Fatal(err)
	}

	authority := &fakeAuthorityGate{}
	evidence := &fakeEvidenceVerifier{
		result: map[string]any{
			"verdict":         "VERIFIED",
			"evidence_sha256": "must-not-run",
		},
	}
	station, err := NewStation(verifier, evidence, authority)
	if err != nil {
		t.Fatal(err)
	}

	_, err = station.Execute(Request{
		IdentityToken:   "identity-token",
		ActionID:        "identity-unknown-hold-001",
		Intent:          IntentVerifyEvidence,
		Target:          TargetAgentProof,
		EvidenceRef:     "fixture://must-not-run",
		ExternalEffects: false,
	})
	if err == nil {
		t.Fatal("expected identity verification failure")
	}
	status, ok := IdentityVerificationStatusOf(errors.Unwrap(err))
	if !ok || status != IdentityStatusUnknownOrHold {
		t.Fatalf("wrapped identity classification = %q ok=%v err=%v", status, ok, err)
	}
	if authority.calls != 0 {
		t.Fatalf("authority calls = %d, want 0", authority.calls)
	}
	if evidence.calls != 0 {
		t.Fatalf("evidence calls = %d, want 0", evidence.calls)
	}
}

func TestMoltbookUnknownClassificationForReadFailures(t *testing.T) {
	t.Run("transport failure", func(t *testing.T) {
		client := &http.Client{
			Timeout: time.Second,
			Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
				return nil, errors.New("network unavailable")
			}),
		}
		verifier, err := NewMoltbookIdentityVerifier("moltdev_test", client)
		if err != nil {
			t.Fatal(err)
		}
		result, err := verifier.VerifyIdentityResultContext(context.Background(), "identity-token")
		assertUnknownIdentityFailure(t, result, err)
	})

	t.Run("context cancellation", func(t *testing.T) {
		client := &http.Client{
			Timeout: time.Second,
			Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
				<-req.Context().Done()
				return nil, req.Context().Err()
			}),
		}
		verifier, err := NewMoltbookIdentityVerifier("moltdev_test", client)
		if err != nil {
			t.Fatal(err)
		}
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		result, err := verifier.VerifyIdentityResultContext(ctx, "identity-token")
		assertUnknownIdentityFailure(t, result, err)
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("expected context.Canceled, got %v", err)
		}
	})

	t.Run("redirect", func(t *testing.T) {
		client := &http.Client{
			Timeout: time.Second,
			Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
				resp := jsonResponse(req, http.StatusFound, "{}")
				resp.Header.Set("Location", "https://evil.example/steal")
				return resp, nil
			}),
		}
		verifier, err := NewMoltbookIdentityVerifier("moltdev_test", client)
		if err != nil {
			t.Fatal(err)
		}
		result, err := verifier.VerifyIdentityResultContext(context.Background(), "identity-token")
		assertUnknownIdentityFailure(t, result, err)
	})

	t.Run("oversized response", func(t *testing.T) {
		client := &http.Client{
			Timeout: time.Second,
			Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
				return jsonResponse(req, http.StatusOK, strings.Repeat("x", moltbookIdentityResponseLimit+1)), nil
			}),
		}
		verifier, err := NewMoltbookIdentityVerifier("moltdev_test", client)
		if err != nil {
			t.Fatal(err)
		}
		result, err := verifier.VerifyIdentityResultContext(context.Background(), "identity-token")
		assertUnknownIdentityFailure(t, result, err)
	})
}

func assertUnknownIdentityFailure(t *testing.T, result IdentityVerificationResult, err error) {
	t.Helper()
	if err == nil {
		t.Fatalf("expected UNKNOWN/HOLD failure, got result %#v", result)
	}
	status, ok := IdentityVerificationStatusOf(err)
	if !ok || status != IdentityStatusUnknownOrHold {
		t.Fatalf("classified status = %q ok=%v, want UNKNOWN/HOLD; err=%v", status, ok, err)
	}
	if result.Status != IdentityStatusUnknownOrHold {
		t.Fatalf("result status = %q, want UNKNOWN/HOLD", result.Status)
	}
	if result.AgentID != "" || result.VerifiedAt != "" {
		t.Fatalf("UNKNOWN/HOLD leaked verified identity fields: %#v", result)
	}
}
