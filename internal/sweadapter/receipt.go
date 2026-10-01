package sweadapter

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
)

const (
	ContributionReceiptProtocol = "metro.swe.contribution-receipt.v0.1"
	ReviewReceiptProtocol       = "metro.swe.review-receipt.v0.1"
)

var ErrStaleCandidate = errors.New("STALE_CANDIDATE")

type Artifact struct {
	Path          string `json:"path"`
	HashAlgorithm string `json:"hash_algorithm"`
	Digest        string `json:"digest"`
}

type Usage struct {
	ModelCalls   int     `json:"model_calls"`
	InputTokens  int64   `json:"input_tokens"`
	OutputTokens int64   `json:"output_tokens"`
	CostUSD      float64 `json:"cost_usd"`
}

type Timing struct {
	WallTimeMS            int64 `json:"wall_time_ms"`
	TimeToFirstEvidenceMS int64 `json:"time_to_first_evidence_ms"`
	ToolCalls             int64 `json:"tool_calls"`
	ContextBytes          int64 `json:"context_bytes"`
}

type ContributionReceipt struct {
	Protocol          string   `json:"protocol"`
	IssueURL          string   `json:"issue_url"`
	Repository        string   `json:"repository"`
	RunID             string   `json:"run_id"`
	BaseSHA           string   `json:"base_sha"`
	HeadSHA           string   `json:"head_sha"`
	Patch             Artifact `json:"patch"`
	Trajectory        Artifact `json:"trajectory"`
	Config            Artifact `json:"config"`
	Environment       Artifact `json:"environment"`
	Evaluation        Artifact `json:"evaluation"`
	Usage             Usage    `json:"usage"`
	Timing            Timing   `json:"timing"`
	TerminationReason string   `json:"termination_reason"`
	Limitations       []string `json:"limitations,omitempty"`
	ReceiptHash       string   `json:"receipt_hash"`
}

type BuildInput struct {
	IssueURL          string
	Repository        string
	RunID             string
	BaseSHA           string
	HeadSHA           string
	PatchPath         string
	TrajectoryPath    string
	ConfigPath        string
	EnvironmentPath   string
	EvaluationPath    string
	Usage             Usage
	Timing            Timing
	TerminationReason string
	Limitations       []string
}

type VerifyInput struct {
	HeadSHA         string
	PatchPath       string
	TrajectoryPath  string
	ConfigPath      string
	EnvironmentPath string
	EvaluationPath  string
}

type ReviewVerdict string

const (
	ReviewApprove         ReviewVerdict = "approve"
	ReviewChangesRequired ReviewVerdict = "changes_required"
	ReviewDuplicate       ReviewVerdict = "duplicate"
	ReviewBlocked         ReviewVerdict = "blocked"
)

type ReviewReceipt struct {
	Protocol                string        `json:"protocol"`
	ContributionReceiptHash string        `json:"contribution_receipt_hash"`
	HeadSHA                 string        `json:"head_sha"`
	PatchSHA256             string        `json:"patch_sha256"`
	ReviewerID              string        `json:"reviewer_id"`
	ReviewerMode            string        `json:"reviewer_mode"`
	Verdict                 ReviewVerdict `json:"verdict"`
	Evidence                Artifact      `json:"evidence"`
	ReviewHash              string        `json:"review_hash"`
}

type ReviewInput struct {
	VerifyInput
	ReviewerID   string
	Verdict      ReviewVerdict
	EvidencePath string
}

