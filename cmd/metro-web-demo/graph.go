package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"slices"

	"github.com/safal207/Liminal-Rail-Metro/internal/metro"
)

const graphProtocol = "metro.web.graph.v0.1"
const localExecutor = "robis-demo-reader"

type node struct {
	ID    string `json:"id"`
	Label string `json:"label"`
	Kind  string `json:"kind,omitempty"`
}
type edge struct {
	ID         string `json:"id"`
	From       string `json:"from"`
	To         string `json:"to"`
	Scope      string `json:"scope"`
	SideEffect bool   `json:"side_effect"`
	Action     string `json:"action,omitempty"`
}
type graph struct {
	Protocol  string         `json:"protocol"`
	Version   string         `json:"version"`
	Start     string         `json:"start"`
	Goal      string         `json:"goal"`
	Nodes     []node         `json:"nodes"`
	Edges     []edge         `json:"edges"`
	Resources []resourceLink `json:"resources,omitempty"`
}

type resourceLink struct {
	ID       string `json:"id"`
	Manifest string `json:"manifest"`
	ReadEdge string `json:"read_edge"`
	Origin   string `json:"origin,omitempty"`
}

// demoGraph advertises the synthetic menu path and an unexecutable purchase edge.
func demoGraph() graph {
	return graph{graphProtocol, "robis-demo-1", "start", "done",
		[]node{{ID: "start", Label: "Вход"}, {ID: "menu", Label: "Меню"}, {ID: "filtered", Label: "Подбор"}, {ID: "details", Label: "Состав"}, {ID: "done", Label: "Результат"}, {ID: "paid", Label: "Заказ: запрещён"}},
		[]edge{{ID: "read_menu", From: "start", To: "menu", Scope: "menu.read"}, {ID: "filter", From: "menu", To: "filtered", Scope: "menu.read"}, {ID: "ingredients", From: "filtered", To: "details", Scope: "menu.read"}, {ID: "present", From: "details", To: "done", Scope: "menu.read"}, {ID: "purchase", From: "details", To: "paid", Scope: "order.write", SideEffect: true}},
		nil,
	}
}

// graph describes the current adapter and links its resource discovery endpoint.
func (e *engine) graph() graph {
	g := demoGraph()
	if e.resource != nil {
		g.Version = "menu-file-1"
		g.Resources = []resourceLink{{ID: "menu", Manifest: "/api/resource", ReadEdge: "read_menu"}}
		if e.resource.origin().Transport == "http" {
			g.Version = "menu-http-1"
			g.Resources[0].Origin = e.resource.origin().Origin
		}
	}
	return g
}

// validate checks graph bounds and references without granting execution rights.
func (g graph) validate() error {
	if (g.Protocol != graphProtocol && g.Protocol != remoteGraphProtocol) || g.Version == "" || len(g.Version) > 80 || len(g.Nodes) < 2 || len(g.Nodes) > 64 || len(g.Edges) > 256 {
		return fmt.Errorf("invalid graph bounds or protocol")
	}
	nodes := map[string]bool{}
	for _, n := range g.Nodes {
		if n.ID == "" || len(n.ID) > 80 || nodes[n.ID] {
			return fmt.Errorf("invalid or duplicate node")
		}
		nodes[n.ID] = true
	}
	if !nodes[g.Start] || !nodes[g.Goal] || g.Start == g.Goal {
		return fmt.Errorf("invalid start or goal")
	}
	ids := map[string]bool{}
	for _, e := range g.Edges {
		if e.ID == "" || len(e.ID) > 80 || ids[e.ID] || !nodes[e.From] || !nodes[e.To] || e.Scope == "" {
			return fmt.Errorf("invalid edge")
		}
		ids[e.ID] = true
	}
	return nil
}

// permitted requires explicit read permission and rejects every side effect.
func permitted(e edge, scopes map[string]bool) bool { return !e.SideEffect && scopes[e.Scope] }

