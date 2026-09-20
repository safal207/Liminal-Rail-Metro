package moltbook

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

const (
	moltbookVerifyIdentityURL      = "https://www.moltbook.com/api/v1/agents/verify-identity"
	moltbookIdentityResponseLimit  = 256 * 1024
	moltbookIdentityDefaultTimeout = 5 * time.Second
)

type MoltbookIdentityVerifier struct {
	appKey string
	client *http.Client
}

type moltbookIdentityRequest struct {
	Token string `json:"token"`
}

type moltbookIdentityResponse struct {
	Success bool `json:"success"`
	Valid   bool `json:"valid"`
	Agent   struct {
		ID string `json:"id"`
	} `json:"agent"`
}

func NewMoltbookIdentityVerifier(appKey string, client *http.Client) (*MoltbookIdentityVerifier, error) {
	appKey = strings.TrimSpace(appKey)
	if appKey == "" {
		return nil, errors.New("Moltbook app key is required")
	}
	if !strings.HasPrefix(appKey, "moltdev_") {
		return nil, errors.New("Moltbook app key has unexpected format")
	}

	configured := &http.Client{}
	if client != nil {
		*configured = *client
	} else {
		*configured = *http.DefaultClient
	}
	if configured.Timeout <= 0 {
		configured.Timeout = moltbookIdentityDefaultTimeout
	}
	configured.CheckRedirect = func(_ *http.Request, _ []*http.Request) error {
		return http.ErrUseLastResponse
	}

	return &MoltbookIdentityVerifier{
		appKey: appKey,
		client: configured,
	}, nil
}

func (v *MoltbookIdentityVerifier) VerifyIdentity(token string) (VerifiedIdentity, error) {
	return v.VerifyIdentityContext(context.Background(), token)
}

func (v *MoltbookIdentityVerifier) VerifyIdentityContext(ctx context.Context, token string) (VerifiedIdentity, error) {
	if v == nil || v.client == nil || strings.TrimSpace(v.appKey) == "" {
		return VerifiedIdentity{}, errors.New("Moltbook identity verifier is not configured")
	}

	token = strings.TrimSpace(token)
	if token == "" {
		return VerifiedIdentity{}, errors.New("Moltbook identity token is required")
	}

	body, err := json.Marshal(moltbookIdentityRequest{Token: token})
	if err != nil {
		return VerifiedIdentity{}, errors.New("encode Moltbook identity request")
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, moltbookVerifyIdentityURL, bytes.NewReader(body))
	if err != nil {
		return VerifiedIdentity{}, errors.New("build Moltbook identity request")
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("X-Moltbook-App-Key", v.appKey)

	resp, err := v.client.Do(req)
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return VerifiedIdentity{}, fmt.Errorf("Moltbook identity verification canceled: %w", ctxErr)
		}
		return VerifiedIdentity{}, errors.New("Moltbook identity verification request failed")
	}
	defer resp.Body.Close()

	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return VerifiedIdentity{}, fmt.Errorf("Moltbook identity verification returned HTTP %d", resp.StatusCode)
	}

	payload, err := io.ReadAll(io.LimitReader(resp.Body, moltbookIdentityResponseLimit+1))
	if err != nil {
		return VerifiedIdentity{}, errors.New("read Moltbook identity verification response")
	}
	if len(payload) > moltbookIdentityResponseLimit {
		return VerifiedIdentity{}, errors.New("Moltbook identity verification response exceeds limit")
	}

	var decoded moltbookIdentityResponse
	if err := json.Unmarshal(payload, &decoded); err != nil {
		return VerifiedIdentity{}, errors.New("Moltbook identity verification returned invalid JSON")
	}
	if !decoded.Success || !decoded.Valid {
		return VerifiedIdentity{}, errors.New("Moltbook identity token is invalid")
	}

	agentID := strings.TrimSpace(decoded.Agent.ID)
	if agentID == "" {
		return VerifiedIdentity{}, errors.New("Moltbook identity response is missing agent id")
	}

	return VerifiedIdentity{
		AgentID:  agentID,
		Verified: true,
	}, nil
}
