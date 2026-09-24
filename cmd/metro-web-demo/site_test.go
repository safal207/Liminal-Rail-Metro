package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/safal207/Liminal-Rail-Metro/internal/metro"
)

func siteFixture(t *testing.T) (string, string, string, []byte, []byte) {
	t.Helper()
	dir := t.TempDir()
	a := []byte{0, 255, 1, 128, 0, 44}
	b := make([]byte, assetChunkBytes+17)
	for i := range b {
		b[i] = byte(i*71 + 9)
	}
	aPath, bPath := filepath.Join(dir, "alpha.bin"), filepath.Join(dir, "beta.bin")
	if err := os.WriteFile(aPath, a, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(bPath, b, 0600); err != nil {
		t.Fatal(err)
	}
	config := siteConfig{
		Protocol: siteProtocol, Start: "start",
		Nodes: []siteNode{
			{ID: "start", Label: "Вход"},
			{ID: "alpha", Label: "Альфа", ResourceID: "alpha"},
			{ID: "branch", Label: "Ветвь", ResourceID: "alpha"},
			{ID: "beta", Label: "Бета", ResourceID: "beta"},
		},
		Edges: []siteEdge{
			{ID: "open_alpha", From: "start", To: "alpha", Action: siteAction, Scope: siteScope, Resource: "alpha"},
			{ID: "other_alpha", From: "start", To: "branch", Action: siteAction, Scope: siteScope, Resource: "alpha"},
			{ID: "open_beta", From: "alpha", To: "beta", Action: siteAction, Scope: siteScope, Resource: "beta"},
			{ID: "other_beta", From: "branch", To: "beta", Action: siteAction, Scope: siteScope, Resource: "beta"},
			{ID: "cycle", From: "beta", To: "branch", Action: siteAction, Scope: siteScope, Resource: "alpha"},
		},
		Resources: []siteConfigResource{{ID: "alpha", Label: "Первый файл", Path: "alpha.bin"}, {ID: "beta", Label: "Второй файл", Path: "beta.bin"}},
	}
	configPath := filepath.Join(dir, "site.json")
	raw, err := json.Marshal(config)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(configPath, raw, 0600); err != nil {
		t.Fatal(err)
	}
	return configPath, aPath, bPath, a, b
}

func sitePublisherFixture(t *testing.T) (*siteService, string, string, []byte, []byte) {
	t.Helper()
	config, aPath, bPath, a, b := siteFixture(t)
	publisher, err := newSitePublisher(config)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(publisher.close)
	return publisher, aPath, bPath, a, b
}

func TestSiteTwoFileRouteMemoryAndFreshBytes(t *testing.T) {
	publisher, _, bPath, a, b := sitePublisherFixture(t)
	public, err := publisher.loadMap(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(public.Resources) != 2 || public.Resources[0].SHA256 != assetHash(a) || public.Resources[1].SHA256 != assetHash(b) || len(public.Edges) != 5 {
		t.Fatalf("wrong public map: %+v", public)
	}
	server := httptest.NewServer(http.HandlerFunc(publisher.serve))
	defer server.Close()
	reader, err := newSiteReader(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	defer reader.close()

	for attempt := 0; attempt < 2; attempt++ {
		out, err := reader.run(context.Background(), siteRunRequest{Target: "beta"})
		if err != nil {
			t.Fatal(err)
		}
		wantMode := "graph"
		if attempt == 1 {
			wantMode = "memory_revalidated"
		}
		if out.Status != "CONFIRMED_LOCAL" || out.Mode != wantMode || out.PlannedSteps != 2 || out.FreshReads != 2 || len(out.Events) != 2 || len(out.Resources) != 2 || !out.Learned {
			t.Fatalf("attempt %d: %+v", attempt, out)
		}
		for _, event := range out.Events {
			if event.Status != "CONFIRMED_LOCAL" || event.Receipt == nil || event.Receipt.Status != "SUCCEEDED" || event.Packet.Action.Kind != siteAction || event.Packet.Constraints.SideEffect {
				t.Fatalf("missing verified read-only receipt: %+v", event)
			}
		}
		if out.Resources[0].SHA256 != assetHash(a) || out.Resources[1].SHA256 != assetHash(b) {
			t.Fatalf("wrong resource evidence: %+v", out.Resources)
		}
	}

	mapResponse, err := http.Get(server.URL + "/api/site")
	if err != nil {
		t.Fatal(err)
	}
	mapBytes, readErr := io.ReadAll(mapResponse.Body)
	_ = mapResponse.Body.Close()
	if readErr != nil {
		t.Fatal(readErr)
	}
	if bytes.Contains(mapBytes, []byte(bPath)) || bytes.Contains(mapBytes, []byte("beta.bin")) {
		t.Fatal("public map leaked an operator path")
	}

	changed := append([]byte(nil), b...)
	changed[1] ^= 0xff
	if err := os.WriteFile(bPath, changed, 0600); err != nil {
		t.Fatal(err)
	}
	stale, err := http.Get(server.URL + siteFixedFullURL("beta", assetHash(b)))
	if err != nil {
		t.Fatal(err)
	}
	_ = stale.Body.Close()
	if stale.StatusCode != http.StatusConflict {
		t.Fatalf("stale pin: %d", stale.StatusCode)
	}
	out, err := reader.run(context.Background(), siteRunRequest{Target: "beta"})
	if err != nil {
		t.Fatal(err)
	}
	if out.Status != "CONFIRMED_LOCAL" || out.Mode != "graph" || out.Resources[1].SHA256 != assetHash(changed) || out.FreshReads != 2 {
		t.Fatalf("changed bytes not refreshed: %+v", out)
	}
	full, err := http.Get(server.URL + siteFixedFullURL("beta", assetHash(changed)))
	if err != nil {
		t.Fatal(err)
	}
	defer full.Body.Close()
	var got bytes.Buffer
	_, _ = got.ReadFrom(full.Body)
	if full.StatusCode != http.StatusOK || !bytes.Equal(got.Bytes(), changed) || full.Header.Get("X-Content-Type-Options") != "nosniff" || !strings.HasPrefix(full.Header.Get("Content-Disposition"), "attachment;") {
		t.Fatal("published full asset differs from pinned file")
	}
}

func siteRunHTTP(t *testing.T, h http.Handler, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "http://metro.test/api/site/run", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	return w
}

func TestSiteClientChallengeBindsBothRouteModesAndPreservesLegacy(t *testing.T) {
	publisher, _, _, _, _ := sitePublisherFixture(t)
	h := handler(&engine{site: publisher}, "metro.test")
	challenges := []string{strings.Repeat("a", 64), strings.Repeat("b", 64)}
	for attempt, challenge := range challenges {
		body := `{"target":"beta","client_challenge":"` + challenge + `"}`
		response := siteRunHTTP(t, h, body)
		if response.Code != http.StatusOK || response.Header().Get("Cache-Control") != "no-store" {
			t.Fatalf("attempt %d: status %d, cache %q: %s", attempt, response.Code, response.Header().Get("Cache-Control"), response.Body.String())
		}
		var out siteRunResult
		if err := json.Unmarshal(response.Body.Bytes(), &out); err != nil {
			t.Fatal(err)
		}
		wantMode := "graph"
		if attempt == 1 {
			wantMode = "memory_revalidated"
		}
		if out.Status != "CONFIRMED_LOCAL" || out.Mode != wantMode || out.ClientChallenge != challenge || len(out.Events) != 2 || out.FreshReads != 2 || out.MemoryEntries != 1 {
			t.Fatalf("attempt %d: %+v", attempt, out)
		}
		for step, event := range out.Events {
			if event.Receipt == nil || event.Packet.Action.Inputs["client_challenge"] != challenge || event.Result["client_challenge"] != challenge {
				t.Fatalf("attempt %d step %d missing challenge: %+v", attempt, step, event)
			}
			inputHash, err := metro.HashJSON(event.Packet.Action.Inputs)
			if err != nil {
				t.Fatal(err)
			}
			resultHash, err := metro.HashJSON(event.Result)
			if err != nil {
				t.Fatal(err)
			}
			if event.Receipt.InputHash != inputHash || event.Receipt.ResultHash != resultHash || metro.Verify(event.Packet, event.Route, event.Result, *event.Receipt) != nil {
				t.Fatalf("attempt %d step %d has unbound receipt: %+v", attempt, step, event.Receipt)
			}
		}
	}

	legacy := siteRunHTTP(t, h, `{"target":"beta"}`)
	if legacy.Code != http.StatusOK || bytes.Contains(legacy.Body.Bytes(), []byte("client_challenge")) {
		t.Fatalf("legacy response changed: %d %s", legacy.Code, legacy.Body.String())
	}
	var out siteRunResult
	if err := json.Unmarshal(legacy.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if out.Status != "CONFIRMED_LOCAL" || out.Mode != "memory_revalidated" || len(out.Events) != 2 || out.MemoryEntries != 1 {
		t.Fatalf("legacy route changed: %+v", out)
	}
}

func TestSiteMalformedClientChallengeRejectedBeforeRun(t *testing.T) {
	publisher, _, _, _, _ := sitePublisherFixture(t)
	h := handler(&engine{site: publisher}, "metro.test")
	// Holding the execution gate makes any accidental call to run() observable:
	// it would wait for cancellation and return 503 instead of an immediate 400.
	publisher.gate <- struct{}{}
	defer func() { <-publisher.gate }()
	valid := strings.Repeat("a", 64)
	cases := map[string]string{
		"empty":             `{"target":"beta","client_challenge":""}`,
		"null":              `{"target":"beta","client_challenge":null}`,
		"number":            `{"target":"beta","client_challenge":7}`,
		"array":             `{"target":"beta","client_challenge":[]}`,
		"short":             `{"target":"beta","client_challenge":"abc"}`,
		"long":              `{"target":"beta","client_challenge":"` + valid + `0"}`,
		"uppercase":         `{"target":"beta","client_challenge":"` + strings.Repeat("A", 64) + `"}`,
		"nonhex":            `{"target":"beta","client_challenge":"` + strings.Repeat("g", 64) + `"}`,
		"duplicate":         `{"target":"beta","client_challenge":"` + valid + `","client_challenge":"` + valid + `"}`,
		"escaped duplicate": `{"target":"beta","client_challenge":"` + valid + `","client_\u0063hallenge":"` + valid + `"}`,
		"unknown":           `{"target":"beta","client_challenge":"` + valid + `","unexpected":true}`,
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
			defer cancel()
			req := httptest.NewRequest(http.MethodPost, "http://metro.test/api/site/run", strings.NewReader(body)).WithContext(ctx)
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()
			h.ServeHTTP(w, req)
			if w.Code != http.StatusBadRequest || len(publisher.memory) != 0 {
				t.Fatalf("invalid request reached run or memory: status %d, memory %d, body %s", w.Code, len(publisher.memory), w.Body.String())
			}
		})
	}
}

func TestSiteRejectsHostileConfigAndPaths(t *testing.T) {
	configPath, _, _, _, _ := siteFixture(t)
	raw, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	tests := map[string][]byte{
		"write action":     bytes.Replace(raw, []byte(`"action":"open_resource"`), []byte(`"action":"write_resource"`), 1),
		"wrong scope":      bytes.Replace(raw, []byte(`"scope":"resource.read"`), []byte(`"scope":"resource.write"`), 1),
		"side effect":      bytes.Replace(raw, []byte(`"side_effect":false`), []byte(`"side_effect":true`), 1),
		"missing boolean":  bytes.Replace(raw, []byte(`,"side_effect":false`), nil, 1),
		"duplicate key":    bytes.Replace(raw, []byte(`"action":"open_resource"`), []byte(`"action":"open_resource","action":"open_resource"`), 1),
		"unknown field":    bytes.Replace(raw, []byte(`"path":"alpha.bin"`), []byte(`"path":"alpha.bin","url":"http://evil"`), 1),
		"unbound resource": bytes.Replace(raw, []byte(`"resource_id":"beta"`), []byte(`"resource_id":"alpha"`), 1),
	}
	for name, altered := range tests {
		t.Run(name, func(t *testing.T) {
			if bytes.Equal(raw, altered) {
				t.Fatal("fixture mutation did not apply")
			}
			path := filepath.Join(filepath.Dir(configPath), "hostile.json")
			if err := os.WriteFile(path, altered, 0600); err != nil {
				t.Fatal(err)
			}
			service, err := newSitePublisher(path)
			if service != nil {
				service.close()
			}
			if err == nil {
				t.Fatal("hostile configuration accepted")
			}
		})
	}
	if reader, err := newSiteReader("http://example.com"); err == nil {
		reader.close()
		t.Fatal("insecure non-loopback origin accepted")
	}
	if reader, err := newSiteReader("http://127.0.0.1:8788/private"); err == nil {
		reader.close()
		t.Fatal("origin with caller path accepted")
	}
	publisher, err := newSitePublisher(configPath)
	if err != nil {
		t.Fatal(err)
	}
	defer publisher.close()
	for _, path := range []string{"/api/site/assets/../beta", "/api/site/assets/alpha/extra", "/api/site/assets/http://evil", "/api/site/assets/beta/full?sha256=bad", "/api/site?path=alpha.bin"} {
		recorder := httptest.NewRecorder()
		publisher.serve(recorder, httptest.NewRequest(http.MethodGet, path, nil))
		if recorder.Code == http.StatusOK {
			t.Fatalf("caller path accepted: %q", path)
		}
	}
}

func TestSiteIDsStaySingleLiteralURLSegments(t *testing.T) {
	for _, id := range []string{"a", "Alpha_2", "guide.page-1"} {
		if !siteID(id) {
			t.Fatalf("safe ID rejected: %q", id)
		}
	}
	for _, id := range []string{"", ".", "..", ".hidden", "-name", "_name", "a/b", "a%2e", "a?b", "a b", "ресурс"} {
		if siteID(id) {
			t.Fatalf("unsafe URL segment accepted: %q", id)
		}
		if _, _, ok := sitePath("/api/site/assets/" + id); ok {
			t.Fatalf("unsafe asset endpoint accepted: %q", id)
		}
	}

	configPath, _, _, _, _ := siteFixture(t)
	raw, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	for _, bad := range []string{".", ".."} {
		var config siteConfig
		if err := strictSiteJSON(raw, &config, maxSiteConfigBytes); err != nil {
			t.Fatal(err)
		}
		config.Resources[0].ID = bad
		for i := range config.Nodes {
			if config.Nodes[i].ResourceID == "alpha" {
				config.Nodes[i].ResourceID = bad
			}
		}
		for i := range config.Edges {
			if config.Edges[i].Resource == "alpha" {
				config.Edges[i].Resource = bad
			}
		}
		mutated, err := json.Marshal(config)
		if err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(filepath.Dir(configPath), "dot-site.json")
		if err := os.WriteFile(path, mutated, 0600); err != nil {
			t.Fatal(err)
		}
		service, err := newSitePublisher(path)
		if service != nil {
			service.close()
		}
		if err == nil {
			t.Fatalf("publisher accepted dot-segment resource ID %q", bad)
		}
	}
}

func TestSiteRemoteInvalidMapDoesNotFollowUntrustedEndpoint(t *testing.T) {
	publisher, _, _, _, _ := sitePublisherFixture(t)
	base, err := publisher.loadMap(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for name, alter := range map[string]func(*siteMap){
		"untrusted href": func(m *siteMap) { m.Resources[0].Manifest = "https://example.com/private" },
		"write edge":     func(m *siteMap) { m.Edges[0].Scope = "resource.write" },
		"unbound edge":   func(m *siteMap) { m.Edges[0].Resource = "beta" },
		"huge claim":     func(m *siteMap) { m.Resources[0].Size = maxAssetBytes + 1 },
	} {
		t.Run(name, func(t *testing.T) {
			m := base
			m.Resources = append([]sitePublicResource(nil), base.Resources...)
			m.Edges = append([]siteEdge(nil), base.Edges...)
			alter(&m)
			var followups atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/api/site" {
					followups.Add(1)
				}
				w.Header().Set("Content-Type", "application/json")
				_ = json.NewEncoder(w).Encode(m)
			}))
			defer server.Close()
			reader, err := newSiteReader(server.URL)
			if err != nil {
				t.Fatal(err)
			}
			defer reader.close()
			out, err := reader.run(context.Background(), siteRunRequest{Target: "beta"})
			if err != nil || out.Status == "CONFIRMED_LOCAL" || len(out.Events) != 0 || followups.Load() != 0 {
				t.Fatalf("untrusted map led to action: output=%+v err=%v followups=%d", out, err, followups.Load())
			}
		})
	}
}

