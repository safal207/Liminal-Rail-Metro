package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/safal207/Liminal-Rail-Metro/integrations/moltbook"
	"github.com/safal207/Liminal-Rail-Metro/internal/metro"
)

const (
	testKey   = "moltdev_secret-test-application-key"
	testToken = "secret-test-identity-token"
	testAgent = "private-test-agent-id"
)

var cleanRevision = revision{Commit: strings.Repeat("a", 40)}

type transportFunc func(*http.Request) (*http.Response, error)

func (f transportFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func testCredentials(name string) string {
	switch name {
	case "MOLTBOOK_APP_KEY":
		return testKey
	case "MOLTBOOK_IDENTITY_TOKEN":
		return testToken
	default:
		panic("unexpected credential lookup")
	}
}

func decodeReport(t *testing.T, b *bytes.Buffer) report {
	t.Helper()
	var r report
	if err := json.Unmarshal(b.Bytes(), &r); err != nil {
		t.Fatalf("decode smoke report: %v", err)
	}
	return r
}

func noCredentials(string) string { panic("offline mode read credentials") }

func TestOfflineRehearsalNeverUsesNetworkOrCredentials(t *testing.T) {
	client := &http.Client{Transport: transportFunc(func(*http.Request) (*http.Response, error) {
		t.Fatal("offline mode used the supplied network transport")
		return nil, nil
	})}
	var out, stderr bytes.Buffer
	if code := run(nil, &out, &stderr, noCredentials, client, revision{}); code != 0 {
		t.Fatalf("rehearsal failed: %s %s", &out, &stderr)
	}
	r := decodeReport(t, &out)
	if r.Mode != "offline_rehearsal" || r.LiveIdentityVerified ||
		r.IdentityProvenance != identityProvenanceSynthetic ||
		r.IdentityStatus != "" || r.IdentityVerifiedAt != "" ||
		r.VerificationSource != syntheticIdentitySource ||
		r.IdentityAttempts != 1 || r.AuthorityCalls != 1 || r.EvidenceCalls != 1 {
		t.Fatalf("unexpected rehearsal metadata: %+v", r)
	}
	if r.StationResult == nil || moltbook.VerifyResult(*r.StationResult) != nil {
		t.Fatal("rehearsal did not produce a valid bound receipt")
	}
	if r.StationResult.Route.SelectedTarget != smokeTarget || r.StationResult.Receipt.ExecutorID != smokeTarget {
		t.Fatalf("offline receipt target/executor = %q/%q, want %q", r.StationResult.Route.SelectedTarget, r.StationResult.Receipt.ExecutorID, smokeTarget)
	}
}

func TestLivePreflightBlocksBeforeNetwork(t *testing.T) {
	cases := []struct {
		name   string
		rev    revision
		getenv func(string) string
		code   string
	}{
		{"unknown revision", revision{}, noCredentials, "clean_recorded_build_required"},
		{"dirty build", revision{Commit: cleanRevision.Commit, Dirty: true}, noCredentials, "clean_recorded_build_required"},
		{"missing credentials", cleanRevision, func(string) string { return "" }, "developer_credentials_required"},
		{"whitespace credentials", cleanRevision, func(string) string { return " \t" }, "developer_credentials_required"},
		{"wrong key prefix", cleanRevision, func(string) string { return testToken }, "app_key_configuration_invalid"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			client := &http.Client{Transport: transportFunc(func(*http.Request) (*http.Response, error) {
				t.Fatal("blocked preflight made an HTTP request")
				return nil, nil
			})}
			var out, stderr bytes.Buffer
			if run([]string{"-live"}, &out, &stderr, tc.getenv, client, tc.rev) == 0 {
				t.Fatal("blocked live run succeeded")
			}
			r := decodeReport(t, &out)
			if r.Status != "BLOCKED" || r.FailureCode != tc.code || r.IdentityAttempts != 0 || r.AuthorityCalls != 0 || r.EvidenceCalls != 0 || r.LiveIdentityVerified {
				t.Fatalf("unexpected blocked report: %+v", r)
			}
		})
	}
}

