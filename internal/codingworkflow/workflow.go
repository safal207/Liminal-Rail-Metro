package codingworkflow

import (
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"path"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/safal207/Liminal-Rail-Metro/internal/decisionplane"
	"github.com/safal207/Liminal-Rail-Metro/internal/metro"
)

const (
	ContractProtocol = "liminal.codex.coding.contract.v0.3"
	ReceiptProtocol  = "liminal.codex.coding.receipt.v0.3"
	SignatureEd25519 = "ed25519"

	StatusVerified = "VERIFIED"
	StatusHold     = "HOLD"
)

var (
	repositoryPattern = regexp.MustCompile(`^[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+$`)
	shaPattern        = regexp.MustCompile(`^[0-9a-f]{40}$`)
	sha256Pattern     = regexp.MustCompile(`^[0-9a-f]{64}$`)
)

type Signer struct {
	issuerID   string
	privateKey ed25519.PrivateKey
	publicKey  ed25519.PublicKey
	keyID      string
}

type IssuerInfo struct {
	IssuerID        string `json:"issuer_id"`
	IssuerKeyID     string `json:"issuer_key_id"`
	IssuerPublicKey string `json:"issuer_public_key_base64"`
}

type StartInput struct {
	WorkflowID          string
	RequestID           string
	ActionID            string
	Repository          string
	IssueNumber         int
	BaseSHA             string
	AllowedPathPrefixes []string
	RequiredChecks      []string
	Choices             []decisionplane.Choice
}

type Contract struct {
	Protocol            string                    `json:"protocol"`
	WorkflowID          string                    `json:"workflow_id"`
	Repository          string                    `json:"repository"`
	IssueNumber         int                       `json:"issue_number"`
	IssueURL            string                    `json:"issue_url"`
	IssueTitle          string                    `json:"issue_title"`
	IssueSnapshotHash   string                    `json:"issue_snapshot_hash"`
	BaseSHA             string                    `json:"base_sha"`
	AllowedPathPrefixes []string                  `json:"allowed_path_prefixes"`
	RequiredChecks      []string                  `json:"required_checks"`
	Packet              metro.Packet              `json:"packet"`
	Request             decisionplane.Request     `json:"decision_request"`
	Decision            decisionplane.Decision    `json:"decision"`
	Gate                decisionplane.GateResult  `json:"gate"`
	CreatedAt           string                    `json:"created_at"`
	IssuerID            string                    `json:"issuer_id"`
	IssuerKeyID         string                    `json:"issuer_key_id"`
	IssuerPublicKey     string                    `json:"issuer_public_key_base64"`
	SignatureAlgorithm  string                    `json:"signature_algorithm"`
	ContractHash        string                    `json:"contract_hash"`
	Signature           string                    `json:"signature_base64"`
}

type Receipt struct {
	Protocol           string           `json:"protocol"`
	WorkflowID         string           `json:"workflow_id"`
	ContractHash       string           `json:"contract_hash"`
	Repository         string           `json:"repository"`
	IssueNumber        int              `json:"issue_number"`
	PullRequestNumber  int              `json:"pull_request_number"`
	PullRequestURL     string           `json:"pull_request_url"`
	BaseSHA            string           `json:"base_sha"`
	HeadSHA            string           `json:"head_sha"`
	FilesHash          string           `json:"files_hash"`
	ChecksHash         string           `json:"checks_hash"`
	RequiredChecks     []string         `json:"required_checks"`
	Status             string           `json:"status"`
	VerifiedAt         string           `json:"verified_at"`
	IssuerID           string           `json:"issuer_id"`
	IssuerKeyID        string           `json:"issuer_key_id"`
	IssuerPublicKey    string           `json:"issuer_public_key_base64"`
	SignatureAlgorithm string           `json:"signature_algorithm"`
	ReceiptHash        string           `json:"receipt_hash"`
	Signature          string           `json:"signature_base64"`
}

