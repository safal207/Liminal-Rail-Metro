package main

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func memoryEngine(t *testing.T, path string) *engine {
	t.Helper()
	requireFileResources(t)
	e := newEngine()
	if err := e.enableMemory(path); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(e.store.close)
	return e
}

func TestMemoryRestartFreshRemoteData(t *testing.T) {
	requireFileResources(t)
	dir := t.TempDir()
	menuPath, graphPath, memoryPath := filepath.Join(dir, "menu.json"), filepath.Join(dir, "graph.json"), filepath.Join(dir, "memory.json")
	writeMenu(t, menuPath, demoMenu())
	g := publishedFixture(t)
	writeGraph(t, graphPath, g)
	publisher := fileEngine(t, menuPath)
	var err error
	publisher.graphFile, err = newResourceSource(graphPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(publisher.graphFile.close)
	server := httptest.NewServer(nil)
	server.Config.Handler = handler(publisher, strings.TrimPrefix(server.URL, "http://"))
	t.Cleanup(server.Close)
	first := publishedReader(t, server.URL)
	if err := first.enableMemory(memoryPath); err != nil {
		t.Fatal(err)
	}
	a, err := first.run(runRequest{300, "normal"})
	first.store.close()
	if err != nil || a.Status != "CONFIRMED_LOCAL" || !a.MemorySaved || a.MemoryRestored {
		t.Fatalf("first: %+v %v", a, err)
	}
	menu := demoMenu()
	menu[0].Price = 999
	writeMenu(t, menuPath, menu)
	second := publishedReader(t, server.URL)
	if err := second.enableMemory(memoryPath); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(second.store.close)
	b, err := second.run(runRequest{200, "normal"})
	if err != nil || b.Mode != "memory_revalidated" || !b.MemoryRestored || !b.MemorySaved || b.GraphAttempts != 1 || b.HTTPAttempts != 2 || b.FreshReads != 1 || a.ResourceHash == b.ResourceHash {
		t.Fatalf("restart: %+v %v", b, err)
	}
	for _, item := range b.Items {
		if item.Price > 200 {
			t.Fatal("stale menu result")
		}
	}
	before, _ := os.ReadFile(memoryPath)
	for _, scenario := range []string{"denied", "lost_response", "tampered", "step_limit"} {
		out, err := second.run(runRequest{300, scenario})
		if err != nil || out.Status == "CONFIRMED_LOCAL" || out.MemorySaved || out.Learned {
			t.Fatalf("failure learned: %s %+v %v", scenario, out, err)
		}
		after, _ := os.ReadFile(memoryPath)
		if !bytes.Equal(before, after) {
			t.Fatal("failed run modified disk cache")
		}
	}
	g.Edges = append(g.Edges[:2], g.Edges[3:]...)
	writeGraph(t, graphPath, g)
	c, err := second.run(runRequest{300, "normal"})
	if err != nil || c.Status != "CONFIRMED_LOCAL" || c.Mode != "graph" || c.MemoryRestored || c.GraphHash == b.GraphHash {
		t.Fatalf("changed map: %+v %v", c, err)
	}
	// A different publisher origin must not use the first publisher's memory.
	other := httptest.NewServer(nil)
	other.Config.Handler = handler(publisher, strings.TrimPrefix(other.URL, "http://"))
	t.Cleanup(other.Close)
	second.store.close()
	third := publishedReader(t, other.URL)
	if err := third.enableMemory(memoryPath); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(third.store.close)
	d, err := third.run(runRequest{300, "normal"})
	if err != nil || d.Mode != "graph" || d.MemoryRestored {
		t.Fatalf("origin leaked: %+v %v", d, err)
	}
}

func TestMemoryPoisonedPathReplanned(t *testing.T) {
	path := filepath.Join(t.TempDir(), "routes.json")
	e := memoryEngine(t, path)
	out, err := e.run(runRequest{300, "normal"})
	if err != nil || !out.MemorySaved {
		t.Fatal(out, err)
	}
	for key := range e.memory {
		e.memory[key] = []string{"purchase"}
	}
	if err := e.store.save(e.memory); err != nil {
		t.Fatal(err)
	}
	e.store.close()
	restarted := memoryEngine(t, path)
	out, err = restarted.run(runRequest{300, "normal"})
	if err != nil || out.Status != "CONFIRMED_LOCAL" || out.Mode != "graph" || out.MemoryRestored || !out.MemorySaved {
		t.Fatalf("poisoned cache: %+v %v", out, err)
	}
	for _, event := range out.Events {
		if event.Edge.SideEffect {
			t.Fatal("cached write executed")
		}
	}
}

func TestMemoryBoundsAndMalformedFiles(t *testing.T) {
	requireFileResources(t)
	key := strings.Repeat("a", 64)
	cases := map[string]string{
		"protocol":       `{"protocol":"wrong","routes":{}}`,
		"missing":        `{"protocol":"metro.web.route-memory.v0.1"}`,
		"null":           `{"protocol":"metro.web.route-memory.v0.1","routes":null}`,
		"unknown":        `{"protocol":"metro.web.route-memory.v0.1","routes":{},"permissions":["*"]}`,
		"trailing":       `{"protocol":"metro.web.route-memory.v0.1","routes":{}} {}`,
		"key":            `{"protocol":"metro.web.route-memory.v0.1","routes":{"bad":["read_menu"]}}`,
		"empty path":     `{"protocol":"metro.web.route-memory.v0.1","routes":{"` + key + `":[]}}`,
		"duplicate step": `{"protocol":"metro.web.route-memory.v0.1","routes":{"` + key + `":["read_menu","read_menu"]}}`,
		"oversize":       strings.Repeat(" ", maxMemoryBytes+1),
		"encoding":       string([]byte{0xff}),
		"partial":        `{"protocol":`,
	}
	for name, raw := range cases {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "memory.json")
			if err := os.WriteFile(path, []byte(raw), 0600); err != nil {
				t.Fatal(err)
			}
			if store, _, err := openMemory(path); err == nil {
				store.close()
				t.Fatal("invalid cache accepted")
			}
			after, err := os.ReadFile(path)
			if err != nil || string(after) != raw {
				t.Fatal("invalid cache overwritten")
			}
		})
	}
	doc := memoryDocument{Protocol: memoryProtocol, Routes: map[string][]string{}}
	for i := 0; i < 17; i++ {
		doc.Routes[strings.Repeat("a", 62)+string("0123456789abcdef"[i/16])+string("0123456789abcdef"[i%16])] = []string{"read_menu"}
	}
	if validateMemory(doc) == nil {
		t.Fatal("too many entries accepted")
	}
	doc.Routes = map[string][]string{key: make([]string, 17)}
	if validateMemory(doc) == nil {
		t.Fatal("too many steps accepted")
	}
}

