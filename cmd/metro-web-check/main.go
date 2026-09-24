// metro-web-check independently checks the bounded, read-only Metro Web 007
// demonstration. It does not trust a publisher's claim of verification or
// use the publisher's routing and receipt verification implementation.
package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"mime"
	"net"
	"net/http"
	"net/url"
	"os"
	"reflect"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

const (
	siteProtocol    = "metro.web.site.v0.1"
	assetProtocol   = "metro.web.site.asset.v0.1"
	checkProtocol   = "metro.web.independent-check.v0.1"
	scope           = "resource.read"
	assetChunkBytes = 64 << 10
	maxAssetBytes   = 8 << 20
	maxTotalBytes   = 16 << 20
	maxSteps        = 8
	maxMapBytes     = 16 << 10
	maxRunBytes     = 128 << 10
)

type node struct {
	ID         string `json:"id"`
	Label      string `json:"label"`
	ResourceID string `json:"resource_id,omitempty"`
}

type edge struct {
	ID         string `json:"id"`
	From       string `json:"from"`
	To         string `json:"to"`
	Action     string `json:"action"`
	Scope      string `json:"scope"`
	SideEffect bool   `json:"side_effect"`
	Resource   string `json:"resource"`
}

type resource struct {
	ID       string `json:"id"`
	Label    string `json:"label"`
	Manifest string `json:"manifest"`
	SHA256   string `json:"sha256"`
	Size     int    `json:"size_bytes"`
}

// The field order is the wire order used by Go's canonical json.Marshal in
// the publisher's map_hash. This package computes it without calling Metro.
type siteMap struct {
	Protocol  string     `json:"protocol"`
	Start     string     `json:"start"`
	Nodes     []node     `json:"nodes"`
	Edges     []edge     `json:"edges"`
	Resources []resource `json:"resources"`
}

type manifest struct {
	Protocol  string   `json:"protocol"`
	ID        string   `json:"id"`
	MediaType string   `json:"media_type"`
	Size      int      `json:"size_bytes"`
	SHA256    string   `json:"sha256"`
	Version   string   `json:"version"`
	ChunkSize int      `json:"chunk_size_bytes"`
	Chunks    []string `json:"chunk_sha256"`
	Href      string   `json:"href"`
}

type action struct {
	Kind   string         `json:"kind"`
	Inputs map[string]any `json:"inputs"`
}

type constraints struct {
	TimeoutMS  int  `json:"timeout_ms,omitempty"`
	SideEffect bool `json:"side_effect"`
}

type packet struct {
	Protocol           string      `json:"protocol"`
	ActionID           string      `json:"action_id"`
	SourceAgent        string      `json:"source_agent"`
	CreatedAt          string      `json:"created_at"`
	Goal               string      `json:"goal"`
	Action             action      `json:"action"`
	AllowedTargets     []string    `json:"allowed_targets"`
	ContextRefs        []string    `json:"context_refs,omitempty"`
	Constraints        constraints `json:"constraints,omitempty"`
	PreviousReceiptRef string      `json:"previous_receipt_ref,omitempty"`
}

type candidate struct {
	Target string  `json:"target"`
	Score  float64 `json:"score,omitempty"`
}

type route struct {
	Protocol       string      `json:"protocol"`
	RouteID        string      `json:"route_id"`
	ActionID       string      `json:"action_id"`
	RouterID       string      `json:"router_id"`
	DecisionMode   string      `json:"decision_mode"`
	SelectedTarget string      `json:"selected_target"`
	Candidates     []candidate `json:"candidates,omitempty"`
	Confidence     float64     `json:"confidence,omitempty"`
	PolicyRef      string      `json:"policy_ref,omitempty"`
	DecidedAt      string      `json:"decided_at"`
}