type Verification struct {
	Status     string   `json:"status"`
	ReasonCode string   `json:"reason_code"`
	Reason     string   `json:"reason,omitempty"`
	Receipt    *Receipt `json:"receipt,omitempty"`
}

type issueSnapshotMaterial struct {
	Number  int    `json:"number"`
	State   string `json:"state"`
	Title   string `json:"title"`
	Body    string `json:"body"`
	HTMLURL string `json:"html_url"`
}

type contractMaterial struct {
	Protocol            string                   `json:"protocol"`
	WorkflowID          string                   `json:"workflow_id"`
	Repository          string                   `json:"repository"`
	IssueNumber         int                      `json:"issue_number"`
	IssueURL            string                   `json:"issue_url"`
	IssueTitle          string                   `json:"issue_title"`
	IssueSnapshotHash   string                   `json:"issue_snapshot_hash"`
	BaseSHA             string                   `json:"base_sha"`
	AllowedPathPrefixes []string                 `json:"allowed_path_prefixes"`
	RequiredChecks      []string                 `json:"required_checks"`
	Packet              metro.Packet             `json:"packet"`
	Request             decisionplane.Request    `json:"decision_request"`
	Decision            decisionplane.Decision   `json:"decision"`
	Gate                decisionplane.GateResult `json:"gate"`
	CreatedAt           string                   `json:"created_at"`
	IssuerID            string                   `json:"issuer_id"`
	IssuerKeyID         string                   `json:"issuer_key_id"`
	IssuerPublicKey     string                   `json:"issuer_public_key_base64"`
	SignatureAlgorithm  string                   `json:"signature_algorithm"`
}

type receiptMaterial struct {
	Protocol           string   `json:"protocol"`
	WorkflowID         string   `json:"workflow_id"`
	ContractHash       string   `json:"contract_hash"`
	Repository         string   `json:"repository"`
	IssueNumber        int      `json:"issue_number"`
	PullRequestNumber  int      `json:"pull_request_number"`
	PullRequestURL     string   `json:"pull_request_url"`
	BaseSHA            string   `json:"base_sha"`
	HeadSHA            string   `json:"head_sha"`
	FilesHash          string   `json:"files_hash"`
	ChecksHash         string   `json:"checks_hash"`
	RequiredChecks     []string `json:"required_checks"`
	Status             string   `json:"status"`
	VerifiedAt         string   `json:"verified_at"`
	IssuerID           string   `json:"issuer_id"`
	IssuerKeyID        string   `json:"issuer_key_id"`
	IssuerPublicKey    string   `json:"issuer_public_key_base64"`
	SignatureAlgorithm string   `json:"signature_algorithm"`
}

func NewSignerFromBase64(issuerID, encodedPrivateKey string) (*Signer, error) {
	if strings.TrimSpace(issuerID) == "" {
		return nil, errors.New("coding workflow issuer_id is required")
	}
	decoded, err := base64.StdEncoding.DecodeString(strings.TrimSpace(encodedPrivateKey))
	if err != nil {
		return nil, errors.New("decode coding workflow private key")
	}
	var privateKey ed25519.PrivateKey
	switch len(decoded) {
	case ed25519.SeedSize:
		privateKey = ed25519.NewKeyFromSeed(decoded)
	case ed25519.PrivateKeySize:
		privateKey = ed25519.PrivateKey(append([]byte(nil), decoded...))
	default:
		return nil, errors.New("coding workflow private key must be an Ed25519 seed or private key")
	}
	return NewSigner(issuerID, privateKey)
}