func TestLiveIdentityBoundaryAndRedaction(t *testing.T) {
	cases := []struct {
		name   string
		body   string
		status int
		err    error
		want   moltbook.IdentityVerificationStatus
	}{
		{"verified", `{"success":true,"valid":true,"agent":{"id":"` + testAgent + `"}}`, 200, nil, moltbook.IdentityStatusVerified},
		{"explicit rejection wins", `{"success":false,"valid":false}`, 200, nil, moltbook.IdentityStatusInvalid},
		{"valid absent", `{"success":true}`, 200, nil, moltbook.IdentityStatusUnknownOrHold},
		{"invalid Unicode", `{"success":true,"valid":true,"agent":{"id":"\uD800"}}`, 200, nil, moltbook.IdentityStatusUnknownOrHold},
		{"provider failure", testKey + testToken + testAgent, 503, nil, moltbook.IdentityStatusUnknownOrHold},
		{"redirect", "", 307, nil, moltbook.IdentityStatusUnknownOrHold},
		{"transport failure", "", 0, errors.New(testKey + testToken), moltbook.IdentityStatusUnknownOrHold},
		{"deadline", "", 0, context.DeadlineExceeded, moltbook.IdentityStatusUnknownOrHold},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			client := &http.Client{Transport: transportFunc(func(req *http.Request) (*http.Response, error) {
				calls++
				if req.Method != http.MethodPost || req.URL.String() != "https://www.moltbook.com/api/v1/agents/verify-identity" || req.Header.Get("X-Moltbook-App-Key") != testKey {
					t.Fatal("unexpected identity request destination, method or key")
				}
				var body map[string]string
				if json.NewDecoder(req.Body).Decode(&body) != nil || len(body) != 1 || body["token"] != testToken {
					t.Fatal("unexpected identity request body")
				}
				if tc.err != nil {
					return nil, tc.err
				}
				return &http.Response{StatusCode: tc.status, Request: req, Header: http.Header{"Location": {"https://must-not-follow.example/"}}, Body: io.NopCloser(strings.NewReader(tc.body))}, nil
			})}
			var out, stderr bytes.Buffer
			code := run([]string{"-live"}, &out, &stderr, testCredentials, client, cleanRevision)
			r := decodeReport(t, &out)
			verified := tc.want == moltbook.IdentityStatusVerified
			if (code == 0) != verified || r.IdentityStatus != tc.want || r.LiveIdentityVerified != verified || r.IdentityAttempts != 1 || calls != 1 {
				t.Fatalf("unexpected boundary outcome: exit=%d, report=%+v, requests=%d", code, r, calls)
			}
			if r.IdentityProvenance != identityProvenanceMoltbook {
				t.Fatalf("live identity provenance = %q", r.IdentityProvenance)
			}
			if verified {
				if r.AuthorityCalls != 1 || r.EvidenceCalls != 1 || r.StationResult == nil || moltbook.VerifyResult(*r.StationResult) != nil {
					t.Fatal("verified identity did not complete the bound local fixture path")
				}
				if r.StationResult.Route.SelectedTarget != smokeTarget || r.StationResult.Receipt.ExecutorID != smokeTarget {
					t.Fatalf("live receipt target/executor = %q/%q, want %q", r.StationResult.Route.SelectedTarget, r.StationResult.Receipt.ExecutorID, smokeTarget)
				}
			} else if r.AuthorityCalls != 0 || r.EvidenceCalls != 0 || r.StationResult != nil {
				t.Fatal("unverified identity reached authority or evidence dispatch")
			}
			for _, secret := range []string{testKey, testToken, testAgent} {
				if strings.Contains(out.String()+stderr.String(), secret) {
					t.Fatal("smoke output contains raw credentials or agent ID")
				}
			}
		})
	}
}