type receipt struct {
	Protocol      string `json:"protocol"`
	ReceiptID     string `json:"receipt_id"`
	ActionID      string `json:"action_id"`
	RouteID       string `json:"route_id"`
	ExecutorID    string `json:"executor_id"`
	Status        string `json:"status"`
	HashAlgorithm string `json:"hash_algorithm"`
	InputHash     string `json:"input_hash"`
	ResultHash    string `json:"result_hash"`
	ResultRef     string `json:"result_ref"`
	StartedAt     string `json:"started_at"`
	CompletedAt   string `json:"completed_at"`
}

type event struct {
	Edge    edge           `json:"edge"`
	Status  string         `json:"status"`
	Packet  packet         `json:"packet"`
	Route   route          `json:"route"`
	Receipt *receipt       `json:"receipt"`
	Result  map[string]any `json:"result"`
}

type runResource struct {
	ID     string `json:"id"`
	SHA256 string `json:"sha256"`
	Size   int    `json:"size_bytes"`
}

type runResult struct {
	Status        string        `json:"status"`
	Reason        string        `json:"reason"`
	Mode          string        `json:"mode"`
	Target        string        `json:"target"`
	Map           siteMap       `json:"map"`
	MapHash       string        `json:"map_hash"`
	PlannedSteps  int           `json:"planned_steps"`
	FreshReads    int           `json:"fresh_reads"`
	MemoryEntries int           `json:"memory_entries"`
	Learned       bool          `json:"learned"`
	Events        []event       `json:"events"`
	Resources     []runResource `json:"resources"`
}

type checkedStep struct {
	EdgeID     string `json:"edge_id"`
	ResourceID string `json:"resource_id"`
	SHA256     string `json:"sha256"`
	Size       int    `json:"size_bytes"`
	ReceiptID  string `json:"receipt_id"`
}

type checkResult struct {
	Protocol                  string        `json:"protocol"`
	Status                    string        `json:"status"`
	Origin                    string        `json:"origin"`
	Target                    string        `json:"target"`
	MapHash                   string        `json:"map_hash"`
	RunFreshnessVerified      bool          `json:"run_freshness_verified"`
	PublisherIdentityVerified bool          `json:"publisher_identity_verified"`
	Route                     []checkedStep `json:"route"`
}

// An uncertain failure means an incomplete observation, not evidence that a
// publisher violated the bounded contract. The CLI never retries such a read.
type uncertainFailure struct{ cause error }

func (e *uncertainFailure) Error() string { return e.cause.Error() }
func (e *uncertainFailure) Unwrap() error { return e.cause }
func uncertain(err error) error           { return &uncertainFailure{cause: err} }

func failureStatus(err error) string {
	var unknown *uncertainFailure
	if errors.As(err, &unknown) {
		return "UNKNOWN"
	}
	return "REJECTED"
}

// exact rejects missing, extra, or null object members; strictJSON separately
// detects duplicates at every depth before decoding any typed value.
func exact(raw []byte, required []string, optional ...string) error {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil || fields == nil {
		return errors.New("expected JSON object")
	}
	allowed := make(map[string]bool, len(required)+len(optional))
	for _, key := range required {
		allowed[key] = true
		v, ok := fields[key]
		if !ok || bytes.Equal(bytes.TrimSpace(v), []byte("null")) {
			return fmt.Errorf("missing or null %s", key)
		}
	}
	for _, key := range optional {
		allowed[key] = true
	}
	for key, v := range fields {
		if !allowed[key] || bytes.Equal(bytes.TrimSpace(v), []byte("null")) {
			return fmt.Errorf("unexpected or null %s", key)
		}
	}
	return nil
}