func NewSigner(issuerID string, privateKey ed25519.PrivateKey) (*Signer, error) {
	if strings.TrimSpace(issuerID) == "" {
		return nil, errors.New("coding workflow issuer_id is required")
	}
	if len(privateKey) != ed25519.PrivateKeySize {
		return nil, errors.New("invalid Ed25519 private key")
	}
	publicKey, ok := privateKey.Public().(ed25519.PublicKey)
	if !ok || len(publicKey) != ed25519.PublicKeySize {
		return nil, errors.New("invalid Ed25519 public key")
	}
	sum := sha256.Sum256(publicKey)
	return &Signer{
		issuerID:   issuerID,
		privateKey: append(ed25519.PrivateKey(nil), privateKey...),
		publicKey:  append(ed25519.PublicKey(nil), publicKey...),
		keyID:      hex.EncodeToString(sum[:]),
	}, nil
}

func (signer *Signer) Issuer() IssuerInfo {
	if signer == nil {
		return IssuerInfo{}
	}
	return IssuerInfo{
		IssuerID:        signer.issuerID,
		IssuerKeyID:     signer.keyID,
		IssuerPublicKey: base64.StdEncoding.EncodeToString(signer.publicKey),
	}
}

func Start(ctx context.Context, github GitHubEvidenceReader, signer *Signer, provider decisionplane.Provider, input StartInput) (Contract, error) {
	if github == nil {
		return Contract{}, errors.New("GitHub client is required")
	}
	if signer == nil {
		return Contract{}, errors.New("coding workflow signer is required")
	}
	if provider == nil {
		return Contract{}, errors.New("decision provider is required")
	}
	normalized, err := normalizeStartInput(input)
	if err != nil {
		return Contract{}, err
	}

	issue, err := github.GetIssue(ctx, normalized.Repository, normalized.IssueNumber)
	if err != nil {
		return Contract{}, err
	}
	if issue.Number != normalized.IssueNumber || issue.PullRequest != nil {
		return Contract{}, errors.New("GitHub issue reference is not a plain issue")
	}
	if issue.State != "open" {
		return Contract{}, errors.New("GitHub issue must be open when coding contract is created")
	}
	snapshotHash, err := metro.HashJSON(issueSnapshotMaterial{
		Number: issue.Number, State: issue.State, Title: issue.Title, Body: issue.Body, HTMLURL: issue.HTMLURL,
	})
	if err != nil {
		return Contract{}, err
	}

	targets := make([]string, 0, len(normalized.Choices))
	for _, choice := range normalized.Choices {
		targets = append(targets, choice.Target)
	}
	packet := metro.NewPacket(
		normalized.ActionID,
		"codex",
		fmt.Sprintf("Implement public GitHub issue %s#%d within the bounded coding contract", normalized.Repository, normalized.IssueNumber),
		metro.Action{
			Kind: "codex.github.coding-workflow",
			Inputs: map[string]any{
				"repository":            normalized.Repository,
				"issue_number":          normalized.IssueNumber,
				"issue_snapshot_hash":   snapshotHash,
				"base_sha":              normalized.BaseSHA,
				"allowed_path_prefixes": normalized.AllowedPathPrefixes,
				"required_checks":       normalized.RequiredChecks,
			},
		},
		targets,
	)
	packet.Constraints.SideEffect = false

	request, err := decisionplane.NewRequest(packet, normalized.RequestID, map[string]any{
		"repository":          normalized.Repository,
		"issue_number":        normalized.IssueNumber,
		"issue_title":         issue.Title,
		"issue_snapshot_hash": snapshotHash,
		"stage":               "coding",
	}, normalized.Choices)
	if err != nil {
		return Contract{}, err
	}
	decision, err := provider.Decide(ctx, request)
	if err != nil {
		return Contract{}, err
	}
	gate, err := decisionplane.ApplyDecision(packet, request, decision, decisionplane.DefaultGatePolicy())
	if err != nil {
		return Contract{}, err
	}
	if gate.Disposition != decisionplane.DispositionAutoRoute || gate.Route == nil {
		return Contract{}, fmt.Errorf("coding workflow is not authorized: %s (%s)", gate.Disposition, gate.ReasonCode)
	}

	contract := Contract{
		Protocol:            ContractProtocol,
		WorkflowID:          normalized.WorkflowID,
		Repository:          normalized.Repository,
		IssueNumber:         normalized.IssueNumber,
		IssueURL:            issue.HTMLURL,
		IssueTitle:          issue.Title,
		IssueSnapshotHash:   snapshotHash,
		BaseSHA:             normalized.BaseSHA,
		AllowedPathPrefixes: append([]string(nil), normalized.AllowedPathPrefixes...),
		RequiredChecks:      append([]string(nil), normalized.RequiredChecks...),
		Packet:              packet,
		Request:             request,
		Decision:            decision,
		Gate:                gate,
		CreatedAt:           metro.NowISO(),
	}
	if err := signer.signContract(&contract); err != nil {
		return Contract{}, err
	}
	return contract, nil
}

