package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/safal207/Liminal-Rail-Metro/internal/metro"
)

func publishedFixture(t *testing.T) graph {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("..", "..", "examples", "metro-web", "graph.json"))
	if err != nil {
		t.Fatal(err)
	}
	var g graph
	if err := json.Unmarshal(raw, &g); err != nil {
		t.Fatal(err)
	}
	return g
}

func graphJSON(t *testing.T, g graph) []byte {
	t.Helper()
	raw, err := json.Marshal(g)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func writeGraph(t *testing.T, path string, g graph) []byte {
	t.Helper()
	raw := graphJSON(t, g)
	if err := os.WriteFile(path, raw, 0600); err != nil {
		t.Fatal(err)
	}
	return raw
}

func publishedReader(t *testing.T, origin string) *engine {
	t.Helper()
	e := newEngine()
	source := testRemoteSource(t, origin)
	e.resource, e.remoteGraph = source, source
	return e
}

// TestPublishedGraphFreshness uses the real publisher to change topology without
// changing its version string. Old receipts stay verifiable, but its remembered
// path cannot survive a changed map, missing map, or a new permission denial.
func TestPublishedGraphFreshness(t *testing.T) {
	requireFileResources(t)
	dir := t.TempDir()
	menuPath, graphPath := filepath.Join(dir, "menu.json"), filepath.Join(dir, "graph.json")
	writeMenu(t, menuPath, demoMenu())
	g := publishedFixture(t)
	original := writeGraph(t, graphPath, g)
	publisher := fileEngine(t, menuPath)
	var err error
	publisher.graphFile, err = newResourceSource(graphPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(publisher.graphFile.close)
	server := testPublisher(t, publisher)
	reader := publishedReader(t, server.URL)
	first := mustRun(t, reader, runRequest{300, "normal"})
	second := mustRun(t, reader, runRequest{200, "normal"})
	if first.Status != "CONFIRMED_LOCAL" || first.Events[2].Edge.ID != "inspect-a" || second.Mode != "memory_revalidated" || len(second.Items) != 1 || second.GraphAttempts != 1 || second.HTTPAttempts != 2 || second.FreshReads != 1 {
		t.Fatalf("fresh map/route reuse failed: %+v / %+v", first, second)
	}
	hash := sha256.Sum256(original)
	if first.GraphSource == nil || first.GraphSource.Origin != server.URL || first.GraphSource.SHA256 != hex.EncodeToString(hash[:]) || first.Graph.Resources[0].Origin != server.URL {
		t.Fatal("received map provenance missing")
	}
	g.Edges = append(g.Edges[:2], g.Edges[3:]...) // Remove inspect-a, leave version unchanged.
	writeGraph(t, graphPath, g)
	changed := mustRun(t, reader, runRequest{300, "normal"})
	if changed.Status != "CONFIRMED_LOCAL" || changed.Mode != "graph" || changed.Events[2].Edge.ID != "inspect-b" || changed.GraphHash == first.GraphHash || changed.Graph.Version != first.Graph.Version {
		t.Fatal("changed map reused an obsolete path")
	}
	for _, ev := range first.Events {
		if ev.Receipt == nil || metro.Verify(ev.Packet, ev.Route, ev.Result, *ev.Receipt) != nil {
			t.Fatal("later map changed old receipt")
		}
		provenance := ev.Result["graph_source"].(graphEvidence)
		provenance.SHA256 = strings.Repeat("0", 64)
		ev.Result["graph_source"] = provenance
		if metro.Verify(ev.Packet, ev.Route, ev.Result, *ev.Receipt) == nil {
			t.Fatal("tampered map provenance verified")
		}
	}
	if err := os.WriteFile(graphPath, []byte("{broken"), 0600); err != nil {
		t.Fatal(err)
	}
	failed := mustRun(t, reader, runRequest{300, "normal"})
	if failed.Status != "UNKNOWN" || failed.GraphAttempts != 1 || failed.HTTPAttempts != 0 || len(failed.Events) != 0 || len(failed.Items) != 0 || failed.Learned || failed.GraphSource != nil || len(failed.Graph.Nodes) != 0 {
		t.Fatal("failed discovery used cached map or menu")
	}
	writeGraph(t, graphPath, publishedFixture(t))
	restored := mustRun(t, reader, runRequest{300, "normal"})
	if restored.Status != "CONFIRMED_LOCAL" || restored.Mode != "memory_revalidated" {
		t.Fatal("prior confirmed path did not recover")
	}
	denied := mustRun(t, reader, runRequest{300, "denied"})
	if denied.Status != "DENIED" || denied.GraphAttempts != 0 || denied.HTTPAttempts != 0 || denied.Learned || len(denied.Events) != 0 {
		t.Fatal("memory supplied missing permission")
	}
	// A graph relay also blocks loops before doing any upstream work.
	relay := testPublisher(t, reader)
	chained := mustRun(t, publishedReader(t, relay.URL), runRequest{300, "normal"})
	if chained.Status != "REJECTED" || chained.GraphAttempts != 1 || chained.HTTPAttempts != 0 {
		t.Fatal("graph relay chain followed")
	}
}

// TestUntrustedGraphRejected requires malformed or executable instructions to
// fail before reading content, producing neither receipts nor learned routes.
func TestUntrustedGraphRejected(t *testing.T) {
	cases := map[string]func(*graph){
		"unknown protocol":  func(g *graph) { g.Protocol = graphProtocol },
		"fake goal":         func(g *graph) { g.Goal = g.Nodes[1].ID },
		"unknown operation": func(g *graph) { g.Edges[2].Action = "shell_exec" },
		"hidden write":      func(g *graph) { g.Edges[6].SideEffect = false },
		"scope":             func(g *graph) { g.Edges[1].Scope = "order.write" },
		"skip filter":       func(g *graph) { g.Edges[1].To = "answer" },
		"missing kind":      func(g *graph) { g.Nodes[0].Kind = "" },
		"missing action":    func(g *graph) { g.Edges[0].Action = "" },
		"dangling":          func(g *graph) { g.Edges[0].To = "missing" },
		"duplicate":         func(g *graph) { g.Nodes = append(g.Nodes, g.Nodes[0]) },
		"foreign origin":    func(g *graph) { g.Resources[0].Origin = "https://another.example" },
		"foreign path":      func(g *graph) { g.Resources[0].Manifest = "/admin" },
		"read edge":         func(g *graph) { g.Resources[0].ReadEdge = "show-a" },
		"oversize label":    func(g *graph) { g.Nodes[0].Label = strings.Repeat("x", 121) },
		"oversize map":      func(g *graph) { g.Version = strings.Repeat("x", maxGraphBytes) },
		"URL action":        func(g *graph) { g.Edges[0].Action = "https://another.example" },
		"trailing JSON":     nil, "unknown field": nil, "invalid UTF8": nil, "redirect": nil,
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			g := publishedFixture(t)
			if mutate != nil {
				mutate(&g)
			}
			raw := graphJSON(t, g)
			switch name {
			case "trailing JSON":
				raw = append(raw, []byte("{}")...)
			case "unknown field":
				raw = append([]byte(`{"executable":"code",`), raw[1:]...)
			case "invalid UTF8":
				raw = append(raw, 0xff)
			}
			var requests atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests.Add(1)
				if r.URL.Path != "/api/manifest" {
					t.Error("invalid graph caused a resource request")
				}
				w.Header().Set("Content-Type", "application/json")
				if name == "redirect" {
					w.Header().Set("Location", "/elsewhere")
					w.WriteHeader(http.StatusFound)
					return
				}
				_, _ = w.Write(raw)
			}))
			defer server.Close()
			out := mustRun(t, publishedReader(t, server.URL), runRequest{300, "normal"})
			if out.Status != "REJECTED" || requests.Load() != 1 || out.GraphAttempts != 1 || out.HTTPAttempts != 0 || len(out.Events) != 0 || out.Learned || len(out.Items) != 0 {
				t.Fatalf("unsafe graph outcome: %+v", out)
			}
		})
	}
}

