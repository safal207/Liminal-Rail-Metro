package main

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"unicode/utf8"
)

const memoryProtocol = "metro.web.route-memory.v0.1"
const maxMemoryBytes = 32 << 10

type memoryDocument struct {
	Protocol string              `json:"protocol"`
	Routes   map[string][]string `json:"routes"`
}

// memoryStore holds one pinned, exclusively opened file for the process lifetime.
// It is an untrusted optimization cache, never evidence of a previous success.
type memoryStore struct{ file *os.File }

func validateMemory(doc memoryDocument) error {
	if doc.Protocol != memoryProtocol || doc.Routes == nil || len(doc.Routes) > 16 {
		return fmt.Errorf("invalid route memory format or bounds")
	}
	for key, route := range doc.Routes {
		decoded, err := hex.DecodeString(key)
		if err != nil || len(decoded) != 32 || hex.EncodeToString(decoded) != key || len(route) == 0 || len(route) > 16 {
			return fmt.Errorf("invalid route memory entry")
		}
		seen := map[string]bool{}
		for _, id := range route {
			if !graphID(id) || seen[id] {
				return fmt.Errorf("invalid remembered transition")
			}
			seen[id] = true
		}
	}
	return nil
}

// openMemory loads once at startup. Malformed/oversized files fail closed and are
// never automatically overwritten. The directory must already exist.
func openMemory(path string) (*memoryStore, map[string][]string, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, nil, err
	}
	parent, err := openResourceParent(abs)
	if err != nil {
		return nil, nil, err
	}
	defer parent.close()
	f, err := openMemoryFile(parent)
	if err != nil {
		return nil, nil, err
	}
	ok := false
	defer func() {
		if !ok {
			_ = f.Close()
		}
	}()
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Size() > maxMemoryBytes {
		return nil, nil, fmt.Errorf("memory must be a regular file of at most %d bytes", maxMemoryBytes)
	}
	raw, err := io.ReadAll(io.LimitReader(f, maxMemoryBytes+1))
	if err != nil || len(raw) > maxMemoryBytes {
		return nil, nil, fmt.Errorf("cannot read bounded route memory")
	}
	doc := memoryDocument{Protocol: memoryProtocol, Routes: map[string][]string{}}
	if len(raw) != 0 {
		doc = memoryDocument{}
		if !utf8.Valid(raw) {
			return nil, nil, fmt.Errorf("invalid memory encoding")
		}
		dec := json.NewDecoder(bytes.NewReader(raw))
		dec.DisallowUnknownFields()
		if err := dec.Decode(&doc); err != nil {
			return nil, nil, fmt.Errorf("invalid memory JSON: %w", err)
		}
		var extra any
		if dec.Decode(&extra) != io.EOF {
			return nil, nil, fmt.Errorf("trailing memory data")
		}
	}
	if err := validateMemory(doc); err != nil {
		return nil, nil, err
	}
	ok = true
	return &memoryStore{file: f}, doc.Routes, nil
}

// save deliberately uses the held file handle, not a newly resolved pathname.
// A crash during this in-place write can lose the cache; this is not a journal.
// Callers report errors separately from the already verified action result.
func (s *memoryStore) save(ctx context.Context, routes map[string][]string) error {
	doc := memoryDocument{Protocol: memoryProtocol, Routes: routes}
	if err := validateMemory(doc); err != nil {
		return err
	}
	raw, err := json.Marshal(doc)
	if err != nil {
		return err
	}
	if len(raw) > maxMemoryBytes {
		return fmt.Errorf("route memory exceeds byte limit")
	}
	// Commit boundary: observe cancellation after preparation, immediately before
	// the first file mutation. Once writing starts, complete truncate and sync;
	// abandoning an in-place write midway would deliberately corrupt the cache.
	if err := ctx.Err(); err != nil {
		return err
	}
	if _, err := s.file.WriteAt(raw, 0); err != nil {
		return err
	}
	if err := s.file.Truncate(int64(len(raw))); err != nil {
		return err
	}
	return s.file.Sync()
}

func (s *memoryStore) close() { _ = s.file.Close() }

func (e *engine) enableMemory(path string) error {
	store, routes, err := openMemory(path)
	if err != nil {
		return err
	}
	e.store, e.memory = store, routes
	e.restored = map[string]bool{}
	for key := range routes {
		e.restored[key] = true
	}
	return nil
}