func Verify(ctx context.Context, github GitHubEvidenceReader, signer *Signer, contract Contract, pullRequestNumber int) (Verification, error) {
	if github == nil {
		return Verification{}, errors.New("GitHub client is required")
	}
	if signer == nil {
		return Verification{}, errors.New("coding workflow signer is required")
	}
	if pullRequestNumber <= 0 {
		return hold("invalid_pull_request", "pull_request_number must be positive"), nil
	}
	if err := signer.VerifyContract(contract); err != nil {
		return hold("invalid_contract", err.Error()), nil
	}

	pull, err := github.GetPullRequest(ctx, contract.Repository, pullRequestNumber)
	if err != nil {
		return Verification{}, err
	}
	if pull.Number != pullRequestNumber || pull.Base.Repo.FullName != contract.Repository {
		return hold("repository_mismatch", "pull request does not belong to the contracted base repository"), nil
	}
	if pull.State != "open" {
		return hold("pull_request_not_open", "pull request must remain open while completion is verified"), nil
	}
	if pull.Draft {
		return hold("pull_request_is_draft", "draft pull request cannot be completion proof"), nil
	}
	if pull.Base.SHA != contract.BaseSHA {
		return hold("stale_base", "pull request base SHA differs from the contracted base SHA"), nil
	}
	if !shaPattern.MatchString(pull.Head.SHA) {
		return hold("invalid_head_sha", "pull request head SHA is not a bounded Git object id"), nil
	}
	if pull.ChangedFiles < 1 {
		return hold("no_changed_files", "pull request has no changed files"), nil
	}
	if pull.ChangedFiles > maxGitHubPRFiles {
		return hold("too_many_changed_files", fmt.Sprintf("pull request exceeds %d changed files", maxGitHubPRFiles)), nil
	}

	files, err := github.GetPullRequestFiles(ctx, contract.Repository, pullRequestNumber)
	if err != nil {
		return Verification{}, err
	}
	if len(files) != pull.ChangedFiles {
		return hold("incomplete_file_evidence", "GitHub file evidence count does not match pull request changed_files"), nil
	}
	sort.Slice(files, func(i, j int) bool { return files[i].Filename < files[j].Filename })
	for _, file := range files {
		if !pathAllowed(file.Filename, contract.AllowedPathPrefixes) {
			return hold("path_outside_contract", fmt.Sprintf("changed file %q is outside allowed path prefixes", file.Filename)), nil
		}
	}
	filesHash, err := metro.HashJSON(files)
	if err != nil {
		return Verification{}, err
	}

	checkRuns, err := github.GetCheckRuns(ctx, contract.Repository, pull.Head.SHA)
	if err != nil {
		return Verification{}, err
	}
	selectedChecks := make([]GitHubCheckRun, 0, len(contract.RequiredChecks))
	for _, required := range contract.RequiredChecks {
		var latest *GitHubCheckRun
		for index := range checkRuns {
			check := &checkRuns[index]
			if check.Name != required || check.HeadSHA != pull.Head.SHA {
				continue
			}
			if latest == nil || check.ID > latest.ID {
				latest = check
			}
		}
		if latest == nil {
			return hold("required_check_missing", fmt.Sprintf("required check %q is missing for exact head SHA", required)), nil
		}
		if latest.Status != "completed" || latest.Conclusion != "success" {
			return hold("required_check_not_success", fmt.Sprintf("required check %q is %s/%s", required, latest.Status, latest.Conclusion)), nil
		}
		selectedChecks = append(selectedChecks, *latest)
	}
	sort.Slice(selectedChecks, func(i, j int) bool { return selectedChecks[i].Name < selectedChecks[j].Name })
	checksHash, err := metro.HashJSON(selectedChecks)
	if err != nil {
		return Verification{}, err
	}

	receipt := Receipt{
		Protocol:          ReceiptProtocol,
		WorkflowID:        contract.WorkflowID,
		ContractHash:      contract.ContractHash,
		Repository:        contract.Repository,
		IssueNumber:       contract.IssueNumber,
		PullRequestNumber: pullRequestNumber,
		PullRequestURL:    pull.HTMLURL,
		BaseSHA:           pull.Base.SHA,
		HeadSHA:           pull.Head.SHA,
		FilesHash:         filesHash,
		ChecksHash:        checksHash,
		RequiredChecks:    append([]string(nil), contract.RequiredChecks...),
		Status:            StatusVerified,
		VerifiedAt:        metro.NowISO(),
	}
	if err := signer.signReceipt(&receipt); err != nil {
		return Verification{}, err
	}
	return Verification{Status: StatusVerified, ReasonCode: "github_evidence_verified", Receipt: &receipt}, nil
}

