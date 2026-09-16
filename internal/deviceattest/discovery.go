package deviceattest

import (
	"context"
	"crypto"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

const (
	DiscoveryProtocol   = "liminal.attestation.discovery.v0.1"
	OIDCReceiptProtocol = "liminal.attestation.external-identity.v0.1"
)

type DiscoveryProbe struct {
	Kind             string `json:"kind"`
	Source           string `json:"source"`
	Present          bool   `json:"present"`
	ProviderReady    bool   `json:"provider_ready"`
	HardwareBacked   bool   `json:"hardware_backed"`
	ExternalIdentity bool   `json:"external_identity"`
	Reason           string `json:"reason"`
}

type ExternalIdentityReceipt struct {
	Protocol          string `json:"protocol"`
	Provider          string `json:"provider"`
	Issuer            string `json:"issuer"`
	Audience          string `json:"audience"`
	Subject           string `json:"subject"`
	Repository        string `json:"repository,omitempty"`
	RepositoryID      string `json:"repository_id,omitempty"`
	RepositoryOwnerID string `json:"repository_owner_id,omitempty"`
	Ref               string `json:"ref,omitempty"`
	CommitSHA         string `json:"sha,omitempty"`
	RunID             string `json:"run_id,omitempty"`
	RunnerEnvironment string `json:"runner_environment,omitempty"`
	KeyID             string `json:"key_id"`
	Algorithm         string `json:"algorithm"`
	TokenHash         string `json:"token_hash"`
	SignatureVerified bool   `json:"signature_verified"`
	VerifiedAt        string `json:"verified_at"`
}

type DiscoveryReport struct {
	Protocol                 string                   `json:"protocol"`
	Probes                   []DiscoveryProbe         `json:"probes"`
	ExternalIdentity         *ExternalIdentityReceipt `json:"external_identity,omitempty"`
	SelectedProviderID       string                   `json:"selected_provider_id"`
	SelectedAssuranceLevel   string                   `json:"selected_assurance_level"`
	HardwareUpgradeSelected  bool                     `json:"hardware_upgrade_selected"`
	ExternalIdentityVerified bool                     `json:"external_identity_verified"`
	SelectionReason          string                   `json:"selection_reason"`
	ReportHash               string                   `json:"report_hash"`
}

type discoveryHashMaterial struct {
	Protocol                 string                   `json:"protocol"`
	Probes                   []DiscoveryProbe         `json:"probes"`
	ExternalIdentity         *ExternalIdentityReceipt `json:"external_identity,omitempty"`
	SelectedProviderID       string                   `json:"selected_provider_id"`
	SelectedAssuranceLevel   string                   `json:"selected_assurance_level"`
	HardwareUpgradeSelected  bool                     `json:"hardware_upgrade_selected"`
	ExternalIdentityVerified bool                     `json:"external_identity_verified"`
	SelectionReason          string                   `json:"selection_reason"`
}

func Discover(ctx context.Context, fallback ProviderDescriptor, audience string) (DiscoveryReport, error) {
	if err := fallback.Validate(); err != nil {
		return DiscoveryReport{}, err
	}
	if audience == "" {
		return DiscoveryReport{}, errors.New("discovery audience is required")
	}

	probes := []DiscoveryProbe{
		probeDevice("tpm2", "/dev/tpmrm0", true),
		probeDevice("tpm2", "/dev/tpm0", true),
		probeDevice("sev-snp", "/dev/sev-guest", true),
		probeDevice("tdx", "/dev/tdx_guest", true),
	}

	oidc, oidcProbe := probeGitHubOIDC(ctx, audience)
	probes = append(probes, oidcProbe)

	report := DiscoveryReport{
		Protocol:               DiscoveryProtocol,
		Probes:                 probes,
		SelectedProviderID:     fallback.ProviderID,
		SelectedAssuranceLevel: fallback.AssuranceLevel,
		SelectionReason:        "no verified provider-ready hardware attestation source was available; keep the configured fallback provider",
	}
	if oidc != nil {
		report.ExternalIdentity = oidc
		report.ExternalIdentityVerified = oidc.SignatureVerified
	}

	for _, probe := range probes {
		if probe.ProviderReady && probe.HardwareBacked {
			report.HardwareUpgradeSelected = true
			break
		}
	}

	material := discoveryHashMaterial{
		Protocol:                 report.Protocol,
		Probes:                   report.Probes,
		ExternalIdentity:         report.ExternalIdentity,
		SelectedProviderID:       report.SelectedProviderID,
		SelectedAssuranceLevel:   report.SelectedAssuranceLevel,
		HardwareUpgradeSelected:  report.HardwareUpgradeSelected,
		ExternalIdentityVerified: report.ExternalIdentityVerified,
		SelectionReason:          report.SelectionReason,
	}
	h, err := hashJSON(material)
	if err != nil {
		return DiscoveryReport{}, err
	}
	report.ReportHash = h
	return report, nil
}

func (r DiscoveryReport) Validate() error {
	if r.Protocol != DiscoveryProtocol || r.SelectedProviderID == "" || r.SelectedAssuranceLevel == "" || !shaHex(r.ReportHash) {
		return errors.New("attestation discovery report is incomplete")
	}
	material := discoveryHashMaterial{
		Protocol:                 r.Protocol,
		Probes:                   r.Probes,
		ExternalIdentity:         r.ExternalIdentity,
		SelectedProviderID:       r.SelectedProviderID,
		SelectedAssuranceLevel:   r.SelectedAssuranceLevel,
		HardwareUpgradeSelected:  r.HardwareUpgradeSelected,
		ExternalIdentityVerified: r.ExternalIdentityVerified,
		SelectionReason:          r.SelectionReason,
	}
	expected, err := hashJSON(material)
	if err != nil {
		return err
	}
	if expected != r.ReportHash {
		return errors.New("attestation discovery report hash mismatch")
	}
	if r.ExternalIdentityVerified {
		if r.ExternalIdentity == nil || !r.ExternalIdentity.SignatureVerified || !shaHex(r.ExternalIdentity.TokenHash) {
			return errors.New("external identity verification receipt is incomplete")
		}
	}
	return nil
}

func probeDevice(kind, path string, hardware bool) DiscoveryProbe {
	probe := DiscoveryProbe{Kind: kind, Source: path, HardwareBacked: hardware}
	info, err := os.Stat(path)
	if err != nil {
		probe.Reason = "device node not exposed to this runtime"
		return probe
	}
	probe.Present = true
	probe.ProviderReady = false
	probe.Reason = fmt.Sprintf("device node is present (%s), but v0.8 has no verified provider implementation for it", info.Mode().String())
	return probe
}

func probeGitHubOIDC(ctx context.Context, audience string) (*ExternalIdentityReceipt, DiscoveryProbe) {
	probe := DiscoveryProbe{
		Kind:             "github-actions-oidc",
		Source:           "ACTIONS_ID_TOKEN_REQUEST_URL",
		ExternalIdentity: true,
		HardwareBacked:   false,
	}
	requestURL := os.Getenv("ACTIONS_ID_TOKEN_REQUEST_URL")
	requestToken := os.Getenv("ACTIONS_ID_TOKEN_REQUEST_TOKEN")
	if requestURL == "" || requestToken == "" {
		probe.Reason = "GitHub Actions OIDC request environment is not available"
		return nil, probe
	}
	probe.Present = true
	receipt, err := requestAndVerifyGitHubOIDC(ctx, requestURL, requestToken, audience)
	if err != nil {
		probe.Reason = "OIDC source was present but verification failed: " + err.Error()
		return nil, probe
	}
	probe.Reason = "GitHub OIDC signature verified online; raw JWT intentionally not persisted, so this is an external trust receipt rather than a provider-ready exported attestation"
	probe.ProviderReady = false
	return &receipt, probe
}

type oidcTokenResponse struct {
	Value string `json:"value"`
}

type jwtHeader struct {
	Algorithm string `json:"alg"`
	KeyID     string `json:"kid"`
	Type      string `json:"typ"`
}

type jwtClaims struct {
	Issuer            string      `json:"iss"`
	Audience          interface{} `json:"aud"`
	Subject           string      `json:"sub"`
	ExpiresAt         int64       `json:"exp"`
	IssuedAt          int64       `json:"iat"`
	NotBefore         int64       `json:"nbf"`
	Repository        string      `json:"repository"`
	RepositoryID      string      `json:"repository_id"`
	RepositoryOwnerID string      `json:"repository_owner_id"`
	Ref               string      `json:"ref"`
	CommitSHA         string      `json:"sha"`
	RunID             string      `json:"run_id"`
	RunnerEnvironment string      `json:"runner_environment"`
}

type oidcConfiguration struct {
	Issuer  string `json:"issuer"`
	JWKSURI string `json:"jwks_uri"`
}

type jsonWebKeySet struct {
	Keys []jsonWebKey `json:"keys"`
}

type jsonWebKey struct {
	KeyType   string `json:"kty"`
	KeyID     string `json:"kid"`
	Algorithm string `json:"alg"`
	Use       string `json:"use"`
	N         string `json:"n"`
	E         string `json:"e"`
}

func requestAndVerifyGitHubOIDC(ctx context.Context, requestURL, requestToken, audience string) (ExternalIdentityReceipt, error) {
	u, err := url.Parse(requestURL)
	if err != nil {
		return ExternalIdentityReceipt{}, err
	}
	q := u.Query()
	q.Set("audience", audience)
	u.RawQuery = q.Encode()

	client := &http.Client{Timeout: 8 * time.Second}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return ExternalIdentityReceipt{}, err
	}
	req.Header.Set("Authorization", "Bearer "+requestToken)
	resp, err := client.Do(req)
	if err != nil {
		return ExternalIdentityReceipt{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return ExternalIdentityReceipt{}, fmt.Errorf("OIDC token endpoint returned %s", resp.Status)
	}
	var tokenResponse oidcTokenResponse
	if err := json.NewDecoder(resp.Body).Decode(&tokenResponse); err != nil {
		return ExternalIdentityReceipt{}, err
	}
	if tokenResponse.Value == "" {
		return ExternalIdentityReceipt{}, errors.New("OIDC token endpoint returned an empty token")
	}
	return verifyGitHubOIDCToken(ctx, client, tokenResponse.Value, audience)
}

func verifyGitHubOIDCToken(ctx context.Context, client *http.Client, token, audience string) (ExternalIdentityReceipt, error) {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return ExternalIdentityReceipt{}, errors.New("OIDC token is not a three-part JWT")
	}
	headerBytes, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return ExternalIdentityReceipt{}, err
	}
	claimsBytes, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return ExternalIdentityReceipt{}, err
	}
	signature, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil {
		return ExternalIdentityReceipt{}, err
	}
	var header jwtHeader
	if err := json.Unmarshal(headerBytes, &header); err != nil {
		return ExternalIdentityReceipt{}, err
	}
	if header.Algorithm != "RS256" || header.KeyID == "" {
		return ExternalIdentityReceipt{}, errors.New("OIDC token must use RS256 with a key id")
	}
	var claims jwtClaims
	if err := json.Unmarshal(claimsBytes, &claims); err != nil {
		return ExternalIdentityReceipt{}, err
	}
	issuerURL, err := url.Parse(claims.Issuer)
	if err != nil || issuerURL.Scheme != "https" || issuerURL.Hostname() != "token.actions.githubusercontent.com" {
		return ExternalIdentityReceipt{}, errors.New("unexpected GitHub OIDC issuer")
	}
	if !audienceMatches(claims.Audience, audience) {
		return ExternalIdentityReceipt{}, errors.New("OIDC token audience mismatch")
	}
	now := time.Now().Unix()
	if claims.ExpiresAt <= now || claims.NotBefore > now+60 || claims.IssuedAt > now+60 {
		return ExternalIdentityReceipt{}, errors.New("OIDC token validity window rejected")
	}

	configURL := "https://token.actions.githubusercontent.com/.well-known/openid-configuration"
	config, err := fetchOIDCConfiguration(ctx, client, configURL)
	if err != nil {
		return ExternalIdentityReceipt{}, err
	}
	if config.Issuer != "https://token.actions.githubusercontent.com" || config.JWKSURI == "" {
		return ExternalIdentityReceipt{}, errors.New("GitHub OIDC configuration is unexpected")
	}
	jwksURL, err := url.Parse(config.JWKSURI)
	if err != nil || jwksURL.Scheme != "https" || jwksURL.Hostname() != "token.actions.githubusercontent.com" {
		return ExternalIdentityReceipt{}, errors.New("GitHub OIDC JWKS endpoint is unexpected")
	}
	keys, err := fetchJWKS(ctx, client, config.JWKSURI)
	if err != nil {
		return ExternalIdentityReceipt{}, err
	}
	key, err := rsaKeyForID(keys, header.KeyID)
	if err != nil {
		return ExternalIdentityReceipt{}, err
	}
	signed := []byte(parts[0] + "." + parts[1])
	digest := sha256.Sum256(signed)
	if err := rsa.VerifyPKCS1v15(key, crypto.SHA256, digest[:], signature); err != nil {
		return ExternalIdentityReceipt{}, errors.New("GitHub OIDC signature verification failed")
	}

	tokenDigest := sha256.Sum256([]byte(token))
	return ExternalIdentityReceipt{
		Protocol:          OIDCReceiptProtocol,
		Provider:          "github-actions-oidc",
		Issuer:            claims.Issuer,
		Audience:          audience,
		Subject:           claims.Subject,
		Repository:        claims.Repository,
		RepositoryID:      claims.RepositoryID,
		RepositoryOwnerID: claims.RepositoryOwnerID,
		Ref:               claims.Ref,
		CommitSHA:         claims.CommitSHA,
		RunID:             claims.RunID,
		RunnerEnvironment: claims.RunnerEnvironment,
		KeyID:             header.KeyID,
		Algorithm:         header.Algorithm,
		TokenHash:         fmt.Sprintf("%x", tokenDigest[:]),
		SignatureVerified: true,
		VerifiedAt:        time.Now().UTC().Format(time.RFC3339Nano),
	}, nil
}

