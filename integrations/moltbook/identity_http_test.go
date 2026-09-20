package moltbook

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/safal207/Liminal-Rail-Metro/internal/decisionplane"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

type identityIntegrationEvidence struct {
	calls atomic.Int64
}

func (e *identityIntegrationEvidence) VerifyEvidence(string) (map[string]any, error) {
	e.calls.Add(1)
	return map[string]any{
		"verdict":         "VERIFIED",
		"evidence_sha256": "identity-integration-fixture",
	}, nil
}

func TestMoltbookIdentityVerifierNormalizesDocumentedContract(t *testing.T) {
	const (
		appKey = "moltdev_test_app_key"
		token  = "identity-token-value"
	)

	client := &http.Client{
		Timeout: time.Second,
		Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			if req.Method != http.MethodPost {
				t.Fatalf("method = %q, want POST", req.Method)
			}
			if req.URL.String() != moltbookVerifyIdentityURL {
				t.Fatalf("url = %q, want %q", req.URL.String(), moltbookVerifyIdentityURL)
			}
			if got := req.Header.Get("X-Moltbook-App-Key"); got != appKey {
				t.Fatalf("app key header = %q, want configured key", got)
			}
			if got := req.Header.Get("Authorization"); got != "" {
				t.Fatalf("unexpected Authorization header %q", got)
			}
			if got := req.Header.Get("Content-Type"); got != "application/json" {
				t.Fatalf("content type = %q, want application/json", got)
			}
			body, err := io.ReadAll(req.Body)
			if err != nil {
				t.Fatal(err)
			}
			if got := string(body); got != `{"token":"identity-token-value"}` {
				t.Fatalf("request body = %s", got)
			}

			return jsonResponse(req, http.StatusOK, `{
				"success": true,
				"valid": true,
				"agent": {
					"id": "agent-123",
					"name": "ExampleBot",
					"karma": 999999,
					"is_claimed": false,
					"follower_count": 12345,
					"owner": {"x_handle":"owner","x_verified":true}
				}
			}`), nil
		}),
	}

	verifier, err := NewMoltbookIdentityVerifier(appKey, client)
	if err != nil {
		t.Fatal(err)
	}
	identity, err := verifier.VerifyIdentity(token)
	if err != nil {
		t.Fatal(err)
	}
	if identity.AgentID != "agent-123" || !identity.Verified {
		t.Fatalf("unexpected normalized identity %#v", identity)
	}
}

func TestMoltbookIdentityVerifierFailsClosedOnInvalidResponses(t *testing.T) {
	tests := []struct {
		name       string
		statusCode int
		body       string
		want       string
	}{
		{
			name:       "invalid token",
			statusCode: http.StatusOK,
			body:       `{"success":true,"valid":false,"agent":{"id":"agent-123"}}`,
			want:       "token is invalid",
		},
		{
			name:       "unsuccessful response",
			statusCode: http.StatusOK,
			body:       `{"success":false,"valid":true,"agent":{"id":"agent-123"}}`,
			want:       "token is invalid",
		},
		{
			name:       "missing agent id",
			statusCode: http.StatusOK,
			body:       `{"success":true,"valid":true,"agent":{"id":"   "}}`,
			want:       "missing agent id",
		},
		{
			name:       "malformed json",
			statusCode: http.StatusOK,
			body:       `{not-json`,
			want:       "invalid JSON",
		},
		{
			name:       "non 2xx",
			statusCode: http.StatusUnauthorized,
			body:       `{"error":"nope"}`,
			want:       "HTTP 401",
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
			identity, err := verifier.VerifyIdentity("identity-token")
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("expected error containing %q, got identity=%#v err=%v", tt.want, identity, err)
			}
			if identity.Verified || identity.AgentID != "" {
				t.Fatalf("failure returned verified identity %#v", identity)
			}
		})
	}
}

func TestMoltbookIdentityVerifierRejectsMissingOrMalformedAppKey(t *testing.T) {
	for _, appKey := range []string{"", "not-a-moltbook-key"} {
		if verifier, err := NewMoltbookIdentityVerifier(appKey, nil); err == nil || verifier != nil {
			t.Fatalf("app key %q should fail closed, verifier=%#v err=%v", appKey, verifier, err)
		}
	}
}

func TestMoltbookIdentityVerifierHonorsContextCancellation(t *testing.T) {
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
	_, err = verifier.VerifyIdentityContext(ctx, "identity-token")
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled, got %v", err)
	}
}

