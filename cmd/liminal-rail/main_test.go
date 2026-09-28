package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/safal207/Liminal-Rail-Metro/internal/codexadapter"
)

const validAction = `{"protocol":"metro.codex.action.v0.1","request_id":"request-001","action_id":"action-001","kind":"hash_text","text":"hello","side_effect":false}`

func request(handler http.Handler, path, body string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(http.MethodPost, "http://127.0.0.1"+path, strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)
	return w
}

func TestQuickstartHashAndSeparateVerification(t *testing.T) {
	a := newAPI()
	w := request(a, "/v1/actions", validAction)
	if w.Code != http.StatusOK {
		t.Fatalf("action: %d %s", w.Code, w.Body.String())
	}
	var proof codexadapter.Response
	if err := codexadapter.Decode(w.Body, &proof, 4*codexadapter.MaxInputBytes); err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256([]byte("hello"))
	if !proof.Evidence.Dispatched || proof.Evidence.Receipt == nil || proof.Evidence.Packet.ActionID != "action-001" || proof.Evidence.Result["sha256"] != hex.EncodeToString(digest[:]) {
		t.Fatal("receipt/result not bound to submitted action")
	}
	if err := codexadapter.Verify(proof); err != nil {
		t.Fatal(err)
	}
	body, _ := json.Marshal(proof)
	w = request(a, "/v1/verify", string(body))
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"verified":true`) || !strings.Contains(w.Body.String(), scope) {
		t.Fatalf("verify: %d %s", w.Code, w.Body.String())
	}
}

func TestQuickstartTamperingRejected(t *testing.T) {
	for _, field := range []string{"digest", "status", "text"} {
		t.Run(field, func(t *testing.T) {
			a := newAPI()
			w := request(a, "/v1/actions", validAction)
			var proof codexadapter.Response
			if err := codexadapter.Decode(w.Body, &proof, 4*codexadapter.MaxInputBytes); err != nil {
				t.Fatal(err)
			}
			switch field {
			case "digest":
				proof.EvidenceHash = "changed"
			case "status":
				proof.Evidence.Receipt.Status = "UNKNOWN"
			case "text":
				proof.Evidence.Packet.Action.Inputs["text"] = "changed"
			}
			body, _ := json.Marshal(proof)
			w = request(a, "/v1/verify", string(body))
			if w.Code != http.StatusUnprocessableEntity || strings.Contains(w.Body.String(), `"verified":true`) {
				t.Fatalf("tampered proof accepted: %d %s", w.Code, w.Body.String())
			}
		})
	}
}

func TestQuickstartStrictJSONBeforeAdmission(t *testing.T) {
	cases := []string{
		`null`, `[]`, `{`, validAction + `{}`,
		strings.Replace(validAction, `"side_effect":false`, `"side_effect":null`, 1),
		strings.Replace(validAction, `"side_effect":false`, `"side_effect":false,"side_effect":true`, 1),
		strings.Replace(validAction, `"kind"`, `"Kind"`, 1),
		strings.Replace(validAction, `"kind"`, `"target":"shell","kind"`, 1),
		strings.Replace(validAction, `"hello"`, `"\uD800"`, 1),
		strings.Repeat("x", codexadapter.MaxInputBytes+1),
	}
	for _, body := range cases {
		a := newAPI()
		w := request(a, "/v1/actions", body)
		if w.Code != http.StatusBadRequest || len(a.used) != 0 {
			t.Fatalf("malformed JSON admitted: code=%d consumed=%d", w.Code, len(a.used))
		}
	}
}

func TestQuickstartDuplicateConcurrency(t *testing.T) {
	a := newAPI()
	var executions atomic.Int64
	a.execute = func(ctx context.Context, action codexadapter.Action) (codexadapter.Response, error) {
		executions.Add(1)
		return codexadapter.Run(ctx, action)
	}
	const callers = 32
	start := make(chan struct{})
	codes := make(chan int, callers)
	var wg sync.WaitGroup
	for i := 0; i < callers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			codes <- request(a, "/v1/actions", validAction).Code
		}()
	}
	close(start)
	wg.Wait()
	close(codes)
	counts := map[int]int{}
	for code := range codes {
		counts[code]++
	}
	if executions.Load() != 1 || counts[http.StatusOK] != 1 || counts[http.StatusConflict] != callers-1 {
		t.Fatalf("executions=%d statuses=%v", executions.Load(), counts)
	}
}

func TestQuickstartFailureRemainsConsumed(t *testing.T) {
	a := newAPI()
	var calls int
	a.execute = func(context.Context, codexadapter.Action) (codexadapter.Response, error) {
		calls++
		return codexadapter.Response{}, errors.New("sensitive input must not be echoed")
	}
	w := request(a, "/v1/actions", validAction)
	if w.Code != http.StatusUnprocessableEntity || strings.Contains(w.Body.String(), "sensitive") || strings.Contains(w.Body.String(), "receipt") {
		t.Fatalf("unsafe failure: %d %s", w.Code, w.Body.String())
	}
	if w := request(a, "/v1/actions", validAction); w.Code != http.StatusConflict || calls != 1 {
		t.Fatalf("ambiguous action replayed: code=%d calls=%d", w.Code, calls)
	}
}

func TestQuickstartRegistryBoundDoesNotEvict(t *testing.T) {
	a := newAPI()
	a.capacity = 1
	if request(a, "/v1/actions", validAction).Code != http.StatusOK {
		t.Fatal("first action failed")
	}
	other := strings.Replace(validAction, "action-001", "action-002", 1)
	if request(a, "/v1/actions", other).Code != http.StatusServiceUnavailable {
		t.Fatal("registry limit not enforced")
	}
	if request(a, "/v1/actions", validAction).Code != http.StatusConflict || len(a.used) != 1 {
		t.Fatal("consumed ID was evicted")
	}
	// A new process/instance has no memory of prior IDs; document, do not hide it.
	if request(newAPI(), "/v1/actions", validAction).Code != http.StatusOK {
		t.Fatal("unexpected durable-state claim")
	}
}

func TestQuickstartBrowserAndHostBoundaries(t *testing.T) {
	for _, tt := range []struct{ host, origin string }{
		{"evil.example", ""}, {"127.0.0.1", "https://evil.example"}, {"127.0.0.1", "null"},
	} {
		a := newAPI()
		r := httptest.NewRequest(http.MethodPost, "http://127.0.0.1/v1/actions", strings.NewReader(validAction))
		r.Host = tt.host
		r.Header.Set("Content-Type", "application/json")
		if tt.origin != "" {
			r.Header.Set("Origin", tt.origin)
		}
		w := httptest.NewRecorder()
		a.ServeHTTP(w, r)
		if w.Code != http.StatusForbidden || len(a.used) != 0 || w.Header().Get("Access-Control-Allow-Origin") != "" {
			t.Fatal("browser/host boundary failed")
		}
	}
}

func TestQuickstartHTTPBoundaries(t *testing.T) {
	for _, tt := range []struct {
		method, path, contentType string
		status                    int
	}{
		{"GET", "/v1/actions", "application/json", 405},
		{"POST", "/healthz", "application/json", 405},
		{"POST", "/v1/actions", "text/plain", 415},
		{"POST", "/missing", "application/json", 404},
		{"POST", "/v1/actions?target=shell", "application/json", 400},
	} {
		r := httptest.NewRequest(tt.method, "http://127.0.0.1"+tt.path, strings.NewReader(validAction))
		r.Header.Set("Content-Type", tt.contentType)
		w := httptest.NewRecorder()
		a := newAPI()
		a.ServeHTTP(w, r)
		if w.Code != tt.status || len(a.used) != 0 {
			t.Fatalf("%s %s: code=%d", tt.method, tt.path, w.Code)
		}
	}
}

func TestQuickstartAdmissionBound(t *testing.T) {
	a := newAPI()
	a.slots = make(chan struct{}, 1)
	a.slots <- struct{}{}
	if w := request(a, "/v1/actions", validAction); w.Code != http.StatusServiceUnavailable || len(a.used) != 0 {
		t.Fatal("concurrent admission bound failed")
	}
}

type brokenWriter struct{ header http.Header }

func (w *brokenWriter) Header() http.Header     { return w.header }
func (*brokenWriter) WriteHeader(int)           {}
func (*brokenWriter) Write([]byte) (int, error) { return 0, io.ErrClosedPipe }

func TestQuickstartLostResponseDoesNotReexecute(t *testing.T) {
	a := newAPI()
	var calls int
	a.execute = func(ctx context.Context, action codexadapter.Action) (codexadapter.Response, error) {
		calls++
		return codexadapter.Run(ctx, action)
	}
	r := httptest.NewRequest(http.MethodPost, "http://127.0.0.1/v1/actions", strings.NewReader(validAction))
	r.Header.Set("Content-Type", "application/json")
	a.ServeHTTP(&brokenWriter{header: make(http.Header)}, r)
	if request(a, "/v1/actions", validAction).Code != http.StatusConflict || calls != 1 {
		t.Fatal("lost response caused re-execution")
	}
}

func TestQuickstartListenerAndClientScope(t *testing.T) {
	if !listenAllowed("127.0.0.1:8788", false) || !listenAllowed("[::1]:8788", false) || listenAllowed("0.0.0.0:8788", false) || !listenAllowed("0.0.0.0:8788", true) || listenAllowed("192.0.2.1:8788", true) {
		t.Fatal("listener scope failed")
	}
	for _, address := range []string{"https://example.com", "http://example.com", "http://user:secret@localhost", defaultURL + "/path", defaultURL + "?query=x"} {
		if _, err := previewURL(address); err == nil {
			t.Fatal("non-local origin accepted")
		}
	}
	s := server("127.0.0.1:8788", newAPI())
	if s.ReadHeaderTimeout <= 0 || s.ReadTimeout <= 0 || s.WriteTimeout <= 0 || s.IdleTimeout <= 0 || s.MaxHeaderBytes > 8<<10 {
		t.Fatal("missing server bounds")
	}
}

func TestQuickstartDemoEndToEnd(t *testing.T) {
	s := httptest.NewServer(newAPI())
	defer s.Close()
	if err := healthcheck(s.URL); err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	if err := run([]string{"demo", "-url", s.URL}, &output); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output.String(), `"demo":"PASS"`) || !strings.Contains(output.String(), `"live_provider":false`) {
		t.Fatalf("unexpected demo output: %s", output.String())
	}
}
