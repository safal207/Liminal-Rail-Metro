package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path/filepath"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/safal207/Liminal-Rail-Metro/internal/metro"
)

const (
	siteProtocol       = "metro.web.site.v0.1"
	siteAssetProtocol  = "metro.web.site.asset.v0.1"
	siteScope          = "resource.read"
	siteAction         = "open_resource"
	siteExecutor       = "metro-site-reader"
	maxSiteConfigBytes = 32 << 10
	maxSiteMapBytes    = 16 << 10
	maxSiteAggregate   = 16 << 20
	maxSiteSteps       = 8
	siteRunTimeout     = 10 * time.Second
)

// Paths occur only in the operator's local configuration. No public type
// contains one and a remote reader cannot select a file or network address.
type siteConfigResource struct {
	ID    string `json:"id"`
	Label string `json:"label"`
	Path  string `json:"path"`
}

type siteConfig struct {
	Protocol  string               `json:"protocol"`
	Start     string               `json:"start"`
	Nodes     []siteNode           `json:"nodes"`
	Edges     []siteEdge           `json:"edges"`
	Resources []siteConfigResource `json:"resources"`
}

type siteNode struct {
	ID         string `json:"id"`
	Label      string `json:"label"`
	ResourceID string `json:"resource_id,omitempty"`
}

type siteEdge struct {
	ID         string `json:"id"`
	From       string `json:"from"`
	To         string `json:"to"`
	Action     string `json:"action"`
	Scope      string `json:"scope"`
	SideEffect bool   `json:"side_effect"`
	Resource   string `json:"resource"`
}

type sitePublicResource struct {
	ID       string `json:"id"`
	Label    string `json:"label"`
	Manifest string `json:"manifest"`
	SHA256   string `json:"sha256"`
	Size     int    `json:"size_bytes"`
}

type siteMap struct {
	Protocol  string               `json:"protocol"`
	Start     string               `json:"start"`
	Nodes     []siteNode           `json:"nodes"`
	Edges     []siteEdge           `json:"edges"`
	Resources []sitePublicResource `json:"resources"`
}

type siteAssetManifest struct {
	Protocol  string   `json:"protocol"`
	ID        string   `json:"id"`
	MediaType string   `json:"media_type"`
	Size      int      `json:"size_bytes"`
	SHA256    string   `json:"sha256"`
	Version   string   `json:"version"`
	ChunkSize int      `json:"chunk_size_bytes"`
	ChunkHash []string `json:"chunk_sha256"`
	Href      string   `json:"href"`
}

type siteRunRequest struct {
	Target string `json:"target"`
}

type siteRunResource struct {
	ID     string `json:"id"`
	SHA256 string `json:"sha256"`
	Size   int    `json:"size_bytes"`
}

type siteEvent struct {
	Edge    siteEdge       `json:"edge"`
	Status  string         `json:"status"`
	Packet  metro.Packet   `json:"packet"`
	Route   metro.Route    `json:"route"`
	Receipt *metro.Receipt `json:"receipt,omitempty"`
	Result  map[string]any `json:"result,omitempty"`
}

type siteRunResult struct {
	Status        string            `json:"status"`
	Reason        string            `json:"reason"`
	Mode          string            `json:"mode"`
	Target        string            `json:"target"`
	Map           siteMap           `json:"map"`
	MapHash       string            `json:"map_hash"`
	PlannedSteps  int               `json:"planned_steps"`
	FreshReads    int               `json:"fresh_reads"`
	MemoryEntries int               `json:"memory_entries"`
	Learned       bool              `json:"learned"`
	Events        []siteEvent       `json:"events"`
	Resources     []siteRunResource `json:"resources"`
}

type siteService struct {
	remote *httpResourceSource
	config siteConfig
	files  map[string]*resourceSource
	gate   chan struct{}
	memory map[string][]string
}

func siteExactFields(raw []byte, fields ...string) error {
	var members map[string]json.RawMessage
	if json.Unmarshal(raw, &members) != nil || len(members) != len(fields) {
		return errRemoteInvalid
	}
	for _, name := range fields {
		value, ok := members[name]
		if !ok || bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
			return errRemoteInvalid
		}
	}
	return nil
}