func (signer *Signer) VerifyContract(contract Contract) error {
	if signer == nil {
		return errors.New("coding workflow signer is required")
	}
	if contract.Protocol != ContractProtocol {
		return errors.New("unsupported coding contract protocol")
	}
	if contract.IssuerID != signer.issuerID || contract.IssuerKeyID != signer.keyID ||
		contract.IssuerPublicKey != base64.StdEncoding.EncodeToString(signer.publicKey) {
		return errors.New("coding contract issuer is not the configured server authority")
	}
	if contract.SignatureAlgorithm != SignatureEd25519 {
		return errors.New("unsupported coding contract signature algorithm")
	}
	expectedHash, materialBytes, err := hashContractMaterial(contract)
	if err != nil {
		return err
	}
	if expectedHash != contract.ContractHash {
		return errors.New("coding contract hash mismatch")
	}
	signature, err := base64.StdEncoding.DecodeString(contract.Signature)
	if err != nil || len(signature) != ed25519.SignatureSize {
		return errors.New("invalid coding contract signature encoding")
	}
	if !ed25519.Verify(signer.publicKey, materialBytes, signature) {
		return errors.New("coding contract signature verification failed")
	}

	recomputedGate, err := decisionplane.ApplyDecision(contract.Packet, contract.Request, contract.Decision, decisionplane.DefaultGatePolicy())
	if err != nil {
		return err
	}
	want, err := metro.HashJSON(recomputedGate)
	if err != nil {
		return err
	}
	got, err := metro.HashJSON(contract.Gate)
	if err != nil {
		return err
	}
	if want != got || contract.Gate.Disposition != decisionplane.DispositionAutoRoute || contract.Gate.Route == nil {
		return errors.New("coding contract decision gate mismatch")
	}
	return validateContractShape(contract)
}