func BuildContributionReceipt(input BuildInput) (ContributionReceipt, error) {
	if input.IssueURL == "" || input.Repository == "" || input.RunID == "" || input.TerminationReason == "" {
		return ContributionReceipt{}, errors.New("issue_url, repository, run_id and termination_reason are required")
	}
	if !isGitSHA(input.BaseSHA) || !isGitSHA(input.HeadSHA) {
		return ContributionReceipt{}, errors.New("base_sha and head_sha must be 40-character hex Git SHAs")
	}
	if err := validateUsage(input.Usage); err != nil {
		return ContributionReceipt{}, err
	}
	if err := validateTiming(input.Timing); err != nil {
		return ContributionReceipt{}, err
	}

	artifacts, err := loadArtifacts(input.PatchPath, input.TrajectoryPath, input.ConfigPath, input.EnvironmentPath, input.EvaluationPath)
	if err != nil {
		return ContributionReceipt{}, err
	}

	receipt := ContributionReceipt{
		Protocol:          ContributionReceiptProtocol,
		IssueURL:          input.IssueURL,
		Repository:        input.Repository,
		RunID:             input.RunID,
		BaseSHA:           input.BaseSHA,
		HeadSHA:           input.HeadSHA,
		Patch:             artifacts[0],
		Trajectory:        artifacts[1],
		Config:            artifacts[2],
		Environment:       artifacts[3],
		Evaluation:        artifacts[4],
		Usage:             input.Usage,
		Timing:            input.Timing,
		TerminationReason: input.TerminationReason,
		Limitations:       append([]string(nil), input.Limitations...),
	}
	if receipt.Limitations == nil {
		receipt.Limitations = []string{}
	}
	var hashErr error
	receipt.ReceiptHash, hashErr = contributionHash(receipt)
	if hashErr != nil {
		return ContributionReceipt{}, hashErr
	}
	if err := receipt.Validate(); err != nil {
		return ContributionReceipt{}, err
	}
	return receipt, nil
}

func (r ContributionReceipt) Validate() error {
	if r.Protocol != ContributionReceiptProtocol {
		return fmt.Errorf("unexpected contribution receipt protocol %q", r.Protocol)
	}
	if r.IssueURL == "" || r.Repository == "" || r.RunID == "" || r.TerminationReason == "" {
		return errors.New("contribution receipt required identity fields are empty")
	}
	if !isGitSHA(r.BaseSHA) || !isGitSHA(r.HeadSHA) {
		return errors.New("contribution receipt base/head SHA is invalid")
	}
	if err := validateUsage(r.Usage); err != nil {
		return err
	}
	if err := validateTiming(r.Timing); err != nil {
		return err
	}
	for name, artifact := range map[string]Artifact{
		"patch": r.Patch, "trajectory": r.Trajectory, "config": r.Config,
		"environment": r.Environment, "evaluation": r.Evaluation,
	} {
		if err := artifact.Validate(); err != nil {
			return fmt.Errorf("%s artifact: %w", name, err)
		}
	}
	expected, err := contributionHash(r)
	if err != nil {
		return err
	}
	if r.ReceiptHash != expected {
		return errors.New("contribution receipt hash mismatch")
	}
	return nil
}

func VerifyContributionReceipt(r ContributionReceipt, input VerifyInput) error {
	if err := r.Validate(); err != nil {
		return err
	}
	if input.HeadSHA != r.HeadSHA {
		return fmt.Errorf("%w: head changed: receipt=%s observed=%s", ErrStaleCandidate, r.HeadSHA, input.HeadSHA)
	}
	paths := []string{input.PatchPath, input.TrajectoryPath, input.ConfigPath, input.EnvironmentPath, input.EvaluationPath}
	expected := []Artifact{r.Patch, r.Trajectory, r.Config, r.Environment, r.Evaluation}
	labels := []string{"patch", "trajectory", "config", "environment", "evaluation"}
	for i, path := range paths {
		observed, err := artifactFromFile(path)
		if err != nil {
			return fmt.Errorf("verify %s: %w", labels[i], err)
		}
		if observed.Digest != expected[i].Digest {
			return fmt.Errorf("%w: %s digest changed: receipt=%s observed=%s", ErrStaleCandidate, labels[i], expected[i].Digest, observed.Digest)
		}
	}
	return nil
}

func BuildReviewReceipt(contribution ContributionReceipt, input ReviewInput) (ReviewReceipt, error) {
	if err := VerifyContributionReceipt(contribution, input.VerifyInput); err != nil {
		return ReviewReceipt{}, err
	}
	if input.ReviewerID == "" {
		return ReviewReceipt{}, errors.New("reviewer_id is required")
	}
	if !validVerdict(input.Verdict) {
		return ReviewReceipt{}, fmt.Errorf("unsupported reviewer verdict %q", input.Verdict)
	}
	evidence, err := artifactFromFile(input.EvidencePath)
	if err != nil {
		return ReviewReceipt{}, fmt.Errorf("review evidence: %w", err)
	}
	review := ReviewReceipt{
		Protocol:                ReviewReceiptProtocol,
		ContributionReceiptHash: contribution.ReceiptHash,
		HeadSHA:                 contribution.HeadSHA,
		PatchSHA256:             contribution.Patch.Digest,
		ReviewerID:              input.ReviewerID,
		ReviewerMode:            "read-only",
		Verdict:                 input.Verdict,
		Evidence:                evidence,
	}
	var hashErr error
	review.ReviewHash, hashErr = reviewHash(review)
	if hashErr != nil {
		return ReviewReceipt{}, hashErr
	}
	if err := review.Validate(contribution); err != nil {
		return ReviewReceipt{}, err
	}
	return review, nil
}