func (e *siteEdge) UnmarshalJSON(raw []byte) error {
	if err := siteExactFields(raw, "id", "from", "to", "action", "scope", "side_effect", "resource"); err != nil {
		return err
	}
	type plain siteEdge
	return json.Unmarshal(raw, (*plain)(e))
}

func (r *siteConfigResource) UnmarshalJSON(raw []byte) error {
	if err := siteExactFields(raw, "id", "label", "path"); err != nil {
		return err
	}
	type plain siteConfigResource
	return json.Unmarshal(raw, (*plain)(r))
}

func (r *sitePublicResource) UnmarshalJSON(raw []byte) error {
	if err := siteExactFields(raw, "id", "label", "manifest", "sha256", "size_bytes"); err != nil {
		return err
	}
	type plain sitePublicResource
	return json.Unmarshal(raw, (*plain)(r))
}

func (m *siteAssetManifest) UnmarshalJSON(raw []byte) error {
	if err := siteExactFields(raw, "protocol", "id", "media_type", "size_bytes", "sha256", "version", "chunk_size_bytes", "chunk_sha256", "href"); err != nil {
		return err
	}
	type plain siteAssetManifest
	return json.Unmarshal(raw, (*plain)(m))
}

// strictSiteJSON rejects duplicate keys, unknown fields and trailing JSON.
// encoding/json alone otherwise accepts ambiguous duplicate object members.
func strictSiteJSON(raw []byte, dst any, limit int) error {
	if len(raw) == 0 || len(raw) > limit || !utf8.Valid(raw) {
		return errRemoteInvalid
	}
	check := json.NewDecoder(bytes.NewReader(raw))
	if err := siteUniqueValue(check, 0); err != nil {
		return errRemoteInvalid
	}
	if _, err := check.Token(); err != io.EOF {
		return errRemoteInvalid
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if dec.Decode(dst) != nil {
		return errRemoteInvalid
	}
	var trailing any
	if dec.Decode(&trailing) != io.EOF {
		return errRemoteInvalid
	}
	return nil
}

func siteUniqueValue(dec *json.Decoder, depth int) error {
	if depth > 12 {
		return errRemoteInvalid
	}
	token, err := dec.Token()
	if err != nil {
		return err
	}
	delim, ok := token.(json.Delim)
	if !ok {
		return nil
	}
	switch delim {
	case '{':
		seen := map[string]bool{}
		for dec.More() {
			keyToken, err := dec.Token()
			if err != nil {
				return err
			}
			key, ok := keyToken.(string)
			if !ok || seen[key] {
				return errRemoteInvalid
			}
			seen[key] = true
			if err := siteUniqueValue(dec, depth+1); err != nil {
				return err
			}
		}
	case '[':
		for dec.More() {
			if err := siteUniqueValue(dec, depth+1); err != nil {
				return err
			}
		}
	default:
		return errRemoteInvalid
	}
	end, err := dec.Token()
	if err != nil || end != json.Delim(map[json.Delim]rune{'{': '}', '[': ']'}[delim]) {
		return errRemoteInvalid
	}
	return nil
}

func siteLabel(s string) bool { return strings.TrimSpace(s) != "" && len(s) <= 120 }

// A site ID is one literal URL path segment. Requiring an alphanumeric first
// byte rules out "." and "..", whose browser URL normalization would change
// the fixed asset endpoint before the server sees the request.
func siteID(s string) bool {
	if len(s) == 0 || len(s) > 80 {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		alphanumeric := c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9'
		if !alphanumeric && (i == 0 || c != '_' && c != '-' && c != '.') {
			return false
		}
	}
	return true
}

func siteManifestPath(id string) string { return "/api/site/assets/" + id }
func siteFullPath(id string) string     { return siteManifestPath(id) + "/full" }

func (m siteMap) validate() error {
	if m.Protocol != siteProtocol || len(m.Resources) < 2 || len(m.Resources) > 8 || len(m.Nodes) < 3 || len(m.Nodes) > 16 || len(m.Edges) < 2 || len(m.Edges) > 32 || !siteID(m.Start) {
		return errRemoteInvalid
	}
	resources := map[string]sitePublicResource{}
	total := 0
	for _, r := range m.Resources {
		if !siteID(r.ID) || !siteLabel(r.Label) || !validAssetHash(r.SHA256) || r.Size < 0 || r.Size > maxAssetBytes || r.Manifest != siteManifestPath(r.ID) || resources[r.ID].ID != "" {
			return errRemoteInvalid
		}
		resources[r.ID] = r
		total += r.Size
		if total > maxSiteAggregate {
			return errRemoteInvalid
		}
	}
	nodes := map[string]siteNode{}
	for _, n := range m.Nodes {
		if !siteID(n.ID) || !siteLabel(n.Label) || nodes[n.ID].ID != "" {
			return errRemoteInvalid
		}
		if n.ID == m.Start {
			if n.ResourceID != "" {
				return errRemoteInvalid
			}
		} else if resources[n.ResourceID].ID == "" {
			return errRemoteInvalid
		}
		nodes[n.ID] = n
	}
	if nodes[m.Start].ID == "" {
		return errRemoteInvalid
	}
	edges := map[string]bool{}
	bound := map[string]bool{}
	for _, e := range m.Edges {
		if !siteID(e.ID) || edges[e.ID] || nodes[e.From].ID == "" || nodes[e.To].ID == "" || e.To == m.Start || e.Action != siteAction || e.Scope != siteScope || e.SideEffect || resources[e.Resource].ID == "" || nodes[e.To].ResourceID != e.Resource {
			return errRemoteInvalid
		}
		edges[e.ID] = true
		bound[e.Resource] = true
	}
	for id := range resources {
		if !bound[id] {
			return errRemoteInvalid
		}
	}
	return nil
}

func (m siteAssetManifest) valid(id string) bool {
	if m.Protocol != siteAssetProtocol || m.ID != id || m.MediaType != "application/octet-stream" || m.Size < 0 || m.Size > maxAssetBytes || !validAssetHash(m.SHA256) || m.Version != "sha256:"+m.SHA256 || m.ChunkSize != assetChunkBytes || m.Href != siteFullPath(id) || m.ChunkHash == nil || len(m.ChunkHash) != assetChunkCount(m.Size) {
		return false
	}
	for _, h := range m.ChunkHash {
		if !validAssetHash(h) {
			return false
		}
	}
	return m.Size != 0 || m.SHA256 == assetHash(nil)
}

func siteManifestFor(id string, raw []byte) siteAssetManifest {
	chunks := make([]string, 0, assetChunkCount(len(raw)))
	for start := 0; start < len(raw); start += assetChunkBytes {
		end := start + assetChunkBytes
		if end > len(raw) {
			end = len(raw)
		}
		chunks = append(chunks, assetHash(raw[start:end]))
	}
	hash := assetHash(raw)
	return siteAssetManifest{siteAssetProtocol, id, "application/octet-stream", len(raw), hash, "sha256:" + hash, assetChunkBytes, chunks, siteFullPath(id)}
}

func siteConfigToMap(c siteConfig) siteMap {
	m := siteMap{Protocol: c.Protocol, Start: c.Start, Nodes: c.Nodes, Edges: c.Edges, Resources: make([]sitePublicResource, 0, len(c.Resources))}
	for _, r := range c.Resources {
		m.Resources = append(m.Resources, sitePublicResource{ID: r.ID, Label: r.Label, Manifest: siteManifestPath(r.ID), SHA256: assetHash(nil)})
	}
	return m
}

func newSitePublisher(configPath string) (*siteService, error) {
	configSource, err := newResourceSource(configPath)
	if err != nil {
		return nil, fmt.Errorf("site config unavailable")
	}
	defer configSource.close()
	raw, err := configSource.readBytes(context.Background(), maxSiteConfigBytes)
	if err != nil {
		return nil, fmt.Errorf("site config unavailable")
	}
	var config siteConfig
	if err := strictSiteJSON(raw, &config, maxSiteConfigBytes); err != nil {
		return nil, fmt.Errorf("invalid site config")
	}
	for _, r := range config.Resources {
		if r.Path == "" || len(r.Path) > 4096 || strings.ContainsRune(r.Path, 0) {
			return nil, fmt.Errorf("invalid site resource path")
		}
	}
	// Validate the complete public contract before opening any asset path.
	if err := siteConfigToMap(config).validate(); err != nil {
		return nil, fmt.Errorf("invalid site graph")
	}
	s := &siteService{config: config, files: map[string]*resourceSource{}, gate: make(chan struct{}, 1), memory: map[string][]string{}}
	for _, r := range config.Resources {
		path := r.Path
		if !filepath.IsAbs(path) {
			path = filepath.Join(filepath.Dir(configPath), path)
		}
		src, err := newResourceSource(path)
		if err != nil {
			s.close()
			return nil, fmt.Errorf("site resource %s unavailable", r.ID)
		}
		s.files[r.ID] = src
	}
	if _, err := s.loadMap(context.Background()); err != nil {
		s.close()
		return nil, fmt.Errorf("site resource unavailable or over aggregate limit")
	}
	return s, nil
}

func newSiteReader(origin string) (*siteService, error) {
	remote, err := newHTTPResourceSource(origin)
	if err != nil {
		return nil, err
	}
	return &siteService{remote: remote, gate: make(chan struct{}, 1), memory: map[string][]string{}}, nil
}

func (s *siteService) close() {
	if s == nil {
		return
	}
	if s.remote != nil {
		s.remote.close()
	}
	for _, f := range s.files {
		f.close()
	}
}

func (s *siteService) loadMap(ctx context.Context) (siteMap, error) {
	if s == nil {
		return siteMap{}, errRemoteInvalid
	}
	if s.remote != nil {
		raw, err := s.remote.fetch(ctx, "/api/site", maxSiteMapBytes)
		if err != nil {
			return siteMap{}, err
		}
		var m siteMap
		if err := strictSiteJSON(raw, &m, maxSiteMapBytes); err != nil || m.validate() != nil {
			return siteMap{}, errRemoteInvalid
		}
		return m, nil
	}
	m := siteConfigToMap(s.config)
	total := 0
	for i := range m.Resources {
		if err := ctx.Err(); err != nil {
			return siteMap{}, errRemoteIncomplete
		}
		raw, err := s.files[m.Resources[i].ID].readBytes(ctx, maxAssetBytes)
		if err != nil {
			return siteMap{}, errRemoteIncomplete
		}
		m.Resources[i].SHA256 = assetHash(raw)
		m.Resources[i].Size = len(raw)
		total += len(raw)
		if total > maxSiteAggregate {
			return siteMap{}, errRemoteInvalid
		}
	}
	if m.validate() != nil {
		return siteMap{}, errRemoteInvalid
	}
	return m, nil
}

func siteResourceByID(m siteMap, id string) (sitePublicResource, bool) {
	for _, r := range m.Resources {
		if r.ID == id {
			return r, true
		}
	}
	return sitePublicResource{}, false
}

func (s *siteService) manifest(ctx context.Context, resource sitePublicResource) (siteAssetManifest, error) {
	if s.remote != nil {
		raw, err := s.remote.fetch(ctx, siteManifestPath(resource.ID), maxAssetPassportBytes)
		if err != nil {
			return siteAssetManifest{}, err
		}
		var manifest siteAssetManifest
		if strictSiteJSON(raw, &manifest, maxAssetPassportBytes) != nil || !manifest.valid(resource.ID) {
			return siteAssetManifest{}, errRemoteInvalid
		}
		if manifest.SHA256 != resource.SHA256 || manifest.Size != resource.Size {
			return siteAssetManifest{}, errRemoteChanged
		}
		return manifest, nil
	}
	raw, err := s.files[resource.ID].readBytes(ctx, maxAssetBytes)
	if err != nil || ctx.Err() != nil {
		return siteAssetManifest{}, errRemoteIncomplete
	}
	manifest := siteManifestFor(resource.ID, raw)
	if manifest.SHA256 != resource.SHA256 || manifest.Size != resource.Size {
		return siteAssetManifest{}, errRemoteChanged
	}
	return manifest, nil
}

// readResource returns bytes only after checking the fresh map, the passport,
// each block and the whole SHA-256. Failed or uncertain attempts yield no bytes.
func (s *siteService) readResource(ctx context.Context, resource sitePublicResource) ([]byte, error) {
	if s.remote != nil {
		manifest, err := s.manifest(ctx, resource)
		if err != nil {
			return nil, err
		}
		raw, err := s.remote.fetchAssetBytes(ctx, siteFixedFullURL(resource.ID, resource.SHA256), resource.Size)
		if err != nil {
			return nil, err
		}
		if assetHash(raw) != resource.SHA256 {
			return nil, errRemoteInvalid
		}
		for i, expected := range manifest.ChunkHash {
			start := i * assetChunkBytes
			end := start + assetChunkSize(manifest.Size, i)
			if assetHash(raw[start:end]) != expected {
				return nil, errRemoteInvalid
			}
		}
		if ctx.Err() != nil {
			return nil, errRemoteIncomplete
		}
		return raw, nil
	}
	raw, err := s.files[resource.ID].readBytes(ctx, maxAssetBytes)
	if err != nil || ctx.Err() != nil {
		return nil, errRemoteIncomplete
	}
	if len(raw) != resource.Size || assetHash(raw) != resource.SHA256 {
		return nil, errRemoteChanged
	}
	return raw, nil
}

func sitePath(path string) (id string, full bool, ok bool) {
	parts := strings.Split(path, "/")
	if len(parts) != 5 && len(parts) != 6 {
		return "", false, false
	}
	if parts[0] != "" || parts[1] != "api" || parts[2] != "site" || parts[3] != "assets" || !siteID(parts[4]) {
		return "", false, false
	}
	if len(parts) == 6 && parts[5] != "full" {
		return "", false, false
	}
	return parts[4], len(parts) == 6, true
}

func (s *siteService) serve(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if s == nil || r.URL.ForceQuery || r.URL.EscapedPath() != r.URL.Path {
		http.Error(w, "invalid site request", http.StatusBadRequest)
		return
	}
	if s.remote != nil && r.Header.Get("X-Metro-Resource-Read") != "" {
		http.Error(w, "reader chains are unsupported", http.StatusLoopDetected)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), siteRunTimeout)
	defer cancel()
	if r.URL.Path == "/api/site" {
		if r.URL.RawQuery != "" {
			http.Error(w, "invalid site query", http.StatusBadRequest)
			return
		}
		m, err := s.loadMap(ctx)
		if err != nil {
			siteError(w, err)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Cache-Control", "no-store")
		_ = json.NewEncoder(w).Encode(m)
		return
	}
	id, full, ok := sitePath(r.URL.Path)
	if !ok {
		http.NotFound(w, r)
		return
	}
	var hash string
	if full {
		var err error
		hash, err = parseAssetFullQuery(r.URL.RawQuery)
		if err != nil {
			http.Error(w, "invalid pin", http.StatusBadRequest)
			return
		}
	} else if r.URL.RawQuery != "" {
		http.Error(w, "invalid site query", http.StatusBadRequest)
		return
	}
	m, err := s.loadMap(ctx)
	if err != nil {
		siteError(w, err)
		return
	}
	resource, found := siteResourceByID(m, id)
	if !found {
		http.NotFound(w, r)
		return
	}
	if full {
		if hash != resource.SHA256 {
			siteError(w, errRemoteChanged)
			return
		}
		raw, err := s.readResource(ctx, resource)
		if err != nil {
			siteError(w, err)
			return
		}
		w.Header().Set("Content-Type", "application/octet-stream")
		w.Header().Set("Content-Disposition", "attachment; filename=\"metro-site-asset.bin\"")
		w.Header().Set("Content-Length", strconv.Itoa(len(raw)))
		w.Header().Set("ETag", `"`+hash+`"`)
		w.Header().Set("X-Metro-Asset-Whole-Verified", "true")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		_, _ = w.Write(raw)
		return
	}
	manifest, err := s.manifest(ctx, resource)
	if err != nil {
		siteError(w, err)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("X-Metro-Asset-Whole-Verified", strconv.FormatBool(s.remote == nil))
	_ = json.NewEncoder(w).Encode(manifest)
}

func siteError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, errRemoteChanged):
		http.Error(w, "site version changed", http.StatusConflict)
	case errors.Is(err, errRemoteIncomplete), errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		http.Error(w, "site read incomplete", http.StatusServiceUnavailable)
	default:
		http.Error(w, "site failed verification", http.StatusBadGateway)
	}
}

