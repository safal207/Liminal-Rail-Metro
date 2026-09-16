package decisionplane

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestRemoteProviderRejectsRedirects(t *testing.T) {
	packet := decisionTestPacket(false)
	request := mustDecisionRequest(t, packet)

	target := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		t.Fatal("redirect target should never receive provider request")
	}))
	defer target.Close()

	redirector := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		http.Redirect(writer, request, target.URL, http.StatusTemporaryRedirect)
	}))
	defer redirector.Close()

	provider := mustRemoteProvider(t, redirector.URL, "remote-proof")
	_, err := provider.Decide(context.Background(), request)
	var httpErr *RemoteHTTPError
	if !errors.As(err, &httpErr) {
		t.Fatalf("expected redirect to remain a typed HTTP error, got %T: %v", err, err)
	}
	if httpErr.StatusCode != http.StatusTemporaryRedirect {
		t.Fatalf("expected redirect status %d, got %d", http.StatusTemporaryRedirect, httpErr.StatusCode)
	}
}