func TestSiteChangedDuringRemoteReadHasNoFalseReceipt(t *testing.T) {
	publisher, _, bPath, _, b := sitePublisherFixture(t)
	changed := append([]byte(nil), b...)
	changed[0] ^= 0x7f
	var changedOnce atomic.Bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == siteFullPath("beta") && changedOnce.CompareAndSwap(false, true) {
			if err := os.WriteFile(bPath, changed, 0600); err != nil {
				t.Errorf("change file: %v", err)
			}
		}
		publisher.serve(w, r)
	}))
	defer server.Close()
	reader, err := newSiteReader(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	defer reader.close()
	out, err := reader.run(context.Background(), siteRunRequest{Target: "beta"})
	if err != nil {
		t.Fatal(err)
	}
	if !changedOnce.Load() || out.Status == "CONFIRMED_LOCAL" || len(out.Events) != 2 || out.Events[0].Receipt == nil || out.Events[1].Receipt != nil || out.MemoryEntries != 0 {
		t.Fatalf("stale content confirmed: %+v", out)
	}
}

func TestSiteCancellationReaderChainAndSizeLimits(t *testing.T) {
	publisher, _, _, _, _ := sitePublisherFixture(t)
	started := make(chan struct{}, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == siteFullPath("alpha") {
			started <- struct{}{}
			<-r.Context().Done()
			return
		}
		publisher.serve(w, r)
	}))
	defer server.Close()
	reader, err := newSiteReader(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	defer reader.close()
	ctx, cancel := context.WithCancel(context.Background())
	result := make(chan siteRunResult, 1)
	go func() { out, _ := reader.run(ctx, siteRunRequest{Target: "alpha"}); result <- out }()
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("full read was not attempted")
	}
	cancel()
	select {
	case out := <-result:
		if out.Status != "UNKNOWN" || len(out.Events) != 1 || out.Events[0].Receipt != nil || out.MemoryEntries != 0 {
			t.Fatalf("cancelled read confirmed: %+v", out)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("cancelled remote read did not terminate")
	}

	readerServer := httptest.NewServer(http.HandlerFunc(reader.serve))
	defer readerServer.Close()
	readerOfReader, err := newSiteReader(readerServer.URL)
	if err != nil {
		t.Fatal(err)
	}
	defer readerOfReader.close()
	out, err := readerOfReader.run(context.Background(), siteRunRequest{Target: "alpha"})
	if err != nil || out.Status == "CONFIRMED_LOCAL" || len(out.Events) != 0 {
		t.Fatalf("reader chain accepted: %+v %v", out, err)
	}

	configPath, aPath, _, _, _ := siteFixture(t)
	if err := os.WriteFile(aPath, make([]byte, maxAssetBytes+1), 0600); err != nil {
		t.Fatal(err)
	}
	service, err := newSitePublisher(configPath)
	if service != nil {
		service.close()
	}
	if err == nil {
		t.Fatal("oversized asset accepted")
	}
}

