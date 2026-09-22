package main

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func assetFileService(t *testing.T, path string) *assetService {
	t.Helper()
	requireFileResources(t)
	source, err := newResourceSource(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(source.close)
	return &assetService{local: source}
}

func assetRequest(t *testing.T, service *assetService, target string) *httptest.ResponseRecorder {
	t.Helper()
	e := newEngine()
	e.asset = service
	r := httptest.NewRequestWithContext(context.Background(), http.MethodGet, target, nil)
	r.Host = "127.0.0.1:8787"
	w := httptest.NewRecorder()
	handler(e, r.Host).ServeHTTP(w, r)
	return w
}

func assetPublisher(t *testing.T, service *assetService) *httptest.Server {
	t.Helper()
	e := newEngine()
	e.asset = service
	return testPublisher(t, e)
}

// A file with zero and invalid UTF-8 bytes survives both chunk boundaries and
// browser-safe HTTP headers; a stale pin cannot retrieve a changed version.
func TestAssetLocalBinaryAndPin(t *testing.T) {
	requireFileResources(t)
	raw := bytes.Repeat([]byte{0x00, 0xff, 0x3c, 0x21, 0x7f}, assetChunkBytes/5+2)
	path := filepath.Join(t.TempDir(), "arbitrary.bin")
	if err := os.WriteFile(path, raw, 0600); err != nil {
		t.Fatal(err)
	}
	service := assetFileService(t, path)
	w := assetRequest(t, service, "/api/asset")
	var passport assetManifest
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &passport) != nil || !passport.valid() ||
		passport.Size != len(raw) || passport.SHA256 != assetHash(raw) || len(passport.ChunkHash) != 2 ||
		w.Header().Get("X-Metro-Asset-Whole-Verified") != "true" {
		t.Fatalf("local passport failed: %d %s", w.Code, w.Body.String())
	}
	var collected []byte
	for i := range passport.ChunkHash {
		url := passport.Href + "?sha256=" + passport.SHA256 + "&index=" + string(rune('0'+i))
		chunk := assetRequest(t, service, url)
		if chunk.Code != 200 || chunk.Header().Get("Content-Type") != "application/octet-stream" ||
			chunk.Header().Get("X-Content-Type-Options") != "nosniff" ||
			chunk.Header().Get("Content-Disposition") != `attachment; filename="metro-asset.bin"` ||
			chunk.Header().Get("X-Metro-Asset-Chunk-SHA256") != assetHash(chunk.Body.Bytes()) {
			t.Fatalf("unsafe or invalid chunk %d: %d %+v", i, chunk.Code, chunk.Header())
		}
		collected = append(collected, chunk.Body.Bytes()...)
	}
	if !bytes.Equal(collected, raw) {
		t.Fatal("binary bytes changed across chunks")
	}
	if err := os.WriteFile(path, append(raw, 0x80), 0600); err != nil {
		t.Fatal(err)
	}
	old := assetRequest(t, service, passport.Href+"?sha256="+passport.SHA256+"&index=1")
	if old.Code != http.StatusConflict {
		t.Fatal("stale whole-file pin accepted", old.Code)
	}
	if err := os.WriteFile(path, make([]byte, maxAssetBytes), 0600); err != nil {
		t.Fatal(err)
	}
	limit := assetRequest(t, service, "/api/asset")
	var maximum assetManifest
	if limit.Code != 200 || json.Unmarshal(limit.Body.Bytes(), &maximum) != nil ||
		maximum.Size != maxAssetBytes || len(maximum.ChunkHash) != 128 || !maximum.valid() {
		t.Fatal("exact 8 MiB boundary rejected", limit.Code)
	}
	if err := os.WriteFile(path, make([]byte, maxAssetBytes+1), 0600); err != nil {
		t.Fatal(err)
	}
	if tooLarge := assetRequest(t, service, "/api/asset"); tooLarge.Code != http.StatusServiceUnavailable {
		t.Fatal("oversized local asset accepted", tooLarge.Code)
	}
}

