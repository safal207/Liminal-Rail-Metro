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
	"unicode"
)

const (
	moltbookVerifyIdentityURL      = "https://www.moltbook.com/api/v1/agents/verify-identity"
	moltbookIdentityResponseLimit  = 256 * 1024
	moltbookIdentityDefaultTimeout = 5 * time.Second
	moltbookAgentIDMaxBytes        = 256

	IdentityStatusVerified      IdentityVerificationStatus = "VERIFIED"
	IdentityStatusInvalid       IdentityVerificationStatus = "INVALID"
	IdentityStatusUnknownOrHold IdentityVerificationStatus = "UNKNOWN_OR_HOLD"
)

type IdentityVerificationStatus string

type IdentityVerificationResult struct {
	Provider           string                     `json:"provider"`
	AgentID            string                     `json:"agent_id,omitempty"`
	Status             IdentityVerificationStatus `json:"status"`
	VerifiedAt         string                     `json:"verified_at,omitempty"`
	VerificationSource string                     `json:"verification_source"`
}

type IdentityVerificationError struct {
	Status  IdentityVerificationStatus
	Code    string
	Message string
	Cause   error
}

func (e *IdentityVerificationError) Error() string {
	if e == nil {
		return ""
	}
	return e.Message
}

func (e *IdentityVerificationError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Cause
}

func IdentityVerificationStatusOf(err error) (IdentityVerificationStatus, bool) {
	var target *IdentityVerificationError
	if !errors.As(err, &target) {
		return "", false
	}
	return target.Status, true
}

type MoltbookIdentityVerifier struct {
	appKey string
	client *http.Client
	now    func() time.Time
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
		now:    time.Now,
	}, nil
}

func (v *MoltbookIdentityVerifier) VerifyIdentity(token string) (VerifiedIdentity, error) {
	return v.VerifyIdentityContext(context.Background(), token)
}

func (v *MoltbookIdentityVerifier) VerifyIdentityContext(ctx context.Context, token string) (VerifiedIdentity, error) {
	result, err := v.VerifyIdentityResultContext(ctx, token)
	if err != nil {
		return VerifiedIdentity{}, err
	}
	if result.Status != IdentityStatusVerified || result.AgentID == "" {
		return VerifiedIdentity{}, newIdentityVerificationError(
			IdentityStatusUnknownOrHold,
			"unexpected_result_state",
			"Moltbook identity verification did not produce a verified identity",
			nil,
		)
	}
	return VerifiedIdentity{
		AgentID:  result.AgentID,
		Verified: true,
	}, nil
}