func (r ReviewReceipt) Validate(contribution ContributionReceipt) error {
	if err := contribution.Validate(); err != nil {
		return fmt.Errorf("contribution receipt: %w", err)
	}
	if r.Protocol != ReviewReceiptProtocol {
		return fmt.Errorf("unexpected review receipt protocol %q", r.Protocol)
	}
	if r.ContributionReceiptHash != contribution.ReceiptHash || r.HeadSHA != contribution.HeadSHA || r.PatchSHA256 != contribution.Patch.Digest {
		return fmt.Errorf("%w: review is not bound to this exact contribution", ErrStaleCandidate)
	}
	if r.ReviewerID == "" || r.ReviewerMode != "read-only" || !validVerdict(r.Verdict) {
		return errors.New("invalid reviewer identity, mode, or verdict")
	}
	if err := r.Evidence.Validate(); err != nil {
		return fmt.Errorf("review evidence: %w", err)
	}
	expected, err := reviewHash(r)
	if err != nil {
		return err
	}
	if r.ReviewHash != expected {
		return errors.New("review receipt hash mismatch")
	}
	return nil
}

func (a Artifact) Validate() error {
	if a.Path == "" {
		return errors.New("path is required")
	}
	if a.HashAlgorithm != "sha256" {
		return fmt.Errorf("unsupported hash algorithm %q", a.HashAlgorithm)
	}
	if !isSHA256(a.Digest) {
		return errors.New("digest must be 64-character sha256 hex")
	}
	return nil
}

func loadArtifacts(paths ...string) ([]Artifact, error) {
	artifacts := make([]Artifact, 0, len(paths))
	for _, path := range paths {
		artifact, err := artifactFromFile(path)
		if err != nil {
			return nil, err
		}
		artifacts = append(artifacts, artifact)
	}
	return artifacts, nil
}

func artifactFromFile(path string) (Artifact, error) {
	if path == "" {
		return Artifact{}, errors.New("artifact path is required")
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return Artifact{}, fmt.Errorf("read %s: %w", path, err)
	}
	sum := sha256.Sum256(b)
	return Artifact{Path: path, HashAlgorithm: "sha256", Digest: hex.EncodeToString(sum[:])}, nil
}

func contributionHash(r ContributionReceipt) (string, error) {
	r.ReceiptHash = ""
	b, err := json.Marshal(r)
	if err != nil {
		return "", fmt.Errorf("marshal contribution receipt: %w", err)
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:]), nil
}

func reviewHash(r ReviewReceipt) (string, error) {
	r.ReviewHash = ""
	b, err := json.Marshal(r)
	if err != nil {
		return "", fmt.Errorf("marshal review receipt: %w", err)
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:]), nil
}

func validateUsage(usage Usage) error {
	if usage.ModelCalls < 0 || usage.InputTokens < 0 || usage.OutputTokens < 0 || usage.CostUSD < 0 {
		return errors.New("usage values must be non-negative")
	}
	return nil
}

func validateTiming(timing Timing) error {
	if timing.WallTimeMS < 0 || timing.TimeToFirstEvidenceMS < 0 || timing.ToolCalls < 0 || timing.ContextBytes < 0 {
		return errors.New("timing values must be non-negative")
	}
	if timing.WallTimeMS > 0 && timing.TimeToFirstEvidenceMS > timing.WallTimeMS {
		return errors.New("time_to_first_evidence_ms cannot exceed wall_time_ms")
	}
	return nil
}

func validVerdict(v ReviewVerdict) bool {
	switch v {
	case ReviewApprove, ReviewChangesRequired, ReviewDuplicate, ReviewBlocked:
		return true
	default:
		return false
	}
}

func isGitSHA(s string) bool {
	return len(s) == 40 && isHex(s)
}

func isSHA256(s string) bool {
	return len(s) == 64 && isHex(s)
}

func isHex(s string) bool {
	_, err := hex.DecodeString(s)
	return err == nil
}
