package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"unicode/utf8"
)

const (
	assetProtocol         = "metro.web.asset.v0.1"
	assetContentPath      = "/api/asset/content"
	assetFullPath         = "/api/asset/full"
	assetChunkBytes       = 64 << 10
	maxAssetBytes         = 8 << 20
	maxAssetPassportBytes = 16 << 10
)

// An asset passport describes one bounded byte sequence. Its whole-file hash
// is a publisher claim for a remote reader until every chunk has been read.
type assetManifest struct {
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

type assetService struct {
	local  *resourceSource
	remote *httpResourceSource
}

var errAssetIndex = errors.New("asset chunk index out of range")

func assetHash(raw []byte) string {
	digest := sha256.Sum256(raw)
	return hex.EncodeToString(digest[:])
}

func validAssetHash(value string) bool {
	if len(value) != sha256.Size*2 || value != strings.ToLower(value) {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}

func assetChunkCount(size int) int {
	return (size + assetChunkBytes - 1) / assetChunkBytes
}

func assetChunkSize(size, index int) int {
	remaining := size - index*assetChunkBytes
	if remaining > assetChunkBytes {
		return assetChunkBytes
	}
	return remaining
}

func manifestForAsset(raw []byte) assetManifest {
	wholeHash := assetHash(raw)
	chunks := make([]string, 0, assetChunkCount(len(raw)))
	for start := 0; start < len(raw); start += assetChunkBytes {
		end := start + assetChunkBytes
		if end > len(raw) {
			end = len(raw)
		}
		chunks = append(chunks, assetHash(raw[start:end]))
	}
	return assetManifest{
		Protocol: assetProtocol, ID: "asset", MediaType: "application/octet-stream",
		Size: len(raw), SHA256: wholeHash, Version: "sha256:" + wholeHash, ChunkSize: assetChunkBytes,
		ChunkHash: chunks, Href: assetContentPath,
	}
}

func (m assetManifest) valid() bool {
	if m.Protocol != assetProtocol || m.ID != "asset" || m.MediaType != "application/octet-stream" ||
		m.Size < 0 || m.Size > maxAssetBytes || !validAssetHash(m.SHA256) || m.Version != "sha256:"+m.SHA256 ||
		m.ChunkSize != assetChunkBytes || m.Href != assetContentPath ||
		m.ChunkHash == nil || len(m.ChunkHash) != assetChunkCount(m.Size) {
		return false
	}
	for _, hash := range m.ChunkHash {
		if !validAssetHash(hash) {
			return false
		}
	}
	if m.Size == 0 && m.SHA256 != assetHash(nil) {
		return false
	}
	return true
}

func parseAssetManifest(raw []byte) (assetManifest, error) {
	var manifest assetManifest
	if len(raw) == 0 || len(raw) > maxAssetPassportBytes || !utf8.Valid(raw) {
		return manifest, errRemoteInvalid
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if dec.Decode(&manifest) != nil {
		return assetManifest{}, errRemoteInvalid
	}
	var trailing any
	if dec.Decode(&trailing) != io.EOF || !manifest.valid() {
		return assetManifest{}, errRemoteInvalid
	}
	return manifest, nil
}

// manifest reads one fresh local file or one remote passport. A remote whole
// hash is a claimed value; content reads verify the selected chunk separately.
func (s *assetService) manifest(ctx context.Context) (assetManifest, error) {
	if s == nil {
		return assetManifest{}, errRemoteInvalid
	}
	if s.local != nil && s.remote == nil {
		raw, err := s.local.readBytes(ctx, maxAssetBytes)
		if err != nil || ctx.Err() != nil {
			return assetManifest{}, errRemoteIncomplete
		}
		return manifestForAsset(raw), nil
	}
	if s.remote != nil && s.local == nil {
		ctx, cancel := context.WithTimeout(ctx, remoteReadTimeout)
		defer cancel()
		raw, err := s.remote.fetch(ctx, "/api/asset", maxAssetPassportBytes)
		if err != nil {
			return assetManifest{}, err
		}
		return parseAssetManifest(raw)
	}
	return assetManifest{}, errRemoteInvalid
}

// readChunk pins a full local snapshot or fetches one remote passport and one
// chosen chunk under one deadline. It never constructs a URL from a publisher
// supplied string or tries a second network request after a failed attempt.
func (s *assetService) readChunk(parent context.Context, hash string, index int) (assetManifest, []byte, error) {
	if s == nil || !validAssetHash(hash) || index < 0 || index >= maxAssetBytes/assetChunkBytes {
		return assetManifest{}, nil, errRemoteInvalid
	}
	if s.local != nil && s.remote == nil {
		raw, err := s.local.readBytes(parent, maxAssetBytes)
		if err != nil || parent.Err() != nil {
			return assetManifest{}, nil, errRemoteIncomplete
		}
		manifest := manifestForAsset(raw)
		if hash != manifest.SHA256 {
			return manifest, nil, errRemoteChanged
		}
		if index >= len(manifest.ChunkHash) {
			return manifest, nil, errAssetIndex
		}
		start := index * assetChunkBytes
		end := start + assetChunkSize(manifest.Size, index)
		return manifest, raw[start:end], nil
	}
	if s.remote != nil && s.local == nil {
		ctx, cancel := context.WithTimeout(parent, remoteReadTimeout)
		defer cancel()
		if ctx.Err() != nil {
			return assetManifest{}, nil, errRemoteIncomplete
		}
		passport, err := s.remote.fetch(ctx, "/api/asset", maxAssetPassportBytes)
		if err != nil {
			return assetManifest{}, nil, err
		}
		manifest, err := parseAssetManifest(passport)
		if err != nil {
			return assetManifest{}, nil, err
		}
		if hash != manifest.SHA256 {
			return manifest, nil, errRemoteChanged
		}
		if index >= len(manifest.ChunkHash) {
			return manifest, nil, errAssetIndex
		}
		path := assetContentPath + "?sha256=" + manifest.SHA256 + "&index=" + strconv.Itoa(index)
		chunk, err := s.remote.fetchAssetChunk(ctx, path, assetChunkSize(manifest.Size, index))
		if err != nil {
			return manifest, nil, err
		}
		if assetHash(chunk) != manifest.ChunkHash[index] {
			return manifest, nil, errRemoteInvalid
		}
		return manifest, chunk, nil
	}
	return assetManifest{}, nil, errRemoteInvalid
}

// readFull verifies the whole requested version in one bounded file read or
// one passport plus one pinned network response. The reader checks both the
// whole digest and every chunk digest before releasing any downloaded bytes.
func (s *assetService) readFull(parent context.Context, hash string) ([]byte, error) {
	if s == nil || !validAssetHash(hash) {
		return nil, errRemoteInvalid
	}
	if s.local != nil && s.remote == nil {
		raw, err := s.local.readBytes(parent, maxAssetBytes)
		if err != nil || parent.Err() != nil {
			return nil, errRemoteIncomplete
		}
		if assetHash(raw) != hash {
			return nil, errRemoteChanged
		}
		return raw, nil
	}
	if s.remote != nil && s.local == nil {
		ctx, cancel := context.WithTimeout(parent, remoteReadTimeout)
		defer cancel()
		if ctx.Err() != nil {
			return nil, errRemoteIncomplete
		}
		passport, err := s.remote.fetch(ctx, "/api/asset", maxAssetPassportBytes)
		if err != nil {
			return nil, err
		}
		manifest, err := parseAssetManifest(passport)
		if err != nil {
			return nil, err
		}
		if hash != manifest.SHA256 {
			return nil, errRemoteChanged
		}
		path := assetFullPath + "?sha256=" + manifest.SHA256
		raw, err := s.remote.fetchAssetBytes(ctx, path, manifest.Size)
		if err != nil {
			return nil, err
		}
		if assetHash(raw) != manifest.SHA256 {
			return nil, errRemoteInvalid
		}
		for index, expected := range manifest.ChunkHash {
			start := index * assetChunkBytes
			end := start + assetChunkSize(manifest.Size, index)
			if assetHash(raw[start:end]) != expected {
				return nil, errRemoteInvalid
			}
		}
		if ctx.Err() != nil {
			return nil, errRemoteIncomplete
		}
		return raw, nil
	}
	return nil, errRemoteInvalid
}

// fetchAssetChunk shares the origin, transport and no-redirect client policy
// with menu reads, but requires exact binary media and a bounded chunk body.
func (s *httpResourceSource) fetchAssetChunk(ctx context.Context, path string, expected int) ([]byte, error) {
	if expected < 1 || expected > assetChunkBytes {
		return nil, errRemoteInvalid
	}
	return s.fetchAssetBytes(ctx, path, expected)
}

// fetchAssetBytes uses the existing fixed-origin client. A full download is
// capped at 8 MiB; a chunk caller additionally caps its own expected length.
func (s *httpResourceSource) fetchAssetBytes(ctx context.Context, path string, expected int) ([]byte, error) {
	if expected < 0 || expected > maxAssetBytes {
		return nil, errRemoteInvalid
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, s.base+path, nil)
	if err != nil {
		return nil, errRemoteInvalid
	}
	req.Header.Set("Accept", "application/octet-stream")
	req.Header.Set("Cache-Control", "no-cache")
	req.Header.Set("X-Metro-Resource-Read", "1")
	resp, err := s.client.Do(req)
	if err != nil {
		return nil, errRemoteIncomplete
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode == http.StatusConflict {
		return nil, errRemoteChanged
	}
	if resp.StatusCode >= 500 && resp.StatusCode != http.StatusLoopDetected {
		return nil, errRemoteIncomplete
	}
	if resp.StatusCode != http.StatusOK {
		return nil, errRemoteInvalid
	}
	media, params, err := mime.ParseMediaType(resp.Header.Get("Content-Type"))
	if err != nil || media != "application/octet-stream" || len(params) != 0 ||
		resp.Header.Get("Content-Encoding") != "" || resp.Header.Get("Content-Range") != "" ||
		resp.ContentLength > int64(expected) {
		return nil, errRemoteInvalid
	}
	raw, err := io.ReadAll(io.LimitReader(resp.Body, int64(expected)+1))
	if err != nil {
		return nil, errRemoteIncomplete
	}
	if len(raw) != expected {
		return nil, errRemoteInvalid
	}
	if ctx.Err() != nil {
		return nil, errRemoteIncomplete
	}
	return raw, nil
}

func parseAssetQuery(raw string, content bool) (string, int, error) {
	query, err := url.ParseQuery(raw)
	if err != nil {
		return "", 0, err
	}
	if !content {
		if len(query) == 0 {
			return "", 0, nil
		}
		return "", 0, errRemoteInvalid
	}
	if len(query) != 2 || len(query["sha256"]) != 1 || len(query["index"]) != 1 {
		return "", 0, errRemoteInvalid
	}
	hash, rawIndex := query.Get("sha256"), query.Get("index")
	if !validAssetHash(hash) || rawIndex == "" || (len(rawIndex) > 1 && rawIndex[0] == '0') {
		return "", 0, errRemoteInvalid
	}
	for _, c := range rawIndex {
		if c < '0' || c > '9' {
			return "", 0, errRemoteInvalid
		}
	}
	index, err := strconv.Atoi(rawIndex)
	if err != nil || index < 0 || index >= maxAssetBytes/assetChunkBytes {
		return "", 0, errRemoteInvalid
	}
	return hash, index, nil
}

func parseAssetFullQuery(raw string) (string, error) {
	query, err := url.ParseQuery(raw)
	if err != nil || len(query) != 1 || len(query["sha256"]) != 1 {
		return "", errRemoteInvalid
	}
	hash := query.Get("sha256")
	if !validAssetHash(hash) {
		return "", errRemoteInvalid
	}
	return hash, nil
}

// serve publishes a single operator-selected asset. The content endpoint is
// always an attachment with a generic filename, never interpreted as HTML.
func (s *assetService) serve(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if s == nil || (s.local == nil) == (s.remote == nil) {
		http.NotFound(w, r)
		return
	}
	content := r.URL.Path == assetContentPath
	full := r.URL.Path == assetFullPath
	if !content && !full && r.URL.Path != "/api/asset" {
		http.NotFound(w, r)
		return
	}
	if r.URL.ForceQuery {
		http.Error(w, "invalid asset query", http.StatusBadRequest)
		return
	}
	var hash string
	var index int
	var err error
	if full {
		hash, err = parseAssetFullQuery(r.URL.RawQuery)
	} else {
		hash, index, err = parseAssetQuery(r.URL.RawQuery, content)
	}
	if err != nil {
		http.Error(w, "invalid asset query", http.StatusBadRequest)
		return
	}
	if s.remote != nil && r.Header.Get("X-Metro-Resource-Read") != "" {
		http.Error(w, "reader chains are unsupported", http.StatusLoopDetected)
		return
	}
	if !content && !full {
		manifest, err := s.manifest(r.Context())
		if err != nil {
			assetError(w, err)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("X-Metro-Asset-Whole-Verified", strconv.FormatBool(s.local != nil))
		_ = json.NewEncoder(w).Encode(manifest)
		return
	}
	if full {
		raw, err := s.readFull(r.Context(), hash)
		if err != nil {
			assetError(w, err)
			return
		}
		w.Header().Set("Content-Type", "application/octet-stream")
		w.Header().Set("Content-Disposition", "attachment; filename=\"metro-asset.bin\"")
		w.Header().Set("Content-Length", strconv.Itoa(len(raw)))
		w.Header().Set("ETag", `"`+hash+`"`)
		w.Header().Set("X-Metro-Asset-Whole-Verified", "true")
		_, _ = w.Write(raw)
		return
	}
	manifest, chunk, err := s.readChunk(r.Context(), hash, index)
	if err != nil {
		assetError(w, err)
		return
	}
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Disposition", "attachment; filename=\"metro-asset.bin\"")
	w.Header().Set("Content-Length", strconv.Itoa(len(chunk)))
	w.Header().Set("ETag", `"`+manifest.SHA256+`"`)
	w.Header().Set("X-Metro-Asset-Chunk-SHA256", manifest.ChunkHash[index])
	w.Header().Set("X-Metro-Asset-Whole-Verified", strconv.FormatBool(s.local != nil))
	_, _ = w.Write(chunk)
}

func assetError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, errRemoteChanged):
		http.Error(w, "asset changed; fetch its passport again", http.StatusConflict)
	case errors.Is(err, errRemoteIncomplete):
		http.Error(w, "asset unavailable", http.StatusServiceUnavailable)
	case errors.Is(err, errAssetIndex):
		http.Error(w, "asset chunk index out of range", http.StatusBadRequest)
	default:
		http.Error(w, "asset failed verification", http.StatusBadGateway)
	}
}
