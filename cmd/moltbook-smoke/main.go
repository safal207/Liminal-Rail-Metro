// moltbook-smoke rehearses the bounded identity-to-receipt path offline, or
// performs one explicitly requested live identity check with a local fixture.
package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"runtime/debug"
	"strings"
	"time"

	"github.com/safal207/Liminal-Rail-Metro/integrations/moltbook"
	"github.com/safal207/Liminal-Rail-Metro/internal/metro"
)

const (
	smokeReportSchema            = "moltbook.smoke.v0.2"
	identityProvenanceSynthetic = "synthetic_local_rehearsal"
	identityProvenanceMoltbook  = "moltbook_live_verification"
	syntheticIdentitySource      = "local://moltbook-smoke/rehearsal"
)

type revision struct {
	Commit string `json:"commit"`
	Dirty  bool   `json:"dirty"`
}

type report struct {
	Schema               string                              `json:"schema"`
	Mode                 string                              `json:"mode"`
	Status               string                              `json:"status"`
	FailureCode          string                              `json:"failure_code,omitempty"`
	Revision             revision                            `json:"revision"`
	ObservedAt           string                              `json:"observed_at"`
	IdentityStatus       moltbook.IdentityVerificationStatus `json:"identity_status,omitempty"`
	IdentityVerifiedAt   string                              `json:"identity_verified_at,omitempty"`
	VerificationSource   string                              `json:"verification_source,omitempty"`
	IdentityProvenance   string                              `json:"identity_provenance"`
	LiveIdentityVerified bool                                `json:"live_identity_verified"`
	IdentityAttempts     int                                 `json:"identity_attempts"`
	AuthorityCalls       int                                 `json:"authority_calls"`
	EvidenceCalls        int                                 `json:"evidence_calls"`
	EvidenceBackend      string                              `json:"evidence_backend"`
	ReceiptSHA256        string                              `json:"receipt_sha256,omitempty"`
	StationResult        *moltbook.Result                    `json:"station_result,omitempty"`
}

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr, os.Getenv, nil, buildRevision()))
}

