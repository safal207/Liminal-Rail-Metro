package moltbook

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestMoltbookIdentityRequiresExplicitBooleanVerdicts(t *testing.T) {
	values := []struct {
		name string
		json string
	}{
		{name: "true", json: "true"},
		{name: "false", json: "false"},
		{name: "null", json: "null"},
		{name: "absent"},
	}
	for _, success := range values {
		for _, valid := range values {
			t.Run("success_"+success.name+"_valid_"+valid.name, func(t *testing.T) {
				fields := []string{`"agent":{"id":"agent-123"}`}
				if success.json != "" {
					fields = append(fields, `"success":`+success.json)
				}
				if valid.json != "" {
					fields = append(fields, `"valid":`+valid.json)
				}
				verifier := strictIdentityVerifier(t, "{"+strings.Join(fields, ",")+"}")
				want := IdentityStatusUnknownOrHold
				if valid.json == "false" {
					want = IdentityStatusInvalid
				} else if success.json == "true" && valid.json == "true" {
					want = IdentityStatusVerified
				}

				result, err := verifier.VerifyIdentityResultContext(context.Background(), "identity-token")
				if want == IdentityStatusVerified {
					if err != nil || result.Status != want || result.AgentID != "agent-123" || result.VerifiedAt == "" {
						t.Fatalf("explicit positive verdicts: result=%#v err=%v", result, err)
					}
					return
				}
				assertStrictIdentityFailure(t, result, err, want)
				identity, err := verifier.VerifyIdentity("identity-token")
				if err == nil || identity.Verified || identity.AgentID != "" {
					t.Fatalf("failure exposed verified identity: identity=%#v err=%v", identity, err)
				}
			})
		}
	}
}

func TestMoltbookIdentityRejectsMalformedBooleanTypes(t *testing.T) {
	for _, verdicts := range []string{
		`"success":"true","valid":true`,
		`"success":true,"valid":"false"`,
		`"success":1,"valid":true`,
		`"success":true,"valid":0`,
	} {
		t.Run(verdicts, func(t *testing.T) {
			verifier := strictIdentityVerifier(t, `{`+verdicts+`,"agent":{"id":"agent-123"}}`)
			result, err := verifier.VerifyIdentityResultContext(context.Background(), "identity-token")
			assertStrictIdentityFailure(t, result, err, IdentityStatusUnknownOrHold)
		})
	}
}

func TestMoltbookIdentityRejectsLossyAgentIDDecoding(t *testing.T) {
	tests := []struct {
		name  string
		rawID string
	}{
		{name: "invalid UTF8 byte", rawID: "\"agent-\xff\""},
		{name: "truncated UTF8 sequence", rawID: "\"agent-\xe2\x82\""},
		{name: "overlong UTF8 sequence", rawID: "\"agent-\xc0\xaf\""},
		{name: "UTF8 encoded high surrogate", rawID: "\"agent-\xed\xa0\x80\""},
		{name: "UTF8 encoded low surrogate", rawID: "\"agent-\xed\xbf\xbf\""},
		{name: "UTF8 above Unicode maximum", rawID: "\"agent-\xf4\x90\x80\x80\""},
		{name: "lone high surrogate", rawID: `"agent-\uD800"`},
		{name: "lone last high surrogate", rawID: `"agent-\uDBFF"`},
		{name: "lone low surrogate", rawID: `"agent-\uDC00"`},
		{name: "lone last low surrogate", rawID: `"agent-\uDFFF"`},
		{name: "high followed by high", rawID: `"agent-\uD800\uD801"`},
		{name: "low followed by high", rawID: `"agent-\uDC00\uD800"`},
		{name: "high followed by BMP escape", rawID: `"agent-\uD800\u0041"`},
		{name: "high followed by literal text", rawID: `"agent-\uD800x"`},
		{name: "high followed by literal backslash u", rawID: `"agent-\uD800\\uDC00"`},
		{name: "valid pair followed by lone low", rawID: `"agent-\uD800\uDC00\uDC00"`},
		{name: "null ID", rawID: `null`},
		{name: "numeric ID", rawID: `123`},
		{name: "260 decoded UTF8 bytes", rawID: `"` + strings.Repeat(`\uD800\uDC00`, 65) + `"`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			verifier := strictIdentityVerifier(t, `{"success":true,"valid":true,"agent":{"id":`+tt.rawID+`}}`)
			result, err := verifier.VerifyIdentityResultContext(context.Background(), "identity-token")
			assertStrictIdentityFailure(t, result, err, IdentityStatusUnknownOrHold)
			identity, err := verifier.VerifyIdentity("identity-token")
			if err == nil || identity.Verified || identity.AgentID != "" {
				t.Fatalf("lossy ID exposed verified identity: identity=%#v err=%v", identity, err)
			}
		})
	}
}