func (signer *Signer) VerifyReceipt(receipt Receipt) error {
	if signer == nil {
		return errors.New("coding workflow signer is required")
	}
	if receipt.Protocol != ReceiptProtocol || receipt.Status != StatusVerified {
		return errors.New("unsupported coding receipt protocol or status")
	}
	if receipt.IssuerID != signer.issuerID || receipt.IssuerKeyID != signer.keyID ||
		receipt.IssuerPublicKey != base64.StdEncoding.EncodeToString(signer.publicKey) {
		return errors.New("coding receipt issuer is not the configured server authority")
	}
	if receipt.SignatureAlgorithm != SignatureEd25519 {
		return errors.New("unsupported coding receipt signature algorithm")
	}
	expectedHash, materialBytes, err := hashReceiptMaterial(receipt)
	if err != nil {
		return err
	}
	if expectedHash != receipt.ReceiptHash {
		return errors.New("coding receipt hash mismatch")
	}
	signature, err := base64.StdEncoding.DecodeString(receipt.Signature)
	if err != nil || len(signature) != ed25519.SignatureSize {
		return errors.New("invalid coding receipt signature encoding")
	}
	if !ed25519.Verify(signer.publicKey, materialBytes, signature) {
		return errors.New("coding receipt signature verification failed")
	}
	if !repositoryPattern.MatchString(receipt.Repository) || receipt.IssueNumber <= 0 || receipt.PullRequestNumber <= 0 ||
		!shaPattern.MatchString(receipt.BaseSHA) || !shaPattern.MatchString(receipt.HeadSHA) ||
		!sha256Pattern.MatchString(receipt.ContractHash) || !sha256Pattern.MatchString(receipt.FilesHash) ||
		!sha256Pattern.MatchString(receipt.ChecksHash) || !sha256Pattern.MatchString(receipt.ReceiptHash) ||
		receipt.PullRequestURL == "" || receipt.VerifiedAt == "" || len(receipt.RequiredChecks) == 0 {
		return errors.New("coding receipt fields are incomplete or malformed")
	}
	return nil
}
func (signer *Signer) signContract(contract *Contract) error {
	contract.IssuerID = signer.issuerID
	contract.IssuerKeyID = signer.keyID
	contract.IssuerPublicKey = base64.StdEncoding.EncodeToString(signer.publicKey)
	contract.SignatureAlgorithm = SignatureEd25519
	hash, materialBytes, err := hashContractMaterial(*contract)
	if err != nil {
		return err
	}
	contract.ContractHash = hash
	contract.Signature = base64.StdEncoding.EncodeToString(ed25519.Sign(signer.privateKey, materialBytes))
	return nil
}

func (signer *Signer) signReceipt(receipt *Receipt) error {
	receipt.IssuerID = signer.issuerID
	receipt.IssuerKeyID = signer.keyID
	receipt.IssuerPublicKey = base64.StdEncoding.EncodeToString(signer.publicKey)
	receipt.SignatureAlgorithm = SignatureEd25519
	hash, materialBytes, err := hashReceiptMaterial(*receipt)
	if err != nil {
		return err
	}
	receipt.ReceiptHash = hash
	receipt.Signature = base64.StdEncoding.EncodeToString(ed25519.Sign(signer.privateKey, materialBytes))
	return nil
}

func hashContractMaterial(contract Contract) (string, []byte, error) {
	material := contractMaterial{
		Protocol: contract.Protocol, WorkflowID: contract.WorkflowID, Repository: contract.Repository,
		IssueNumber: contract.IssueNumber, IssueURL: contract.IssueURL, IssueTitle: contract.IssueTitle,
		IssueSnapshotHash: contract.IssueSnapshotHash, BaseSHA: contract.BaseSHA,
		AllowedPathPrefixes: contract.AllowedPathPrefixes, RequiredChecks: contract.RequiredChecks,
		Packet: contract.Packet, Request: contract.Request, Decision: contract.Decision, Gate: contract.Gate,
		CreatedAt: contract.CreatedAt, IssuerID: contract.IssuerID, IssuerKeyID: contract.IssuerKeyID,
		IssuerPublicKey: contract.IssuerPublicKey, SignatureAlgorithm: contract.SignatureAlgorithm,
	}
	bytes, err := json.Marshal(material)
	if err != nil {
		return "", nil, err
	}
	sum := sha256.Sum256(bytes)
	return hex.EncodeToString(sum[:]), bytes, nil
}

