package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"unicode/utf8"
)

const menuSchema = "metro.web.menu.v0.1"
const maxResourceBytes = 1 << 20

type menuDocument struct {
	Schema string `json:"schema"`
	Title  string `json:"title"`
	Items  []item `json:"items"`
}

type resourceManifest struct {
	Protocol  string `json:"protocol"`
	ID        string `json:"id"`
	Title     string `json:"title"`
	Schema    string `json:"schema"`
	MediaType string `json:"media_type"`
	Version   string `json:"version"`
	SHA256    string `json:"sha256"`
	Size      int    `json:"size_bytes"`
	Href      string `json:"href"`
	Scope     string `json:"scope"`
}

type resourceSnapshot struct {
	Manifest resourceManifest
	Document menuDocument
	Bytes    []byte
}

// readResource reads one bounded snapshot from the operator-configured regular
// file. Parsing and its byte digest always refer to the same read, never a cache.
func readResource(path string) (resourceSnapshot, error) {
	var out resourceSnapshot
	info, err := os.Stat(path)
	if err != nil || !info.Mode().IsRegular() || info.Size() > maxResourceBytes {
		return out, fmt.Errorf("resource must be a regular file of at most 1 MiB")
	}
	f, err := os.Open(path)
	if err != nil {
		return out, fmt.Errorf("resource cannot be opened")
	}
	defer f.Close()
	info, err = f.Stat()
	if err != nil || !info.Mode().IsRegular() {
		return out, fmt.Errorf("resource must be a regular file")
	}
	raw, err := io.ReadAll(io.LimitReader(f, maxResourceBytes+1))
	if err != nil || len(raw) > maxResourceBytes || !utf8.Valid(raw) {
		return out, fmt.Errorf("resource must be bounded UTF-8 JSON")
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	var doc menuDocument
	if err := dec.Decode(&doc); err != nil {
		return out, fmt.Errorf("invalid menu JSON")
	}
	var extra any
	if dec.Decode(&extra) != io.EOF {
		return out, fmt.Errorf("trailing menu data")
	}
	if err := doc.validate(); err != nil {
		return out, err
	}
	digest := sha256.Sum256(raw)
	hash := hex.EncodeToString(digest[:])
	manifest := resourceManifest{
		Protocol: "metro.web.resource.v0.1", ID: "menu", Title: doc.Title,
		Schema: doc.Schema, MediaType: "application/json", Version: "sha256:" + hash,
		SHA256: hash, Size: len(raw), Href: "/api/resource/content?sha256=" + hash, Scope: "menu.read",
	}
	return resourceSnapshot{Manifest: manifest, Document: doc, Bytes: raw}, nil
}

// validate enforces the small menu adapter's schema before any item is used.
func (d menuDocument) validate() error {
	if d.Schema != menuSchema || strings.TrimSpace(d.Title) == "" || len(d.Title) > 200 || d.Items == nil || len(d.Items) > 128 {
		return fmt.Errorf("invalid menu schema, title or item count")
	}
	ids := map[string]bool{}
	for _, it := range d.Items {
		if strings.TrimSpace(it.ID) == "" || len(it.ID) > 80 || ids[it.ID] || strings.TrimSpace(it.Name) == "" || len(it.Name) > 200 || it.Price < 1 || it.Price > 1000000 || len(it.Ingredients) < 1 || len(it.Ingredients) > 32 {
			return fmt.Errorf("invalid or duplicate menu item")
		}
		ids[it.ID] = true
		for _, ingredient := range it.Ingredients {
			if strings.TrimSpace(ingredient) == "" || len(ingredient) > 200 {
				return fmt.Errorf("invalid ingredient")
			}
		}
	}
	return nil
}

// serveResource exposes only the configured menu, never a client-supplied path.
// Content requests must pin its digest; a changed version fails with HTTP 409.
func (e *engine) serveResource(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if e.resourcePath == "" {
		http.NotFound(w, r)
		return
	}
	content := r.URL.Path == "/api/resource/content"
	query, err := parseResourceQuery(r.URL.RawQuery, content)
	if err != nil {
		http.Error(w, "invalid resource query", http.StatusBadRequest)
		return
	}
	snapshot, err := readResource(e.resourcePath)
	if err != nil {
		http.Error(w, "resource unavailable or invalid", http.StatusServiceUnavailable)
		return
	}
	if content && query.Get("sha256") != snapshot.Manifest.SHA256 {
		http.Error(w, "resource changed; fetch its manifest again", http.StatusConflict)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	if content {
		w.Header().Set("ETag", `"`+snapshot.Manifest.SHA256+`"`)
		_, _ = w.Write(snapshot.Bytes)
		return
	}
	_ = json.NewEncoder(w).Encode(snapshot.Manifest)
}

// parseResourceQuery requires exactly one lowercase SHA-256 for content and no
// query for metadata, rejecting path parameters and ambiguous duplicate values.
func parseResourceQuery(raw string, content bool) (url.Values, error) {
	q, err := url.ParseQuery(raw)
	if err != nil {
		return nil, err
	}
	if !content && len(q) == 0 {
		return q, nil
	}
	if content && len(q) == 1 && len(q["sha256"]) == 1 {
		h := q.Get("sha256")
		if b, err := hex.DecodeString(h); err == nil && len(b) == sha256.Size && h == strings.ToLower(h) {
			return q, nil
		}
	}
	return nil, fmt.Errorf("invalid resource query")
}