// plan finds a shortest permitted path within the step budget without revisiting nodes.
func plan(g graph, scopes map[string]bool, maxSteps int) ([]edge, error) {
	if err := g.validate(); err != nil {
		return nil, err
	}
	if maxSteps < 1 || maxSteps > 16 {
		return nil, fmt.Errorf("invalid step budget")
	}
	type candidate struct {
		state string
		path  []edge
	}
	queue := []candidate{{g.Start, nil}}
	visited := map[string]bool{g.Start: true}
	for len(queue) > 0 {
		c := queue[0]
		queue = queue[1:]
		if c.state == g.Goal {
			return c.path, nil
		}
		if len(c.path) >= maxSteps {
			continue
		}
		for _, e := range g.Edges {
			if e.From != c.state || !permitted(e, scopes) || visited[e.To] {
				continue
			}
			visited[e.To] = true
			path := append(append([]edge(nil), c.path...), e)
			queue = append(queue, candidate{e.To, path})
		}
	}
	return nil, fmt.Errorf("no permitted path within step budget")
}

// revalidate checks remembered transitions against the current graph, scopes and budget.
func revalidate(g graph, ids []string, scopes map[string]bool, maxSteps int) ([]edge, error) {
	if err := g.validate(); err != nil {
		return nil, err
	}
	if len(ids) == 0 || len(ids) > maxSteps {
		return nil, fmt.Errorf("invalid remembered path length")
	}
	byID := map[string]edge{}
	for _, e := range g.Edges {
		byID[e.ID] = e
	}
	state := g.Start
	seen := map[string]bool{state: true}
	path := []edge{}
	for _, id := range ids {
		e, ok := byID[id]
		if !ok || e.From != state || seen[e.To] || !permitted(e, scopes) {
			return nil, fmt.Errorf("remembered path rejected")
		}
		path = append(path, e)
		state = e.To
		seen[state] = true
	}
	if state != g.Goal {
		return nil, fmt.Errorf("remembered path has wrong goal")
	}
	return path, nil
}

type item struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Price       int      `json:"price_rub"`
	Available   bool     `json:"available"`
	Ingredients []string `json:"ingredients"`
}

// demoMenu returns a fresh synthetic fixture, including an unavailable drink.
func demoMenu() []item {
	return []item{
		{"americano", "Американо", 190, true, []string{"кофе", "вода"}},
		{"cappuccino", "Капучино", 280, true, []string{"кофе", "молоко"}},
		{"matcha", "Матча", 320, true, []string{"матча", "молоко"}},
		{"cocoa", "Какао", 260, false, []string{"какао", "молоко"}},
	}
}

type runRequest struct {
	Budget   int    `json:"budget"`
	Scenario string `json:"scenario"`
}

// valid accepts only bounded budgets and the closed set of demonstration scenarios.
func (r runRequest) valid() bool {
	if r.Budget < 1 || r.Budget > 10000 {
		return false
	}
	switch r.Scenario {
	case "normal", "new_version", "denied", "lost_response", "tampered", "cycle", "step_limit":
		return true
	}
	return false
}

type event struct {
	Edge    edge           `json:"edge"`
	Status  string         `json:"status"`
	Packet  metro.Packet   `json:"packet"`
	Route   metro.Route    `json:"route"`
	Result  map[string]any `json:"result,omitempty"`
	Receipt *metro.Receipt `json:"receipt,omitempty"`
}
type runResult struct {
	ID             string            `json:"run_id"`
	Status         string            `json:"status"`
	Reason         string            `json:"reason"`
	Mode           string            `json:"route_source"`
	Graph          graph             `json:"graph"`
	GraphHash      string            `json:"graph_hash"`
	GraphSource    *graphEvidence    `json:"graph_source,omitempty"`
	GraphAttempts  int               `json:"graph_http_attempts,omitempty"`
	ResourceHash   string            `json:"resource_hash,omitempty"`
	Resource       *resourceManifest `json:"resource,omitempty"`
	ResourceSource *resourceOrigin   `json:"resource_source,omitempty"`
	HTTPAttempts   int               `json:"http_attempts,omitempty"`
	Budget         int               `json:"budget"`
	FreshReads     int               `json:"fresh_reads"`
	PlannedSteps   int               `json:"planned_steps"`
	Events         []event           `json:"events"`
	Items          []item            `json:"items"`
	Learned        bool              `json:"learned"`
	MemoryEntries  int               `json:"memory_entries"`
	MemoryStorage  string            `json:"memory_storage"`
	MemoryRestored bool              `json:"memory_restored"`
	MemorySaved    bool              `json:"memory_saved"`
	MemoryWarning  string            `json:"memory_warning,omitempty"`
	EvidenceScope  string            `json:"evidence_scope"`
}
type engine struct {
	gate     chan struct{}
	memory   map[string][]string
	store    *memoryStore
	restored map[string]bool
	// Fixtures are caller-owned only in tests. Production demo uses fresh fixtures.
	menu func() []item
	// Fixed at startup; HTTP callers cannot select another source.
	resource    resourceReader
	graphFile   *resourceSource
	remoteGraph *httpResourceSource
	asset       *assetService
	site        *siteService
}