func sitePlan(m siteMap, target string) ([]siteEdge, error) {
	if m.validate() != nil || !siteID(target) {
		return nil, errRemoteInvalid
	}
	if _, ok := siteResourceByID(m, target); !ok {
		return nil, errRemoteInvalid
	}
	type candidate struct {
		state string
		path  []siteEdge
	}
	queue := []candidate{{state: m.Start}}
	visited := map[string]bool{m.Start: true}
	for len(queue) > 0 {
		c := queue[0]
		queue = queue[1:]
		if len(c.path) >= maxSiteSteps {
			continue
		}
		for _, e := range m.Edges {
			if e.From != c.state || visited[e.To] {
				continue
			}
			path := append(append([]siteEdge(nil), c.path...), e)
			if e.Resource == target {
				return path, nil
			}
			visited[e.To] = true
			queue = append(queue, candidate{state: e.To, path: path})
		}
	}
	return nil, fmt.Errorf("no permitted path within step budget")
}

func siteRevalidate(m siteMap, target string, ids []string) ([]siteEdge, error) {
	if m.validate() != nil || !siteID(target) || len(ids) == 0 || len(ids) > maxSiteSteps {
		return nil, errRemoteInvalid
	}
	byID := map[string]siteEdge{}
	for _, e := range m.Edges {
		byID[e.ID] = e
	}
	state := m.Start
	seen := map[string]bool{state: true}
	path := make([]siteEdge, 0, len(ids))
	for i, id := range ids {
		e, ok := byID[id]
		if !ok || e.From != state || seen[e.To] || (e.Resource == target) != (i == len(ids)-1) {
			return nil, errRemoteInvalid
		}
		seen[e.To] = true
		state = e.To
		path = append(path, e)
	}
	return path, nil
}