func TestSiteAggregateAndTransitionLimits(t *testing.T) {
	configPath, aPath, bPath, _, _ := siteFixture(t)
	raw, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	var config siteConfig
	if err := strictSiteJSON(raw, &config, maxSiteConfigBytes); err != nil {
		t.Fatal(err)
	}
	config.Nodes = []siteNode{{ID: "start", Label: "Вход"}}
	config.Edges = nil
	last := "start"
	for i := 0; i < 9; i++ {
		id := "step" + strconv.Itoa(i)
		resource := "alpha"
		if i == 8 {
			resource = "beta"
		}
		config.Nodes = append(config.Nodes, siteNode{ID: id, Label: id, ResourceID: resource})
		config.Edges = append(config.Edges, siteEdge{ID: "edge" + strconv.Itoa(i), From: last, To: id, Action: siteAction, Scope: siteScope, Resource: resource})
		last = id
	}
	raw, err = json.Marshal(config)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(configPath, raw, 0600); err != nil {
		t.Fatal(err)
	}
	publisher, err := newSitePublisher(configPath)
	if err != nil {
		t.Fatal(err)
	}
	out, err := publisher.run(context.Background(), siteRunRequest{Target: "beta"})
	publisher.close()
	if err != nil || out.Status != "NO_ROUTE" || len(out.Events) != 0 || out.MemoryEntries != 0 {
		t.Fatalf("nine-step route exceeded limit: %+v %v", out, err)
	}

	config.Nodes = append(config.Nodes, siteNode{ID: "gamma", Label: "Гамма", ResourceID: "gamma"})
	config.Edges = append(config.Edges, siteEdge{ID: "open_gamma", From: "start", To: "gamma", Action: siteAction, Scope: siteScope, Resource: "gamma"})
	config.Resources = append(config.Resources, siteConfigResource{ID: "gamma", Label: "Третий", Path: "gamma.bin"})
	large := bytes.Repeat([]byte{123}, 6<<20)
	for _, path := range []string{aPath, bPath, filepath.Join(filepath.Dir(configPath), "gamma.bin")} {
		if err := os.WriteFile(path, large, 0600); err != nil {
			t.Fatal(err)
		}
	}
	raw, err = json.Marshal(config)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(configPath, raw, 0600); err != nil {
		t.Fatal(err)
	}
	publisher, err = newSitePublisher(configPath)
	if publisher != nil {
		publisher.close()
	}
	if err == nil {
		t.Fatal("18 MiB aggregate map accepted")
	}
}

