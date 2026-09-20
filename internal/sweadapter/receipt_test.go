package sweadapter

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestContributionReceiptExactCandidateAndStalePatch(t *testing.T) {
	dir := t.TempDir()
	paths := fixtureFiles(t, dir)
	input := BuildInput{
		IssueURL:          "https://github.com/SWE-agent/SWE-agent/issues/1524",
		Repository:        "SWE-agent/SWE-agent",
		RunID:             "fixture-1524",
		BaseSHA:           "3ea751c087f32b16e039a2233dd6eefecef325d5",
		HeadSHA:           "1111111111111111111111111111111111111111",
		PatchPath:         paths[0],
		TrajectoryPath:    paths[1],
		ConfigPath:        paths[2],
		EnvironmentPath:   paths[3],
		EvaluationPath:    paths[4],
		Usage:             Usage{ModelCalls: 7, InputTokens: 9000, OutputTokens: 1200, CostUSD: 0.42},
		Timing:            Timing{WallTimeMS: 42000, TimeToFirstEvidenceMS: 7000, ToolCalls: 11, ContextBytes: 64000},
		TerminationReason: "submitted",
		Limitations:       []string{"contract fixture; not a live SWE-agent execution"},
	}
	receipt, err := BuildContributionReceipt(input)
	if err != nil {
		t.Fatal(err)
	}
	verify := VerifyInput{
		HeadSHA: input.HeadSHA, PatchPath: paths[0], TrajectoryPath: paths[1], ConfigPath: paths[2], EnvironmentPath: paths[3], EvaluationPath: paths[4],
	}
	if err := VerifyContributionReceipt(receipt, verify); err != nil {
		t.Fatalf("verify exact candidate: %v", err)
	}
	if err := os.WriteFile(paths[0], []byte("changed patch\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := VerifyContributionReceipt(receipt, verify); !errors.Is(err, ErrStaleCandidate) {
		t.Fatalf("expected stale candidate, got %v", err)
	}
}

func TestContributionReceiptStaleHead(t *testing.T) {
	dir := t.TempDir()
	paths := fixtureFiles(t, dir)
	receipt, err := BuildContributionReceipt(BuildInput{
		IssueURL: "https://github.com/SWE-agent/SWE-agent/issues/1524", Repository: "SWE-agent/SWE-agent", RunID: "fixture-1524",
		BaseSHA: "3ea751c087f32b16e039a2233dd6eefecef325d5", HeadSHA: "1111111111111111111111111111111111111111",
		PatchPath: paths[0], TrajectoryPath: paths[1], ConfigPath: paths[2], EnvironmentPath: paths[3], EvaluationPath: paths[4],
		TerminationReason: "submitted",
	})
	if err != nil {
		t.Fatal(err)
	}
	verify := VerifyInput{HeadSHA: "2222222222222222222222222222222222222222", PatchPath: paths[0], TrajectoryPath: paths[1], ConfigPath: paths[2], EnvironmentPath: paths[3], EvaluationPath: paths[4]}
	if err := VerifyContributionReceipt(receipt, verify); !errors.Is(err, ErrStaleCandidate) {
		t.Fatalf("expected stale candidate, got %v", err)
	}
}


func TestContributionReceiptValidateRejectsInvalidTelemetry(t *testing.T) {
	dir := t.TempDir()
	paths := fixtureFiles(t, dir)
	base := BuildInput{
		IssueURL: "https://github.com/SWE-agent/SWE-agent/issues/1524", Repository: "SWE-agent/SWE-agent", RunID: "fixture-1524",
		BaseSHA: "3ea751c087f32b16e039a2233dd6eefecef325d5", HeadSHA: "1111111111111111111111111111111111111111",
		PatchPath: paths[0], TrajectoryPath: paths[1], ConfigPath: paths[2], EnvironmentPath: paths[3], EvaluationPath: paths[4],
		TerminationReason: "submitted",
	}
	receipt, err := BuildContributionReceipt(base)
	if err != nil {
		t.Fatal(err)
	}

	negativeUsage := receipt
	negativeUsage.Usage.InputTokens = -1
	negativeUsage.ReceiptHash, _ = contributionHash(negativeUsage)
	if err := negativeUsage.Validate(); err == nil {
		t.Fatal("expected validation to reject negative usage")
	}

	invalidTiming := receipt
	invalidTiming.Timing.WallTimeMS = 100
	invalidTiming.Timing.TimeToFirstEvidenceMS = 101
	invalidTiming.ReceiptHash, _ = contributionHash(invalidTiming)
	if err := invalidTiming.Validate(); err == nil {
		t.Fatal("expected validation to reject time_to_first_evidence_ms greater than wall_time_ms")
	}
}

func TestReviewReceiptBindsExactContribution(t *testing.T) {
	dir := t.TempDir()
	paths := fixtureFiles(t, dir)
	receipt, err := BuildContributionReceipt(BuildInput{
		IssueURL: "https://github.com/SWE-agent/SWE-agent/issues/1524", Repository: "SWE-agent/SWE-agent", RunID: "fixture-1524",
		BaseSHA: "3ea751c087f32b16e039a2233dd6eefecef325d5", HeadSHA: "1111111111111111111111111111111111111111",
		PatchPath: paths[0], TrajectoryPath: paths[1], ConfigPath: paths[2], EnvironmentPath: paths[3], EvaluationPath: paths[4],
		TerminationReason: "submitted",
	})
	if err != nil {
		t.Fatal(err)
	}
	evidence := filepath.Join(dir, "review.txt")
	if err := os.WriteFile(evidence, []byte("exact-head checks passed\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	review, err := BuildReviewReceipt(receipt, ReviewInput{
		VerifyInput: VerifyInput{HeadSHA: receipt.HeadSHA, PatchPath: paths[0], TrajectoryPath: paths[1], ConfigPath: paths[2], EnvironmentPath: paths[3], EvaluationPath: paths[4]},
		ReviewerID:  "independent-fixture-reviewer", Verdict: ReviewApprove, EvidencePath: evidence,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := review.Validate(receipt); err != nil {
		t.Fatal(err)
	}
	mutated := receipt
	mutated.HeadSHA = "2222222222222222222222222222222222222222"
	mutated.ReceiptHash, _ = contributionHash(mutated)
	if err := review.Validate(mutated); !errors.Is(err, ErrStaleCandidate) {
		t.Fatalf("expected stale review, got %v", err)
	}
}

func fixtureFiles(t *testing.T, dir string) []string {
	t.Helper()
	names := []string{"candidate.patch", "run.traj", "config.yaml", "environment.json", "evaluation.json"}
	contents := []string{"diff --git a/a b/a\n", "{\"step\":1}\n", "model: fixture\n", "{\"image\":\"fixture\"}\n", "{\"command\":\"pytest\",\"exit_code\":0}\n"}
	paths := make([]string, len(names))
	for i := range names {
		paths[i] = filepath.Join(dir, names[i])
		if err := os.WriteFile(paths[i], []byte(contents[i]), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return paths
}
