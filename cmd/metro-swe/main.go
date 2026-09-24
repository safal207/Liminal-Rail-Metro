package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/safal207/Liminal-Rail-Metro/internal/sweadapter"
)

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		usage(stderr)
		return 2
	}
	switch args[0] {
	case "pack":
		return runPack(args[1:], stdout, stderr)
	case "verify":
		return runVerify(args[1:], stdout, stderr)
	case "review":
		return runReview(args[1:], stdout, stderr)
	case "help", "-h", "--help":
		usage(stdout)
		return 0
	default:
		fmt.Fprintf(stderr, "unknown command %q\n", args[0])
		usage(stderr)
		return 2
	}
}

type commonPaths struct {
	head, patch, trajectory, config, environment, evaluation *string
}

func addCommonPaths(fs *flag.FlagSet) commonPaths {
	return commonPaths{
		head:        fs.String("head", "", "exact candidate head SHA"),
		patch:       fs.String("patch", "", "candidate patch file"),
		trajectory:  fs.String("trajectory", "", "SWE-agent trajectory file"),
		config:      fs.String("config", "", "SWE-agent config file"),
		environment: fs.String("environment", "", "environment/setup description file"),
		evaluation:  fs.String("evaluation", "", "evaluation commands/results file"),
	}
}

func (p commonPaths) verifyInput() sweadapter.VerifyInput {
	return sweadapter.VerifyInput{HeadSHA: *p.head, PatchPath: *p.patch, TrajectoryPath: *p.trajectory, ConfigPath: *p.config, EnvironmentPath: *p.environment, EvaluationPath: *p.evaluation}
}

func runPack(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("pack", flag.ContinueOnError)
	fs.SetOutput(stderr)
	issue := fs.String("issue", "", "issue URL")
	repo := fs.String("repo", "", "repository owner/name")
	runID := fs.String("run", "", "SWE-agent run ID")
	base := fs.String("base", "", "base SHA")
	paths := addCommonPaths(fs)
	modelCalls := fs.Int("model-calls", 0, "model calls")
	inputTokens := fs.Int64("input-tokens", 0, "input tokens")
	outputTokens := fs.Int64("output-tokens", 0, "output tokens")
	costUSD := fs.Float64("cost-usd", 0, "reported model cost in USD")
	wallMS := fs.Int64("wall-ms", 0, "wall-clock runtime in milliseconds")
	firstEvidenceMS := fs.Int64("first-evidence-ms", 0, "time to first evidence in milliseconds")
	toolCalls := fs.Int64("tool-calls", 0, "tool calls")
	contextBytes := fs.Int64("context-bytes", 0, "context bytes moved when measured")
	termination := fs.String("termination", "", "termination reason")
	limitations := fs.String("limitations", "", "semicolon-separated claim limitations")
	out := fs.String("out", "contribution_receipt.json", "output receipt path")
	if err := fs.Parse(args); err != nil || fs.NArg() != 0 {
		return 2
	}
	receipt, err := sweadapter.BuildContributionReceipt(sweadapter.BuildInput{
		IssueURL: *issue, Repository: *repo, RunID: *runID, BaseSHA: *base, HeadSHA: *paths.head,
		PatchPath: *paths.patch, TrajectoryPath: *paths.trajectory, ConfigPath: *paths.config, EnvironmentPath: *paths.environment, EvaluationPath: *paths.evaluation,
		Usage:             sweadapter.Usage{ModelCalls: *modelCalls, InputTokens: *inputTokens, OutputTokens: *outputTokens, CostUSD: *costUSD},
		Timing:            sweadapter.Timing{WallTimeMS: *wallMS, TimeToFirstEvidenceMS: *firstEvidenceMS, ToolCalls: *toolCalls, ContextBytes: *contextBytes},
		TerminationReason: *termination, Limitations: splitLimitations(*limitations),
	})
	if err != nil {
		fmt.Fprintf(stderr, "pack: %v\n", err)
		return 1
	}
	if err := writeJSON(*out, receipt); err != nil {
		fmt.Fprintf(stderr, "pack: %v\n", err)
		return 1
	}
	fmt.Fprintf(stdout, "receipt=%s\nhead=%s\npatch_sha256=%s\n", receipt.ReceiptHash, receipt.HeadSHA, receipt.Patch.Digest)
	return 0
}