func hashReceiptMaterial(receipt Receipt) (string, []byte, error) {
	material := receiptMaterial{
		Protocol: receipt.Protocol, WorkflowID: receipt.WorkflowID, ContractHash: receipt.ContractHash,
		Repository: receipt.Repository, IssueNumber: receipt.IssueNumber, PullRequestNumber: receipt.PullRequestNumber,
		PullRequestURL: receipt.PullRequestURL, BaseSHA: receipt.BaseSHA, HeadSHA: receipt.HeadSHA,
		FilesHash: receipt.FilesHash, ChecksHash: receipt.ChecksHash, RequiredChecks: receipt.RequiredChecks,
		Status: receipt.Status, VerifiedAt: receipt.VerifiedAt, IssuerID: receipt.IssuerID,
		IssuerKeyID: receipt.IssuerKeyID, IssuerPublicKey: receipt.IssuerPublicKey,
		SignatureAlgorithm: receipt.SignatureAlgorithm,
	}
	bytes, err := json.Marshal(material)
	if err != nil {
		return "", nil, err
	}
	sum := sha256.Sum256(bytes)
	return hex.EncodeToString(sum[:]), bytes, nil
}

func normalizeStartInput(input StartInput) (StartInput, error) {
	input.WorkflowID = strings.TrimSpace(input.WorkflowID)
	input.RequestID = strings.TrimSpace(input.RequestID)
	input.ActionID = strings.TrimSpace(input.ActionID)
	input.Repository = strings.TrimSpace(input.Repository)
	input.BaseSHA = strings.ToLower(strings.TrimSpace(input.BaseSHA))
	if input.WorkflowID == "" || input.RequestID == "" || input.ActionID == "" {
		return StartInput{}, errors.New("workflow_id, request_id, and action_id are required")
	}
	if !repositoryPattern.MatchString(input.Repository) {
		return StartInput{}, errors.New("repository must be public GitHub owner/name")
	}
	if input.IssueNumber <= 0 {
		return StartInput{}, errors.New("issue_number must be positive")
	}
	if !shaPattern.MatchString(input.BaseSHA) {
		return StartInput{}, errors.New("base_sha must be a 40-character lowercase Git SHA")
	}
	paths, err := normalizePaths(input.AllowedPathPrefixes)
	if err != nil {
		return StartInput{}, err
	}
	checks, err := normalizeChecks(input.RequiredChecks)
	if err != nil {
		return StartInput{}, err
	}
	if len(input.Choices) == 0 {
		return StartInput{}, errors.New("at least one bounded decision choice is required")
	}
	input.AllowedPathPrefixes = paths
	input.RequiredChecks = checks
	input.Choices = append([]decisionplane.Choice(nil), input.Choices...)
	return input, nil
}

func normalizePaths(values []string) ([]string, error) {
	if len(values) == 0 || len(values) > 64 {
		return nil, errors.New("allowed_path_prefixes must contain 1..64 entries")
	}
	seen := map[string]struct{}{}
	output := make([]string, 0, len(values))
	for _, value := range values {
		if strings.Contains(value, "\\") {
			return nil, errors.New("allowed path prefixes must use forward slashes")
		}
		clean := path.Clean(strings.TrimSpace(value))
		if clean == "." || clean == "/" || strings.HasPrefix(clean, "../") || strings.HasPrefix(clean, "/") {
			return nil, fmt.Errorf("invalid allowed path prefix %q", value)
		}
		if _, ok := seen[clean]; ok {
			continue
		}
		seen[clean] = struct{}{}
		output = append(output, clean)
	}
	sort.Strings(output)
	return output, nil
}

