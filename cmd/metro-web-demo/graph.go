package main

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"sync"

	"github.com/safal207/Liminal-Rail-Metro/internal/metro"
)

const graphProtocol = "metro.web.graph.v0.1"
const localExecutor = "robis-demo-reader"

type node struct {
	ID    string `json:"id"`
	Label string `json:"label"`
}
type edge struct {
	ID         string `json:"id"`
	From       string `json:"from"`
	To         string `json:"to"`
	Scope      string `json:"scope"`
	SideEffect bool   `json:"side_effect"`
}
type graph struct {
	Protocol string `json:"protocol"`
	Version  string `json:"version"`
	Start    string `json:"start"`
	Goal     string `json:"goal"`
	Nodes    []node `json:"nodes"`
	Edges    []edge `json:"edges"`
}

func demoGraph() graph {
	return graph{graphProtocol, "robis-demo-1", "start", "done",
		[]node{{"start", "Вход"}, {"menu", "Меню"}, {"filtered", "Подбор"}, {"details", "Состав"}, {"done", "Результат"}, {"paid", "Заказ: запрещён"}},
		[]edge{{"read_menu", "start", "menu", "menu.read", false}, {"filter", "menu", "filtered", "menu.read", false}, {"ingredients", "filtered", "details", "menu.read", false}, {"present", "details", "done", "menu.read", false}, {"purchase", "details", "paid", "order.write", true}},
	}
}

// A graph advertises possibilities, never grants authority. This planner only
// traverses read-only, explicitly permitted edges. Runtime repeats these checks.
func (g graph) validate() error {
	if g.Protocol != graphProtocol || g.Version == "" || len(g.Version) > 80 || len(g.Nodes) < 2 || len(g.Nodes) > 64 || len(g.Edges) > 256 {
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
func permitted(e edge, scopes map[string]bool) bool { return !e.SideEffect && scopes[e.Scope] }
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
	ID            string  `json:"run_id"`
	Status        string  `json:"status"`
	Reason        string  `json:"reason"`
	Mode          string  `json:"route_source"`
	Graph         graph   `json:"graph"`
	GraphHash     string  `json:"graph_hash"`
	ResourceHash  string  `json:"resource_hash,omitempty"`
	Budget        int     `json:"budget"`
	FreshReads    int     `json:"fresh_reads"`
	PlannedSteps  int     `json:"planned_steps"`
	Events        []event `json:"events"`
	Items         []item  `json:"items"`
	Learned       bool    `json:"learned"`
	MemoryEntries int     `json:"memory_entries"`
	EvidenceScope string  `json:"evidence_scope"`
}
type engine struct {
	mu     sync.Mutex
	memory map[string][]string
	// Fixtures are caller-owned only in tests. Production demo uses fresh fixtures.
	menu func() []item
}

func newEngine() *engine { return &engine{memory: map[string][]string{}, menu: demoMenu} }
func freshID() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(b[:]), nil
}
func (e *engine) run(req runRequest) (out runResult, err error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	out = runResult{Status: "REJECTED", Mode: "graph", Budget: req.Budget, Events: []event{}, Items: []item{}, EvidenceScope: "local fixture consistency; no external attestation or LLM"}
	defer func() { out.MemoryEntries = len(e.memory) }()
	if !req.valid() {
		out.Reason = "invalid request"
		return out, nil
	}
	out.ID, err = freshID()
	if err != nil {
		return out, err
	}
	g := demoGraph()
	scopes := map[string]bool{"menu.read": true}
	maxSteps := 8
	switch req.Scenario {
	case "new_version":
		g.Version = "robis-demo-2"
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
		path, err = revalidate(g, ids, scopes, maxSteps)
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
	contracts := map[string][2]string{"read_menu": {"start", "menu"}, "filter": {"menu", "filtered"}, "ingredients": {"filtered", "details"}, "present": {"details", "done"}}
	for _, transition := range path {
		contract, known := contracts[transition.ID]
		if !known || !permitted(transition, scopes) || transition.Scope != "menu.read" || transition.From != state || contract[0] != state || contract[1] != transition.To {
			out.Status = "DENIED"
			out.Reason = "runtime precondition or authority rejected"
			return out, nil
		}
		actionID, idErr := freshID()
		if idErr != nil {
			return out, idErr
		}
		input := map[string]any{"graph_hash": out.GraphHash, "state": state, "budget": req.Budget, "resource_hash": out.ResourceHash, "edge": transition.ID}
		packet := metro.NewPacket(actionID, "metro-web-demo", "find available drinks within budget", metro.Action{Kind: transition.ID, Inputs: input}, []string{localExecutor})
		packet.Constraints = metro.Constraints{TimeoutMS: 1000, SideEffect: false}
		router := metro.Router{ID: "metro-web-demo", Policy: map[string]string{transition.ID: localExecutor}}
		route, routeErr := router.Route(packet)
		if routeErr != nil {
			return out, routeErr
		}
		observation := event{Edge: transition, Status: "UNKNOWN", Packet: packet, Route: route}
		switch transition.ID {
		case "read_menu":
			// Always read again, including route-memory hits. This is a local fixture,
			// not a network request to Robis or an independent external readback.
			data = e.menu()
			out.FreshReads++
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
		receipt, receiptErr := metro.MakeSuccessReceipt(packet, route, observation.Result, "")
		if receiptErr != nil {
			return out, receiptErr
		}
		// Fault injection tampers with bytes AFTER the receipt was made.
		if req.Scenario == "tampered" && transition.ID == "read_menu" {
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
	out.Status = "CONFIRMED_LOCAL"
	out.Reason = "read-only goal reached; local bindings and result predicates checked"
	out.Items = data
	ids := make([]string, len(path))
	for i, t := range path {
		ids[i] = t.ID
	}
	if _, ok := e.memory[key]; !ok && len(e.memory) >= 16 {
		e.memory = map[string][]string{}
	}
	e.memory[key] = ids
	out.Learned = true
	return out, nil
}
