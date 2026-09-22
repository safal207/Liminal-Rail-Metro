package main

import (
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/safal207/Liminal-Rail-Metro/internal/metro"
)

func mustRun(t *testing.T, e *engine, r runRequest) runResult {
	t.Helper()
	out, err := e.run(r)
	if err != nil {
		t.Fatal(err)
	}
	return out
}
func TestReadOnlyPath(t *testing.T) {
	g := demoGraph()
	p, err := plan(g, map[string]bool{"menu.read": true, "order.write": true}, 8)
	if err != nil || len(p) != 4 {
		t.Fatalf("path=%v err=%v", p, err)
	}
	for _, e := range p {
		if e.SideEffect || e.ID == "purchase" {
			t.Fatal("side effect selected")
		}
	}
}
func TestRepeatedRouteFreshInputsAndIDs(t *testing.T) {
	e := newEngine()
	first := mustRun(t, e, runRequest{300, "normal"})
	second := mustRun(t, e, runRequest{200, "normal"})
	if first.Status != "CONFIRMED_LOCAL" || len(first.Items) != 2 || second.Status != "CONFIRMED_LOCAL" || len(second.Items) != 1 {
		t.Fatalf("unexpected results: %s / %s", first.Status, second.Status)
	}
	if first.Mode != "graph" || second.Mode != "memory_revalidated" || second.FreshReads != 1 || second.MemoryEntries != 1 {
		t.Fatalf("memory not correctly reused: %+v", second)
	}
	seen := map[string]bool{}
	for _, r := range []runResult{first, second} {
		for _, s := range r.Events {
			if seen[s.Packet.ActionID] || s.Packet.ActionID == "" {
				t.Fatal("action identity reused")
			}
			seen[s.Packet.ActionID] = true
			if s.Route.SelectedTarget != localExecutor {
				t.Fatal("wrong executor")
			}
		}
	}
}
func TestMenuRefreshWithoutGraphChange(t *testing.T) {
	e := newEngine()
	reads := 0
	e.menu = func() []item {
		reads++
		m := demoMenu()
		if reads > 1 {
			m[1].Price = 900
		}
		return m
	}
	first := mustRun(t, e, runRequest{300, "normal"})
	second := mustRun(t, e, runRequest{300, "normal"})
	if len(first.Items) != 2 || len(second.Items) != 1 || reads != 2 || second.Mode != "memory_revalidated" || first.ResourceHash == second.ResourceHash {
		t.Fatal("stale menu was reused")
	}
}
func TestVersionInvalidatesRoute(t *testing.T) {
	e := newEngine()
	a := mustRun(t, e, runRequest{300, "normal"})
	b := mustRun(t, e, runRequest{300, "new_version"})
	if a.GraphHash == b.GraphHash || b.Mode != "graph" || b.MemoryEntries != 2 || len(b.Items) != 1 {
		t.Fatalf("bad invalidation: %+v", b)
	}
}
func TestFailureScenariosNeverLearn(t *testing.T) {
	cases := map[string]string{"denied": "DENIED", "lost_response": "UNKNOWN", "tampered": "REJECTED", "cycle": "NO_ROUTE", "step_limit": "STEP_LIMIT"}
	for scenario, status := range cases {
		t.Run(scenario, func(t *testing.T) {
			out := mustRun(t, newEngine(), runRequest{300, scenario})
			if out.Status != status || out.Learned || out.MemoryEntries != 0 || len(out.Items) != 0 {
				t.Fatalf("unexpected outcome %+v", out)
			}
			for _, ev := range out.Events {
				if ev.Status != "CONFIRMED_LOCAL" && ev.Receipt != nil {
					t.Fatal("unconfirmed event has success receipt")
				}
			}
			if scenario == "lost_response" && (out.FreshReads != 1 || len(out.Events) != 1) {
				t.Fatal("lost response was retried")
			}
		})
	}
}
func TestPriorExperienceCannotGrantAccess(t *testing.T) {
	e := newEngine()
	mustRun(t, e, runRequest{300, "normal"})
	out := mustRun(t, e, runRequest{300, "denied"})
	if out.Status != "DENIED" || len(out.Events) != 0 || out.FreshReads != 0 || out.Learned || out.MemoryEntries != 1 {
		t.Fatal("permission leaked from memory")
	}
}
func TestFailedAttemptDoesNotReplaceConfirmedMemory(t *testing.T) {
	e := newEngine()
	mustRun(t, e, runRequest{300, "normal"})
	out := mustRun(t, e, runRequest{300, "lost_response"})
	if out.Mode != "memory_revalidated" || out.Status != "UNKNOWN" || out.MemoryEntries != 1 || out.Learned {
		t.Fatal("unknown outcome learned")
	}
	after := mustRun(t, e, runRequest{300, "normal"})
	if after.Mode != "memory_revalidated" || after.Status != "CONFIRMED_LOCAL" {
		t.Fatal("prior route corrupted")
	}
}
func TestCorruptedMemoryFailsBeforeDispatch(t *testing.T) {
	e := newEngine()
	mustRun(t, e, runRequest{300, "normal"})
	for k := range e.memory {
		e.memory[k] = []string{"purchase"}
	}
	out := mustRun(t, e, runRequest{300, "normal"})
	if out.Status == "CONFIRMED_LOCAL" || len(out.Events) != 0 || out.FreshReads != 0 || out.Learned {
		t.Fatal("poisoned route executed")
	}
}
func TestEmptyResultIsNotInvented(t *testing.T) {
	out := mustRun(t, newEngine(), runRequest{50, "normal"})
	if out.Status != "CONFIRMED_LOCAL" || len(out.Items) != 0 || len(out.Events) != 4 {
		t.Fatal("empty result mishandled")
	}
}
func TestMetroReceiptBindings(t *testing.T) {
	out := mustRun(t, newEngine(), runRequest{300, "normal"})
	for _, ev := range out.Events {
		if ev.Receipt == nil {
			t.Fatal("missing receipt")
		}
		if err := metro.Verify(ev.Packet, ev.Route, ev.Result, *ev.Receipt); err != nil {
			t.Fatal(err)
		}
		r := *ev.Receipt
		r.ActionID = "another-action"
		if metro.Verify(ev.Packet, ev.Route, ev.Result, r) == nil {
			t.Fatal("foreign receipt accepted")
		}
		r = *ev.Receipt
		r.ResultHash = strings.Repeat("0", 64)
		if metro.Verify(ev.Packet, ev.Route, ev.Result, r) == nil {
			t.Fatal("false result digest accepted")
		}
	}
}
func TestGraphValidation(t *testing.T) {
	changes := map[string]func(*graph){
		"unknown protocol": func(g *graph) { g.Protocol = "other" },
		"duplicate node":   func(g *graph) { g.Nodes = append(g.Nodes, g.Nodes[0]) },
		"duplicate edge":   func(g *graph) { g.Edges = append(g.Edges, g.Edges[0]) },
		"missing goal":     func(g *graph) { g.Goal = "absent" },
		"dangling edge":    func(g *graph) { g.Edges[0].To = "absent" },
		"too many nodes": func(g *graph) {
			for i := 0; i < 65; i++ {
				g.Nodes = append(g.Nodes, node{fmt.Sprint(i), "node"})
			}
		},
	}
	for name, change := range changes {
		t.Run(name, func(t *testing.T) {
			g := demoGraph()
			change(&g)
			if _, err := plan(g, map[string]bool{"menu.read": true}, 8); err == nil {
				t.Fatal("invalid graph accepted")
			}
		})
	}
}
func TestRevalidateRejectsChangedPaths(t *testing.T) {
	g := demoGraph()
	scopes := map[string]bool{"menu.read": true}
	for _, ids := range [][]string{nil, {"read_menu", "present"}, {"read_menu", "filter", "ingredients", "purchase"}, {"read_menu", "filter", "ingredients"}, {"absent"}} {
		if _, err := revalidate(g, ids, scopes, 8); err == nil {
			t.Fatal("invalid path accepted", ids)
		}
	}
	if _, err := revalidate(g, []string{"read_menu", "filter", "ingredients", "present"}, scopes, 2); err == nil {
		t.Fatal("limit ignored")
	}
}
func TestConcurrentRuns(t *testing.T) {
	e := newEngine()
	var wg sync.WaitGroup
	for i := 0; i < 12; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			out, err := e.run(runRequest{300, "normal"})
			if err != nil || out.Status != "CONFIRMED_LOCAL" {
				t.Errorf("concurrent failure: %v %s", err, out.Status)
			}
		}()
	}
	wg.Wait()
	if len(e.memory) != 1 {
		t.Fatal("unexpected memory cardinality")
	}
}
func TestMemoryIsProcessLocal(t *testing.T) {
	mustRun(t, newEngine(), runRequest{300, "normal"})
	out := mustRun(t, newEngine(), runRequest{300, "normal"})
	if out.Mode != "graph" {
		t.Fatal("unexpected shared memory")
	}
}
func TestHTTPGuards(t *testing.T) {
	cases := []struct {
		name, method, path, body, host, origin, content string
		want                                            int
	}{
		{"valid", "POST", "/api/run", `{"budget":300,"scenario":"normal"}`, "127.0.0.1:8787", "", "application/json", 200},
		{"host", "POST", "/api/run", `{}`, "evil.example", "", "application/json", 403},
		{"origin", "POST", "/api/run", `{}`, "127.0.0.1:8787", "https://evil.example", "application/json", 403},
		{"type", "POST", "/api/run", `{}`, "127.0.0.1:8787", "", "text/plain", 415},
		{"unknown field", "POST", "/api/run", `{"budget":300,"scenario":"normal","grant":true}`, "127.0.0.1:8787", "", "application/json", 400},
		{"trailing JSON", "POST", "/api/run", `{"budget":300,"scenario":"normal"}{}`, "127.0.0.1:8787", "", "application/json", 400},
		{"negative", "POST", "/api/run", `{"budget":-1,"scenario":"normal"}`, "127.0.0.1:8787", "", "application/json", 400},
		{"external action", "POST", "/api/run", `{"budget":300,"scenario":"purchase"}`, "127.0.0.1:8787", "", "application/json", 400},
		{"oversized", "POST", "/api/run", strings.Repeat("x", 3000), "127.0.0.1:8787", "", "application/json", 400},
		{"method", "GET", "/api/run", "", "127.0.0.1:8787", "", "application/json", 405},
		{"unknown path", "GET", "/missing", "", "127.0.0.1:8787", "", "", 404},
		{"manifest", "GET", "/api/manifest", "", "127.0.0.1:8787", "", "", 200},
		{"home", "GET", "/", "", "127.0.0.1:8787", "", "", 200},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(tc.method, tc.path, strings.NewReader(tc.body))
			req.Host = tc.host
			req.Header.Set("Origin", tc.origin)
			req.Header.Set("Content-Type", tc.content)
			w := httptest.NewRecorder()
			handler(newEngine(), "127.0.0.1:8787").ServeHTTP(w, req)
			if w.Code != tc.want {
				t.Fatalf("got %d want %d: %s", w.Code, tc.want, w.Body.String())
			}
			if w.Header().Get("X-Content-Type-Options") != "nosniff" {
				t.Fatal("missing nosniff")
			}
		})
	}
}
func TestOfflineExportContainsActualRuns(t *testing.T) {
	path := filepath.Join(t.TempDir(), "replay.html")
	if err := exportReplay(path); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	s := string(b)
	const marker = "window.__REPLAY__="
	start := strings.Index(s, marker)
	if start < 0 {
		t.Fatal("replay missing")
	}
	rest := s[start+len(marker):]
	end := strings.Index(rest, ";</script>")
	if end < 0 {
		t.Fatal("replay terminator missing")
	}
	var data struct {
		Cases []recordedCase `json:"cases"`
	}
	if err := json.Unmarshal([]byte(rest[:end]), &data); err != nil {
		t.Fatal(err)
	}
	if len(data.Cases) != 9 || data.Cases[1].Result.Mode != "memory_revalidated" || data.Cases[5].Result.Status != "UNKNOWN" {
		t.Fatal("incorrect recorded runs")
	}
}