func TestMoltbookIdentityPreservesValidUnicodeAgentID(t *testing.T) {
	tests := []struct {
		name  string
		rawID string
		want  string
	}{
		{name: "first supplementary code point", rawID: `"agent-\uD800\uDC00"`, want: "agent-\U00010000"},
		{name: "last Unicode code point", rawID: `"agent-\uDBFF\uDFFF"`, want: "agent-\U0010FFFF"},
		{name: "mixed case surrogate pair", rawID: `"agent-\ud83D\uDe80"`, want: "agent-\U0001F680"},
		{name: "adjacent surrogate pairs", rawID: `"agent-\uD800\uDC00\uDBFF\uDFFF"`, want: "agent-\U00010000\U0010FFFF"},
		{name: "literal supplementary character", rawID: "\"agent-\U0001F680\"", want: "agent-\U0001F680"},
		{name: "literal replacement character", rawID: "\"agent-\uFFFD\"", want: "agent-\uFFFD"},
		{name: "escaped replacement character", rawID: `"agent-\uFFFD"`, want: "agent-\uFFFD"},
		{name: "literal backslash u text", rawID: `"agent-\\uD800"`, want: `agent-\uD800`},
		{name: "Unicode escaped backslash u text", rawID: `"agent-\u005CuD800"`, want: `agent-\uD800`},
		{name: "escaped backslash then valid pair", rawID: `"agent-\\\uD800\uDC00"`, want: "agent-\\\U00010000"},
		{name: "escaped combining character", rawID: `"Agent-e\u0301-CaseSensitive"`, want: "Agent-e\u0301-CaseSensitive"},
		{name: "literal combining character", rawID: "\"Agent-e\u0301-CaseSensitive\"", want: "Agent-e\u0301-CaseSensitive"},
		{name: "256 decoded UTF8 bytes", rawID: `"` + strings.Repeat(`\uD800\uDC00`, 64) + `"`, want: strings.Repeat("\U00010000", 64)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			verifier := strictIdentityVerifier(t, `{"success":true,"valid":true,"agent":{"id":`+tt.rawID+`}}`)
			result, err := verifier.VerifyIdentityResultContext(context.Background(), "identity-token")
			if err != nil || result.Status != IdentityStatusVerified || result.AgentID != tt.want || result.VerifiedAt == "" {
				t.Fatalf("valid ID was not preserved: want=%q result=%#v err=%v", tt.want, result, err)
			}
			identity, err := verifier.VerifyIdentity("identity-token")
			if err != nil || !identity.Verified || identity.AgentID != tt.want {
				t.Fatalf("verified identity changed: want=%q identity=%#v err=%v", tt.want, identity, err)
			}
		})
	}
}

func TestMoltbookStrictIdentityFailuresNeverDispatch(t *testing.T) {
	tests := []struct {
		name string
		body string
		want IdentityVerificationStatus
	}{
		{name: "explicit false overrides unsuccessful", body: `{"success":false,"valid":false,"agent":{"id":"agent-123"}}`, want: IdentityStatusInvalid},
		{name: "missing valid", body: `{"success":true,"agent":{"id":"agent-123"}}`, want: IdentityStatusUnknownOrHold},
		{name: "null success", body: `{"success":null,"valid":true,"agent":{"id":"agent-123"}}`, want: IdentityStatusUnknownOrHold},
		{name: "malformed surrogate", body: `{"success":true,"valid":true,"agent":{"id":"agent-\uD800"}}`, want: IdentityStatusUnknownOrHold},
		{name: "invalid UTF8", body: "{\"success\":true,\"valid\":true,\"agent\":{\"id\":\"agent-\xff\"}}", want: IdentityStatusUnknownOrHold},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			authority := &fakeAuthorityGate{}
			evidence := &fakeEvidenceVerifier{}
			station, err := NewStation(strictIdentityVerifier(t, tt.body), evidence, authority)
			if err != nil {
				t.Fatal(err)
			}
			_, err = station.Execute(Request{
				IdentityToken:   "identity-token",
				ActionID:        "strict-identity-no-dispatch-001",
				Intent:          IntentVerifyEvidence,
				Target:          TargetAgentProof,
				EvidenceRef:     "fixture://must-not-run",
				ExternalEffects: false,
			})
			status, ok := IdentityVerificationStatusOf(err)
			if err == nil || !ok || status != tt.want {
				t.Fatalf("station classification = %q ok=%v, want %q; err=%v", status, ok, tt.want, err)
			}
			if authority.calls != 0 || evidence.calls != 0 {
				t.Fatalf("identity failure dispatched: authority calls=%d evidence calls=%d", authority.calls, evidence.calls)
			}
		})
	}
}

func strictIdentityVerifier(t *testing.T, body string) *MoltbookIdentityVerifier {
	t.Helper()
	client := &http.Client{
		Timeout: time.Second,
		Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			return jsonResponse(req, http.StatusOK, body), nil
		}),
	}
	verifier, err := NewMoltbookIdentityVerifier("moltdev_test", client)
	if err != nil {
		t.Fatal(err)
	}
	return verifier
}

func assertStrictIdentityFailure(t *testing.T, result IdentityVerificationResult, err error, want IdentityVerificationStatus) {
	t.Helper()
	status, ok := IdentityVerificationStatusOf(err)
	if err == nil || !ok || status != want || result.Status != want {
		t.Fatalf("classification = %q ok=%v, want %q; result=%#v err=%v", status, ok, want, result, err)
	}
	if result.AgentID != "" || result.VerifiedAt != "" {
		t.Fatalf("failure leaked verified identity fields: %#v", result)
	}
}
