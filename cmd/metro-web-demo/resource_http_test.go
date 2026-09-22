package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/safal207/Liminal-Rail-Metro/internal/metro"
)

// testRemoteSource configures one owned server without any external requests.
func testRemoteSource(t *testing.T, origin string) *httpResourceSource {
	t.Helper()
	source, err := newHTTPResourceSource(origin)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(source.close)
	return source
}

// testPublisher starts the actual resource handler with its assigned Host guard.
func testPublisher(t *testing.T, e *engine) *httptest.Server {
	t.Helper()
	server := httptest.NewUnstartedServer(nil)
	server.Config.Handler = handler(e, server.Listener.Addr().String())
	server.Start()
	t.Cleanup(server.Close)
	return server
}

// remoteFixture constructs protocol claims independently from the parser.
func remoteFixture(t *testing.T) ([]byte, resourceManifest) {
	t.Helper()
	raw, err := json.Marshal(menuDocument{Schema: menuSchema, Title: "HTTP test menu", Items: demoMenu()})
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(raw)
	hash := hex.EncodeToString(digest[:])
	return raw, resourceManifest{
		Protocol: "metro.web.resource.v0.1", ID: "menu", Title: "HTTP test menu",
		Schema: menuSchema, MediaType: "application/json", Version: "sha256:" + hash,
		SHA256: hash, Size: len(raw), Href: "/api/resource/content?sha256=" + hash, Scope: "menu.read",
	}
}

// TestHTTPResourceRefresh crosses a real HTTP boundary, refreshes remembered
// routes, binds the configured publisher into receipts, and preserves evidence.
func TestHTTPResourceRefresh(t *testing.T) {
	requireFileResources(t)
	path := filepath.Join(t.TempDir(), "menu.json")
	writeMenu(t, path, demoMenu())
	publisher := testPublisher(t, fileEngine(t, path))
	reader := newEngine()
	reader.resource = testRemoteSource(t, publisher.URL)
	first := mustRun(t, reader, runRequest{300, "normal"})
	changed := demoMenu()
	changed[1].Price = 500
	writeMenu(t, path, changed)
	second := mustRun(t, reader, runRequest{300, "normal"})
	if first.Status != "CONFIRMED_LOCAL" || len(first.Items) != 2 || second.Status != "CONFIRMED_LOCAL" || len(second.Items) != 1 || second.Mode != "memory_revalidated" || second.FreshReads != 1 || second.HTTPAttempts != 2 || first.ResourceHash == second.ResourceHash {
		t.Fatalf("network freshness failed: %+v / %+v", first, second)
	}
	if first.ResourceSource == nil || first.ResourceSource.Origin != publisher.URL || first.Graph.Resources[0].Origin != publisher.URL {
		t.Fatal("publisher provenance missing")
	}
	for _, event := range first.Events {
		if event.Receipt == nil || metro.Verify(event.Packet, event.Route, event.Result, *event.Receipt) != nil {
			t.Fatal("prior receipt changed")
		}
		source := event.Result["resource_source"].(resourceOrigin)
		source.Origin = "https://another.example"
		event.Result["resource_source"] = source
		if metro.Verify(event.Packet, event.Route, event.Result, *event.Receipt) == nil {
			t.Fatal("foreign publisher accepted")
		}
	}
	// The local UI relay serves verified bytes and still enforces the old pin.
	relay := testPublisher(t, reader)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, relay.URL+first.Resource.Href, nil)
	if err != nil {
		t.Fatal(err)
	}
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	_ = response.Body.Close()
	if response.StatusCode != http.StatusConflict {
		t.Fatal("relay accepted old version")
	}
	// Reader-to-reader forwarding stops at the next hop instead of recursing.
	chained := newEngine()
	chained.resource = testRemoteSource(t, relay.URL)
	rejected := mustRun(t, chained, runRequest{300, "normal"})
	if rejected.Status != "REJECTED" || rejected.HTTPAttempts != 1 || rejected.Learned {
		t.Fatal("reader chain followed")
	}
	if err := os.WriteFile(path, []byte("{broken"), 0600); err != nil {
		t.Fatal(err)
	}
	failed := mustRun(t, reader, runRequest{300, "normal"})
	if failed.Status != "UNKNOWN" || failed.Learned || len(failed.Items) != 0 || failed.Resource != nil || failed.HTTPAttempts != 1 {
		t.Fatal("unavailable publisher reused old data")
	}
}