func TestSiteRejectsForgedPassportAndFullBytes(t *testing.T) {
	publisher, _, _, a, _ := sitePublisherFixture(t)
	for name, intercept := range map[string]func(http.ResponseWriter, *http.Request, *siteService, *atomic.Int32){
		"passport href": func(w http.ResponseWriter, r *http.Request, p *siteService, full *atomic.Int32) {
			if r.URL.Path == siteManifestPath("alpha") {
				manifest := siteManifestFor("alpha", a)
				manifest.Href = "https://example.com/other"
				w.Header().Set("Content-Type", "application/json")
				_ = json.NewEncoder(w).Encode(manifest)
				return
			}
			if r.URL.Path == siteFullPath("alpha") {
				full.Add(1)
			}
			p.serve(w, r)
		},
		"full tamper": func(w http.ResponseWriter, r *http.Request, p *siteService, full *atomic.Int32) {
			if r.URL.Path == siteFullPath("alpha") {
				full.Add(1)
				changed := append([]byte(nil), a...)
				changed[0] ^= 1
				w.Header().Set("Content-Type", "application/octet-stream")
				_, _ = w.Write(changed)
				return
			}
			p.serve(w, r)
		},
	} {
		t.Run(name, func(t *testing.T) {
			var full atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { intercept(w, r, publisher, &full) }))
			defer server.Close()
			reader, err := newSiteReader(server.URL)
			if err != nil {
				t.Fatal(err)
			}
			defer reader.close()
			challenge := strings.Repeat("c", 64)
			out, err := reader.run(context.Background(), siteRunRequest{Target: "alpha", ClientChallenge: challenge})
			if err != nil || out.Status == "CONFIRMED_LOCAL" || out.ClientChallenge != challenge || len(out.Events) != 1 || out.Events[0].Receipt != nil || out.Events[0].Packet.Action.Inputs["client_challenge"] != challenge || out.Events[0].Result["client_challenge"] != challenge || out.MemoryEntries != 0 {
				t.Fatalf("forged bytes confirmed: %+v %v", out, err)
			}
			if name == "passport href" && full.Load() != 0 {
				t.Fatal("followed an untrusted passport URL")
			}
		})
	}
}