func runVerify(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("verify", flag.ContinueOnError)
	fs.SetOutput(stderr)
	receiptPath := fs.String("receipt", "", "contribution receipt JSON")
	paths := addCommonPaths(fs)
	if err := fs.Parse(args); err != nil || fs.NArg() != 0 {
		return 2
	}
	var receipt sweadapter.ContributionReceipt
	if err := readJSON(*receiptPath, &receipt); err != nil {
		fmt.Fprintf(stderr, "verify: %v\n", err)
		return 1
	}
	if err := sweadapter.VerifyContributionReceipt(receipt, paths.verifyInput()); err != nil {
		fmt.Fprintf(stderr, "verify: %v\n", err)
		if errors.Is(err, sweadapter.ErrStaleCandidate) {
			fmt.Fprintln(stderr, "VERDICT: STALE_CANDIDATE")
		}
		return 1
	}
	fmt.Fprintln(stdout, "VERDICT: EXACT_CANDIDATE")
	return 0
}

func runReview(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("review", flag.ContinueOnError)
	fs.SetOutput(stderr)
	receiptPath := fs.String("receipt", "", "contribution receipt JSON")
	paths := addCommonPaths(fs)
	reviewer := fs.String("reviewer", "", "independent reviewer identity")
	verdict := fs.String("verdict", "", "approve|changes_required|duplicate|blocked")
	evidence := fs.String("evidence", "", "review evidence file")
	out := fs.String("out", "review_receipt.json", "output review receipt path")
	if err := fs.Parse(args); err != nil || fs.NArg() != 0 {
		return 2
	}
	var receipt sweadapter.ContributionReceipt
	if err := readJSON(*receiptPath, &receipt); err != nil {
		fmt.Fprintf(stderr, "review: %v\n", err)
		return 1
	}
	review, err := sweadapter.BuildReviewReceipt(receipt, sweadapter.ReviewInput{
		VerifyInput: paths.verifyInput(), ReviewerID: *reviewer, Verdict: sweadapter.ReviewVerdict(*verdict), EvidencePath: *evidence,
	})
	if err != nil {
		fmt.Fprintf(stderr, "review: %v\n", err)
		if errors.Is(err, sweadapter.ErrStaleCandidate) {
			fmt.Fprintln(stderr, "VERDICT: STALE_CANDIDATE")
		}
		return 1
	}
	if err := writeJSON(*out, review); err != nil {
		fmt.Fprintf(stderr, "review: %v\n", err)
		return 1
	}
	fmt.Fprintf(stdout, "review=%s\nverdict=%s\n", review.ReviewHash, review.Verdict)
	return 0
}

func readJSON(path string, target any) error {
	if path == "" {
		return errors.New("JSON path is required")
	}
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	decoder := json.NewDecoder(f)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		if err == nil {
			return errors.New("trailing JSON value is not allowed")
		}
		return err
	}
	return nil
}

func writeJSON(path string, value any) error {
	if path == "" {
		return errors.New("output path is required")
	}
	b, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	b = append(b, '\n')
	return os.WriteFile(path, b, 0o600)
}

func splitLimitations(value string) []string {
	if strings.TrimSpace(value) == "" {
		return []string{}
	}
	parts := strings.Split(value, ";")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		if trimmed := strings.TrimSpace(part); trimmed != "" {
			out = append(out, trimmed)
		}
	}
	return out
}

func usage(w io.Writer) {
	fmt.Fprintln(w, "usage:")
	fmt.Fprintln(w, "  metro-swe pack   -issue URL -repo owner/name -run ID -base SHA -head SHA -patch FILE -trajectory FILE -config FILE -environment FILE -evaluation FILE -termination REASON [-out FILE]")
	fmt.Fprintln(w, "  metro-swe verify -receipt FILE -head SHA -patch FILE -trajectory FILE -config FILE -environment FILE -evaluation FILE")
	fmt.Fprintln(w, "  metro-swe review -receipt FILE -head SHA -patch FILE -trajectory FILE -config FILE -environment FILE -evaluation FILE -reviewer ID -verdict VERDICT -evidence FILE [-out FILE]")
}
