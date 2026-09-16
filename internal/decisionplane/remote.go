package decisionplane

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const defaultRemoteMaxResponseBytes int64 = 1 << 20

type RemoteProviderConfig struct {
	Endpoint         string
	ProviderID       string
	BearerToken      string
	Headers          map[string]string
	Timeout          time.Duration
	MaxResponseBytes int64
	HTTPClient       *http.Client
}

type RemoteProvider struct {
	endpoint         *url.URL
	providerID       string
	bearerToken      string
	headers          map[string]string
	maxResponseBytes int64
	client           *http.Client
}

type RemoteHTTPError struct {
	StatusCode int
	Status     string
	Body       string
}

func (err *RemoteHTTPError) Error() string {
	if err.Body == "" {
		return fmt.Sprintf("remote decision provider returned HTTP %d %s", err.StatusCode, err.Status)
	}
	return fmt.Sprintf("remote decision provider returned HTTP %d %s: %s", err.StatusCode, err.Status, err.Body)
}

func NewRemoteProvider(config RemoteProviderConfig) (*RemoteProvider, error) {
	if config.Endpoint == "" {
		return nil, errors.New("remote provider endpoint is required")
	}
	endpoint, err := url.Parse(config.Endpoint)
	if err != nil {
		return nil, fmt.Errorf("parse remote provider endpoint: %w", err)
	}
	if endpoint.Scheme != "http" && endpoint.Scheme != "https" {
		return nil, fmt.Errorf("remote provider endpoint scheme %q is not supported", endpoint.Scheme)
	}
	if endpoint.Host == "" {
		return nil, errors.New("remote provider endpoint host is required")
	}
	if config.ProviderID == "" {
		return nil, errors.New("remote provider id is required")
	}

	maxResponseBytes := config.MaxResponseBytes
	if maxResponseBytes == 0 {
		maxResponseBytes = defaultRemoteMaxResponseBytes
	}
	if maxResponseBytes < 1 {
		return nil, errors.New("remote provider max response bytes must be positive")
	}

	client := config.HTTPClient
	if client == nil {
		client = &http.Client{}
	} else {
		copyClient := *client
		client = &copyClient
	}
	if config.Timeout > 0 {
		client.Timeout = config.Timeout
	}

	headers := make(map[string]string, len(config.Headers))
	for name, value := range config.Headers {
		if strings.EqualFold(name, "Authorization") || strings.EqualFold(name, "Content-Type") || strings.EqualFold(name, "Accept") {
			return nil, fmt.Errorf("remote provider header %q is reserved", name)
		}
		headers[name] = value
	}

	return &RemoteProvider{
		endpoint:         endpoint,
		providerID:       config.ProviderID,
		bearerToken:      config.BearerToken,
		headers:          headers,
		maxResponseBytes: maxResponseBytes,
		client:           client,
	}, nil
}

func (provider *RemoteProvider) Decide(ctx context.Context, request Request) (Decision, error) {
	if provider == nil || provider.endpoint == nil || provider.client == nil {
		return Decision{}, errors.New("remote provider is not initialized")
	}
	payload, err := json.Marshal(request)
	if err != nil {
		return Decision{}, fmt.Errorf("encode remote decision request: %w", err)
	}

	httpRequest, err := http.NewRequestWithContext(ctx, http.MethodPost, provider.endpoint.String(), bytes.NewReader(payload))
	if err != nil {
		return Decision{}, fmt.Errorf("create remote decision request: %w", err)
	}
	httpRequest.Header.Set("Content-Type", "application/json")
	httpRequest.Header.Set("Accept", "application/json")
	if provider.bearerToken != "" {
		httpRequest.Header.Set("Authorization", "Bearer "+provider.bearerToken)
	}
	for name, value := range provider.headers {
		httpRequest.Header.Set(name, value)
	}

	response, err := provider.client.Do(httpRequest)
	if err != nil {
		return Decision{}, fmt.Errorf("call remote decision provider: %w", err)
	}
	defer response.Body.Close()

	body, err := readBoundedBody(response.Body, provider.maxResponseBytes)
	if err != nil {
		return Decision{}, err
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return Decision{}, &RemoteHTTPError{
			StatusCode: response.StatusCode,
			Status:     response.Status,
			Body:       strings.TrimSpace(string(body)),
		}
	}

	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.DisallowUnknownFields()
	var decision Decision
	if err := decoder.Decode(&decision); err != nil {
		return Decision{}, fmt.Errorf("decode remote decision response: %w", err)
	}
	if err := requireRemoteJSONEOF(decoder); err != nil {
		return Decision{}, err
	}
	if decision.ProviderID != provider.providerID {
		return Decision{}, fmt.Errorf("remote decision provider_id mismatch: expected %q, got %q", provider.providerID, decision.ProviderID)
	}
	if err := validateRemoteBinding(request, decision); err != nil {
		return Decision{}, err
	}
	return decision, nil
}

func readBoundedBody(reader io.Reader, maxBytes int64) ([]byte, error) {
	limited := io.LimitReader(reader, maxBytes+1)
	body, err := io.ReadAll(limited)
	if err != nil {
		return nil, fmt.Errorf("read remote decision response: %w", err)
	}
	if int64(len(body)) > maxBytes {
		return nil, fmt.Errorf("remote decision response exceeds %d bytes", maxBytes)
	}
	return body, nil
}

func requireRemoteJSONEOF(decoder *json.Decoder) error {
	var extra any
	if err := decoder.Decode(&extra); err == io.EOF {
		return nil
	} else if err != nil {
		return fmt.Errorf("decode trailing remote decision response: %w", err)
	}
	return errors.New("remote decision response contains multiple JSON values")
}

func validateRemoteBinding(request Request, decision Decision) error {
	if decision.Protocol != ResultProtocol {
		return fmt.Errorf("unsupported remote decision result protocol %q", decision.Protocol)
	}
	if decision.RequestID != request.RequestID {
		return errors.New("remote decision request_id mismatch")
	}
	if decision.ActionID != request.ActionID {
		return errors.New("remote decision action_id mismatch")
	}
	if decision.PacketHash != request.PacketHash {
		return errors.New("remote decision packet_hash mismatch")
	}
	if decision.StateHash != request.StateHash {
		return errors.New("remote decision state_hash mismatch")
	}
	if decision.ChoicesHash != request.ChoicesHash {
		return errors.New("remote decision choices_hash mismatch")
	}
	return nil
}