// TestRemoteOriginPolicy rejects credentials, arbitrary paths and plain HTTP
// outside literal loopback without attempting DNS or a network connection.
func TestRemoteOriginPolicy(t *testing.T) {
	for _, origin := range []string{"http://127.0.0.1:8788", "http://[::1]:8788/", "https://example.test"} {
		source, err := newHTTPResourceSource(origin)
		if err != nil {
			t.Fatal(origin, err)
		}
		transport := source.client.Transport.(*http.Transport)
		if transport.Proxy != nil || !transport.DisableKeepAlives || !transport.DisableCompression {
			t.Fatal("unsafe transport defaults")
		}
		source.close()
	}
	for _, origin := range []string{"http://localhost:8788", "http://example.test", "file:///menu.json", "https://user:password@example.test", "https://example.test/api/resource", "https://example.test/?token=secret", "https://example.test/#", "https://:443", "https://example.test:", "https://example.test:0", "https://example.test:65536"} {
		if source, err := newHTTPResourceSource(origin); err == nil {
			source.close()
			t.Fatalf("origin accepted: %s", origin)
		}
	}
}

// TestRemotePublisherFailures covers untrusted passports, content corruption,
// redirects, changing versions and bounded response bodies without retries.
func TestRemotePublisherFailures(t *testing.T) {
	raw, good := remoteFixture(t)
	cases := []struct {
		name     string
		mutate   func(*resourceManifest)
		expected string
		attempts int
	}{
		{"cross origin", func(p *resourceManifest) { p.Href = "https://another.example/menu" }, "REJECTED", 1},
		{"other path", func(p *resourceManifest) { p.Href = "/admin/shutdown" }, "REJECTED", 1},
		{"version", func(p *resourceManifest) { p.Version = "old" }, "REJECTED", 1},
		{"scope", func(p *resourceManifest) { p.Scope = "order.write" }, "REJECTED", 1},
		{"schema", func(p *resourceManifest) { p.Schema = "unknown" }, "REJECTED", 1},
		{"large claim", func(p *resourceManifest) { p.Size = maxResourceBytes + 1 }, "REJECTED", 1},
		{"wrong size", func(p *resourceManifest) { p.Size++ }, "REJECTED", 2},
		{"wrong title", func(p *resourceManifest) { p.Title = "another menu" }, "REJECTED", 2},
		{"corrupt content", nil, "REJECTED", 2},
		{"stale version", nil, "REJECTED", 2},
		{"redirect", nil, "REJECTED", 1},
		{"media type", nil, "REJECTED", 1},
		{"encoding", nil, "REJECTED", 1},
		{"large passport", nil, "REJECTED", 1},
		{"trailing JSON", nil, "REJECTED", 1},
		{"invalid UTF8", nil, "REJECTED", 1},
		{"large content", nil, "REJECTED", 2},
		{"content disconnected", nil, "UNKNOWN", 2},
		{"unavailable", nil, "UNKNOWN", 1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var count atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				count.Add(1)
				w.Header().Set("Content-Type", "application/json")
				if r.URL.Path == "/api/resource" {
					p := good
					if tc.mutate != nil {
						tc.mutate(&p)
					}
					switch tc.name {
					case "redirect":
						w.Header().Set("Location", "/redirect-target")
						w.WriteHeader(302)
						return
					case "media type":
						w.Header().Set("Content-Type", "text/html")
					case "encoding":
						w.Header().Set("Content-Encoding", "gzip")
					case "large passport":
						_, _ = w.Write([]byte(strings.Repeat(" ", maxPassportBytes+1)))
						return
					case "trailing JSON":
						_ = json.NewEncoder(w).Encode(p)
						_, _ = w.Write([]byte("{}"))
						return
					case "invalid UTF8":
						_, _ = w.Write([]byte{0xff})
						return
					case "unavailable":
						w.WriteHeader(503)
						return
					}
					_ = json.NewEncoder(w).Encode(p)
					return
				}
				if r.URL.RequestURI() != good.Href {
					t.Error("unadvertised content request", r.URL.String())
					w.WriteHeader(400)
					return
				}
				switch tc.name {
				case "stale version":
					w.WriteHeader(409)
				case "corrupt content":
					_, _ = w.Write(append(append([]byte{}, raw...), '\n'))
				case "large content":
					_, _ = w.Write([]byte(strings.Repeat("x", maxResourceBytes+1)))
				case "content disconnected":
					w.Header().Set("Content-Length", "1000")
					_, _ = w.Write([]byte("{"))
				default:
					_, _ = w.Write(raw)
				}
			}))
			defer server.Close()
			e := newEngine()
			e.resource = testRemoteSource(t, server.URL)
			out := mustRun(t, e, runRequest{300, "normal"})
			if out.Status != tc.expected || out.HTTPAttempts != tc.attempts || int(count.Load()) != tc.attempts || out.Learned || len(out.Items) != 0 || out.Resource != nil {
				t.Fatalf("unexpected failure outcome: %+v; requests=%d", out, count.Load())
			}
			for _, event := range out.Events {
				if event.Receipt != nil {
					t.Fatal("failure got a receipt")
				}
			}
		})
	}
}