func TestMemoryFileOwnershipAndLinks(t *testing.T) {
	requireFileResources(t)
	dir := t.TempDir()
	path := filepath.Join(dir, "memory.json")
	store, _, err := openMemory(path)
	if err != nil {
		t.Fatal(err)
	}
	if second, _, err := openMemory(path); err == nil {
		second.close()
		t.Fatal("second writer accepted")
	}
	store.close()
	for _, kind := range []string{"hard", "symbolic", "parent"} {
		t.Run(kind, func(t *testing.T) {
			link := filepath.Join(dir, kind)
			switch kind {
			case "hard":
				if err := os.Link(path, link); err != nil {
					t.Fatal(err)
				}
				defer os.Remove(link)
			case "symbolic":
				makeTestSymlink(t, path, link)
			case "parent":
				makeTestSymlink(t, dir, link)
				link = filepath.Join(link, "memory.json")
			}
			if s, _, err := openMemory(link); err == nil {
				s.close()
				t.Fatal("linked cache accepted")
			}
		})
	}
	if s, _, err := openMemory(dir); err == nil {
		s.close()
		t.Fatal("directory cache accepted")
	}
}

func TestMemorySaveFailureAndCancellation(t *testing.T) {
	path := filepath.Join(t.TempDir(), "memory.json")
	e := memoryEngine(t, path)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := e.runContext(ctx, runRequest{300, "normal"}); err == nil {
		t.Fatal("cancelled run accepted")
	}
	if len(e.memory) != 0 {
		t.Fatal("cancelled run learned")
	}
	e.store.close()
	out, err := e.run(runRequest{300, "normal"})
	if err != nil || out.Status != "CONFIRMED_LOCAL" || !out.Learned || out.MemorySaved || out.MemoryWarning == "" {
		t.Fatalf("save failure misreported: %+v %v", out, err)
	}
	if strings.Contains(out.MemoryWarning, path) {
		t.Fatal("local path leaked")
	}
}

func TestMemoryBoundedEvictionAndNoResults(t *testing.T) {
	path := filepath.Join(t.TempDir(), "memory.json")
	e := memoryEngine(t, path)
	for i := 0; i < 17; i++ {
		e.memory[strings.Repeat("b", 62)+string("0123456789abcdef"[i/16])+string("0123456789abcdef"[i%16])] = []string{"read_menu"}
	}
	out, err := e.run(runRequest{300, "normal"})
	if err != nil || out.MemoryEntries != 1 || !out.MemorySaved {
		t.Fatal(out, err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var doc memoryDocument
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	if len(doc.Routes) != 1 || bytes.Contains(raw, []byte("price_rub")) || bytes.Contains(raw, []byte("receipt")) {
		t.Fatal("cache retained results or unbounded entries")
	}
}
