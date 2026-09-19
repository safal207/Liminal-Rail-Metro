package codingworkflow

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/safal207/Liminal-Rail-Metro/internal/decisionplane"
)

const (
	testRepository = "acme/repo"
	testBaseSHA    = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	testHeadSHA    = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
)

type githubFixture struct {
	issueState  string
	prState     string
	prDraft     bool
	prBaseSHA   string
	files       []GitHubPRFile
	checkRuns   []GitHubCheckRun
	totalChecks int
}

func defaultFixture() githubFixture {
	return githubFixture{
		issueState: "open",
		prState:    "open",
		prBaseSHA:  testBaseSHA,
		files: []GitHubPRFile{
			{SHA: "blob-one", Filename: "internal/codingworkflow/workflow.go", Status: "modified", Additions: 10, Changes: 10},
			{SHA: "blob-two", Filename: "plugins/liminal-rail-metro/README.md", Status: "modified", Additions: 4, Changes: 4},
		},
		checkRuns: []GitHubCheckRun{
			{ID: 101, Name: "coding-unit", HeadSHA: testHeadSHA, Status: "completed", Conclusion: "success", CompletedAt: "2026-09-19T15:00:00Z"},
		},
		totalChecks: 1,
	}
}

func TestStartAndVerifyPublicGitHubWorkflow(t *testing.T) {
	fixture := defaultFixture()
	client, closeServer := fixtureClient(t, fixture)
	defer closeServer()

	signer := testSigner(t, 7)
	contract, err := Start(context.Background(), client, signer, testProvider(), testStartInput())
	if err != nil {
		t.Fatal(err)
	}
	if contract.Protocol != ContractProtocol || contract.ContractHash == "" || contract.Signature == "" {
		t.Fatalf("incomplete signed contract: %#v", contract)
	}
	if err := signer.VerifyContract(contract); err != nil {
		t.Fatal(err)
	}

	verification, err := Verify(context.Background(), client, signer, contract, 17)
	if err != nil {
		t.Fatal(err)
	}
	if verification.Status != StatusVerified || verification.Receipt == nil {
		t.Fatalf("expected VERIFIED, got %#v", verification)
	}
	if verification.Receipt.HeadSHA != testHeadSHA || verification.Receipt.FilesHash == "" || verification.Receipt.ChecksHash == "" {
		t.Fatalf("receipt missing GitHub evidence binding: %#v", verification.Receipt)
	}
	if err := signer.VerifyReceipt(*verification.Receipt); err != nil {
		t.Fatal(err)
	}
}

func TestVerifyHoldsTamperedContract(t *testing.T) {
	client, closeServer := fixtureClient(t, defaultFixture())
	defer closeServer()
	signer := testSigner(t, 7)
	contract, err := Start(context.Background(), client, signer, testProvider(), testStartInput())
	if err != nil {
		t.Fatal(err)
	}
	contract.RequiredChecks = []string{"different-check"}

	verification, err := Verify(context.Background(), client, signer, contract, 17)
	if err != nil {
		t.Fatal(err)
	}
	if verification.Status != StatusHold || verification.ReasonCode != "invalid_contract" {
		t.Fatalf("tampered contract must HOLD: %#v", verification)
	}
}

func TestVerifyRejectsInternallyContradictorySignedContract(t *testing.T) {
	client, closeServer := fixtureClient(t, defaultFixture())
	defer closeServer()
	signer := testSigner(t, 7)
	contract, err := Start(context.Background(), client, signer, testProvider(), testStartInput())
	if err != nil {
		t.Fatal(err)
	}
	contract.Packet.Action.Inputs["repository"] = "other/repo"
	if err := signer.signContract(&contract); err != nil {
		t.Fatal(err)
	}
	if err := signer.VerifyContract(contract); err == nil || !strings.Contains(err.Error(), "packet inputs") {
		t.Fatalf("internally contradictory signed contract must be rejected, got %v", err)
	}
}

func TestVerifyHoldsContractFromDifferentIssuer(t *testing.T) {
	client, closeServer := fixtureClient(t, defaultFixture())
	defer closeServer()
	issuerA := testSigner(t, 7)
	issuerB := testSigner(t, 8)
	contract, err := Start(context.Background(), client, issuerA, testProvider(), testStartInput())
	if err != nil {
		t.Fatal(err)
	}

	verification, err := Verify(context.Background(), client, issuerB, contract, 17)
	if err != nil {
		t.Fatal(err)
	}
	if verification.Status != StatusHold || verification.ReasonCode != "invalid_contract" {
		t.Fatalf("foreign issuer contract must HOLD: %#v", verification)
	}
}