func normalizeChecks(values []string) ([]string, error) {
	if len(values) == 0 || len(values) > 64 {
		return nil, errors.New("required_checks must contain 1..64 entries")
	}
	seen := map[string]struct{}{}
	output := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" || len(value) > 200 {
			return nil, errors.New("required check names must be non-empty and at most 200 bytes")
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		output = append(output, value)
	}
	sort.Strings(output)
	return output, nil
}

func validateContractShape(contract Contract) error {
	if contract.WorkflowID == "" || !repositoryPattern.MatchString(contract.Repository) ||
		contract.IssueNumber <= 0 || !shaPattern.MatchString(contract.BaseSHA) ||
		contract.IssueURL == "" || contract.IssueTitle == "" || !sha256Pattern.MatchString(contract.IssueSnapshotHash) ||
		contract.CreatedAt == "" || !sha256Pattern.MatchString(contract.ContractHash) || contract.Signature == "" {
		return errors.New("coding contract fields are incomplete")
	}
	expectedIssueURL := fmt.Sprintf("https://github.com/%s/issues/%d", contract.Repository, contract.IssueNumber)
	if contract.IssueURL != expectedIssueURL {
		return errors.New("coding contract issue URL does not match repository and issue number")
	}
	paths, err := normalizePaths(contract.AllowedPathPrefixes)
	if err != nil {
		return err
	}
	checks, err := normalizeChecks(contract.RequiredChecks)
	if err != nil {
		return err
	}
	if !equalStrings(paths, contract.AllowedPathPrefixes) || !equalStrings(checks, contract.RequiredChecks) {
		return errors.New("coding contract paths/checks are not canonical")
	}
	if contract.Packet.Protocol != metro.PacketProtocol || contract.Packet.SourceAgent != "codex" ||
		contract.Packet.Action.Kind != "codex.github.coding-workflow" || contract.Packet.Constraints.SideEffect {
		return errors.New("coding contract packet shape is invalid")
	}
	expectedInputs := map[string]any{
		"repository":            contract.Repository,
		"issue_number":          contract.IssueNumber,
		"issue_snapshot_hash":   contract.IssueSnapshotHash,
		"base_sha":              contract.BaseSHA,
		"allowed_path_prefixes": contract.AllowedPathPrefixes,
		"required_checks":       contract.RequiredChecks,
	}
	wantInputs, err := metro.HashJSON(expectedInputs)
	if err != nil {
		return err
	}
	gotInputs, err := metro.HashJSON(contract.Packet.Action.Inputs)
	if err != nil || wantInputs != gotInputs {
		return errors.New("coding contract packet inputs do not match contract")
	}
	expectedState := map[string]any{
		"repository":          contract.Repository,
		"issue_number":        contract.IssueNumber,
		"issue_title":         contract.IssueTitle,
		"issue_snapshot_hash": contract.IssueSnapshotHash,
		"stage":               "coding",
	}
	wantState, err := metro.HashJSON(expectedState)
	if err != nil {
		return err
	}
	gotState, err := metro.HashJSON(contract.Request.State)
	if err != nil || wantState != gotState {
		return errors.New("coding contract decision state does not match contract")
	}
	targets := make([]string, 0, len(contract.Request.Choices))
	for _, choice := range contract.Request.Choices {
		targets = append(targets, choice.Target)
	}
	if !equalStrings(targets, contract.Packet.AllowedTargets) {
		return errors.New("coding contract allowed targets do not match decision choices")
	}
	return nil
}
func pathAllowed(filename string, prefixes []string) bool {
	clean := path.Clean(filename)
	if clean == "." || strings.HasPrefix(clean, "../") || strings.HasPrefix(clean, "/") {
		return false
	}
	for _, prefix := range prefixes {
		if clean == prefix || strings.HasPrefix(clean, prefix+"/") {
			return true
		}
	}
	return false
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for index := range a {
		if a[index] != b[index] {
			return false
		}
	}
	return true
}

func hold(code, reason string) Verification {
	return Verification{Status: StatusHold, ReasonCode: code, Reason: reason}
}
