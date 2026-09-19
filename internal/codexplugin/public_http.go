package codexplugin

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"
)

const (
	defaultPublicMaxRequestBytes int64 = 256 << 10
	defaultPublicMaxConcurrent         = 64
	defaultPublicRequestTimeout        = 30 * time.Second
)

type PublicHTTPConfig struct {
	MaxRequestBytes int64
	MaxConcurrent   int
	RequestTimeout  time.Duration
}

func DefaultPublicHTTPConfig() PublicHTTPConfig {
	return PublicHTTPConfig{
		MaxRequestBytes: defaultPublicMaxRequestBytes,
		MaxConcurrent:   defaultPublicMaxConcurrent,
		RequestTimeout:  defaultPublicRequestTimeout,
	}
}

func PublicHTTPConfigFromEnv() (PublicHTTPConfig, error) {
	config := DefaultPublicHTTPConfig()

	if raw := strings.TrimSpace(os.Getenv("LIMINAL_MCP_MAX_REQUEST_BYTES")); raw != "" {
		value, err := strconv.ParseInt(raw, 10, 64)
		if err != nil || value <= 0 {
			return PublicHTTPConfig{}, errors.New("LIMINAL_MCP_MAX_REQUEST_BYTES must be a positive integer")
		}
		config.MaxRequestBytes = value
	}
	if raw := strings.TrimSpace(os.Getenv("LIMINAL_MCP_MAX_CONCURRENT")); raw != "" {
		value, err := strconv.Atoi(raw)
		if err != nil || value <= 0 {
			return PublicHTTPConfig{}, errors.New("LIMINAL_MCP_MAX_CONCURRENT must be a positive integer")
		}
		config.MaxConcurrent = value
	}
	if raw := strings.TrimSpace(os.Getenv("LIMINAL_MCP_REQUEST_TIMEOUT_MS")); raw != "" {
		value, err := strconv.ParseInt(raw, 10, 64)
		if err != nil || value <= 0 {
			return PublicHTTPConfig{}, errors.New("LIMINAL_MCP_REQUEST_TIMEOUT_MS must be a positive integer")
		}
		config.RequestTimeout = time.Duration(value) * time.Millisecond
	}
	return config, nil
}

func HardenMCPHandler(next http.Handler, config PublicHTTPConfig) (http.Handler, error) {
	if next == nil {
		return nil, errors.New("MCP handler is required")
	}
	if config.MaxRequestBytes <= 0 {
		return nil, errors.New("public MCP max request bytes must be positive")
	}
	if config.MaxConcurrent <= 0 {
		return nil, errors.New("public MCP max concurrent requests must be positive")
	}
	if config.RequestTimeout <= 0 {
		return nil, errors.New("public MCP request timeout must be positive")
	}

	slots := make(chan struct{}, config.MaxConcurrent)
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Cache-Control", "no-store")
		writer.Header().Set("X-Content-Type-Options", "nosniff")
		writer.Header().Set("Referrer-Policy", "no-referrer")

		select {
		case slots <- struct{}{}:
			defer func() { <-slots }()
		default:
			writer.Header().Set("Retry-After", "1")
			http.Error(writer, "MCP capacity exhausted", http.StatusTooManyRequests)
			return
		}

		if request.Body != nil {
			request.Body = http.MaxBytesReader(writer, request.Body, config.MaxRequestBytes)
		}
		ctx, cancel := context.WithTimeout(request.Context(), config.RequestTimeout)
		defer cancel()
		next.ServeHTTP(writer, request.WithContext(ctx))
	}), nil
}

func ListenAddressFromEnv() (string, error) {
	if raw := strings.TrimSpace(os.Getenv("LIMINAL_LISTEN_ADDR")); raw != "" {
		return raw, nil
	}
	port := strings.TrimSpace(os.Getenv("PORT"))
	if port == "" {
		return "127.0.0.1:8787", nil
	}
	value, err := strconv.Atoi(port)
	if err != nil || value < 1 || value > 65535 {
		return "", fmt.Errorf("PORT must be an integer in [1,65535]")
	}
	return fmt.Sprintf("0.0.0.0:%d", value), nil
}