func TestAssetEmptyAndStrictQuery(t *testing.T) {
	requireFileResources(t)
	path := filepath.Join(t.TempDir(), "empty.bin")
	if err := os.WriteFile(path, nil, 0600); err != nil {
		t.Fatal(err)
	}
	service := assetFileService(t, path)
	w := assetRequest(t, service, "/api/asset")
	var passport assetManifest
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &passport) != nil || !passport.valid() ||
		passport.Size != 0 || len(passport.ChunkHash) != 0 || passport.SHA256 != assetHash(nil) {
		t.Fatal("empty asset passport invalid", w.Body.String())
	}
	if w := assetRequest(t, service, passport.Href+"?sha256="+passport.SHA256+"&index=0"); w.Code != 400 {
		t.Fatal("empty asset exposed a chunk", w.Code)
	}
	for _, target := range []string{
		"/api/asset?", "/api/asset?path=secret", "/api/asset/content", "/api/asset/content?sha256=" + passport.SHA256,
		passport.Href + "?sha256=" + passport.SHA256 + "&index=00",
		passport.Href + "?sha256=" + passport.SHA256 + "&index=-1",
		passport.Href + "?sha256=" + passport.SHA256 + "&index=0&path=secret",
		passport.Href + "?sha256=" + passport.SHA256 + "&index=0&index=1",
		passport.Href + "?sha256=" + strings.ToUpper(passport.SHA256) + "&index=0",
	} {
		if w := assetRequest(t, service, target); w.Code != 400 {
			t.Fatalf("query accepted: %s (%d)", target, w.Code)
		}
	}
}

func TestAssetPassportSyntax(t *testing.T) {
	valid, err := json.Marshal(manifestForAsset([]byte{0, 0xff, '<'}))
	if err != nil {
		t.Fatal(err)
	}
	cases := map[string][]byte{
		"unknown field": append(append([]byte{}, bytes.TrimSuffix(valid, []byte("}"))...), []byte(`,"secret":1}`)...),
		"trailing JSON": append(append([]byte{}, valid...), []byte("{}")...),
		"invalid UTF-8": {0xff},
	}
	for name, raw := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := parseAssetManifest(raw); err == nil {
				t.Fatal("untrusted passport accepted")
			}
		})
	}
}

// Real HTTP publisher and reader exchange only the selected verified chunk.
// Reader metadata states that the whole hash is still the publisher's claim.
func TestAssetRemoteSelectedChunkAndChain(t *testing.T) {
	requireFileResources(t)
	raw := bytes.Repeat([]byte{0xff, 0x00, 0x3c}, assetChunkBytes/3+4)
	path := filepath.Join(t.TempDir(), "network.bin")
	if err := os.WriteFile(path, raw, 0600); err != nil {
		t.Fatal(err)
	}
	publisher := assetPublisher(t, assetFileService(t, path))
	source := testRemoteSource(t, publisher.URL)
	reader := &assetService{remote: source}
	w := assetRequest(t, reader, "/api/asset")
	var passport assetManifest
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &passport) != nil ||
		passport.SHA256 != assetHash(raw) || w.Header().Get("X-Metro-Asset-Whole-Verified") != "false" {
		t.Fatalf("remote passport failed: %d %s", w.Code, w.Body.String())
	}
	selected := assetRequest(t, reader, passport.Href+"?sha256="+passport.SHA256+"&index=1")
	if selected.Code != 200 || !bytes.Equal(selected.Body.Bytes(), raw[assetChunkBytes:]) ||
		selected.Header().Get("X-Metro-Asset-Whole-Verified") != "false" {
		t.Fatal("remote chunk was not verified and relayed", selected.Code)
	}
	if err := os.WriteFile(path, append(raw, 0), 0600); err != nil {
		t.Fatal(err)
	}
	if stale := assetRequest(t, reader, passport.Href+"?sha256="+passport.SHA256+"&index=1"); stale.Code != 409 {
		t.Fatal("remote version pin did not catch change", stale.Code)
	}
	request := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/api/asset", nil)
	request.Header.Set("X-Metro-Resource-Read", "1")
	blocked := httptest.NewRecorder()
	reader.serve(blocked, request)
	if blocked.Code != http.StatusLoopDetected {
		t.Fatal("reader chain accepted", blocked.Code)
	}
	secondReader := &assetService{remote: testRemoteSource(t, assetPublisher(t, reader).URL)}
	if chained := assetRequest(t, secondReader, "/api/asset"); chained.Code != http.StatusBadGateway {
		t.Fatal("reader-to-reader chain forwarded", chained.Code)
	}
}