// TestRemoteCancellationAndDenial bounds a slow response and ensures denied or
// unreachable paths produce no outbound request, including after route reuse.
func TestRemoteCancellationAndDenial(t *testing.T) {
	var count atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		count.Add(1)
		select {
		case <-r.Context().Done():
		case <-time.After(time.Second):
		}
	}))
	defer server.Close()
	source := testRemoteSource(t, server.URL)
	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Millisecond)
	defer cancel()
	start := time.Now()
	snapshot, err := source.read(ctx)
	if !errors.Is(err, errRemoteIncomplete) || snapshot.Requests != 1 || time.Since(start) > time.Second {
		t.Fatal("cancellation ignored", err)
	}
	before := count.Load()
	e := newEngine()
	e.resource = source
	for _, scenario := range []string{"denied", "cycle", "step_limit"} {
		out := mustRun(t, e, runRequest{300, scenario})
		if out.HTTPAttempts != 0 || len(out.Events) != 0 {
			t.Fatal("planning failure dispatched network request")
		}
	}
	if count.Load() != before {
		t.Fatal("denial sent a request")
	}
	cancelled, stop := context.WithCancel(context.Background())
	stop()
	snapshot, err = source.read(cancelled)
	if !errors.Is(err, errRemoteIncomplete) || snapshot.Requests != 0 || count.Load() != before {
		t.Fatal("cancelled read reached publisher")
	}
}

// TestRemoteTLSValidation accepts explicitly trusted test TLS and rejects an
// untrusted certificate with the production client's default verification.
func TestRemoteTLSValidation(t *testing.T) {
	raw, manifest := remoteFixture(t)
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/api/resource" {
			_ = json.NewEncoder(w).Encode(manifest)
		} else {
			_, _ = w.Write(raw)
		}
	}))
	defer server.Close()
	source := testRemoteSource(t, server.URL)
	if _, err := source.read(context.Background()); !errors.Is(err, errRemoteIncomplete) {
		t.Fatal("untrusted certificate accepted", err)
	}
	source.client.Transport.(*http.Transport).TLSClientConfig = server.Client().Transport.(*http.Transport).TLSClientConfig.Clone()
	result, err := source.read(context.Background())
	if err != nil || result.Manifest.SHA256 != manifest.SHA256 || result.Requests != 2 {
		t.Fatal("trusted TLS failed", err)
	}
}