func (v *MoltbookIdentityVerifier) VerifyIdentityResultContext(ctx context.Context, token string) (IdentityVerificationResult, error) {
	base := IdentityVerificationResult{
		Provider:           "moltbook",
		Status:             IdentityStatusUnknownOrHold,
		VerificationSource: moltbookVerifyIdentityURL,
	}

	if v == nil || v.client == nil || strings.TrimSpace(v.appKey) == "" || v.now == nil {
		return base, newIdentityVerificationError(
			IdentityStatusUnknownOrHold,
			"not_configured",
			"Moltbook identity verifier is not configured",
			nil,
		)
	}

	token = strings.TrimSpace(token)
	if token == "" {
		invalid := base
		invalid.Status = IdentityStatusInvalid
		return invalid, newIdentityVerificationError(
			IdentityStatusInvalid,
			"empty_token",
			"Moltbook identity token is required",
			nil,
		)
	}

	body, err := json.Marshal(moltbookIdentityRequest{Token: token})
	if err != nil {
		return base, newIdentityVerificationError(
			IdentityStatusUnknownOrHold,
			"request_encode_failed",
			"encode Moltbook identity request",
			nil,
		)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, moltbookVerifyIdentityURL, bytes.NewReader(body))
	if err != nil {
		return base, newIdentityVerificationError(
			IdentityStatusUnknownOrHold,
			"request_build_failed",
			"build Moltbook identity request",
			nil,
		)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("X-Moltbook-App-Key", v.appKey)

	resp, err := v.client.Do(req)
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return base, newIdentityVerificationError(
				IdentityStatusUnknownOrHold,
				"context_canceled",
				"Moltbook identity verification canceled",
				ctxErr,
			)
		}
		return base, newIdentityVerificationError(
			IdentityStatusUnknownOrHold,
			"transport_failed",
			"Moltbook identity verification request failed",
			nil,
		)
	}
	defer resp.Body.Close()

	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return base, newIdentityVerificationError(
			IdentityStatusUnknownOrHold,
			"non_success_http",
			fmt.Sprintf("Moltbook identity verification returned HTTP %d", resp.StatusCode),
			nil,
		)
	}

	payload, err := io.ReadAll(io.LimitReader(resp.Body, moltbookIdentityResponseLimit+1))
	if err != nil {
		return base, newIdentityVerificationError(
			IdentityStatusUnknownOrHold,
			"response_read_failed",
			"read Moltbook identity verification response",
			nil,
		)
	}
	if len(payload) > moltbookIdentityResponseLimit {
		return base, newIdentityVerificationError(
			IdentityStatusUnknownOrHold,
			"response_too_large",
			"Moltbook identity verification response exceeds limit",
			nil,
		)
	}

	var decoded moltbookIdentityResponse
	if err := json.Unmarshal(payload, &decoded); err != nil {
		return base, newIdentityVerificationError(
			IdentityStatusUnknownOrHold,
			"malformed_response",
			"Moltbook identity verification returned invalid JSON",
			nil,
		)
	}
	if !decoded.Success {
		return base, newIdentityVerificationError(
			IdentityStatusUnknownOrHold,
			"provider_unsuccessful",
			"Moltbook identity provider did not complete verification",
			nil,
		)
	}
	if !decoded.Valid {
		invalid := base
		invalid.Status = IdentityStatusInvalid
		return invalid, newIdentityVerificationError(
			IdentityStatusInvalid,
			"provider_rejected_identity",
			"Moltbook identity token is invalid",
			nil,
		)
	}

	if err := validateMoltbookAgentID(decoded.Agent.ID); err != nil {
		return base, err
	}

	verified := base
	verified.AgentID = decoded.Agent.ID
	verified.Status = IdentityStatusVerified
	verified.VerifiedAt = v.now().UTC().Format(time.RFC3339Nano)
	return verified, nil
}

func validateMoltbookAgentID(agentID string) error {
	if agentID == "" {
		return newIdentityVerificationError(
			IdentityStatusUnknownOrHold,
			"missing_agent_id",
			"Moltbook identity response is missing agent id",
			nil,
		)
	}
	if strings.TrimSpace(agentID) != agentID {
		return newIdentityVerificationError(
			IdentityStatusUnknownOrHold,
			"non_canonical_agent_id",
			"Moltbook identity response contains surrounding whitespace in agent id",
			nil,
		)
	}
	if len(agentID) > moltbookAgentIDMaxBytes {
		return newIdentityVerificationError(
			IdentityStatusUnknownOrHold,
			"agent_id_too_long",
			"Moltbook identity response agent id exceeds limit",
			nil,
		)
	}
	for _, r := range agentID {
		if unicode.IsControl(r) {
			return newIdentityVerificationError(
				IdentityStatusUnknownOrHold,
				"agent_id_control_character",
				"Moltbook identity response agent id contains a control character",
				nil,
			)
		}
	}
	return nil
}

func newIdentityVerificationError(
	status IdentityVerificationStatus,
	code string,
	message string,
	cause error,
) *IdentityVerificationError {
	return &IdentityVerificationError{
		Status:  status,
		Code:    code,
		Message: message,
		Cause:   cause,
	}
}
