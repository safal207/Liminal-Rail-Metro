// liminal-rail provides a localhost-only developer preview of the existing
// codexadapter. It does not authenticate agents or connect to live providers.
package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"mime"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"regexp"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/safal207/Liminal-Rail-Metro/internal/codexadapter"
)

const scope = "local_consistency_only"
const defaultURL = "http://127.0.0.1:8788"

var actionIDPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$`)

type api struct {
	mu       sync.Mutex
	used     map[string]struct{}
	capacity int
	slots    chan struct{}
	execute  func(context.Context, codexadapter.Action) (codexadapter.Response, error)
}

func newAPI() *api {
	return &api{
		used: make(map[string]struct{}), capacity: 1024,
		slots: make(chan struct{}, 64), execute: codexadapter.Run,
	}
}

// reserve never evicts IDs. A failed or disconnected admitted request remains
// consumed. This is one-instance memory only, NOT durable replay protection.
func (a *api) reserve(id string) (int, string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if _, exists := a.used[id]; exists {
		return http.StatusConflict, "ACTION_ALREADY_CONSUMED"
	}
	if len(a.used) >= a.capacity {
		return http.StatusServiceUnavailable, "ACTION_REGISTRY_FULL"
	}
	a.used[id] = struct{}{}
	return 0, ""
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	// A failed response write must never cause re-execution.
	_ = json.NewEncoder(w).Encode(value)
}

func fail(w http.ResponseWriter, status int, code string) {
	writeJSON(w, status, map[string]any{"error": code, "scope": scope})
}

func localHost(host string) bool {
	return host == "localhost" || host == "rail" || (net.ParseIP(host) != nil && net.ParseIP(host).IsLoopback())
}

func requestHostAllowed(hostport string) bool {
	host, _, err := net.SplitHostPort(hostport)
	if err != nil {
		host = strings.Trim(hostport, "[]")
	}
	return localHost(host)
}

func (a *api) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("X-Liminal-Scope", scope)
	if !requestHostAllowed(r.Host) || len(r.Header.Values("Origin")) != 0 {
		fail(w, http.StatusForbidden, "LOCAL_CLI_ONLY")
		return
	}
	method := http.MethodPost
	switch r.URL.Path {
	case "/healthz":
		method = http.MethodGet
	case "/v1/actions", "/v1/verify":
	default:
		fail(w, http.StatusNotFound, "NOT_FOUND")
		return
	}
	if r.Method != method {
		w.Header().Set("Allow", method)
		fail(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED")
		return
	}
	if r.URL.RawQuery != "" {
		fail(w, http.StatusBadRequest, "QUERY_NOT_SUPPORTED")
		return
	}
	if r.URL.Path == "/healthz" {
		writeJSON(w, http.StatusOK, map[string]any{
			"ok": true, "mode": "developer_preview", "scope": scope,
			"authenticated_identity": false, "live_provider": false,
			"replay_scope": "single_instance_memory", "action_capacity": a.capacity,
		})
		return
	}
	mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || mediaType != "application/json" || r.Header.Get("Content-Encoding") != "" {
		fail(w, http.StatusUnsupportedMediaType, "JSON_REQUIRED")
		return
	}
	select {
	case a.slots <- struct{}{}:
		defer func() { <-a.slots }()
	default:
		fail(w, http.StatusServiceUnavailable, "ADMISSION_FULL")
		return
	}
	limit := int64(codexadapter.MaxInputBytes)
	if r.URL.Path == "/v1/verify" {
		limit *= 4
	}
	r.Body = http.MaxBytesReader(w, r.Body, limit)
	defer r.Body.Close()
	if r.URL.Path == "/v1/verify" {
		var proof codexadapter.Response
		if err := codexadapter.Decode(r.Body, &proof, limit); err != nil {
			fail(w, http.StatusBadRequest, "INVALID_JSON")
			return
		}
		if err := codexadapter.Verify(proof); err != nil {
			fail(w, http.StatusUnprocessableEntity, "PROOF_REJECTED")
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"verified": true, "scope": scope, "dispatched": proof.Evidence.Dispatched,
			"evidence_hash": proof.EvidenceHash,
		})
		return
	}
	var action codexadapter.Action
	if err := codexadapter.Decode(r.Body, &action, limit); err != nil {
		fail(w, http.StatusBadRequest, "INVALID_JSON")
		return
	}
	if !actionIDPattern.MatchString(action.ActionID) {
		fail(w, http.StatusBadRequest, "INVALID_ACTION_ID")
		return
	}
	if status, code := a.reserve(action.ActionID); status != 0 {
		fail(w, status, code)
		return
	}
	proof, err := a.execute(r.Context(), action)
	if err != nil {
		fail(w, http.StatusUnprocessableEntity, "ACTION_REJECTED_OR_UNCERTAIN")
		return
	}
	writeJSON(w, http.StatusOK, proof)
}

func server(addr string, handler http.Handler) *http.Server {
	return &http.Server{
		Addr: addr, Handler: handler, ReadHeaderTimeout: 3 * time.Second,
		ReadTimeout: 5 * time.Second, WriteTimeout: 10 * time.Second,
		IdleTimeout: 30 * time.Second, MaxHeaderBytes: 8 << 10,
	}
}

func listenAllowed(addr string, container bool) bool {
	host, port, err := net.SplitHostPort(addr)
	if err != nil || port == "" {
		return false
	}
	ip := net.ParseIP(host)
	if ip != nil && ip.IsLoopback() {
		return true
	}
	return container && host == "0.0.0.0"
}

func serve(addr string, container bool) error {
	if !listenAllowed(addr, container) {
		return errors.New("use a loopback listen address; 0.0.0.0 requires explicit -container")
	}
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		return errors.New("could not bind local preview listener")
	}
	srv := server(addr, newAPI())
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	done := make(chan struct{})
	go func() {
		select {
		case <-ctx.Done():
			shutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			if srv.Shutdown(shutdown) != nil {
				_ = srv.Close()
			}
		case <-done:
		}
	}()
	defer close(done)
	log.Print("developer preview: local hashing only; no authenticated identity or live provider")
	err = srv.Serve(listener)
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}

func previewURL(raw string) (string, error) {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "http" || !localHost(u.Hostname()) || u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Path != "" && u.Path != "/") {
		return "", errors.New("demo URL must be a loopback HTTP origin or the Compose service rail")
	}
	return strings.TrimSuffix(u.String(), "/"), nil
}

func previewClient() *http.Client {
	return &http.Client{
		Timeout: 5 * time.Second, Transport: &http.Transport{Proxy: nil},
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
}

func exchange(client *http.Client, method, endpoint string, value any) (int, []byte, error) {
	var body []byte
	var err error
	if value != nil {
		body, err = json.Marshal(value)
		if err != nil {
			return 0, nil, err
		}
	}
	req, err := http.NewRequest(method, endpoint, bytes.NewReader(body))
	if err != nil {
		return 0, nil, errors.New("could not construct local request")
	}
	if value != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	response, err := client.Do(req)
	if err != nil {
		return 0, nil, errors.New("local preview request failed; no automatic retry")
	}
	defer response.Body.Close()
	payload, err := io.ReadAll(io.LimitReader(response.Body, 4*codexadapter.MaxInputBytes+1))
	if err != nil || len(payload) > 4*codexadapter.MaxInputBytes {
		return 0, nil, errors.New("invalid local response size or read")
	}
	return response.StatusCode, payload, nil
}

func healthcheck(base string) error {
	client := previewClient()
	defer client.CloseIdleConnections()
	status, body, err := exchange(client, http.MethodGet, base+"/healthz", nil)
	var health struct {
		OK    bool   `json:"ok"`
		Mode  string `json:"mode"`
		Scope string `json:"scope"`
	}
	if err != nil || status != http.StatusOK || json.Unmarshal(body, &health) != nil || !health.OK || health.Mode != "developer_preview" || health.Scope != scope {
		return errors.New("developer preview health check failed")
	}
	return nil
}

func demo(base string, output io.Writer) error {
	client := previewClient()
	defer client.CloseIdleConnections()
	var random [16]byte
	if _, err := rand.Read(random[:]); err != nil {
		return err
	}
	id := hex.EncodeToString(random[:])
	text := "Hello from a third-party agent"
	noEffects := false
	action := codexadapter.Action{
		Protocol: codexadapter.ActionProtocol, RequestID: "request-" + id,
		ActionID: "demo-" + id, Kind: "hash_text", Text: &text, SideEffect: &noEffects,
	}
	status, body, err := exchange(client, http.MethodPost, base+"/v1/actions", action)
	if err != nil || status != http.StatusOK {
		return errors.New("demo action failed; not retried")
	}
	var proof codexadapter.Response
	if err := codexadapter.Decode(bytes.NewReader(body), &proof, 4*codexadapter.MaxInputBytes); err != nil {
		return err
	}
	if err := codexadapter.Verify(proof); err != nil {
		return err
	}
	digest := sha256.Sum256([]byte(text))
	if !proof.Evidence.Dispatched || proof.Evidence.Receipt == nil || proof.Evidence.Packet.ActionID != action.ActionID || proof.Evidence.Request.RequestID != action.RequestID || proof.Evidence.Result["sha256"] != hex.EncodeToString(digest[:]) {
		return errors.New("demo result is not bound to the submitted local action")
	}
	status, body, err = exchange(client, http.MethodPost, base+"/v1/verify", proof)
	var checked struct {
		Verified bool   `json:"verified"`
		Scope    string `json:"scope"`
	}
	if err != nil || status != http.StatusOK || json.Unmarshal(body, &checked) != nil || !checked.Verified || checked.Scope != scope {
		return errors.New("separate receipt verification failed")
	}
	// Deliberate negative test, not an automatic retry after uncertainty.
	status, _, err = exchange(client, http.MethodPost, base+"/v1/actions", action)
	if err != nil || status != http.StatusConflict {
		return errors.New("same-instance duplicate was not refused")
	}
	proof.EvidenceHash = "tampered"
	status, _, err = exchange(client, http.MethodPost, base+"/v1/verify", proof)
	if err != nil || status != http.StatusUnprocessableEntity {
		return errors.New("tampered proof was not refused")
	}
	// An external action can be described, but this service has no executor for it.
	yesEffects, description := true, "negative test: external action must not execute"
	action.ActionID += "-blocked"
	action.RequestID += "-blocked"
	action.Kind, action.Text, action.Description, action.SideEffect = "external_action", nil, &description, &yesEffects
	status, body, err = exchange(client, http.MethodPost, base+"/v1/actions", action)
	if err != nil || status != http.StatusOK {
		return errors.New("approval-only negative test failed")
	}
	var blocked codexadapter.Response
	if err := codexadapter.Decode(bytes.NewReader(body), &blocked, 4*codexadapter.MaxInputBytes); err != nil {
		return err
	}
	if codexadapter.Verify(blocked) != nil || blocked.Evidence.Dispatched || blocked.Evidence.Receipt != nil || blocked.Evidence.Gate.Disposition != "REQUIRE_APPROVAL" {
		return errors.New("external action crossed the local-only boundary")
	}
	return json.NewEncoder(output).Encode(map[string]any{
		"demo": "PASS", "scope": scope, "live_provider": false,
		"checks": []string{"local_hash_and_receipt", "separate_verification", "duplicate_refused", "tampering_refused", "external_action_not_dispatched"},
	})
}

func run(args []string, output io.Writer) error {
	if len(args) == 0 {
		return errors.New("usage: liminal-rail serve|demo|healthcheck [flags]")
	}
	flags := flag.NewFlagSet(args[0], flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	if args[0] == "serve" {
		addr := flags.String("listen", "127.0.0.1:8788", "loopback listener")
		container := flags.Bool("container", false, "permit 0.0.0.0 only inside the local Compose deployment")
		if err := flags.Parse(args[1:]); err != nil || flags.NArg() != 0 {
			return errors.New("invalid serve arguments")
		}
		return serve(*addr, *container)
	}
	if args[0] != "demo" && args[0] != "healthcheck" {
		return errors.New("usage: liminal-rail serve|demo|healthcheck [flags]")
	}
	address := flags.String("url", defaultURL, "local preview origin")
	if err := flags.Parse(args[1:]); err != nil || flags.NArg() != 0 {
		return errors.New("invalid client arguments")
	}
	base, err := previewURL(*address)
	if err != nil {
		return err
	}
	if args[0] == "healthcheck" {
		return healthcheck(base)
	}
	return demo(base, output)
}

func main() {
	if err := run(os.Args[1:], os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, "liminal-rail:", err)
		os.Exit(1)
	}
}
