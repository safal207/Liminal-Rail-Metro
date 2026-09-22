package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"unicode/utf8"
)

const remoteGraphProtocol = "metro.web.graph.v0.2"
const maxGraphBytes = 64 << 10

type graphEvidence struct {
	Transport string `json:"transport"`
	Origin    string `json:"origin,omitempty"`
	SHA256    string `json:"sha256"`
}

type graphSnapshot struct {
	Graph    graph
	Evidence *graphEvidence
	Bytes    []byte
	Attempts int
}

// operation and stateKind preserve the built-in v0.1 adapter. Published v0.2
// maps must explicitly declare these types; their IDs are publisher-defined.
func (e edge) operation() string {
	if e.Action != "" {
		return e.Action
	}
	return e.ID
}

func (g graph) stateKind(id string) string {
	for _, n := range g.Nodes {
		if n.ID == id {
			if n.Kind != "" {
				return n.Kind
			}
			return n.ID
		}
	}
	return ""
}

// readContract is the local capability vocabulary, never supplied by a map.
func readContract(action string) (from, to string, known bool) {
	switch action {
	case "read_menu":
		return "start", "menu", true
	case "filter":
		return "menu", "filtered", true
	case "ingredients":
		return "filtered", "details", true
	case "present":
		return "details", "done", true
	}
	return "", "", false
}

func graphID(s string) bool {
	if len(s) == 0 || len(s) > 80 {
		return false
	}
	for _, c := range s {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '_' || c == '-' || c == '.') {
			return false
		}
	}
	return true
}

// validatePublishedGraph rejects unsupported instructions before any menu read.
// It may contain unavailable paths and advertised write edges, but those edges
// never become executable merely because the publisher describes them.
func (g graph) validatePublishedGraph() error {
	if err := g.validate(); err != nil {
		return err
	}
	if g.Protocol != remoteGraphProtocol || len(g.Nodes) > 16 || len(g.Edges) > 32 || len(g.Resources) != 1 || g.stateKind(g.Start) != "start" || g.stateKind(g.Goal) != "done" {
		return fmt.Errorf("unsupported published graph")
	}
	for _, n := range g.Nodes {
		if !graphID(n.ID) || strings.TrimSpace(n.Label) == "" || len(n.Label) > 120 {
			return fmt.Errorf("invalid graph node")
		}
		switch n.Kind {
		case "start", "menu", "filtered", "details", "done", "blocked":
		default:
			return fmt.Errorf("unsupported state kind")
		}
	}
	resource := g.Resources[0]
	if resource.ID != "menu" || resource.Manifest != "/api/resource" || resource.Origin != "" {
		return fmt.Errorf("unsupported graph resource")
	}
	reads := 0
	for _, e := range g.Edges {
		if !graphID(e.ID) || !graphID(e.Action) || len(e.Scope) > 80 {
			return fmt.Errorf("invalid graph edge")
		}
		if e.SideEffect {
			continue
		}
		from, to, known := readContract(e.Action)
		if !known || e.Scope != "menu.read" || g.stateKind(e.From) != from || g.stateKind(e.To) != to {
			return fmt.Errorf("graph violates local action contract")
		}
		if e.Action == "read_menu" {
			reads++
			if e.From != g.Start || resource.ReadEdge != e.ID {
				return fmt.Errorf("graph read edge mismatch")
			}
		}
	}
	if reads != 1 {
		return fmt.Errorf("graph requires one bound menu read")
	}
	return nil
}

func parsePublishedGraph(raw []byte) (graphSnapshot, error) {
	var out graphSnapshot
	if len(raw) > maxGraphBytes || !utf8.Valid(raw) {
		return out, fmt.Errorf("graph must be bounded UTF-8 JSON")
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	var g graph
	if err := dec.Decode(&g); err != nil {
		return out, fmt.Errorf("invalid graph JSON")
	}
	var extra any
	if dec.Decode(&extra) != io.EOF {
		return out, fmt.Errorf("trailing graph data")
	}
	if err := g.validatePublishedGraph(); err != nil {
		return out, err
	}
	hash := sha256.Sum256(raw)
	return graphSnapshot{Graph: g, Bytes: raw, Evidence: &graphEvidence{SHA256: hex.EncodeToString(hash[:])}}, nil
}

// loadGraph obtains a fresh map. No prior map is used after a failed read.
func (e *engine) loadGraph(ctx context.Context) (graphSnapshot, error) {
	if e.remoteGraph != nil {
		out := graphSnapshot{Attempts: 1}
		if ctx.Err() != nil {
			out.Attempts = 0
			return out, errRemoteIncomplete
		}
		raw, err := e.remoteGraph.fetch(ctx, "/api/manifest", maxGraphBytes)
		if err != nil {
			return out, err
		}
		snapshot, err := parsePublishedGraph(raw)
		if err != nil {
			return out, errRemoteInvalid
		}
		snapshot.Attempts = 1
		snapshot.Evidence.Transport = "http"
		snapshot.Evidence.Origin = e.remoteGraph.base
		// Bind the trusted startup origin into the effective map and route key.
		snapshot.Graph.Resources[0].Origin = e.remoteGraph.base
		return snapshot, nil
	}
	if e.graphFile != nil {
		raw, err := e.graphFile.readBytes(ctx, maxGraphBytes)
		if err != nil {
			return graphSnapshot{}, err
		}
		snapshot, err := parsePublishedGraph(raw)
		if err == nil {
			snapshot.Evidence.Transport = "file"
		}
		return snapshot, err
	}
	return graphSnapshot{Graph: e.graph()}, nil
}

// serveGraph publishes the configured document, or relays a fresh validated
// map to the local UI. The relay marker stops reader-to-reader loops.
func (e *engine) serveGraph(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if r.URL.RawQuery != "" || r.URL.ForceQuery {
		http.Error(w, "graph query unsupported", http.StatusBadRequest)
		return
	}
	if e.remoteGraph != nil && r.Header.Get("X-Metro-Resource-Read") != "" {
		http.Error(w, "reader chains are unsupported", http.StatusLoopDetected)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), remoteReadTimeout)
	defer cancel()
	snapshot, err := e.loadGraph(ctx)
	if err != nil {
		http.Error(w, "graph unavailable or invalid", http.StatusServiceUnavailable)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	if e.graphFile != nil {
		_, _ = w.Write(snapshot.Bytes)
	} else {
		_ = json.NewEncoder(w).Encode(snapshot.Graph)
	}
}