func (v *node) UnmarshalJSON(b []byte) error {
	if err := exact(b, []string{"id", "label"}, "resource_id"); err != nil {
		return err
	}
	type plain node
	return json.Unmarshal(b, (*plain)(v))
}
func (v *edge) UnmarshalJSON(b []byte) error {
	if err := exact(b, []string{"id", "from", "to", "action", "scope", "side_effect", "resource"}); err != nil {
		return err
	}
	type plain edge
	return json.Unmarshal(b, (*plain)(v))
}
func (v *resource) UnmarshalJSON(b []byte) error {
	if err := exact(b, []string{"id", "label", "manifest", "sha256", "size_bytes"}); err != nil {
		return err
	}
	type plain resource
	return json.Unmarshal(b, (*plain)(v))
}
func (v *siteMap) UnmarshalJSON(b []byte) error {
	if err := exact(b, []string{"protocol", "start", "nodes", "edges", "resources"}); err != nil {
		return err
	}
	type plain siteMap
	return json.Unmarshal(b, (*plain)(v))
}
func (v *manifest) UnmarshalJSON(b []byte) error {
	if err := exact(b, []string{"protocol", "id", "media_type", "size_bytes", "sha256", "version", "chunk_size_bytes", "chunk_sha256", "href"}); err != nil {
		return err
	}
	type plain manifest
	return json.Unmarshal(b, (*plain)(v))
}
func (v *action) UnmarshalJSON(b []byte) error {
	if err := exact(b, []string{"kind", "inputs"}); err != nil {
		return err
	}
	type plain action
	return json.Unmarshal(b, (*plain)(v))
}
func (v *constraints) UnmarshalJSON(b []byte) error {
	if err := exact(b, []string{"timeout_ms", "side_effect"}); err != nil {
		return err
	}
	type plain constraints
	return json.Unmarshal(b, (*plain)(v))
}
func (v *packet) UnmarshalJSON(b []byte) error {
	if err := exact(b, []string{"protocol", "action_id", "source_agent", "created_at", "goal", "action", "allowed_targets", "constraints"}, "context_refs", "previous_receipt_ref"); err != nil {
		return err
	}
	type plain packet
	return json.Unmarshal(b, (*plain)(v))
}
func (v *candidate) UnmarshalJSON(b []byte) error {
	if err := exact(b, []string{"target", "score"}); err != nil {
		return err
	}
	type plain candidate
	return json.Unmarshal(b, (*plain)(v))
}
func (v *route) UnmarshalJSON(b []byte) error {
	if err := exact(b, []string{"protocol", "route_id", "action_id", "router_id", "decision_mode", "selected_target", "candidates", "confidence", "policy_ref", "decided_at"}); err != nil {
		return err
	}
	type plain route
	return json.Unmarshal(b, (*plain)(v))
}
func (v *receipt) UnmarshalJSON(b []byte) error {
	if err := exact(b, []string{"protocol", "receipt_id", "action_id", "route_id", "executor_id", "status", "hash_algorithm", "input_hash", "result_hash", "result_ref", "started_at", "completed_at"}); err != nil {
		return err
	}
	type plain receipt
	return json.Unmarshal(b, (*plain)(v))
}
func (v *event) UnmarshalJSON(b []byte) error {
	if err := exact(b, []string{"edge", "status", "packet", "route", "receipt", "result"}); err != nil {
		return err
	}
	type plain event
	return json.Unmarshal(b, (*plain)(v))
}
func (v *runResource) UnmarshalJSON(b []byte) error {
	if err := exact(b, []string{"id", "sha256", "size_bytes"}); err != nil {
		return err
	}
	type plain runResource
	return json.Unmarshal(b, (*plain)(v))
}
func (v *runResult) UnmarshalJSON(b []byte) error {
	if err := exact(b, []string{"status", "reason", "mode", "target", "map", "map_hash", "planned_steps", "fresh_reads", "memory_entries", "learned", "events", "resources"}); err != nil {
		return err
	}
	type plain runResult
	return json.Unmarshal(b, (*plain)(v))
}