// newEngine creates isolated process-local route memory and a synthetic menu reader.
func newEngine() *engine {
	return &engine{gate: make(chan struct{}, 1), memory: map[string][]string{}, menu: demoMenu}
}

// freshID creates a new run or action identity and propagates entropy-source failures.
func freshID() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(b[:]), nil
}

// run executes a standalone route without an HTTP caller's cancellation context.
func (e *engine) run(req runRequest) (runResult, error) {
	return e.runContext(context.Background(), req)
}

// runContext serializes read-only execution, verifies local receipts and remembers only
// confirmed paths. Each run snapshots fresh menu data instead of caching results.
func (e *engine) runContext(ctx context.Context, req runRequest) (out runResult, err error) {
	out = runResult{Status: "REJECTED", Mode: "graph", Budget: req.Budget, Events: []event{}, Items: []item{}, EvidenceScope: "local fixture consistency; no external attestation or LLM"}
	out.Graph = graph{Nodes: []node{}, Edges: []edge{}}
	select {
	case e.gate <- struct{}{}:
		defer func() { <-e.gate }()
	case <-ctx.Done():
		return out, ctx.Err()
	}
	if e.resource != nil {
		out.EvidenceScope = "local file snapshot and exact-byte SHA-256; no external attestation or LLM"
		source := e.resource.origin()
		out.ResourceSource = &source
		if source.Transport == "http" {
			out.EvidenceScope = "HTTP bytes verified against publisher passport; local receipt, no independent attestation or LLM"
		}
	}
	defer func() { out.MemoryEntries = len(e.memory) }()
	out.MemoryStorage = "process"
	if e.store != nil {
		out.MemoryStorage = "file"
	}
	if !req.valid() {
		out.Reason = "invalid request"
		return out, nil
	}
	if err := ctx.Err(); err != nil {
		return out, err
	}
	out.ID, err = freshID()
	if err != nil {
		return out, err
	}
	published := e.remoteGraph != nil || e.graphFile != nil
	if published && req.Scenario == "denied" {
		out.Status, out.Reason = "DENIED", "no permitted path within step budget"
		return out, nil
	}
	if published && (req.Scenario == "new_version" || req.Scenario == "cycle") {
		out.Reason = "update the publisher graph to test topology changes"
		return out, nil
	}
	if e.remoteGraph != nil {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, remoteReadTimeout)
		defer cancel()
	}
	snapshot, graphErr := e.loadGraph(ctx)
	out.GraphAttempts = snapshot.Attempts
	if graphErr != nil {
		out.Reason = "graph unavailable or invalid"
		if errors.Is(graphErr, errRemoteIncomplete) {
			out.Status = "UNKNOWN"
			out.Reason = "remote graph read incomplete; no automatic retry"
		} else if e.remoteGraph != nil {
			out.Reason = "remote graph failed verification"
		}
		return out, nil
	}
	g := snapshot.Graph
	out.GraphSource = snapshot.Evidence
	scopes := map[string]bool{"menu.read": true}
	maxSteps := 8
	switch req.Scenario {
	case "new_version":
		g.Version = "robis-demo-2"
		if e.resource != nil {
			g.Version = "menu-file-2"
			if e.resource.origin().Transport == "http" {
				g.Version = "menu-http-2"
			}
		}
	case "denied":
		scopes["menu.read"] = false
	case "cycle":
		g.Edges[3].To = "menu"
	case "step_limit":
		maxSteps = 2
	}
	out.Graph = g
	out.GraphHash, err = metro.HashJSON(g)
	if err != nil {
		return out, err
	}
	// Parameter values and menu results are NOT cached. A route is scoped to this
	// graph, goal, local adapter version and effective permission snapshot.
	key, err := metro.HashJSON(map[string]any{"graph": out.GraphHash, "goal": g.Goal, "adapter": "robis-read-v1", "scopes": scopes})
	if err != nil {
		return out, err
	}
	var path []edge
	if ids, ok := e.memory[key]; ok {
		out.Mode = "memory_revalidated"
		out.MemoryRestored = e.restored[key]
		path, err = revalidate(g, ids, scopes, maxSteps)
		if err != nil {
			// A disk entry is only a hint. Discard it and plan under today's rules.
			out.Mode, out.MemoryRestored = "graph", false
			path, err = plan(g, scopes, maxSteps)
		}
	} else {
		path, err = plan(g, scopes, maxSteps)
	}
	if err != nil {
		out.Status = "NO_ROUTE"
		out.Reason = err.Error()
		if req.Scenario == "denied" {
			out.Status = "DENIED"
		}
		if req.Scenario == "step_limit" {
			out.Status = "STEP_LIMIT"
		}
		return out, nil
	}
	out.PlannedSteps = len(path)
	state := g.Start
	data := []item{}
	for _, transition := range path {
		action := transition.operation()
		from, to, known := readContract(action)
		if !known || !permitted(transition, scopes) || transition.Scope != "menu.read" || transition.From != state || from != g.stateKind(state) || to != g.stateKind(transition.To) {
			out.Status = "DENIED"
			out.Reason = "runtime precondition or authority rejected"
			return out, nil
		}
		actionID, idErr := freshID()
		if idErr != nil {
			return out, idErr
		}
		input := map[string]any{"graph_hash": out.GraphHash, "state": state, "budget": req.Budget, "resource_hash": out.ResourceHash, "edge": transition.ID}
		if out.ResourceSource != nil {
			input["resource_source"] = *out.ResourceSource
		}
		if out.GraphSource != nil {
			input["graph_source"] = *out.GraphSource
		}
		packet := metro.NewPacket(actionID, "metro-web-demo", "find available drinks within budget", metro.Action{Kind: action, Inputs: input}, []string{localExecutor})
		packet.Constraints = metro.Constraints{TimeoutMS: 1000, SideEffect: false}
		if action == "read_menu" && out.ResourceSource != nil && out.ResourceSource.Transport == "http" {
			packet.Constraints.TimeoutMS = int(remoteReadTimeout.Milliseconds())
		}
		router := metro.Router{ID: "metro-web-demo", Policy: map[string]string{action: localExecutor}}
		route, routeErr := router.Route(packet)
		if routeErr != nil {
			return out, routeErr
		}
		observation := event{Edge: transition, Status: "UNKNOWN", Packet: packet, Route: route}
		switch action {
		case "read_menu":
			if e.resource != nil {
				snapshot, readErr := e.resource.read(ctx)
				out.HTTPAttempts += snapshot.Requests
				if readErr != nil {
					observation.Status = "REJECTED"
					out.Reason = "resource unavailable or invalid"
					if errors.Is(readErr, errRemoteIncomplete) {
						out.Status = "UNKNOWN"
						observation.Status = "UNKNOWN"
						out.Reason = errRemoteIncomplete.Error()
					} else if errors.Is(readErr, errRemoteChanged) || errors.Is(readErr, errRemoteInvalid) {
						out.Reason = readErr.Error()
					}
					out.Events = append(out.Events, observation)
					return out, nil
				}
				data = snapshot.Document.Items
				out.Resource = &snapshot.Manifest
				out.ResourceHash = snapshot.Manifest.SHA256
			} else {
				// Own nested fixture data before fault injection or later provider reads.
				data = slices.Clone(e.menu())
				for i := range data {
					data[i].Ingredients = slices.Clone(data[i].Ingredients)
				}
				if req.Scenario == "new_version" {
					for i := range data {
						if data[i].ID == "cappuccino" {
							data[i].Price = 310
						}
					}
				}
				out.ResourceHash, err = metro.HashJSON(data)
				if err != nil {
					return out, err
				}
			}
			out.FreshReads++
			if req.Scenario == "lost_response" {
				out.Events = append(out.Events, observation)
				out.Status = "UNKNOWN"
				out.Reason = "injected lost response after local read; no retry, no success receipt"
				return out, nil
			}
		case "filter":
			selected := []item{}
			for _, it := range data {
				if it.Available && it.Price <= req.Budget {
					selected = append(selected, it)
				}
			}
			data = selected
		case "ingredients":
			for _, it := range data {
				if len(it.Ingredients) == 0 {
					out.Reason = "missing ingredients"
					return out, nil
				}
			}
		case "present":
			// Recheck result predicates rather than trusting route completion alone.
			for _, it := range data {
				if !it.Available || it.Price > req.Budget {
					out.Reason = "goal predicate failed"
					return out, nil
				}
			}
		}
		observation.Result = map[string]any{"state": transition.To, "graph_hash": out.GraphHash, "resource_hash": out.ResourceHash, "items": data}
		if out.Resource != nil {
			observation.Result["resource"] = *out.Resource
		}
		if out.ResourceSource != nil {
			observation.Result["resource_source"] = *out.ResourceSource
		}
		if out.GraphSource != nil {
			observation.Result["graph_source"] = *out.GraphSource
		}
		receipt, receiptErr := metro.MakeSuccessReceipt(packet, route, observation.Result, "")
		if receiptErr != nil {
			return out, receiptErr
		}
		// Fault injection tampers with bytes AFTER the receipt was made.
		if req.Scenario == "tampered" && action == "read_menu" {
			observation.Result["resource_hash"] = "tampered"
		}
		if verifyErr := metro.Verify(packet, route, observation.Result, receipt); verifyErr != nil {
			observation.Status = "REJECTED"
			out.Events = append(out.Events, observation)
			out.Status = "REJECTED"
			out.Reason = verifyErr.Error()
			return out, nil
		}
		observation.Status = "CONFIRMED_LOCAL"
		observation.Receipt = &receipt
		out.Events = append(out.Events, observation)
		state = transition.To
	}
	if state != g.Goal {
		out.Reason = "goal not reached"
		return out, nil
	}
	if err := ctx.Err(); err != nil {
		return out, err
	}
	ids := make([]string, len(path))
	for i, t := range path {
		ids[i] = t.ID
	}
	// Stage the next cache without publishing it. Cancellation while preparing
	// the candidate must leave both current memory and the disk file unchanged.
	next := make(map[string][]string, len(e.memory)+1)
	_, exists := e.memory[key]
	evicted := !exists && len(e.memory) >= 16
	if !evicted {
		for k, route := range e.memory {
			next[k] = route
		}
	}
	next[key] = ids
	if err := ctx.Err(); err != nil {
		return out, err
	}
	if e.store != nil {
		if err := e.store.save(ctx, next); err != nil {
			if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
				return out, err
			}
			out.MemoryWarning = "route completed, but memory could not be saved to disk"
		} else {
			out.MemorySaved = true
		}
	}
	// The save's commit boundary (or the check above in process-only mode) has
	// passed. Late cancellation does not roll back a completed local result.
	e.memory = next
	if evicted {
		e.restored = map[string]bool{}
	}
	if out.Mode == "graph" {
		delete(e.restored, key)
	}
	out.Status = "CONFIRMED_LOCAL"
	out.Reason = "read-only goal reached; local bindings and result predicates checked"
	out.Items = data
	out.Learned = true
	return out, nil
}