// TestPublishedGraphNoRouteAndLimits fails after discovery but before menu I/O.
func TestPublishedGraphNoRouteAndLimits(t *testing.T) {
	for _, scenario := range []string{"normal", "step_limit"} {
		g := publishedFixture(t)
		want := "STEP_LIMIT"
		if scenario == "normal" {
			want = "NO_ROUTE"
			g.Edges[2].SideEffect, g.Edges[4].SideEffect = true, true
		}
		raw := graphJSON(t, g)
		var count atomic.Int32
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			count.Add(1)
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write(raw)
		}))
		out := mustRun(t, publishedReader(t, server.URL), runRequest{300, scenario})
		server.Close()
		if out.Status != want || count.Load() != 1 || out.HTTPAttempts != 0 || len(out.Events) != 0 || out.Learned {
			t.Fatal("unavailable route fetched menu", out.Status)
		}
	}
}

// TestGraphDiscoveryCancellation checks incomplete discovery without fallback.
func TestGraphDiscoveryCancellation(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	}))
	defer server.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	out, err := publishedReader(t, server.URL).runContext(ctx, runRequest{300, "normal"})
	if err != nil || out.Status != "UNKNOWN" || out.GraphAttempts != 1 || out.HTTPAttempts != 0 || len(out.Events) != 0 || out.Learned {
		t.Fatal("incomplete map accepted", err, out.Status)
	}
}
