package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"testing"

	"github.com/safal207/Liminal-Rail-Metro/internal/metro"
)

// writeMenu creates or replaces an owned test file without sharing engine data.
func writeMenu(t *testing.T, path string, items []item) []byte {
	t.Helper()
	raw, err := json.MarshalIndent(menuDocument{menuSchema, "Test menu", items}, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, raw, 0600); err != nil {
		t.Fatal(err)
	}
	return raw
}

// requireFileResources keeps synthetic-mode tests portable to other platforms.
func requireFileResources(t *testing.T) {
	t.Helper()
	if runtime.GOOS != "linux" && runtime.GOOS != "windows" {
		t.Skip("file adapter requires Linux or Windows")
	}
}

// fileEngine pins an owned test resource and releases directory handles first.
func fileEngine(t *testing.T, path string) *engine {
	t.Helper()
	requireFileResources(t)
	s, err := newResourceSource(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.close)
	e := newEngine()
	e.resource = s
	return e
}

// makeTestSymlink skips only Windows hosts without symlink creation privilege.
func makeTestSymlink(t *testing.T, target, link string) {
	t.Helper()
	if err := os.Symlink(target, link); err != nil {
		if runtime.GOOS == "windows" && errors.Is(err, syscall.Errno(1314)) {
			t.Skip("Windows symlink privilege unavailable")
		}
		t.Fatal(err)
	}
}

// TestResourceSymlinksRejected covers both a selected link and a symlinked parent.
func TestResourceSymlinksRejected(t *testing.T) {
	requireFileResources(t)
	dir := t.TempDir()
	path := filepath.Join(dir, "menu.json")
	writeMenu(t, path, demoMenu())
	link := filepath.Join(dir, "link.json")
	makeTestSymlink(t, path, link)
	if _, err := readResource(link); err == nil {
		t.Fatal("startup symlink accepted")
	}
	parentLink := filepath.Join(t.TempDir(), "linked-parent")
	makeTestSymlink(t, dir, parentLink)
	if _, err := readResource(filepath.Join(parentLink, "menu.json")); err == nil {
		t.Fatal("symlinked parent accepted")
	}
}