func run(args []string, stdout, stderr io.Writer, getenv func(string) string, client *http.Client, rev revision) int {
	flags := flag.NewFlagSet("moltbook-smoke", flag.ContinueOnError)
	// Never echo invalid flags or positional arguments: a caller may have
	// accidentally supplied a credential instead of an environment variable.
	flags.SetOutput(io.Discard)
	live := flags.Bool("live", false, "perform one live identity verification")
	verifyPath := flags.String("verify-report", "", "verify a saved receipt without network access")
	if err := flags.Parse(args); err != nil {
		if err == flag.ErrHelp {
			fmt.Fprintln(stdout, "usage: moltbook-smoke [-live | -verify-report report.json]\nDefault: offline rehearsal. Live mode reads MOLTBOOK_APP_KEY and MOLTBOOK_IDENTITY_TOKEN from the environment.")
			return 0
		}
		fmt.Fprintln(stderr, "invalid arguments; credentials must only be supplied through environment variables")
		return 2
	}
	if flags.NArg() != 0 || (*live && *verifyPath != "") {
		fmt.Fprintln(stderr, "unexpected arguments or conflicting modes")
		return 2
	}
	if *verifyPath != "" {
		return verifyReport(*verifyPath, stdout, stderr)
	}

	r := report{
		Schema: smokeReportSchema, Mode: "offline_rehearsal", Status: "FAIL",
		Revision: rev, ObservedAt: metro.NowISO(),
		IdentityProvenance: identityProvenanceSynthetic,
		VerificationSource: syntheticIdentitySource,
		EvidenceBackend: "metro.Verify/local-control-fixture",
	}
	fail := func(status, code string) int {
		r.Status, r.FailureCode = status, code
		return writeReport(stdout, stderr, r)
	}
	appKey, token := "moltdev_offline_rehearsal", "offline-rehearsal-token"
	if *live {
		r.Mode = "live_identity_local_fixture"
		r.IdentityProvenance = identityProvenanceMoltbook
		r.VerificationSource = ""
		if !validCommit(rev.Commit) || rev.Dirty {
			return fail("BLOCKED", "clean_recorded_build_required")
		}
		appKey, token = getenv("MOLTBOOK_APP_KEY"), getenv("MOLTBOOK_IDENTITY_TOKEN")
		if strings.TrimSpace(appKey) == "" || strings.TrimSpace(token) == "" {
			return fail("BLOCKED", "developer_credentials_required")
		}
	} else {
		// Offline mode never reads credentials or delegates to any network client.
		client = &http.Client{Transport: rehearsalTransport{}}
	}
	verifier, err := moltbook.NewMoltbookIdentityVerifier(appKey, client)
	if err != nil {
		return fail("BLOCKED", "app_key_configuration_invalid")
	}
	identity := &recordingIdentity{verifier: verifier}
	fixture, err := newControlFixture()
	if err != nil {
		return fail("FAIL", "fixture_setup_failed")
	}
	evidence := &fixtureVerifier{proof: fixture}
	authority, err := newSmokeAuthority()
	if err != nil {
		return fail("FAIL", "authority_setup_failed")
	}
	station, err := moltbook.NewStationForTarget(identity, evidence, authority, smokeTarget)
	if err != nil {
		return fail("FAIL", "station_setup_failed")
	}
	var nonce [16]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		return fail("FAIL", "action_id_unavailable")
	}
	result, err := station.Execute(moltbook.Request{
		IdentityToken: token, ActionID: "molt-smoke-" + hex.EncodeToString(nonce[:]),
		Intent: moltbook.IntentVerifyEvidence, Target: smokeTarget,
		EvidenceRef: controlFixtureRef, ExternalEffects: false,
	})
	r.IdentityAttempts, r.AuthorityCalls, r.EvidenceCalls = identity.calls, authority.calls, evidence.calls
	if *live {
		r.IdentityStatus = identity.result.Status
		r.IdentityVerifiedAt = identity.result.VerifiedAt
		r.VerificationSource = identity.result.VerificationSource
		r.LiveIdentityVerified = r.IdentityStatus == moltbook.IdentityStatusVerified
	}
	if err != nil {
		// Neither the provider body nor error strings are part of the report.
		return fail("FAIL", "identity_to_receipt_failed")
	}
	if r.IdentityAttempts != 1 || r.AuthorityCalls != 1 || r.EvidenceCalls != 1 || moltbook.VerifyResult(result) != nil {
		return fail("FAIL", "receipt_or_call_count_invalid")
	}
	r.ReceiptSHA256, err = metro.HashJSON(result.Receipt)
	if err != nil {
		return fail("FAIL", "receipt_hash_failed")
	}
	r.Status, r.StationResult = "PASS", &result
	return writeReport(stdout, stderr, r)
}

func writeReport(stdout, stderr io.Writer, r report) int {
	encoder := json.NewEncoder(stdout)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(r); err != nil {
		fmt.Fprintln(stderr, "could not write smoke report; do not automatically repeat a live run")
		return 1
	}
	if r.Status != "PASS" {
		return 1
	}
	return 0
}

type recordingIdentity struct {
	verifier *moltbook.MoltbookIdentityVerifier
	result   moltbook.IdentityVerificationResult
	calls    int
}

func (v *recordingIdentity) VerifyIdentity(token string) (moltbook.VerifiedIdentity, error) {
	v.calls++
	result, err := v.verifier.VerifyIdentityResultContext(context.Background(), token)
	v.result = result
	if err != nil {
		return moltbook.VerifiedIdentity{}, err
	}
	return moltbook.VerifiedIdentity{AgentID: result.AgentID, Verified: result.Status == moltbook.IdentityStatusVerified}, nil
}

type rehearsalTransport struct{}