func TestVerifyHoldsStaleBase(t *testing.T) {
	fixture := defaultFixture()
	fixture.prBaseSHA = "cccccccccccccccccccccccccccccccccccccccc"
	client, closeServer := fixtureClient(t, fixture)
	defer closeServer()
	signer := testSigner(t, 7)
	contract, err := Start(context.Background(), client, signer, testProvider(), testStartInput())
	if err != nil {
		t.Fatal(err)
	}

	verification, err := Verify(context.Background(), client, signer, contract, 17)
	if err != nil {
		t.Fatal(err)
	}
	if verification.Status != StatusHold || verification.ReasonCode != "stale_base" {
		t.Fatalf("stale base must HOLD: %#v", verification)
	}
}

func TestVerifyHoldsPathOutsideContract(t *testing.T) {
	fixture := defaultFixture()
	fixture.files = append(fixture.files, GitHubPRFile{SHA: "blob-three", Filename: "README.md", Status: "modified", Changes: 1})
	client, closeServer := fixtureClient(t, fixture)
	defer closeServer()
	signer := testSigner(t, 7)
	contract, err := Start(context.Background(), client, signer, testProvider(), testStartInput())
	if err != nil {
		t.Fatal(err)
	}

	verification, err := Verify(context.Background(), client, signer, contract, 17)
	if err != nil {
		t.Fatal(err)
	}
	if verification.Status != StatusHold || verification.ReasonCode != "path_outside_contract" {
		t.Fatalf("out-of-scope path must HOLD: %#v", verification)
	}
}

func TestVerifyHoldsFailedRequiredCheck(t *testing.T) {
	fixture := defaultFixture()
	fixture.checkRuns[0].Conclusion = "failure"
	client, closeServer := fixtureClient(t, fixture)
	defer closeServer()
	signer := testSigner(t, 7)
	contract, err := Start(context.Background(), client, signer, testProvider(), testStartInput())
	if err != nil {
		t.Fatal(err)
	}

	verification, err := Verify(context.Background(), client, signer, contract, 17)
	if err != nil {
		t.Fatal(err)
	}
	if verification.Status != StatusHold || verification.ReasonCode != "required_check_not_success" {
		t.Fatalf("failed check must HOLD: %#v", verification)
	}
}

func TestVerifyUsesLatestCheckRunForExactHead(t *testing.T) {
	fixture := defaultFixture()
	fixture.checkRuns = []GitHubCheckRun{
		{ID: 101, Name: "coding-unit", HeadSHA: testHeadSHA, Status: "completed", Conclusion: "success"},
		{ID: 102, Name: "coding-unit", HeadSHA: testHeadSHA, Status: "completed", Conclusion: "failure"},
		{ID: 103, Name: "coding-unit", HeadSHA: "dddddddddddddddddddddddddddddddddddddddd", Status: "completed", Conclusion: "success"},
	}
	fixture.totalChecks = len(fixture.checkRuns)
	client, closeServer := fixtureClient(t, fixture)
	defer closeServer()
	signer := testSigner(t, 7)
	contract, err := Start(context.Background(), client, signer, testProvider(), testStartInput())
	if err != nil {
		t.Fatal(err)
	}

	verification, err := Verify(context.Background(), client, signer, contract, 17)
	if err != nil {
		t.Fatal(err)
	}
	if verification.Status != StatusHold || verification.ReasonCode != "required_check_not_success" {
		t.Fatalf("latest exact-head failed rerun must win: %#v", verification)
	}
}

func TestGitHubClientRejectsRedirects(t *testing.T) {
	var targetHits atomic.Int32
	target := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		targetHits.Add(1)
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(`{"number":16,"state":"open","title":"wrong"}`))
	}))
	defer target.Close()

	source := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		http.Redirect(writer, request, target.URL, http.StatusFound)
	}))
	defer source.Close()

	client, err := newGitHubClient(source.URL, time.Second, 4096, true)
	if err != nil {
		t.Fatal(err)
	}
	_, err = client.GetIssue(context.Background(), testRepository, 16)
	var httpError *GitHubHTTPError
	if !errorsAs(err, &httpError) || httpError.StatusCode != http.StatusFound {
		t.Fatalf("expected typed 302 without redirect follow, got %v", err)
	}
	if targetHits.Load() != 0 {
		t.Fatalf("redirect target was contacted %d times", targetHits.Load())
	}
}

func TestGitHubClientRejectsOversizedResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(`{"title":"` + strings.Repeat("x", 200) + `"}`))
	}))
	defer server.Close()

	client, err := newGitHubClient(server.URL, time.Second, 32, true)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.GetIssue(context.Background(), testRepository, 16); err == nil || !strings.Contains(err.Error(), "exceeds") {
		t.Fatalf("expected bounded response rejection, got %v", err)
	}
}

func fixtureClient(t *testing.T, fixture githubFixture) (*GitHubClient, func()) {
	t.Helper()
	if fixture.issueState == "" {
		fixture.issueState = "open"
	}
	if fixture.prState == "" {
		fixture.prState = "open"
	}
	if fixture.prBaseSHA == "" {
		fixture.prBaseSHA = testBaseSHA
	}
	if fixture.totalChecks == 0 {
		fixture.totalChecks = len(fixture.checkRuns)
	}
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		switch request.URL.Path {
		case "/repos/acme/repo/issues/16":
			_ = json.NewEncoder(writer).Encode(map[string]any{
				"number":   16,
				"state":    fixture.issueState,
				"title":    "Bounded issue",
				"body":     "Implement one bounded coding workflow.",
				"html_url": "https://github.com/acme/repo/issues/16",
			})
		case "/repos/acme/repo/pulls/17":
			_ = json.NewEncoder(writer).Encode(map[string]any{
				"number":        17,
				"state":         fixture.prState,
				"draft":         fixture.prDraft,
				"html_url":      "https://github.com/acme/repo/pull/17",
				"changed_files": len(fixture.files),
				"base": map[string]any{
					"sha":  fixture.prBaseSHA,
					"repo": map[string]any{"full_name": testRepository},
				},
				"head": map[string]any{
					"sha":  testHeadSHA,
					"repo": map[string]any{"full_name": testRepository},
				},
			})
		case "/repos/acme/repo/pulls/17/files":
			_ = json.NewEncoder(writer).Encode(fixture.files)
		case "/repos/acme/repo/commits/" + testHeadSHA + "/check-runs":
			_ = json.NewEncoder(writer).Encode(map[string]any{
				"total_count": fixture.totalChecks,
				"check_runs":  fixture.checkRuns,
			})
		default:
			http.NotFound(writer, request)
		}
	}))
	client, err := newGitHubClient(server.URL, 2*time.Second, 1<<20, true)
	if err != nil {
		server.Close()
		t.Fatal(err)
	}
	return client, server.Close
}

func testSigner(t *testing.T, seedByte byte) *Signer {
	t.Helper()
	seed := bytes.Repeat([]byte{seedByte}, ed25519.SeedSize)
	signer, err := NewSigner("liminal-test", ed25519.NewKeyFromSeed(seed))
	if err != nil {
		t.Fatal(err)
	}
	return signer
}

func testProvider() decisionplane.Provider {
	return decisionplane.StaticProvider{
		ID: "coding-test-provider",
		Scores: map[string]float64{
			"code": 99,
			"qa":   1,
		},
	}
}

func testStartInput() StartInput {
	return StartInput{
		WorkflowID:  "workflow-16",
		RequestID:   "request-16",
		ActionID:    "action-16",
		Repository:  testRepository,
		IssueNumber: 16,
		BaseSHA:     testBaseSHA,
		AllowedPathPrefixes: []string{
			"internal/codingworkflow",
			"plugins/liminal-rail-metro",
		},
		RequiredChecks: []string{"coding-unit"},
		Choices: []decisionplane.Choice{
			{ID: "code", Target: "code-agent"},
			{ID: "qa", Target: "qa-agent"},
		},
	}
}

func errorsAs(err error, target any) bool {
	if err == nil {
		return false
	}
	switch typed := target.(type) {
	case **GitHubHTTPError:
		value, ok := err.(*GitHubHTTPError)
		if ok {
			*typed = value
			return true
		}
	}
	return false
}
