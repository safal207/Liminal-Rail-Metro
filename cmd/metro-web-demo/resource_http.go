package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

const remoteReadTimeout = 3 * time.Second
const maxPassportBytes = 8192

var errRemoteIncomplete = errors.New("remote read incomplete; no automatic retry")
var errRemoteChanged = errors.New("remote resource changed during read")
var errRemoteInvalid = errors.New("remote resource failed verification")

type httpResourceSource struct {
	base   string
	client *http.Client
}

// newHTTPResourceSource permits one operator-selected origin, with TLS except
// for literal loopback IPs. No URL, credentials or path comes from API callers.
func newHTTPResourceSource(raw string) (*httpResourceSource, error) {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" || u.Hostname() == "" || u.User != nil || u.Opaque != "" || u.RawQuery != "" || u.ForceQuery || strings.Contains(raw, "#") || (u.Path != "" && u.Path != "/") || u.RawPath != "" {
		return nil, fmt.Errorf("remote resource requires an origin URL without credentials, path, query or fragment")
	}
	if port := u.Port(); port != "" {
		n, err := strconv.Atoi(port)
		if err != nil || n < 1 || n > 65535 {
			return nil, fmt.Errorf("invalid remote port")
		}
	} else if strings.HasSuffix(u.Host, ":") {
		return nil, fmt.Errorf("invalid remote port")
	}
	if u.Scheme != "https" {
		ip := net.ParseIP(u.Hostname())
		if u.Scheme != "http" || ip == nil || !ip.IsLoopback() {
			return nil, fmt.Errorf("HTTPS is required outside literal loopback addresses")
		}
	}
	if strings.ContainsAny(u.Host, "\\\r\n") {
		return nil, fmt.Errorf("invalid remote origin")
	}
	base := u.Scheme + "://" + u.Host
	transport := &http.Transport{
		Proxy:                  nil,
		DialContext:            (&net.Dialer{Timeout: time.Second}).DialContext,
		TLSHandshakeTimeout:    time.Second,
		ResponseHeaderTimeout:  remoteReadTimeout,
		MaxResponseHeaderBytes: 16384,
		DisableCompression:     true,
		// Fresh HTTP/1 connections avoid Go's retry on a reused idle connection.
		DisableKeepAlives: true,
	}
	return &httpResourceSource{base: base, client: &http.Client{
		Transport: transport, Timeout: remoteReadTimeout,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}}, nil
}

// origin records the configured publisher separately from its untrusted claims.
func (s *httpResourceSource) origin() resourceOrigin {
	return resourceOrigin{Transport: "http", Origin: s.base}
}

// close releases transport resources after all readers have stopped.
func (s *httpResourceSource) close() { s.client.CloseIdleConnections() }

// fetch performs one bounded GET. Redirects, encodings and non-JSON responses
// are rejected; uncertain transport failures never cause an automatic retry.
func (s *httpResourceSource) fetch(ctx context.Context, path string, limit int) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, s.base+path, nil)
	if err != nil {
		return nil, errRemoteInvalid
	}
	req.Header.Set("Accept", "application/json")
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
	media, _, err := mime.ParseMediaType(resp.Header.Get("Content-Type"))
	if err != nil || media != "application/json" || resp.Header.Get("Content-Encoding") != "" || resp.ContentLength > int64(limit) {
		return nil, errRemoteInvalid
	}
	raw, err := io.ReadAll(io.LimitReader(resp.Body, int64(limit)+1))
	if err != nil {
		return nil, errRemoteIncomplete
	}
	if len(raw) > limit {
		return nil, errRemoteInvalid
	}
	return raw, nil
}

// read fetches a fresh passport and pinned bytes within one overall deadline.
// The local manifest is reconstructed only after all publisher claims verify.
func (s *httpResourceSource) read(parent context.Context) (out resourceSnapshot, err error) {
	ctx, cancel := context.WithTimeout(parent, remoteReadTimeout)
	defer cancel()
	if ctx.Err() != nil {
		return out, errRemoteIncomplete
	}
	out.Requests++
	raw, err := s.fetch(ctx, "/api/resource", maxPassportBytes)
	if err != nil {
		return out, err
	}
	if !utf8.Valid(raw) {
		return out, errRemoteInvalid
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	var passport resourceManifest
	if dec.Decode(&passport) != nil {
		return out, errRemoteInvalid
	}
	var extra any
	if dec.Decode(&extra) != io.EOF || !validRemotePassport(passport) {
		return out, errRemoteInvalid
	}
	out.Requests++
	raw, err = s.fetch(ctx, passport.Href, maxResourceBytes)
	if err != nil {
		return out, err
	}
	digest := sha256.Sum256(raw)
	if len(raw) != passport.Size || hex.EncodeToString(digest[:]) != passport.SHA256 {
		return out, errRemoteInvalid
	}
	snapshot, err := parseResource(raw)
	if err != nil || snapshot.Manifest != passport {
		return out, errRemoteInvalid
	}
	snapshot.Requests = out.Requests
	return snapshot, nil
}

// validRemotePassport checks the closed menu protocol and the exact content
// path before any second request, preventing redirection through publisher data.
func validRemotePassport(p resourceManifest) bool {
	hash, err := hex.DecodeString(p.SHA256)
	return err == nil && len(hash) == sha256.Size && p.SHA256 == strings.ToLower(p.SHA256) &&
		p.Protocol == "metro.web.resource.v0.1" && p.ID == "menu" &&
		p.Schema == menuSchema && p.MediaType == "application/json" && p.Scope == "menu.read" &&
		strings.TrimSpace(p.Title) != "" && len(p.Title) <= 200 &&
		p.Version == "sha256:"+p.SHA256 && p.Size > 0 && p.Size <= maxResourceBytes &&
		p.Href == "/api/resource/content?sha256="+p.SHA256
}