func strictJSON(raw []byte, limit int, out any) error {
	if len(raw) == 0 || len(raw) > limit || !utf8.Valid(raw) {
		return errors.New("invalid JSON size or UTF-8")
	}
	unique := json.NewDecoder(bytes.NewReader(raw))
	if err := uniqueValue(unique, 0); err != nil {
		return fmt.Errorf("ambiguous JSON: %w", err)
	}
	if _, err := unique.Token(); err != io.EOF {
		return errors.New("trailing JSON")
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	dec.UseNumber()
	if err := dec.Decode(out); err != nil {
		return fmt.Errorf("invalid JSON shape: %w", err)
	}
	var extra any
	if err := dec.Decode(&extra); err != io.EOF {
		return errors.New("trailing JSON")
	}
	return nil
}

func uniqueValue(dec *json.Decoder, depth int) error {
	if depth > 16 {
		return errors.New("JSON too deep")
	}
	t, err := dec.Token()
	if err != nil {
		return err
	}
	d, ok := t.(json.Delim)
	if !ok {
		return nil
	}
	var close json.Delim
	switch d {
	case '{':
		close = '}'
		seen := map[string]bool{}
		for dec.More() {
			keyToken, err := dec.Token()
			if err != nil {
				return err
			}
			key, ok := keyToken.(string)
			if !ok || seen[key] {
				return errors.New("duplicate JSON member")
			}
			seen[key] = true
			if err := uniqueValue(dec, depth+1); err != nil {
				return err
			}
		}
	case '[':
		close = ']'
		for dec.More() {
			if err := uniqueValue(dec, depth+1); err != nil {
				return err
			}
		}
	default:
		return errors.New("unexpected JSON delimiter")
	}
	end, err := dec.Token()
	if err != nil || end != close {
		return errors.New("unclosed JSON value")
	}
	return nil
}

func validID(s string) bool {
	if len(s) < 1 || len(s) > 80 {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		alnum := c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9'
		if !alnum && (i == 0 || c != '_' && c != '-' && c != '.') {
			return false
		}
	}
	return true
}
func validLabel(s string) bool { return strings.TrimSpace(s) != "" && len(s) <= 120 }
func validHash(s string) bool {
	if len(s) != 64 || strings.ToLower(s) != s {
		return false
	}
	_, err := hex.DecodeString(s)
	return err == nil
}
func validActionID(s string) bool {
	if len(s) != 32 || s != strings.ToLower(s) {
		return false
	}
	_, err := hex.DecodeString(s)
	return err == nil
}
func hashBytes(b []byte) string { sum := sha256.Sum256(b); return hex.EncodeToString(sum[:]) }
func hashJSON(v any) string     { raw, _ := json.Marshal(v); return hashBytes(raw) }

func validateMap(m siteMap) error {
	if m.Protocol != siteProtocol || !validID(m.Start) || len(m.Resources) < 2 || len(m.Resources) > 8 || len(m.Nodes) < 3 || len(m.Nodes) > 16 || len(m.Edges) < 2 || len(m.Edges) > 32 {
		return errors.New("invalid map bounds or protocol")
	}
	resources := map[string]resource{}
	total := 0
	for _, r := range m.Resources {
		if !validID(r.ID) || !validLabel(r.Label) || !validHash(r.SHA256) || r.Size < 0 || r.Size > maxAssetBytes || r.Manifest != "/api/site/assets/"+r.ID {
			return errors.New("invalid resource")
		}
		if _, exists := resources[r.ID]; exists {
			return errors.New("duplicate resource")
		}
		resources[r.ID] = r
		total += r.Size
		if total > maxTotalBytes {
			return errors.New("aggregate resource limit")
		}
	}
	nodes := map[string]node{}
	for _, n := range m.Nodes {
		if !validID(n.ID) || !validLabel(n.Label) {
			return errors.New("invalid node")
		}
		if _, exists := nodes[n.ID]; exists {
			return errors.New("duplicate node")
		}
		if n.ID == m.Start {
			if n.ResourceID != "" {
				return errors.New("start carries a resource")
			}
		} else if _, ok := resources[n.ResourceID]; !ok {
			return errors.New("node refers to missing resource")
		}
		nodes[n.ID] = n
	}
	if _, ok := nodes[m.Start]; !ok {
		return errors.New("missing start node")
	}
	edges := map[string]bool{}
	bound := map[string]bool{}
	for _, e := range m.Edges {
		_, from := nodes[e.From]
		toNode, to := nodes[e.To]
		_, found := resources[e.Resource]
		if !validID(e.ID) || !from || !to || !found || e.To == m.Start || e.Action != "open_resource" || e.Scope != scope || e.SideEffect || toNode.ResourceID != e.Resource {
			return errors.New("invalid transition")
		}
		if edges[e.ID] {
			return errors.New("duplicate transition")
		}
		edges[e.ID], bound[e.Resource] = true, true
	}
	for id := range resources {
		if !bound[id] {
			return errors.New("unreachable resource binding")
		}
	}
	return nil
}

func plan(m siteMap, target string) ([]edge, error) {
	if !validID(target) {
		return nil, errors.New("invalid target ID")
	}
	resources := map[string]bool{}
	for _, r := range m.Resources {
		resources[r.ID] = true
	}
	if !resources[target] {
		return nil, errors.New("target not published")
	}
	type candidatePath struct {
		node string
		path []edge
	}
	queue := []candidatePath{{node: m.Start}}
	visited := map[string]bool{m.Start: true}
	for len(queue) > 0 {
		c := queue[0]
		queue = queue[1:]
		if len(c.path) >= maxSteps {
			continue
		}
		for _, e := range m.Edges {
			if e.From != c.node || visited[e.To] {
				continue
			}
			path := append(append([]edge(nil), c.path...), e)
			if e.Resource == target {
				return path, nil
			}
			visited[e.To] = true
			queue = append(queue, candidatePath{node: e.To, path: path})
		}
	}
	return nil, errors.New("no path within eight transitions")
}

func validateManifest(m manifest, r resource) error {
	if m.Protocol != assetProtocol || m.ID != r.ID || m.MediaType != "application/octet-stream" || m.Size != r.Size || m.SHA256 != r.SHA256 || m.Version != "sha256:"+r.SHA256 || m.ChunkSize != assetChunkBytes || m.Href != "/api/site/assets/"+r.ID+"/full" || m.Chunks == nil || len(m.Chunks) != (r.Size+assetChunkBytes-1)/assetChunkBytes {
		return errors.New("manifest does not bind map resource")
	}
	for _, h := range m.Chunks {
		if !validHash(h) {
			return errors.New("invalid chunk hash")
		}
	}
	if r.Size == 0 && r.SHA256 != hashBytes(nil) {
		return errors.New("empty resource hash mismatch")
	}
	return nil
}

type checker struct {
	origin string
	client *http.Client
}

func newChecker(raw string) (*checker, error) {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" || u.Hostname() == "" || u.User != nil || u.Opaque != "" || u.RawQuery != "" || u.ForceQuery || u.RawPath != "" || (u.Path != "" && u.Path != "/") || strings.Contains(raw, "#") || strings.ContainsAny(u.Host, "\\\r\n") || strings.Contains(u.Hostname(), "%") {
		return nil, errors.New("origin must be a bare HTTP(S) origin without credentials, path, query, or fragment")
	}
	port := u.Port()
	if port != "" {
		n, err := strconv.Atoi(port)
		if err != nil || n < 1 || n > 65535 {
			return nil, errors.New("invalid origin port")
		}
		port = strconv.Itoa(n)
	} else if strings.HasSuffix(u.Host, ":") {
		return nil, errors.New("invalid origin port")
	}
	if u.Scheme != "https" {
		ip := net.ParseIP(u.Hostname())
		if u.Scheme != "http" || ip == nil || !ip.IsLoopback() {
			return nil, errors.New("HTTPS required outside literal loopback IPs")
		}
	}
	host := strings.ToLower(u.Hostname())
	if ip := net.ParseIP(host); ip != nil {
		host = ip.String()
	}
	if port != "" {
		u.Host = net.JoinHostPort(host, port)
	} else if strings.Contains(host, ":") {
		u.Host = "[" + host + "]"
	} else {
		u.Host = host
	}
	transport := &http.Transport{Proxy: nil, DialContext: (&net.Dialer{Timeout: 2 * time.Second}).DialContext, TLSHandshakeTimeout: 2 * time.Second, ResponseHeaderTimeout: 10 * time.Second, MaxResponseHeaderBytes: 16 << 10, DisableCompression: true, DisableKeepAlives: true}
	return &checker{origin: u.Scheme + "://" + u.Host, client: &http.Client{Transport: transport, Timeout: 15 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}, nil
}

func (c *checker) fetch(ctx context.Context, method, path, contentType string, body []byte, limit int) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, method, c.origin+path, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", contentType)
	req.Header.Set("Cache-Control", "no-cache")
	if method == http.MethodGet {
		req.Header.Set("X-Metro-Resource-Read", "1")
	}
	if method == http.MethodPost {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.client.Do(req)
	if err != nil {
		return nil, uncertain(fmt.Errorf("%s %s: %w", method, path, err))
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		failure := fmt.Errorf("%s %s returned HTTP %d", method, path, resp.StatusCode)
		if (resp.StatusCode >= 500 && resp.StatusCode != http.StatusLoopDetected) || resp.StatusCode == http.StatusRequestTimeout || resp.StatusCode == http.StatusTooManyRequests {
			return nil, uncertain(failure)
		}
		return nil, failure
	}
	media, params, err := mime.ParseMediaType(resp.Header.Get("Content-Type"))
	if err != nil || media != contentType || (media == "application/octet-stream" && len(params) != 0) || resp.Header.Get("Content-Encoding") != "" || resp.Header.Get("Content-Range") != "" || resp.ContentLength > int64(limit) {
		return nil, errors.New("unexpected response type, encoding, range, or size")
	}
	if contentType == "application/octet-stream" {
		if resp.Header.Get("X-Metro-Asset-Whole-Verified") != "true" || resp.Header.Get("Content-Disposition") != `attachment; filename="metro-site-asset.bin"` {
			return nil, errors.New("binary attachment contract missing")
		}
	}
	raw, err := io.ReadAll(io.LimitReader(resp.Body, int64(limit)+1))
	if err != nil {
		return nil, uncertain(fmt.Errorf("%s %s body incomplete: %w", method, path, err))
	}
	if ctx.Err() != nil {
		return nil, uncertain(ctx.Err())
	}
	if len(raw) > limit {
		return nil, errors.New("response exceeded bound")
	}
	return raw, nil
}

func (c *checker) mapAt(ctx context.Context) (siteMap, error) {
	raw, err := c.fetch(ctx, http.MethodGet, "/api/site", "application/json", nil, maxMapBytes)
	if err != nil {
		return siteMap{}, err
	}
	var m siteMap
	if err := strictJSON(raw, maxMapBytes, &m); err != nil {
		return m, err
	}
	return m, validateMap(m)
}

func (c *checker) verifyBytes(ctx context.Context, r resource) error {
	path := "/api/site/assets/" + r.ID
	raw, err := c.fetch(ctx, http.MethodGet, path, "application/json", nil, 16<<10)
	if err != nil {
		return err
	}
	var passport manifest
	if err := strictJSON(raw, 16<<10, &passport); err != nil {
		return err
	}
	if err := validateManifest(passport, r); err != nil {
		return err
	}
	raw, err = c.fetch(ctx, http.MethodGet, path+"/full?sha256="+r.SHA256, "application/octet-stream", nil, r.Size)
	if err != nil {
		return err
	}
	if len(raw) != r.Size || hashBytes(raw) != r.SHA256 {
		return errors.New("whole-file hash or size mismatch")
	}
	for i, expected := range passport.Chunks {
		start := i * assetChunkBytes
		end := start + assetChunkBytes
		if end > len(raw) {
			end = len(raw)
		}
		if hashBytes(raw[start:end]) != expected {
			return fmt.Errorf("chunk %d hash mismatch", i)
		}
	}
	return nil
}

func jsonMapEquals(got map[string]any, want map[string]any) bool {
	return got != nil && len(got) == len(want) && reflect.DeepEqual(got, want)
}

func validTime(s string) bool { _, err := time.Parse(time.RFC3339Nano, s); return err == nil }

func verifyEvent(ev event, e edge, r resource, target, mapHash string, used map[string]bool) error {
	if !reflect.DeepEqual(ev.Edge, e) || ev.Status != "CONFIRMED_LOCAL" || ev.Receipt == nil {
		return errors.New("event transition or status mismatch")
	}
	p, rt, rc := ev.Packet, ev.Route, *ev.Receipt
	if !validActionID(p.ActionID) || used[p.ActionID] {
		return errors.New("missing, reused, or malformed action ID")
	}
	used[p.ActionID] = true
	if p.Protocol != "metro.packet.v0.1" || p.SourceAgent != "metro-web-demo" || !validTime(p.CreatedAt) || p.Goal != "open resource "+target || p.Action.Kind != "open_resource" || len(p.AllowedTargets) != 1 || p.AllowedTargets[0] != "metro-site-reader" || len(p.ContextRefs) != 0 || p.PreviousReceiptRef != "" || p.Constraints.TimeoutMS != 10000 || p.Constraints.SideEffect {
		return errors.New("packet contract mismatch")
	}
	wantInputs := map[string]any{"edge": e.ID, "from": e.From, "map_hash": mapHash, "resource_id": r.ID, "resource_sha256": r.SHA256, "scope": scope, "to": e.To}
	if !jsonMapEquals(p.Action.Inputs, wantInputs) {
		return errors.New("packet action inputs mismatch")
	}
	if rt.Protocol != "metro.route.v0.1" || rt.RouteID != "route-"+p.ActionID || rt.ActionID != p.ActionID || rt.RouterID != "metro-web-demo" || rt.DecisionMode != "deterministic" || rt.SelectedTarget != "metro-site-reader" || len(rt.Candidates) != 1 || rt.Candidates[0].Target != "metro-site-reader" || rt.Candidates[0].Score != 1 || rt.Confidence != 1 || rt.PolicyRef != "policy://demo/route-by-action-kind" || !validTime(rt.DecidedAt) {
		return errors.New("route binding or policy mismatch")
	}
	wantResult := map[string]any{"map_hash": mapHash, "resource_id": r.ID, "sha256": r.SHA256, "size_bytes": float64(r.Size), "state": e.To}
	if !jsonMapEquals(ev.Result, wantResult) {
		return errors.New("event result mismatch")
	}
	if rc.Protocol != "metro.receipt.v0.1" || rc.ReceiptID != "receipt-"+p.ActionID || rc.ActionID != p.ActionID || rc.RouteID != rt.RouteID || rc.ExecutorID != rt.SelectedTarget || rc.Status != "SUCCEEDED" || rc.HashAlgorithm != "sha256" || rc.InputHash != hashJSON(p.Action.Inputs) || rc.ResultHash != hashJSON(ev.Result) || rc.ResultRef != "sha256:"+r.SHA256 || !validTime(rc.StartedAt) || !validTime(rc.CompletedAt) {
		return errors.New("receipt binding, status, or digest mismatch")
	}
	start, _ := time.Parse(time.RFC3339Nano, rc.StartedAt)
	finish, _ := time.Parse(time.RFC3339Nano, rc.CompletedAt)
	if finish.Before(start) {
		return errors.New("receipt time reversed")
	}
	return nil
}

func (c *checker) check(ctx context.Context, target string) (checkResult, error) {
	out := checkResult{Protocol: checkProtocol, Status: "VERIFIED_BYTES_AND_TRANSCRIPT", Origin: c.origin, Target: target, Route: []checkedStep{}}
	if !validID(target) {
		return checkResult{}, errors.New("invalid target ID")
	}
	m, err := c.mapAt(ctx)
	if err != nil {
		return checkResult{}, fmt.Errorf("initial map: %w", err)
	}
	path, err := plan(m, target)
	if err != nil {
		return checkResult{}, err
	}
	resources := map[string]resource{}
	for _, r := range m.Resources {
		resources[r.ID] = r
	}
	selectedBytes := 0
	for _, e := range path {
		selectedBytes += resources[e.Resource].Size
		if selectedBytes > maxTotalBytes {
			return checkResult{}, errors.New("route aggregate byte limit exceeded")
		}
		if err := c.verifyBytes(ctx, resources[e.Resource]); err != nil {
			return checkResult{}, fmt.Errorf("resource %s: %w", e.Resource, err)
		}
	}
	mapHash := hashJSON(m)
	body, _ := json.Marshal(map[string]string{"target": target})
	raw, err := c.fetch(ctx, http.MethodPost, "/api/site/run", "application/json", body, maxRunBytes)
	if err != nil {
		return checkResult{}, err
	}
	var run runResult
	if err := strictJSON(raw, maxRunBytes, &run); err != nil {
		return checkResult{}, fmt.Errorf("run response: %w", err)
	}
	if run.Status == "UNKNOWN" {
		return checkResult{}, uncertain(errors.New("publisher run outcome unknown"))
	}
	if run.Status != "CONFIRMED_LOCAL" || run.Target != target || !validLabel(run.Reason) || (run.Mode != "graph" && run.Mode != "memory_revalidated") || run.MapHash != mapHash || !reflect.DeepEqual(run.Map, m) || run.PlannedSteps != len(path) || run.FreshReads != len(path) || !run.Learned || run.MemoryEntries < 1 || run.MemoryEntries > 16 || len(run.Events) != len(path) || len(run.Resources) != len(path) {
		return checkResult{}, errors.New("run status, map, route count, or fresh-read claim mismatch")
	}
	used := map[string]bool{}
	for i, e := range path {
		r := resources[e.Resource]
		if run.Resources[i] != (runResource{ID: r.ID, SHA256: r.SHA256, Size: r.Size}) {
			return checkResult{}, fmt.Errorf("step %d resource mismatch", i+1)
		}
		if err := verifyEvent(run.Events[i], e, r, target, mapHash, used); err != nil {
			return checkResult{}, fmt.Errorf("step %d: %w", i+1, err)
		}
		out.Route = append(out.Route, checkedStep{EdgeID: e.ID, ResourceID: r.ID, SHA256: r.SHA256, Size: r.Size, ReceiptID: run.Events[i].Receipt.ReceiptID})
	}
	fresh, err := c.mapAt(ctx)
	if err != nil {
		return checkResult{}, fmt.Errorf("final map: %w", err)
	}
	if !reflect.DeepEqual(fresh, m) || hashJSON(fresh) != mapHash {
		return checkResult{}, errors.New("map changed during check")
	}
	out.MapHash = mapHash
	return out, nil
}

func runCLI(parent context.Context, args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("metro-web-check", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	origin := flags.String("origin", "", "fixed HTTP(S) publisher origin")
	target := flags.String("target", "", "resource ID to verify")
	if err := flags.Parse(args); err != nil || flags.NArg() != 0 || *origin == "" || *target == "" {
		_ = json.NewEncoder(stderr).Encode(map[string]string{"status": "REJECTED", "error": "usage: metro-web-check -origin URL -target ID"})
		return 2
	}
	c, err := newChecker(*origin)
	if err == nil {
		ctx, cancel := context.WithTimeout(parent, 90*time.Second)
		defer cancel()
		var result checkResult
		result, err = c.check(ctx, *target)
		if err == nil {
			var raw []byte
			raw, err = json.Marshal(result)
			if err == nil {
				raw = append(raw, '\n')
				written, writeErr := stdout.Write(raw)
				if writeErr == nil && written == len(raw) {
					return 0
				}
				if writeErr == nil {
					writeErr = io.ErrShortWrite
				}
				err = uncertain(fmt.Errorf("result output incomplete: %w", writeErr))
			} else {
				err = uncertain(fmt.Errorf("result encoding failed: %w", err))
			}
		}
	}
	_ = json.NewEncoder(stderr).Encode(map[string]string{"status": failureStatus(err), "error": err.Error()})
	return 1
}

func main() {
	os.Exit(runCLI(context.Background(), os.Args[1:], os.Stdout, os.Stderr))
}