// TestFileResourceRefresh proves route memory cannot reuse a stale file result
// and that later edits preserve the complete earlier receipt snapshot.
func TestFileResourceRefresh(t *testing.T) {
	requireFileResources(t)
	path := filepath.Join(t.TempDir(), "menu.json")
	original := writeMenu(t, path, demoMenu())
	e := fileEngine(t, path)
	first := mustRun(t, e, runRequest{300, "normal"})
	changed := demoMenu()
	changed[1].Price = 500
	changed[0].Ingredients[0] = "new coffee"
	writeMenu(t, path, changed)
	second := mustRun(t, e, runRequest{300, "normal"})
	digest := sha256.Sum256(original)
	if first.Status != "CONFIRMED_LOCAL" || first.Resource == nil || first.ResourceHash != hex.EncodeToString(digest[:]) || first.Resource.Size != len(original) || len(first.Items) != 2 {
		t.Fatalf("invalid first file snapshot: %+v", first)
	}
	if second.Status != "CONFIRMED_LOCAL" || len(second.Items) != 1 || second.Mode != "memory_revalidated" || second.FreshReads != 1 || first.ResourceHash == second.ResourceHash || second.GraphHash != first.GraphHash {
		t.Fatalf("file update not observed: %+v", second)
	}
	if first.Items[0].Ingredients[0] != "кофе" || second.Items[0].Ingredients[0] != "new coffee" {
		t.Fatal("snapshot aliased")
	}
	for _, ev := range first.Events {
		if ev.Receipt == nil {
			t.Fatal("missing receipt")
		}
		if err := metro.Verify(ev.Packet, ev.Route, ev.Result, *ev.Receipt); err != nil {
			t.Fatal(err)
		}
		manifest := ev.Result["resource"].(resourceManifest)
		if manifest.SHA256 != first.ResourceHash {
			t.Fatal("wrong resource binding")
		}
		manifest.Title = "tampered"
		ev.Result["resource"] = manifest
		if metro.Verify(ev.Packet, ev.Route, ev.Result, *ev.Receipt) == nil {
			t.Fatal("modified passport accepted")
		}
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	failed := mustRun(t, e, runRequest{300, "normal"})
	if failed.Status != "REJECTED" || failed.Learned || failed.Resource != nil || len(failed.Items) != 0 || len(failed.Events) != 1 || failed.Events[0].Receipt != nil || failed.MemoryEntries != 1 {
		t.Fatal("missing file used cached success")
	}
	denied := mustRun(t, e, runRequest{300, "denied"})
	if denied.Status != "DENIED" || len(denied.Events) != 0 || denied.FreshReads != 0 {
		t.Fatal("denied route attempted file read")
	}
	writeMenu(t, path, demoMenu())
	recovered := mustRun(t, e, runRequest{300, "new_version"})
	if recovered.Status != "CONFIRMED_LOCAL" || recovered.ResourceHash != first.ResourceHash || len(recovered.Items) != 2 {
		t.Fatal("graph version fault altered file data")
	}
}

// TestResourceHTTPVersionPin checks discovery, exact byte retrieval, stale
// version rejection and the same Host/Origin boundary as the route endpoint.
func TestResourceHTTPVersionPin(t *testing.T) {
	requireFileResources(t)
	path := filepath.Join(t.TempDir(), "menu.json")
	raw := writeMenu(t, path, demoMenu())
	e := fileEngine(t, path)
	h := handler(e, "127.0.0.1:8787")
	get := func(target string) *httptest.ResponseRecorder {
		r := httptest.NewRequest("GET", target, nil)
		r.Host = "127.0.0.1:8787"
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w
	}
	w := get("/api/resource")
	graphResponse := get("/api/manifest")
	var discovered graph
	if json.Unmarshal(graphResponse.Body.Bytes(), &discovered) != nil || len(discovered.Resources) != 1 || discovered.Resources[0].Manifest != "/api/resource" || discovered.Resources[0].ReadEdge != "read_menu" {
		t.Fatal("graph did not discover its resource")
	}
	var manifest resourceManifest
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &manifest) != nil {
		t.Fatalf("manifest: %s", w.Body.String())
	}
	if manifest.Protocol != "metro.web.resource.v0.1" || manifest.Version != "sha256:"+manifest.SHA256 || manifest.Size != len(raw) {
		t.Fatal("invalid passport")
	}
	content := get(manifest.Href)
	if content.Code != 200 || !bytes.Equal(content.Body.Bytes(), raw) || content.Header().Get("ETag") != `"`+manifest.SHA256+`"` || content.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("exact bytes or headers lost")
	}
	if err := os.WriteFile(path, append(raw, '\n'), 0600); err != nil {
		t.Fatal(err)
	}
	if stale := get(manifest.Href); stale.Code != 409 {
		t.Fatal("whitespace change served under old digest")
	}
	updated := get("/api/resource")
	var latest resourceManifest
	if json.Unmarshal(updated.Body.Bytes(), &latest) != nil || latest.SHA256 == manifest.SHA256 || latest.Size != len(raw)+1 {
		t.Fatal("passport not refreshed")
	}
	if get(latest.Href).Code != 200 {
		t.Fatal("new version not available")
	}
	for _, target := range []string{"/api/resource/content", "/api/resource/content?sha256=no", manifest.Href + "&sha256=" + manifest.SHA256, manifest.Href + "&path=secret", "/api/resource?path=secret", "/api/resource/content?sha256=%ZZ"} {
		if w := get(target); w.Code != 400 {
			t.Fatalf("query accepted: %s, code %d", target, w.Code)
		}
	}
	for _, target := range []string{"/api/resource", latest.Href} {
		for _, guard := range []string{"host", "origin", "method"} {
			r := httptest.NewRequest("GET", target, nil)
			r.Host = "127.0.0.1:8787"
			want := 403
			if guard == "host" {
				r.Host = "evil.example"
			}
			if guard == "origin" {
				r.Header.Set("Origin", "https://evil.example")
			}
			if guard == "method" {
				r.Method = "POST"
				want = 405
			}
			w := httptest.NewRecorder()
			h.ServeHTTP(w, r)
			if w.Code != want {
				t.Fatalf("%s guard: %s", guard, w.Body.String())
			}
		}
	}
	if err := os.WriteFile(path, []byte("broken"), 0600); err != nil {
		t.Fatal(err)
	}
	if w := get("/api/resource"); w.Code != 503 || strings.Contains(w.Body.String(), path) {
		t.Fatal("invalid file did not fail safely")
	}
}