func (rehearsalTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	return &http.Response{
		StatusCode: http.StatusOK, Request: req, Header: make(http.Header),
		Body: io.NopCloser(strings.NewReader(`{"success":true,"valid":true,"agent":{"id":"offline-rehearsal-agent"}}`)),
	}, nil
}

func buildRevision() revision {
	rev := revision{Commit: "unknown", Dirty: true}
	if info, ok := debug.ReadBuildInfo(); ok {
		for _, setting := range info.Settings {
			switch setting.Key {
			case "vcs.revision":
				rev.Commit = setting.Value
			case "vcs.modified":
				rev.Dirty = setting.Value != "false"
			}
		}
	}
	return rev
}

func validCommit(commit string) bool {
	decoded, err := hex.DecodeString(commit)
	return err == nil && len(decoded) == 20
}

func verifyReport(path string, stdout, stderr io.Writer) int {
	fail := func() int {
		fmt.Fprintln(stderr, "saved receipt verification failed")
		return 1
	}
	f, err := os.Open(path)
	if err != nil {
		return fail()
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, 1024*1024+1))
	if err != nil || len(data) > 1024*1024 {
		return fail()
	}
	var r report
	if json.Unmarshal(data, &r) != nil || r.Schema != smokeReportSchema || r.Status != "PASS" || r.StationResult == nil {
		return fail()
	}
	// These are consistency checks, not authentication of the report's author.
	if r.FailureCode != "" ||
		r.IdentityAttempts != 1 || r.AuthorityCalls != 1 || r.EvidenceCalls != 1 ||
		r.EvidenceBackend != "metro.Verify/local-control-fixture" {
		return fail()
	}
	switch r.Mode {
	case "offline_rehearsal":
		if r.LiveIdentityVerified ||
			r.IdentityProvenance != identityProvenanceSynthetic ||
			r.IdentityStatus != "" ||
			r.IdentityVerifiedAt != "" ||
			r.VerificationSource != syntheticIdentitySource {
			return fail()
		}
	case "live_identity_local_fixture":
		if !r.LiveIdentityVerified ||
			r.IdentityProvenance != identityProvenanceMoltbook ||
			r.IdentityStatus != moltbook.IdentityStatusVerified ||
			r.VerificationSource != "https://www.moltbook.com/api/v1/agents/verify-identity" ||
			!validCommit(r.Revision.Commit) || r.Revision.Dirty {
			return fail()
		}
		if _, err := time.Parse(time.RFC3339Nano, r.IdentityVerifiedAt); err != nil {
			return fail()
		}
	default:
		return fail()
	}
	if moltbook.VerifyResult(*r.StationResult) != nil {
		return fail()
	}
	packet := r.StationResult.Packet
	if !allowedSmokePacket(packet, r.StationResult.Route.SelectedTarget) ||
		r.StationResult.Route.SelectedTarget != smokeTarget ||
		r.StationResult.Receipt.ExecutorID != smokeTarget ||
		r.StationResult.Verification["backend"] != r.EvidenceBackend ||
		r.StationResult.Authority.ProviderID != "moltbook-smoke-local-authority" ||
		r.StationResult.Route.PolicyRef != "policy://moltbook/smoke/read-only-control-fixture-v1" {
		return fail()
	}
	hash, err := metro.HashJSON(r.StationResult.Receipt)
	if err != nil || hash != r.ReceiptSHA256 {
		return fail()
	}
	fixture, err := newControlFixture()
	if err != nil {
		return fail()
	}
	fixtureHash, err := metro.HashJSON(fixture)
	if err != nil || r.StationResult.Verification["evidence_sha256"] != fixtureHash || r.StationResult.Verification["verdict"] != "VERIFIED" {
		return fail()
	}
	fmt.Fprintln(stdout, "Receipt bindings and local control-fixture hash verified. Live Moltbook identity is not independently reverified by this offline check.")
	return 0
}
