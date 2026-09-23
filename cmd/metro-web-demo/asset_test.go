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
	fullURL := assetFullPath + "?sha256=" + passport.SHA256
	full := assetRequest(t, service, fullURL)
	if full.Code != 200 || !bytes.Equal(full.Body.Bytes(), raw) ||
		full.Header().Get("Content-Type") != "application/octet-stream" ||
		full.Header().Get("Content-Disposition") != `attachment; filename="metro-asset.bin"` ||
		full.Header().Get("X-Metro-Asset-Whole-Verified") != "true" {
		t.Fatal("full binary response was not verified and safely attached", full.Code)
	}
	if err := os.WriteFile(path, append(raw, 0x80), 0600); err != nil {
		t.Fatal(err)
	}
	old := assetRequest(t, service, passport.Href+"?sha256="+passport.SHA256+"&index=1")
	if old.Code != http.StatusConflict {
		t.Fatal("stale whole-file pin accepted", old.Code)
	}
	if oldFull := assetRequest(t, service, fullURL); oldFull.Code != http.StatusConflict {
		t.Fatal("stale full-file pin accepted", oldFull.Code)
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
	maxFull := assetRequest(t, service, assetFullPath+"?sha256="+maximum.SHA256)
	if maxFull.Code != 200 || maxFull.Body.Len() != maxAssetBytes {
		t.Fatal("exact 8 MiB full download rejected", maxFull.Code, maxFull.Body.Len())
	}
	if err := os.WriteFile(path, make([]byte, maxAssetBytes+1), 0600); err != nil {
		t.Fatal(err)
	}
	if tooLarge := assetRequest(t, service, "/api/asset"); tooLarge.Code != http.StatusServiceUnavailable {
		t.Fatal("oversized local asset accepted", tooLarge.Code)
	}
	if tooLarge := assetRequest(t, service, assetFullPath+"?sha256="+maximum.SHA256); tooLarge.Code != http.StatusServiceUnavailable {
		t.Fatal("oversized full asset accepted", tooLarge.Code)
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
	if full := assetRequest(t, service, assetFullPath+"?sha256="+passport.SHA256); full.Code != 200 || full.Body.Len() != 0 || full.Header().Get("X-Metro-Asset-Whole-Verified") != "true" {
		t.Fatal("empty full download failed", full.Code)
	}
	for _, target := range []string{
		"/api/asset?", "/api/asset?path=secret", "/api/asset/content", "/api/asset/content?sha256=" + passport.SHA256,
		assetFullPath, assetFullPath + "?", assetFullPath + "?sha256=" + passport.SHA256 + "&index=0",
		assetFullPath + "?sha256=" + strings.ToUpper(passport.SHA256),
		assetFullPath + "?sha256=" + passport.SHA256 + "&sha256=" + passport.SHA256,
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

// A maximum-size download requires one passport and one full-byte request.
// The reader checks the whole digest and all 128 chunk digests before relay.
func TestAssetRemoteFullUsesOneTransfer(t *testing.T) {
	raw := bytes.Repeat([]byte{0, 0xff, '<', '>'}, maxAssetBytes/4)
	passport := manifestForAsset(raw)
	var manifests, fulls, chunks atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Metro-Resource-Read") != "1" {
			t.Error("reader marker missing")
		}
		switch r.URL.Path {
		case "/api/asset":
			manifests.Add(1)
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(passport)
		case assetFullPath:
			fulls.Add(1)
			if r.URL.RawQuery != "sha256="+passport.SHA256 {
				t.Error("full request lost its version pin")
			}
			w.Header().Set("Content-Type", "application/octet-stream")
			_, _ = w.Write(raw)
		case assetContentPath:
			chunks.Add(1)
			http.Error(w, "unexpected chunk read", 500)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	reader := &assetService{remote: testRemoteSource(t, server.URL)}
	w := assetRequest(t, reader, assetFullPath+"?sha256="+passport.SHA256)
	if w.Code != 200 || !bytes.Equal(w.Body.Bytes(), raw) ||
		w.Header().Get("X-Metro-Asset-Whole-Verified") != "true" ||
		manifests.Load() != 1 || fulls.Load() != 1 || chunks.Load() != 0 {
		t.Fatalf("full transfer was not bounded to two requests: code=%d manifest=%d full=%d chunks=%d", w.Code, manifests.Load(), fulls.Load(), chunks.Load())
	}
}

func TestAssetRemoteEmptyFull(t *testing.T) {
	requireFileResources(t)
	path := filepath.Join(t.TempDir(), "empty.bin")
	if err := os.WriteFile(path, nil, 0600); err != nil {
		t.Fatal(err)
	}
	publisher := assetPublisher(t, assetFileService(t, path))
	reader := &assetService{remote: testRemoteSource(t, publisher.URL)}
	w := assetRequest(t, reader, assetFullPath+"?sha256="+assetHash(nil))
	if w.Code != 200 || w.Body.Len() != 0 || w.Header().Get("X-Metro-Asset-Whole-Verified") != "true" {
		t.Fatal("empty remote full asset failed", w.Code)
	}
}

func TestAssetRemoteFullRejectsUntrustedBytes(t *testing.T) {
	raw := bytes.Repeat([]byte{0, 0xff, '<'}, assetChunkBytes/3+2)
	good := manifestForAsset(raw)
	cases := []struct {
		name       string
		change     func(*assetManifest)
		respond    func(http.ResponseWriter)
		status     int
		requestNum int32
	}{
		{"forged whole hash", func(m *assetManifest) { m.SHA256 = assetHash([]byte("different")); m.Version = "sha256:" + m.SHA256 }, nil, 502, 2},
		{"forged chunk hash", func(m *assetManifest) { m.ChunkHash[0] = assetHash([]byte("different")) }, nil, 502, 2},
		{"oversized claim", func(m *assetManifest) { m.Size = maxAssetBytes + 1 }, nil, 502, 1},
		{"truncated", nil, func(w http.ResponseWriter) {
			w.Header().Set("Content-Type", "application/octet-stream")
			_, _ = w.Write(raw[:len(raw)-1])
		}, 502, 2},
		{"overlong", nil, func(w http.ResponseWriter) {
			w.Header().Set("Content-Type", "application/octet-stream")
			_, _ = w.Write(append(append([]byte{}, raw...), 0))
		}, 502, 2},
		{"HTML media", nil, func(w http.ResponseWriter) { w.Header().Set("Content-Type", "text/html"); _, _ = w.Write(raw) }, 502, 2},
		{"encoded body", nil, func(w http.ResponseWriter) {
			w.Header().Set("Content-Type", "application/octet-stream")
			w.Header().Set("Content-Encoding", "gzip")
			_, _ = w.Write(raw)
		}, 502, 2},
		{"redirect", nil, func(w http.ResponseWriter) { w.Header().Set("Location", "/elsewhere"); w.WriteHeader(302) }, 502, 2},
		{"changed version", nil, func(w http.ResponseWriter) { w.WriteHeader(http.StatusConflict) }, 409, 2},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			manifest := good
			manifest.ChunkHash = append([]string(nil), good.ChunkHash...)
			if tc.change != nil {
				tc.change(&manifest)
			}
			var calls atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				if r.Header.Get("X-Metro-Resource-Read") != "1" {
					t.Error("reader marker missing")
				}
				switch r.URL.Path {
				case "/api/asset":
					w.Header().Set("Content-Type", "application/json")
					_ = json.NewEncoder(w).Encode(manifest)
				case assetFullPath:
					if r.URL.RawQuery != "sha256="+manifest.SHA256 {
						t.Error("unbounded full request URL")
					}
					if tc.respond == nil {
						w.Header().Set("Content-Type", "application/octet-stream")
						_, _ = w.Write(raw)
					} else {
						tc.respond(w)
					}
				default:
					t.Error("reader requested unexpected path", r.URL.Path)
					http.NotFound(w, r)
				}
			}))
			defer server.Close()
			reader := &assetService{remote: testRemoteSource(t, server.URL)}
			w := assetRequest(t, reader, assetFullPath+"?sha256="+manifest.SHA256)
			if w.Code != tc.status || w.Body.Len() > 256 || calls.Load() != tc.requestNum {
				t.Fatalf("invalid full response escaped: %d bytes=%d requests=%d", w.Code, w.Body.Len(), calls.Load())
			}
		})
	}
}

func TestAssetRemoteFullCancellation(t *testing.T) {
	passport := manifestForAsset([]byte{0, 0xff, '<'})
	var calls atomic.Int32
	entered := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.URL.Path == "/api/asset" {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(passport)
			return
		}
		close(entered)
		<-r.Context().Done()
	}))
	defer server.Close()
	reader := &assetService{remote: testRemoteSource(t, server.URL)}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	r := httptest.NewRequestWithContext(ctx, http.MethodGet, assetFullPath+"?sha256="+passport.SHA256, nil)
	w := httptest.NewRecorder()
	done := make(chan struct{})
	go func() {
		reader.serve(w, r)
		close(done)
	}()
	select {
	case <-entered:
	case <-time.After(2 * time.Second):
		cancel()
		t.Fatal("full read did not reach publisher")
	}
	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("cancelled full read did not stop")
	}
	if w.Code != http.StatusServiceUnavailable || w.Body.Len() > 256 || calls.Load() != 2 {
		t.Fatal("cancelled full read leaked bytes or retried", w.Code, calls.Load())
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