// An adversarial publisher cannot use the passport to select another path,
// inflate the bounded file size, or serve bytes that miss the chosen digest.
func TestAssetRemoteInvalidPublisher(t *testing.T) {
	raw := []byte{0, 0xff, 0x3c, 0x21}
	good := manifestForAsset(raw)
	cases := []struct {
		name     string
		manifest func(assetManifest) assetManifest
		body     func(http.ResponseWriter, assetManifest)
		calls    int32
	}{
		{"foreign href", func(m assetManifest) assetManifest { m.Href = "https://evil.example/file"; return m }, nil, 1},
		{"oversized claim", func(m assetManifest) assetManifest { m.Size = maxAssetBytes + 1; return m }, nil, 1},
		{"wrong chunk digest", func(m assetManifest) assetManifest { m.ChunkHash = []string{assetHash([]byte("other"))}; return m }, nil, 2},
		{"wrong media", nil, func(w http.ResponseWriter, _ assetManifest) {
			w.Header().Set("Content-Type", "text/html")
			_, _ = w.Write(raw)
		}, 2},
		{"redirect", nil, func(w http.ResponseWriter, _ assetManifest) {
			w.Header().Set("Location", "/elsewhere")
			w.WriteHeader(302)
		}, 2},
		{"overlong chunk", nil, func(w http.ResponseWriter, _ assetManifest) {
			w.Header().Set("Content-Type", "application/octet-stream")
			_, _ = w.Write(append(raw, 0))
		}, 2},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var calls atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				if r.Header.Get("X-Metro-Resource-Read") != "1" {
					t.Error("reader marker missing")
				}
				m := good
				if tc.manifest != nil {
					m = tc.manifest(m)
				}
				if r.URL.Path == "/api/asset" {
					w.Header().Set("Content-Type", "application/json")
					_ = json.NewEncoder(w).Encode(m)
					return
				}
				if r.URL.RequestURI() != "/api/asset/content?sha256="+good.SHA256+"&index=0" {
					t.Error("reader requested untrusted path", r.URL.String())
				}
				if tc.body != nil {
					tc.body(w, m)
					return
				}
				w.Header().Set("Content-Type", "application/octet-stream")
				_, _ = w.Write(raw)
			}))
			defer server.Close()
			reader := &assetService{remote: testRemoteSource(t, server.URL)}
			url := good.Href + "?sha256=" + good.SHA256 + "&index=0"
			out := assetRequest(t, reader, url)
			if out.Code != http.StatusBadGateway || calls.Load() != tc.calls {
				t.Fatalf("untrusted response accepted: %s, code=%d, calls=%d", tc.name, out.Code, calls.Load())
			}
		})
	}
}

// Cancellation stops the one in-flight passport request and sends no request
// when the caller was already cancelled.
func TestAssetRemoteCancellation(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		select {
		case <-r.Context().Done():
		case <-time.After(time.Second):
		}
	}))
	defer server.Close()
	reader := &assetService{remote: testRemoteSource(t, server.URL)}
	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Millisecond)
	defer cancel()
	r := httptest.NewRequestWithContext(ctx, http.MethodGet, "/api/asset", nil)
	w := httptest.NewRecorder()
	start := time.Now()
	reader.serve(w, r)
	if w.Code != http.StatusServiceUnavailable || calls.Load() != 1 || time.Since(start) > time.Second {
		t.Fatal("cancelled read continued", w.Code, calls.Load())
	}
	ctx, cancel = context.WithCancel(context.Background())
	cancel()
	r = httptest.NewRequestWithContext(ctx, http.MethodGet, "/api/asset", nil)
	w = httptest.NewRecorder()
	reader.serve(w, r)
	if w.Code != http.StatusServiceUnavailable || calls.Load() != 1 {
		t.Fatal("already cancelled read reached publisher", w.Code, calls.Load())
	}
}