func TestMoltbookIdentityVerifierFailsClosedOnHTTPTimeout(t *testing.T) {
	client := &http.Client{
		Timeout: 10 * time.Millisecond,
		Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			<-req.Context().Done()
			return nil, req.Context().Err()
		}),
	}
	verifier, err := NewMoltbookIdentityVerifier("moltdev_test", client)
	if err != nil {
		t.Fatal(err)
	}

	if _, err := verifier.VerifyIdentity("identity-token"); err == nil {
		t.Fatal("expected timeout to fail closed")
	}
}

func TestMoltbookIdentityVerifierRejectsRedirectWithoutFollowing(t *testing.T) {
	var calls atomic.Int64
	client := &http.Client{
		Timeout: time.Second,
		Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			calls.Add(1)
			resp := jsonResponse(req, http.StatusFound, `{}`)
			resp.Header.Set("Location", "https://evil.example/steal")
			return resp, nil
		}),
	}
	verifier, err := NewMoltbookIdentityVerifier("moltdev_test", client)
	if err != nil {
		t.Fatal(err)
	}

	_, err = verifier.VerifyIdentity("identity-token")
	if err == nil || !strings.Contains(err.Error(), "HTTP 302") {
		t.Fatalf("expected redirect rejection, got %v", err)
	}
	if got := calls.Load(); got != 1 {
		t.Fatalf("round trip calls = %d, want 1", got)
	}
}

func TestMoltbookIdentityVerifierRejectsOversizedResponse(t *testing.T) {
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

	_, err = verifier.VerifyIdentity("identity-token")
	if err == nil || !strings.Contains(err.Error(), "exceeds limit") {
		t.Fatalf("expected oversized response rejection, got %v", err)
	}
}

func TestMoltbookIdentityVerifierDoesNotDiscloseSecretsInErrors(t *testing.T) {
	const (
		appKey = "moltdev_SUPER_SECRET_APP_KEY"
		token  = "SUPER_SECRET_IDENTITY_TOKEN"
	)
	client := &http.Client{
		Timeout: time.Second,
		Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			return jsonResponse(req, http.StatusUnauthorized, `{"error":"`+appKey+` `+token+`"}`), nil
		}),
	}
	verifier, err := NewMoltbookIdentityVerifier(appKey, client)
	if err != nil {
		t.Fatal(err)
	}

	_, err = verifier.VerifyIdentity(token)
	if err == nil {
		t.Fatal("expected verification failure")
	}
	if strings.Contains(err.Error(), appKey) || strings.Contains(err.Error(), token) {
		t.Fatalf("secret leaked in error: %v", err)
	}
}

func TestMoltbookMetadataCannotBecomeStationAuthority(t *testing.T) {
	client := &http.Client{
		Timeout: time.Second,
		Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			return jsonResponse(req, http.StatusOK, `{
				"success":true,
				"valid":true,
				"agent":{
					"id":"agent-social-metadata",
					"karma":-999999,
					"is_claimed":false,
					"follower_count":0,
					"owner":{"x_verified":false,"x_follower_count":0}
				}
			}`), nil
		}),
	}
	verifier, err := NewMoltbookIdentityVerifier("moltdev_test", client)
	if err != nil {
		t.Fatal(err)
	}
	authority, err := NewDecisionPlaneAuthority(decisionplane.StaticProvider{
		ID: "identity-integration-authority",
		Scores: map[string]float64{
			"moltbook-target": 1,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	evidence := &identityIntegrationEvidence{}
	station, err := NewStation(verifier, evidence, authority)
	if err != nil {
		t.Fatal(err)
	}

	out, err := station.Execute(Request{
		IdentityToken:   "identity-token",
		ActionID:        "molt-identity-integration-001",
		Intent:          IntentVerifyEvidence,
		Target:          TargetAgentProof,
		EvidenceRef:     "fixture://identity-integration",
		ExternalEffects: false,
	})
	if err != nil {
		t.Fatal(err)
	}
	if out.Packet.SourceAgent == "" || !strings.HasPrefix(out.Packet.SourceAgent, "moltbook:") {
		t.Fatalf("unexpected source agent %q", out.Packet.SourceAgent)
	}
	if out.Authority.ProviderID != "identity-integration-authority" {
		t.Fatalf("authority provider = %q", out.Authority.ProviderID)
	}
	if out.Authority.Disposition != DispositionAutoRoute {
		t.Fatalf("authority disposition = %q", out.Authority.Disposition)
	}
	if got := evidence.calls.Load(); got != 1 {
		t.Fatalf("evidence calls = %d, want 1", got)
	}
}

func jsonResponse(req *http.Request, status int, body string) *http.Response {
	return &http.Response{
		StatusCode: status,
		Status:     http.StatusText(status),
		Header:     make(http.Header),
		Body:       io.NopCloser(strings.NewReader(body)),
		Request:    req,
	}
}
