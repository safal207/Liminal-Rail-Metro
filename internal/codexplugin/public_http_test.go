package codexplugin

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestListenAddressFromEnvDefaultsToLocalhost(t *testing.T) {
	t.Setenv("LIMINAL_LISTEN_ADDR", "")
	t.Setenv("PORT", "")
	addr, err := ListenAddressFromEnv()
	if err != nil {
		t.Fatal(err)
	}
	if addr != "127.0.0.1:8787" {
		t.Fatalf("unexpected default address %q", addr)
	}
}

func TestListenAddressFromEnvUsesHostingPort(t *testing.T) {
	t.Setenv("LIMINAL_LISTEN_ADDR", "")
	t.Setenv("PORT", "9000")
	addr, err := ListenAddressFromEnv()
	if err != nil {
		t.Fatal(err)
	}
	if addr != "0.0.0.0:9000" {
		t.Fatalf("unexpected hosted address %q", addr)
	}
}

func TestHardenMCPHandlerRejectsOversizedRequest(t *testing.T) {
	next := http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		_, err := io.ReadAll(request.Body)
		if err != nil {
			http.Error(writer, err.Error(), http.StatusRequestEntityTooLarge)
			return
		}
		writer.WriteHeader(http.StatusNoContent)
	})
	handler, err := HardenMCPHandler(next, PublicHTTPConfig{
		MaxRequestBytes: 8,
		MaxConcurrent:   1,
		RequestTimeout:  time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}

	request := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader("123456789"))
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("expected 413, got %d: %s", recorder.Code, recorder.Body.String())
	}
}

func TestHardenMCPHandlerAppliesBackpressureBeforeDispatch(t *testing.T) {
	entered := make(chan struct{})
	release := make(chan struct{})
	var dispatched atomic.Int32

	next := http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		dispatched.Add(1)
		close(entered)
		<-release
		writer.WriteHeader(http.StatusNoContent)
	})
	handler, err := HardenMCPHandler(next, PublicHTTPConfig{
		MaxRequestBytes: 1024,
		MaxConcurrent:   1,
		RequestTimeout:  time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}

	firstDone := make(chan struct{})
	go func() {
		defer close(firstDone)
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader("{}")))
	}()

	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("first request did not enter handler")
	}

	second := httptest.NewRecorder()
	handler.ServeHTTP(second, httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader("{}")))
	if second.Code != http.StatusTooManyRequests {
		t.Fatalf("expected 429, got %d", second.Code)
	}
	if dispatched.Load() != 1 {
		t.Fatalf("backpressured request reached MCP handler; dispatched=%d", dispatched.Load())
	}

	close(release)
	<-firstDone
}

func TestHardenMCPHandlerSetsNoStoreHeaders(t *testing.T) {
	handler, err := HardenMCPHandler(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.WriteHeader(http.StatusNoContent)
	}), DefaultPublicHTTPConfig())
	if err != nil {
		t.Fatal(err)
	}
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/mcp", nil))
	if recorder.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("missing Cache-Control no-store")
	}
	if recorder.Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Fatalf("missing nosniff")
	}
}