func fetchOIDCConfiguration(ctx context.Context, client *http.Client, endpoint string) (oidcConfiguration, error) {
	var out oidcConfiguration
	if err := fetchJSON(ctx, client, endpoint, &out); err != nil {
		return out, err
	}
	return out, nil
}

func fetchJWKS(ctx context.Context, client *http.Client, endpoint string) (jsonWebKeySet, error) {
	var out jsonWebKeySet
	if err := fetchJSON(ctx, client, endpoint, &out); err != nil {
		return out, err
	}
	return out, nil
}

func fetchJSON(ctx context.Context, client *http.Client, endpoint string, dst any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return err
	}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("GET %s returned %s", endpoint, resp.Status)
	}
	return json.NewDecoder(resp.Body).Decode(dst)
}

func rsaKeyForID(set jsonWebKeySet, keyID string) (*rsa.PublicKey, error) {
	for _, key := range set.Keys {
		if key.KeyID != keyID || key.KeyType != "RSA" {
			continue
		}
		nBytes, err := base64.RawURLEncoding.DecodeString(key.N)
		if err != nil {
			return nil, err
		}
		eBytes, err := base64.RawURLEncoding.DecodeString(key.E)
		if err != nil || len(eBytes) == 0 || len(eBytes) > 8 {
			return nil, errors.New("invalid RSA exponent in JWKS")
		}
		var padded [8]byte
		copy(padded[8-len(eBytes):], eBytes)
		e64 := binary.BigEndian.Uint64(padded[:])
		if e64 == 0 || e64 > uint64(^uint(0)>>1) {
			return nil, errors.New("RSA exponent is out of range")
		}
		return &rsa.PublicKey{N: new(big.Int).SetBytes(nBytes), E: int(e64)}, nil
	}
	return nil, errors.New("OIDC signing key id was not found in GitHub JWKS")
}

func audienceMatches(value interface{}, expected string) bool {
	switch v := value.(type) {
	case string:
		return v == expected
	case []interface{}:
		for _, item := range v {
			if s, ok := item.(string); ok && s == expected {
				return true
			}
		}
	}
	return false
}