func TestSavedReportVerificationAndTamperRejection(t *testing.T) {
	var original, stderr bytes.Buffer
	if run(nil, &original, &stderr, noCredentials, nil, cleanRevision) != 0 {
		t.Fatal("rehearsal failed")
	}
	cases := []struct {
		name   string
		mutate func(*report)
	}{
		{"untouched", func(*report) {}},
		{"receipt input", func(r *report) { r.StationResult.Receipt.InputHash = "tampered" }},
		{"authority target", func(r *report) { r.StationResult.Authority.Target = "other" }},
		{"evidence hash", func(r *report) { r.StationResult.Verification["evidence_sha256"] = "tampered" }},
		{"receipt hash", func(r *report) { r.ReceiptSHA256 = "tampered" }},
		{"mode contradiction", func(r *report) { r.LiveIdentityVerified = true }},
		{"unknown mode", func(r *report) { r.Mode = "unrecognized" }},
		{"forged live status", func(r *report) { r.IdentityStatus = moltbook.IdentityStatusVerified }},
		{"forged verified_at", func(r *report) { r.IdentityVerifiedAt = "2026-09-20T00:00:00Z" }},
		{"wrong identity provenance", func(r *report) { r.IdentityProvenance = identityProvenanceMoltbook }},
		{"extra dispatch", func(r *report) { r.EvidenceCalls = 2 }},
		{"missing source", func(r *report) { r.VerificationSource = "" }},
		{"failure status", func(r *report) { r.Status = "FAIL" }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := decodeReport(t, &original)
			tc.mutate(&r)
			payload, err := json.Marshal(r)
			if err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(t.TempDir(), "report.json")
			if err := os.WriteFile(path, payload, 0600); err != nil {
				t.Fatal(err)
			}
			var out, stderr bytes.Buffer
			code := run([]string{"-verify-report", path}, &out, &stderr, noCredentials, nil, revision{})
			if (code == 0) != (tc.name == "untouched") {
				t.Fatalf("unexpected verification exit: %d (%s %s)", code, &out, &stderr)
			}
			if code == 0 && !strings.Contains(out.String(), "not independently reverified") {
				t.Fatal("offline verifier omitted the live-attestation limit")
			}
		})
	}
}

func TestControlFixtureIntegrityAndAuthorityScope(t *testing.T) {
	proof, err := newControlFixture()
	if err != nil {
		t.Fatal(err)
	}
	verifier := &fixtureVerifier{proof: proof}
	if _, err := verifier.VerifyEvidence(controlFixtureRef); err != nil {
		t.Fatal(err)
	}
	proof.Result["integrity"] = "corrupted"
	if _, err := verifier.VerifyEvidence(controlFixtureRef); err == nil {
		t.Fatal("changed fixture was accepted")
	}
	if _, err := verifier.VerifyEvidence("https://unapproved.example/evidence"); err == nil {
		t.Fatal("arbitrary evidence reference was accepted")
	}
	for _, field := range []string{"evidence", "side effects", "target", "intent"} {
		t.Run(field, func(t *testing.T) {
			packet := metro.NewPacket("test", "test", "test", metro.Action{Kind: "moltbook.verify_evidence", Inputs: map[string]any{"intent": moltbook.IntentVerifyEvidence, "evidence_ref": controlFixtureRef}}, []string{smokeTarget})
			switch field {
			case "evidence":
				packet.Action.Inputs["evidence_ref"] = "other"
			case "side effects":
				packet.Constraints.SideEffect = true
			case "target":
				packet.AllowedTargets = []string{"other"}
			case "intent":
				packet.Action.Inputs["intent"] = "WRITE"
			}
			authority, err := newSmokeAuthority()
			if err != nil {
				t.Fatal(err)
			}
			if _, err := authority.Authorize(context.Background(), packet, smokeTarget); err == nil {
				t.Fatal("out-of-scope packet was authorized")
			}
		})
	}
}

type failedWriter struct{}

func (failedWriter) Write([]byte) (int, error) { return 0, errors.New("write failed") }

func TestOutputFailureDoesNotRepeatLiveIdentity(t *testing.T) {
	calls := 0
	client := &http.Client{Transport: transportFunc(func(req *http.Request) (*http.Response, error) {
		calls++
		return rehearsalTransport{}.RoundTrip(req)
	})}
	var stderr bytes.Buffer
	if run([]string{"-live"}, failedWriter{}, &stderr, testCredentials, client, cleanRevision) == 0 || calls != 1 {
		t.Fatalf("output failure succeeded or retried: calls=%d", calls)
	}
}

func TestInvalidArgumentsDoNotEchoSecrets(t *testing.T) {
	for _, args := range [][]string{{"-app-key=" + testKey}, {testToken}, {"-live=" + testToken}, {"-live", "-verify-report", testToken}} {
		var out, stderr bytes.Buffer
		if run(args, &out, &stderr, noCredentials, nil, cleanRevision) != 2 {
			t.Fatal("invalid arguments were accepted")
		}
		if strings.Contains(out.String()+stderr.String(), testKey) || strings.Contains(out.String()+stderr.String(), testToken) {
			t.Fatal("argument diagnostic echoed credentials")
		}
	}
}