// TestInvalidResourceFiles rejects malformed, excessive and unsupported input
// before a route or raw-content endpoint can accept it as a menu resource.
func TestInvalidResourceFiles(t *testing.T) {
	requireFileResources(t)
	path := filepath.Join(t.TempDir(), "menu.json")
	valid := string(writeMenu(t, path, demoMenu()))
	cases := map[string][]byte{
		"invalid JSON":     []byte("{"),
		"trailing JSON":    []byte(valid + "{}"),
		"unknown schema":   []byte(strings.Replace(valid, menuSchema, "unknown", 1)),
		"unknown field":    []byte(strings.Replace(valid, `"title":`, `"unknown":0,"title":`, 1)),
		"missing price":    []byte(strings.Replace(valid, `"price_rub": 190,`, "", 1)),
		"negative price":   []byte(strings.Replace(valid, "190", "-1", 1)),
		"fractional price": []byte(strings.Replace(valid, "190", "1.5", 1)),
		"duplicate id":     []byte(strings.Replace(valid, `"cappuccino"`, `"americano"`, 1)),
		"empty ingredient": []byte(strings.Replace(valid, `"кофе"`, `" "`, 1)),
		"invalid UTF8":     append([]byte(valid), 0xff),
		"oversized":        bytes.Repeat([]byte(" "), maxResourceBytes+1),
		"null items":       []byte(`{"schema":"metro.web.menu.v0.1","title":"Menu","items":null}`),
	}
	for name, raw := range cases {
		t.Run(name, func(t *testing.T) {
			if err := os.WriteFile(path, raw, 0600); err != nil {
				t.Fatal(err)
			}
			if _, err := readResource(path); err == nil {
				t.Fatal("invalid resource accepted")
			}
		})
	}
	if _, err := readResource(t.TempDir()); err == nil {
		t.Fatal("directory accepted")
	}
	if _, err := readResource(path + ".missing"); err == nil {
		t.Fatal("missing file accepted")
	}
	writeMenu(t, path, []item{})
	if snapshot, err := readResource(path); err != nil || len(snapshot.Document.Items) != 0 {
		t.Fatal("valid empty menu rejected", err)
	}
}

// TestResourceRejectsLaterSymlink blocks redirection after a successful first run.
func TestResourceRejectsLaterSymlink(t *testing.T) {
	requireFileResources(t)
	dir := t.TempDir()
	path := filepath.Join(dir, "menu.json")
	writeMenu(t, path, demoMenu())
	e := fileEngine(t, path)
	mustRun(t, e, runRequest{300, "normal"})
	other := filepath.Join(dir, "other.json")
	writeMenu(t, other, demoMenu())
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	makeTestSymlink(t, other, path)
	out := mustRun(t, e, runRequest{300, "normal"})
	if out.Status != "REJECTED" || out.Resource != nil || len(out.Items) != 0 {
		t.Fatal("replacement link was followed")
	}
	r := httptest.NewRequest("GET", "/api/resource", nil)
	r.Host = "127.0.0.1:8787"
	w := httptest.NewRecorder()
	handler(e, r.Host).ServeHTTP(w, r)
	if w.Code != 503 {
		t.Fatal("resource endpoint followed replacement link")
	}
}

// TestResourceParentPinned refuses directory redirection while allowing an
// intentional atomic replacement of the configured file within its parent.
func TestResourceParentPinned(t *testing.T) {
	requireFileResources(t)
	root := t.TempDir()
	dir := filepath.Join(root, "publish")
	if err := os.Mkdir(dir, 0700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "menu.json")
	writeMenu(t, path, demoMenu())
	e := fileEngine(t, path)
	first := mustRun(t, e, runRequest{300, "normal"})
	updated := demoMenu()
	updated[1].Price = 500
	next := filepath.Join(dir, "next.json")
	writeMenu(t, next, updated)
	if err := os.Rename(next, path); err != nil {
		t.Fatal("atomic replacement blocked:", err)
	}
	second := mustRun(t, e, runRequest{300, "normal"})
	if second.Status != "CONFIRMED_LOCAL" || len(second.Items) != 1 || first.ResourceHash == second.ResourceHash {
		t.Fatal("atomic update missed")
	}
	if err := os.Rename(dir, filepath.Join(root, "moved")); err != nil {
		if runtime.GOOS == "windows" {
			return
		} // Retained handles deny parent rename.
		t.Fatal(err)
	}
	if err := os.Mkdir(dir, 0700); err != nil {
		t.Fatal(err)
	}
	writeMenu(t, path, []item{})
	third := mustRun(t, e, runRequest{300, "normal"})
	if third.Status != "CONFIRMED_LOCAL" || third.ResourceHash != second.ResourceHash || len(third.Items) != 1 {
		t.Fatal("replaced directory redirected the read")
	}
}