// run executes only read transitions described by a fresh bounded map. Route
// memory saves transition IDs after every traversed resource was re-read and
// independently verified; it never saves or reuses data bytes.
func (s *siteService) run(parent context.Context, req siteRunRequest) (out siteRunResult, err error) {
	out = siteRunResult{Status: "REJECTED", Mode: "graph", Target: req.Target, Events: []siteEvent{}, Resources: []siteRunResource{}}
	if s == nil || !siteID(req.Target) {
		out.Reason = "invalid target"
		return out, nil
	}
	ctx, cancel := context.WithTimeout(parent, siteRunTimeout)
	defer cancel()
	select {
	case s.gate <- struct{}{}:
		defer func() { <-s.gate }()
	case <-ctx.Done():
		out.Status, out.Reason = "UNKNOWN", "site run cancelled before start"
		return out, ctx.Err()
	}
	defer func() { out.MemoryEntries = len(s.memory) }()
	m, err := s.loadMap(ctx)
	if err != nil {
		out.Reason = "site map unavailable or invalid"
		if errors.Is(err, errRemoteIncomplete) || ctx.Err() != nil {
			out.Status = "UNKNOWN"
		}
		return out, nil
	}
	out.Map = m
	out.MapHash, err = metro.HashJSON(m)
	if err != nil {
		return out, err
	}
	if _, found := siteResourceByID(m, req.Target); !found {
		out.Reason = "target is not in published map"
		return out, nil
	}
	key, err := metro.HashJSON(map[string]any{"map": out.MapHash, "target": req.Target, "scope": siteScope, "origin": func() string {
		if s.remote != nil {
			return s.remote.base
		}
		return "file"
	}()})
	if err != nil {
		return out, err
	}
	var path []siteEdge
	if ids, exists := s.memory[key]; exists {
		path, err = siteRevalidate(m, req.Target, ids)
		if err == nil {
			out.Mode = "memory_revalidated"
		}
	}
	if path == nil {
		out.Mode = "graph"
		path, err = sitePlan(m, req.Target)
	}
	if err != nil {
		out.Status, out.Reason = "NO_ROUTE", "no permitted path within step budget"
		return out, nil
	}
	out.PlannedSteps = len(path)
	state := m.Start
	aggregate := 0
	for _, e := range path {
		if err := ctx.Err(); err != nil {
			out.Status, out.Reason = "UNKNOWN", "site run cancelled; no retry"
			return out, nil
		}
		if e.From != state || e.Action != siteAction || e.Scope != siteScope || e.SideEffect {
			out.Reason = "runtime transition rejected"
			return out, nil
		}
		resource, found := siteResourceByID(m, e.Resource)
		if !found || aggregate+resource.Size > maxSiteAggregate {
			out.Reason = "resource bound or aggregate limit rejected"
			return out, nil
		}
		id, idErr := freshID()
		if idErr != nil {
			return out, idErr
		}
		inputs := map[string]any{"map_hash": out.MapHash, "edge": e.ID, "from": state, "to": e.To, "resource_id": resource.ID, "resource_sha256": resource.SHA256, "scope": siteScope}
		packet := metro.NewPacket(id, "metro-web-demo", "open resource "+req.Target, metro.Action{Kind: siteAction, Inputs: inputs}, []string{siteExecutor})
		packet.Constraints = metro.Constraints{TimeoutMS: int(siteRunTimeout.Milliseconds()), SideEffect: false}
		route, routeErr := (metro.Router{ID: "metro-web-demo", Policy: map[string]string{siteAction: siteExecutor}}).Route(packet)
		if routeErr != nil {
			return out, routeErr
		}
		event := siteEvent{Edge: e, Status: "UNKNOWN", Packet: packet, Route: route}
		raw, readErr := s.readResource(ctx, resource)
		if readErr != nil {
			out.Events = append(out.Events, event)
			out.Reason = "resource changed or failed verification"
			if errors.Is(readErr, errRemoteIncomplete) || ctx.Err() != nil {
				out.Status, out.Reason = "UNKNOWN", "resource read incomplete; no retry"
			} else {
				out.Status = "REJECTED"
				event.Status = "REJECTED"
				out.Events[len(out.Events)-1] = event
			}
			return out, nil
		}
		if ctx.Err() != nil {
			out.Events = append(out.Events, event)
			out.Status, out.Reason = "UNKNOWN", "resource read cancelled; no retry"
			return out, nil
		}
		result := map[string]any{"state": e.To, "resource_id": resource.ID, "sha256": assetHash(raw), "size_bytes": len(raw), "map_hash": out.MapHash}
		event.Result = result
		receipt, receiptErr := metro.MakeSuccessReceipt(packet, route, result, "sha256:"+resource.SHA256)
		if receiptErr != nil {
			return out, receiptErr
		}
		if verifyErr := metro.Verify(packet, route, result, receipt); verifyErr != nil {
			event.Status = "REJECTED"
			out.Events = append(out.Events, event)
			out.Status, out.Reason = "REJECTED", "local receipt verification failed"
			return out, nil
		}
		event.Status = "CONFIRMED_LOCAL"
		event.Receipt = &receipt
		out.Events = append(out.Events, event)
		out.Resources = append(out.Resources, siteRunResource{resource.ID, resource.SHA256, len(raw)})
		out.FreshReads++
		aggregate += len(raw)
		state = e.To
	}
	if len(path) == 0 || path[len(path)-1].Resource != req.Target || ctx.Err() != nil {
		out.Status, out.Reason = "UNKNOWN", "target verification incomplete"
		return out, nil
	}
	ids := make([]string, len(path))
	for i, e := range path {
		ids[i] = e.ID
	}
	if _, exists := s.memory[key]; !exists && len(s.memory) >= 16 {
		s.memory = map[string][]string{}
	}
	s.memory[key] = ids
	out.Learned = true
	out.Status, out.Reason = "CONFIRMED_LOCAL", "read-only route completed with verified bytes and local receipts"
	return out, nil
}

// Never use a publisher-supplied URL for a follow-up read; all endpoints are
// assembled here from validated public resource IDs.
func siteFixedFullURL(id, hash string) string {
	return siteFullPath(id) + "?" + url.Values{"sha256": {hash}}.Encode()
}
