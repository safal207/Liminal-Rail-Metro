package main

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// The fixture speaks HTTP to the checker. It is deliberately separate from
// the demo server so response tampering is exercised at the actual boundary.
type publisherFixture struct {
	mapDoc           siteMap
	runDoc           runResult
	files            map[string][]byte
	passports        map[string]manifest
	mapCalls         int
	manifestGets     int
	fullGets         int
	runPosts         int
	mutateMap        func([]byte, int) []byte
	mutatePass       func([]byte, string) []byte
	mutateFull       func([]byte, string) []byte
	mutateRun        func([]byte) []byte
	supportChallenge bool
	challenges       []string
	runContentLength int
	fullStatus       int
	fullAttempts     int
}

type brokenOutputWriter struct{ failure error }

func (w brokenOutputWriter) Write(p []byte) (int, error) {
	if w.failure != nil {
		return 0, w.failure
	}
	return len(p) / 2, nil // A short write without an error must still fail.
}

func fixtureHash(v any) string {
	b, _ := json.Marshal(v)
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

func fixtureByteHash(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

func newFixture() *publisherFixture {
	files := map[string][]byte{"guide": []byte("First read the guide.\n"), "spec": []byte("Then read the specification.\n")}
	resources := []resource{}
	passports := map[string]manifest{}
	for _, id := range []string{"guide", "spec"} {
		h := fixtureByteHash(files[id])
		resources = append(resources, resource{ID: id, Label: id, Manifest: "/api/site/assets/" + id, SHA256: h, Size: len(files[id])})
		passports[id] = manifest{Protocol: assetProtocol, ID: id, MediaType: "application/octet-stream", Size: len(files[id]), SHA256: h, Version: "sha256:" + h, ChunkSize: assetChunkBytes, Chunks: []string{h}, Href: "/api/site/assets/" + id + "/full"}
	}
	m := siteMap{Protocol: siteProtocol, Start: "entry", Nodes: []node{{ID: "entry", Label: "Entry"}, {ID: "guide", Label: "Guide", ResourceID: "guide"}, {ID: "spec", Label: "Spec", ResourceID: "spec"}}, Edges: []edge{{ID: "first", From: "entry", To: "guide", Action: "open_resource", Scope: scope, Resource: "guide"}, {ID: "second", From: "guide", To: "spec", Action: "open_resource", Scope: scope, Resource: "spec"}}, Resources: resources}
	mapHash := fixtureHash(m)
	run := runResult{Status: "CONFIRMED_LOCAL", Reason: "read-only route completed with verified bytes and local receipts", Mode: "graph", Target: "spec", Map: m, MapHash: mapHash, PlannedSteps: 2, FreshReads: 2, MemoryEntries: 1, Learned: true, Events: []event{}, Resources: []runResource{}}
	for i, e := range m.Edges {
		r := resources[i]
		id := strings.Repeat(string(rune('a'+i)), 32)
		createdAt := fmt.Sprintf("2026-09-24T10:00:%02dZ", i*2)
		completedAt := fmt.Sprintf("2026-09-24T10:00:%02dZ", i*2+1)
		inputs := map[string]any{"edge": e.ID, "from": e.From, "map_hash": mapHash, "resource_id": r.ID, "resource_sha256": r.SHA256, "scope": scope, "to": e.To}
		result := map[string]any{"map_hash": mapHash, "resource_id": r.ID, "sha256": r.SHA256, "size_bytes": float64(r.Size), "state": e.To}
		p := packet{Protocol: "metro.packet.v0.1", ActionID: id, SourceAgent: "metro-web-demo", CreatedAt: createdAt, Goal: "open resource spec", Action: action{Kind: "open_resource", Inputs: inputs}, AllowedTargets: []string{"metro-site-reader"}, Constraints: constraints{TimeoutMS: 10000}}
		rt := route{Protocol: "metro.route.v0.1", RouteID: "route-" + id, ActionID: id, RouterID: "metro-web-demo", DecisionMode: "deterministic", SelectedTarget: "metro-site-reader", Candidates: []candidate{{Target: "metro-site-reader", Score: 1}}, Confidence: 1, PolicyRef: "policy://demo/route-by-action-kind", DecidedAt: createdAt}
		rc := &receipt{Protocol: "metro.receipt.v0.1", ReceiptID: "receipt-" + id, ActionID: id, RouteID: rt.RouteID, ExecutorID: "metro-site-reader", Status: "SUCCEEDED", HashAlgorithm: "sha256", InputHash: fixtureHash(inputs), ResultHash: fixtureHash(result), ResultRef: "sha256:" + r.SHA256, StartedAt: createdAt, CompletedAt: completedAt}
		run.Events = append(run.Events, event{Edge: e, Status: "CONFIRMED_LOCAL", Packet: p, Route: rt, Receipt: rc, Result: result})
		run.Resources = append(run.Resources, runResource{ID: r.ID, SHA256: r.SHA256, Size: r.Size})
	}
	return &publisherFixture{mapDoc: m, runDoc: run, files: files, passports: passports, supportChallenge: true}
}

// This fixture hashes a newly bound transcript from its own saved content.
// It demonstrates why a challenge alone cannot prove a fresh file read.
func boundFixtureRun(base runResult, challenge string) runResult {
	run := base
	run.ClientChallenge = challenge
	run.Events = make([]event, len(base.Events))
	for i, ev := range base.Events {
		beforeInputs := fixtureHash(ev.Packet.Action.Inputs)
		beforeResult := fixtureHash(ev.Result)
		inputs := make(map[string]any, len(ev.Packet.Action.Inputs)+1)
		for key, value := range ev.Packet.Action.Inputs {
			inputs[key] = value
		}
		inputs["client_challenge"] = challenge
		result := make(map[string]any, len(ev.Result)+1)
		for key, value := range ev.Result {
			result[key] = value
		}
		result["client_challenge"] = challenge
		ev.Packet.Action.Inputs = inputs
		ev.Result = result
		rc := *ev.Receipt
		if rc.InputHash == beforeInputs {
			rc.InputHash = fixtureHash(inputs)
		}
		if rc.ResultHash == beforeResult {
			rc.ResultHash = fixtureHash(result)
		}
		ev.Receipt = &rc
		run.Events[i] = ev
	}
	return run
}

func (f *publisherFixture) serve(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path == "/api/site" && r.Method == http.MethodGet {
		f.mapCalls++
		raw, _ := json.Marshal(f.mapDoc)
		if f.mutateMap != nil {
			raw = f.mutateMap(raw, f.mapCalls)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(raw)
		return
	}
	if r.URL.Path == "/api/site/run" && r.Method == http.MethodPost {
		f.runPosts++
		var request struct {
			Target          string `json:"target"`
			ClientChallenge string `json:"client_challenge"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil || request.Target != "spec" {
			http.Error(w, "invalid fixture request", http.StatusBadRequest)
			return
		}
		f.challenges = append(f.challenges, request.ClientChallenge)
		run := f.runDoc
		if f.supportChallenge {
			run = boundFixtureRun(run, request.ClientChallenge)
		}
		raw, _ := json.Marshal(run)
		if f.mutateRun != nil {
			raw = f.mutateRun(raw)
		}
		w.Header().Set("Content-Type", "application/json")
		if f.runContentLength > 0 {
			w.Header().Set("Content-Length", fmt.Sprint(f.runContentLength))
		}
		_, _ = w.Write(raw)
		return
	}
	parts := strings.Split(r.URL.Path, "/")
	if len(parts) >= 5 && parts[1] == "api" && parts[2] == "site" && parts[3] == "assets" {
		id := parts[4]
		passport, ok := f.passports[id]
		if !ok {
			http.NotFound(w, r)
			return
		}
		if len(parts) == 5 {
			f.manifestGets++
			raw, _ := json.Marshal(passport)
			if f.mutatePass != nil {
				raw = f.mutatePass(raw, id)
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write(raw)
			return
		}
		if len(parts) == 6 && parts[5] == "full" {
			f.fullGets++
			f.fullAttempts++
			if f.fullStatus != 0 {
				w.WriteHeader(f.fullStatus)
				return
			}
			if r.URL.Query().Get("sha256") != passport.SHA256 {
				w.WriteHeader(http.StatusConflict)
				return
			}
			body := f.files[id]
			if f.mutateFull != nil {
				body = f.mutateFull(body, id)
			}
			w.Header().Set("Content-Type", "application/octet-stream")
			w.Header().Set("Content-Disposition", `attachment; filename="metro-site-asset.bin"`)
			w.Header().Set("X-Metro-Asset-Whole-Verified", "true")
			_, _ = w.Write(body)
			return
		}
	}
	http.NotFound(w, r)
}

func runFixture(t *testing.T, f *publisherFixture) (checkResult, error) {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(f.serve))
	defer server.Close()
	c, err := newChecker(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return c.check(ctx, "spec")
}

func alterBoundResponse(f *publisherFixture, change func(*runResult)) {
	f.mutateRun = func(raw []byte) []byte {
		var run runResult
		if err := json.Unmarshal(raw, &run); err != nil {
			panic(err)
		}
		change(&run)
		out, err := json.Marshal(run)
		if err != nil {
			panic(err)
		}
		return out
	}
}

func TestCheckerVerifiesHTTPRouteAndReceipts(t *testing.T) {
	f := newFixture()
	got, err := runFixture(t, f)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != "VERIFIED_BYTES_AND_CHALLENGE_BOUND_TRANSCRIPT" || !got.ClientChallengeBound || got.RunFreshnessVerified || got.PublisherIdentityVerified || got.MapHash != f.runDoc.MapHash || len(got.Route) != 2 || got.Route[1].ResourceID != "spec" || f.mapCalls != 2 || f.manifestGets != 2 || f.fullGets != 2 || f.runPosts != 1 || len(f.challenges) != 1 || !validHash(f.challenges[0]) {
		t.Fatalf("unexpected verification or HTTP sequence: %+v, map=%d manifests=%d full=%d runs=%d", got, f.mapCalls, f.manifestGets, f.fullGets, f.runPosts)
	}
}

func TestCheckerRejectsLegacyAndStaticReplay(t *testing.T) {
	t.Run("old publisher", func(t *testing.T) {
		f := newFixture()
		f.supportChallenge = false
		_, err := runFixture(t, f)
		if err == nil || failureStatus(err) != "REJECTED" || f.runPosts != 1 || len(f.challenges) != 1 || !validHash(f.challenges[0]) {
			t.Fatalf("old publisher should be an explicit incompatibility: error=%v challenges=%v posts=%d", err, f.challenges, f.runPosts)
		}
	})
	t.Run("captured response", func(t *testing.T) {
		f := newFixture()
		var captured []byte
		f.mutateRun = func(raw []byte) []byte {
			if captured == nil {
				captured = append([]byte(nil), raw...)
			}
			return captured
		}
		server := httptest.NewServer(http.HandlerFunc(f.serve))
		defer server.Close()
		c, err := newChecker(server.URL)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := c.check(context.Background(), "spec"); err != nil {
			t.Fatalf("first bound response: %v", err)
		}
		_, err = c.check(context.Background(), "spec")
		if err == nil || failureStatus(err) != "REJECTED" || !strings.Contains(err.Error(), "challenge") || len(f.challenges) != 2 || f.challenges[0] == f.challenges[1] {
			t.Fatalf("literal replay was accepted or misclassified: error=%v challenges=%v", err, f.challenges)
		}
	})
}

func TestCheckerRejectsChallengeTamperingAtEveryLayer(t *testing.T) {
	tests := []struct {
		name   string
		change func(*runResult)
	}{
		{"top-level mismatch", func(run *runResult) { run.ClientChallenge = strings.Repeat("0", 64) }},
		{"top-level missing", func(run *runResult) { run.ClientChallenge = "" }},
		{"unknown with mismatched challenge", func(run *runResult) {
			run.ClientChallenge = strings.Repeat("0", 64)
			run.Status = "UNKNOWN"
		}},
		{"first packet missing", func(run *runResult) { delete(run.Events[0].Packet.Action.Inputs, "client_challenge") }},
		{"second packet mismatched", func(run *runResult) { run.Events[1].Packet.Action.Inputs["client_challenge"] = strings.Repeat("0", 64) }},
		{"first result missing", func(run *runResult) { delete(run.Events[0].Result, "client_challenge") }},
		{"second result mismatched", func(run *runResult) { run.Events[1].Result["client_challenge"] = strings.Repeat("0", 64) }},
		{"rehashed wrong packet challenge", func(run *runResult) {
			run.Events[0].Packet.Action.Inputs["client_challenge"] = strings.Repeat("0", 64)
			run.Events[0].Receipt.InputHash = fixtureHash(run.Events[0].Packet.Action.Inputs)
		}},
		{"rehashed wrong result challenge", func(run *runResult) {
			run.Events[0].Result["client_challenge"] = strings.Repeat("0", 64)
			run.Events[0].Receipt.ResultHash = fixtureHash(run.Events[0].Result)
		}},
		{"input hash of pre-challenge packet", func(run *runResult) {
			run.Events[0].Receipt.InputHash = fakedPreChallengeHash(run.Events[0].Packet.Action.Inputs)
		}},
		{"result hash of pre-challenge result", func(run *runResult) { run.Events[0].Receipt.ResultHash = fakedPreChallengeHash(run.Events[0].Result) }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newFixture()
			alterBoundResponse(f, tt.change)
			_, err := runFixture(t, f)
			if err == nil || failureStatus(err) != "REJECTED" || f.runPosts != 1 {
				t.Fatalf("accepted corrupted challenge transcript: error=%v posts=%d", err, f.runPosts)
			}
		})
	}
}

func fakedPreChallengeHash(bound map[string]any) string {
	old := make(map[string]any, len(bound)-1)
	for key, value := range bound {
		if key != "client_challenge" {
			old[key] = value
		}
	}
	return fixtureHash(old)
}

func TestCheckerRandomnessFailureIsUnknownBeforePost(t *testing.T) {
	f := newFixture()
	server := httptest.NewServer(http.HandlerFunc(f.serve))
	defer server.Close()
	c, err := newChecker(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	c.challengeReader = strings.NewReader("")
	_, err = c.check(context.Background(), "spec")
	if err == nil || failureStatus(err) != "UNKNOWN" || f.fullGets != 2 || f.runPosts != 0 {
		t.Fatalf("randomness failure should stop before run POST: error=%v full=%d posts=%d", err, f.fullGets, f.runPosts)
	}
}

func TestCheckerIncompleteRunBodyIsUnknown(t *testing.T) {
	f := newFixture()
	f.runContentLength = maxRunBytes
	_, err := runFixture(t, f)
	if err == nil || failureStatus(err) != "UNKNOWN" || f.runPosts != 1 {
		t.Fatalf("incomplete run transfer must be UNKNOWN: error=%v posts=%d", err, f.runPosts)
	}
}

func TestCheckerCompleteUnknownRunWithoutReceiptIsUnknown(t *testing.T) {
	f := newFixture()
	alterBoundResponse(f, func(run *runResult) {
		run.Status = "UNKNOWN"
		run.Reason = "resource read incomplete; no retry"
		run.FreshReads = 0
		run.Learned = false
		run.Events = run.Events[:1]
		run.Events[0].Status = "UNKNOWN"
		run.Events[0].Receipt = nil
		run.Events[0].Result = map[string]any{"client_challenge": run.ClientChallenge}
		run.Resources = []runResource{}
	})
	mutate := f.mutateRun
	var receiptAbsent bool
	f.mutateRun = func(raw []byte) []byte {
		out := mutate(raw)
		receiptAbsent = !bytes.Contains(out, []byte(`"receipt":`))
		return out
	}
	_, err := runFixture(t, f)
	if err == nil || failureStatus(err) != "UNKNOWN" || !receiptAbsent || f.runPosts != 1 {
		t.Fatalf("complete uncertain run without receipt misclassified: error=%v receiptAbsent=%t posts=%d", err, receiptAbsent, f.runPosts)
	}
}

func TestCheckerSuccessfulRunStillRequiresReceiptAndResult(t *testing.T) {
	for _, tt := range []struct {
		name   string
		change func(*runResult)
	}{
		{"missing receipt", func(run *runResult) { run.Events[0].Receipt = nil }},
		{"missing result", func(run *runResult) { run.Events[0].Result = nil }},
	} {
		t.Run(tt.name, func(t *testing.T) {
			f := newFixture()
			alterBoundResponse(f, tt.change)
			_, err := runFixture(t, f)
			if err == nil || failureStatus(err) != "REJECTED" {
				t.Fatalf("accepted successful event without %s: %v", tt.name, err)
			}
		})
	}
}

func TestCheckerChecksMemoryRouteWithChallenge(t *testing.T) {
	f := newFixture()
	f.runDoc.Mode = "memory_revalidated"
	got, err := runFixture(t, f)
	if err != nil || !got.ClientChallengeBound || got.RunFreshnessVerified || got.PublisherIdentityVerified || len(got.Route) != 2 {
		t.Fatalf("memory route challenge verification failed: result=%+v error=%v", got, err)
	}
}

func TestCheckerRejectsHostileHTTPResponses(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*publisherFixture)
	}{
		{"duplicate map member", func(f *publisherFixture) {
			f.mutateMap = func(b []byte, _ int) []byte {
				return []byte(strings.Replace(string(b), `"protocol":`, `"protocol":"forged","protocol":`, 1))
			}
		}},
		{"write edge", func(f *publisherFixture) { f.mapDoc.Edges[0].SideEffect = true }},
		{"stale pin", func(f *publisherFixture) { f.fullStatus = http.StatusConflict }},
		{"changed bytes", func(f *publisherFixture) {
			f.mutateFull = func(b []byte, _ string) []byte { altered := append([]byte(nil), b...); altered[0] ^= 1; return altered }
		}},
		{"wrong chunk", func(f *publisherFixture) {
			f.mutatePass = func(b []byte, id string) []byte {
				if id != "guide" {
					return b
				}
				p := f.passports[id]
				p.Chunks[0] = strings.Repeat("0", 64)
				raw, _ := json.Marshal(p)
				return raw
			}
		}},
		{"receipt failure status", func(f *publisherFixture) { f.runDoc.Events[0].Receipt.Status = "FAILED" }},
		{"receipt digest tamper", func(f *publisherFixture) { f.runDoc.Events[0].Receipt.ResultHash = strings.Repeat("0", 64) }},
		{"receipt ref tamper", func(f *publisherFixture) { f.runDoc.Events[0].Receipt.ResultRef = "sha256:" + strings.Repeat("0", 64) }},
		{"packet scope tamper", func(f *publisherFixture) { f.runDoc.Events[0].Packet.Action.Inputs["scope"] = "resource.write" }},
		{"route target tamper", func(f *publisherFixture) { f.runDoc.Events[0].Route.SelectedTarget = "other" }},
		{"run map tamper", func(f *publisherFixture) {
			f.runDoc.Map.Resources = append([]resource(nil), f.runDoc.Map.Resources...)
			f.runDoc.Map.Resources[0].SHA256 = strings.Repeat("0", 64)
		}},
		{"final map race", func(f *publisherFixture) {
			f.mutateMap = func(b []byte, count int) []byte {
				if count != 2 {
					return b
				}
				altered := f.mapDoc
				altered.Resources = append([]resource(nil), altered.Resources...)
				altered.Resources[0].Label = "changed"
				raw, _ := json.Marshal(altered)
				return raw
			}
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newFixture()
			tt.mutate(f)
			_, err := runFixture(t, f)
			if err == nil {
				t.Fatal("accepted hostile publisher response")
			}
			if tt.name == "stale pin" && (f.fullAttempts != 1 || f.runPosts != 0) {
				t.Fatalf("HTTP 409 caused retry or run: full attempts=%d, runs=%d", f.fullAttempts, f.runPosts)
			}
			if strings.HasPrefix(tt.name, "receipt ") && (f.fullGets != 2 || f.runPosts != 1) {
				t.Fatalf("receipt case rejected before receipt inspection: full=%d, runs=%d", f.fullGets, f.runPosts)
			}
		})
	}
}

func TestCheckerRejectsNonCausalRunTimes(t *testing.T) {
	for _, tt := range []struct {
		name   string
		mutate func(*runResult)
	}{
		{"route before packet", func(run *runResult) { run.Events[0].Packet.CreatedAt = "2026-09-24T10:00:01Z" }},
		{"receipt before route", func(run *runResult) { run.Events[0].Receipt.StartedAt = "2026-09-24T09:59:59Z" }},
		{"receipt completion before start", func(run *runResult) { run.Events[0].Receipt.CompletedAt = "2026-09-24T09:59:59Z" }},
		{"next packet before prior receipt", func(run *runResult) { run.Events[1].Packet.CreatedAt = "2026-09-24T10:00:00Z" }},
	} {
		t.Run(tt.name, func(t *testing.T) {
			f := newFixture()
			tt.mutate(&f.runDoc)
			_, err := runFixture(t, f)
			if err == nil || !strings.Contains(err.Error(), "causally ordered") {
				t.Fatalf("accepted impossible event sequence or rejected for the wrong reason: %v", err)
			}
		})
	}
}

func TestCheckerOriginPolicy(t *testing.T) {
	for _, raw := range []string{"http://example.com", "http://localhost:8080", "https://user:pass@example.com", "https://example.com/path", "https://example.com?x=1", "https://example.com#frag", "file:///tmp/x", "http://127.0.0.1:0"} {
		t.Run(raw, func(t *testing.T) {
			if _, err := newChecker(raw); err == nil {
				t.Fatalf("accepted %q", raw)
			}
		})
	}
	for _, raw := range []string{"https://example.com", "http://127.0.0.1:8080", "http://[::1]:8080"} {
		t.Run(fmt.Sprintf("accept %s", raw), func(t *testing.T) {
			if _, err := newChecker(raw); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestCheckerUsesCallerDeadlineForBodyTransfer(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte("{"))
		w.(http.Flusher).Flush()
		<-r.Context().Done()
	}))
	defer server.Close()
	c, err := newChecker(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	if c.client.Timeout != 0 {
		t.Fatalf("per-request timeout %s can truncate a valid bounded asset before the caller deadline", c.client.Timeout)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	_, err = c.mapAt(ctx)
	if err == nil || failureStatus(err) != "UNKNOWN" {
		t.Fatalf("incomplete body must be bounded by caller context and remain uncertain: %v", err)
	}
}

// This integration test launches the actual demo publisher in a separate
// process, rather than using the fixture server above. It verifies that the
// independent checker accepts the real wire format and real local receipts.
func TestCheckerAgainstRealPublisher(t *testing.T) {
	if testing.Short() {
		t.Skip("external publisher integration")
	}
	goBin := filepath.Join(runtime.GOROOT(), "bin", "go")
	if runtime.GOOS == "windows" {
		goBin += ".exe"
	}
	if _, err := os.Stat(goBin); err != nil {
		t.Skipf("Go tool unavailable: %v", err)
	}
	temp, err := os.MkdirTemp(".", ".metro-web-check-test-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(temp)
	bin := filepath.Join(temp, "metro-web-publisher")
	if runtime.GOOS == "windows" {
		bin += ".exe"
	}
	buildCtx, buildCancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer buildCancel()
	build := exec.CommandContext(buildCtx, goBin, "build", "-o", bin, "../metro-web-demo")
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build actual publisher: %v\n%s", err, output)
	}
	config, err := filepath.Abs("../../examples/metro-web/site-007/site.json")
	if err != nil {
		t.Fatal(err)
	}
	serverCtx, stopServer := context.WithCancel(context.Background())
	defer stopServer()
	server := exec.CommandContext(serverCtx, bin, "-addr", "127.0.0.1:0", "-site-config", config)
	server.Stdout = io.Discard
	stderr, err := server.StderrPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := server.Start(); err != nil {
		t.Fatal(err)
	}
	originCh := make(chan string, 1)
	stderrDone := make(chan struct{})
	var launchLog strings.Builder
	go func() {
		defer close(stderrDone)
		defer close(originCh)
		scanner := bufio.NewScanner(stderr)
		announced := false
		for scanner.Scan() {
			const banner = "Metro Web local demo: "
			line := scanner.Text()
			launchLog.WriteString(line + "\n")
			if at := strings.Index(line, banner); at >= 0 && !announced {
				fields := strings.Fields(line[at+len(banner):])
				if len(fields) > 0 {
					originCh <- fields[0]
					announced = true
				}
			}
		}
	}()
	defer func() { stopServer(); <-stderrDone; _ = server.Wait() }()
	var origin string
	select {
	case origin = <-originCh:
		if !strings.HasPrefix(origin, "http://127.0.0.1:") {
			t.Fatalf("publisher did not announce a loopback origin: %q (stderr: %s)", origin, launchLog.String())
		}
	case <-time.After(20 * time.Second):
		t.Fatal("publisher did not announce its bound port")
	}
	c, err := newChecker(origin)
	if err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(20 * time.Second)
	for {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		_, err := c.mapAt(ctx)
		cancel()
		if err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("publisher did not start at %s: %v", origin, err)
		}
		time.Sleep(100 * time.Millisecond)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	got, err := c.check(ctx, "spec")
	if err != nil {
		t.Fatalf("real publisher verification at %s: %v", origin, err)
	}
	if got.Status != "VERIFIED_BYTES_AND_CHALLENGE_BOUND_TRANSCRIPT" || !got.ClientChallengeBound || got.RunFreshnessVerified || got.PublisherIdentityVerified || len(got.Route) != 2 || got.Route[0].ResourceID != "guide" || got.Route[1].ResourceID != "spec" {
		t.Fatalf("unexpected real publisher route: %+v", got)
	}
	// The same publisher now has a learned path. Its second run uses the
	// memory_revalidated mode while the checker still requires a new challenge.
	got, err = c.check(ctx, "spec")
	if err != nil || got.Status != "VERIFIED_BYTES_AND_CHALLENGE_BOUND_TRANSCRIPT" || !got.ClientChallengeBound || got.RunFreshnessVerified || got.PublisherIdentityVerified || len(got.Route) != 2 {
		t.Fatalf("real publisher learned-route verification: result=%+v error=%v", got, err)
	}
}

func TestCheckerUsesDistinctChallengesButDoesNotClaimFreshReads(t *testing.T) {
	f := newFixture()
	server := httptest.NewServer(http.HandlerFunc(f.serve))
	defer server.Close()
	c, err := newChecker(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	first, err := c.check(context.Background(), "spec")
	if err != nil {
		t.Fatal(err)
	}
	second, err := c.check(context.Background(), "spec")
	if err != nil {
		t.Fatal(err)
	}
	if first.Status != "VERIFIED_BYTES_AND_CHALLENGE_BOUND_TRANSCRIPT" || second.Status != first.Status || !first.ClientChallengeBound || !second.ClientChallengeBound || first.RunFreshnessVerified || second.RunFreshnessVerified || first.PublisherIdentityVerified || second.PublisherIdentityVerified || len(f.challenges) != 2 || f.challenges[0] == f.challenges[1] || f.runPosts != 2 {
		t.Fatalf("fresh challenges must not imply fresh reads: first=%+v second=%+v challenges=%v posts=%d", first, second, f.challenges, f.runPosts)
	}
}

func TestCLIReportsIncompleteObservationsAsUnknown(t *testing.T) {
	tests := []struct {
		name    string
		handler http.HandlerFunc
		delay   time.Duration
	}{
		{"HTTP 503", func(w http.ResponseWriter, _ *http.Request) {
			http.Error(w, "temporarily unavailable", http.StatusServiceUnavailable)
		}, 0},
		{"truncated response body", func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.Header().Set("Content-Length", "200")
			_, _ = w.Write([]byte(`{"protocol":"metro.web.site.v0.1"}`))
		}, 0},
		{"context timeout", func(w http.ResponseWriter, r *http.Request) { <-r.Context().Done() }, 15 * time.Millisecond},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(tt.handler)
			defer server.Close()
			parent := context.Background()
			if tt.delay != 0 {
				var cancel context.CancelFunc
				parent, cancel = context.WithTimeout(parent, tt.delay)
				defer cancel()
			}
			var out, errorsOut bytes.Buffer
			code := runCLI(parent, []string{"-origin", server.URL, "-target", "spec"}, &out, &errorsOut)
			var result struct {
				Status string `json:"status"`
			}
			if err := json.Unmarshal(errorsOut.Bytes(), &result); err != nil {
				t.Fatalf("invalid CLI error JSON: %v: %s", err, errorsOut.String())
			}
			if code != 1 || result.Status != "UNKNOWN" || out.Len() != 0 {
				t.Fatalf("code=%d, error=%s, stdout=%s", code, errorsOut.String(), out.String())
			}
		})
	}
	t.Run("connection failure", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
		origin := server.URL
		server.Close()
		var out, errorsOut bytes.Buffer
		code := runCLI(context.Background(), []string{"-origin", origin, "-target", "spec"}, &out, &errorsOut)
		var result struct {
			Status string `json:"status"`
		}
		if err := json.Unmarshal(errorsOut.Bytes(), &result); err != nil {
			t.Fatal(err)
		}
		if code != 1 || result.Status != "UNKNOWN" || out.Len() != 0 {
			t.Fatalf("code=%d, error=%s", code, errorsOut.String())
		}
	})
}

func TestCLIRejectsStalePinWithoutRetry(t *testing.T) {
	f := newFixture()
	f.fullStatus = http.StatusConflict
	server := httptest.NewServer(http.HandlerFunc(f.serve))
	defer server.Close()
	var out, errorsOut bytes.Buffer
	code := runCLI(context.Background(), []string{"-origin", server.URL, "-target", "spec"}, &out, &errorsOut)
	var result struct {
		Status string `json:"status"`
	}
	if err := json.Unmarshal(errorsOut.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if code != 1 || result.Status != "REJECTED" || out.Len() != 0 || f.fullAttempts != 1 || f.runPosts != 0 {
		t.Fatalf("stale pin was retried or misclassified: code=%d status=%s attempts=%d posts=%d", code, result.Status, f.fullAttempts, f.runPosts)
	}
}

func TestCLIFailsWhenVerifiedResultCannotBeFullyWritten(t *testing.T) {
	for _, tt := range []struct {
		name   string
		writer brokenOutputWriter
	}{
		{"write error", brokenOutputWriter{failure: io.ErrClosedPipe}},
		{"short write without error", brokenOutputWriter{}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			f := newFixture()
			server := httptest.NewServer(http.HandlerFunc(f.serve))
			defer server.Close()
			var errorsOut bytes.Buffer
			code := runCLI(context.Background(), []string{"-origin", server.URL, "-target", "spec"}, tt.writer, &errorsOut)
			var failure struct {
				Status string `json:"status"`
				Error  string `json:"error"`
			}
			if err := json.Unmarshal(errorsOut.Bytes(), &failure); err != nil {
				t.Fatalf("invalid CLI failure output: %v: %s", err, errorsOut.String())
			}
			if code != 1 || failure.Status != "UNKNOWN" || !strings.Contains(failure.Error, "result output incomplete") || f.runPosts != 1 {
				t.Fatalf("successful exit despite output failure: code=%d failure=%+v posts=%d", code, failure, f.runPosts)
			}
		})
	}
}
