// Package codingworkflow verifies one owner-approved Go patch in a trusted,
// disposable local/CI runner. It is not an execution sandbox or a remote tool.
package codingworkflow

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"path/filepath"
	"reflect"
	"regexp"
	"runtime"
	"strings"
	"time"

	"github.com/safal207/Liminal-Rail-Metro/internal/metro"
)

const (
	Protocol         = "metro.coding.proof.v0.3"
	ContractProtocol = "metro.coding.contract.v0.3"
	Profile          = "go-test-vet-v1"
	MaxProofBytes    = 6 << 20
	MaxLogBytes      = 1 << 20
)

var (
	idPattern     = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$`)
	repoPattern   = regexp.MustCompile(`^[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+$`)
	shaPattern    = regexp.MustCompile(`^[0-9a-f]{40}$`)
	digestPattern = regexp.MustCompile(`^[0-9a-f]{64}$`)
)

// RequiredTest pins both a test identity and owner-reviewed test source bytes.
// The contract must come from the owner/QA, not be rewritten by the coding agent.
type RequiredTest struct {
	File    string `json:"file"`
	SHA256  string `json:"sha256"`
	Package string `json:"package"`
	Name    string `json:"name"`
}

type Contract struct {
	Protocol      string         `json:"protocol"`
	Repository    string         `json:"repository"`
	IssueURL      string         `json:"issue_url"`
	ActionID      string         `json:"action_id"`
	BaseSHA       string         `json:"base_sha"`
	Profile       string         `json:"profile"`
	RequiredTests []RequiredTest `json:"required_tests"`
}

type File struct {
	Path   string `json:"path"`
	Mode   uint32 `json:"mode"`
	SHA256 string `json:"sha256"`
}

type Snapshot struct {
	StatusSHA256   string `json:"status_sha256"`
	HeadSHA        string `json:"head_sha"`
	TreeSHA        string `json:"tree_sha"`
	PatchSHA256    string `json:"patch_sha256"`
	WorktreeSHA256 string `json:"worktree_sha256"`
	Files          []File `json:"files"`
}

type Check struct {
	Name         string   `json:"name"`
	Args         []string `json:"args"`
	ExitCode     int      `json:"exit_code"`
	Started      bool     `json:"started"`
	TimedOut     bool     `json:"timed_out"`
	Truncated    bool     `json:"truncated"`
	Stdout       string   `json:"stdout"`
	Stderr       string   `json:"stderr"`
	StdoutSHA256 string   `json:"stdout_sha256"`
	StderrSHA256 string   `json:"stderr_sha256"`
}

type Proof struct {
	Protocol       string         `json:"protocol"`
	Contract       Contract       `json:"contract"`
	ContractSHA256 string         `json:"contract_sha256"`
	Before         Snapshot       `json:"before"`
	After          Snapshot       `json:"after"`
	Checks         []Check        `json:"checks"`
	Problem        string         `json:"problem,omitempty"`
	AgentClaim     string         `json:"agent_claim,omitempty"`
	RunnerVersion  string         `json:"runner_version"`
	StartedAt      string         `json:"started_at"`
	FinishedAt     string         `json:"finished_at"`
	Packet         metro.Packet   `json:"packet"`
	Route          metro.Route    `json:"route"`
	Result         map[string]any `json:"result,omitempty"`
	Receipt        *metro.Receipt `json:"receipt,omitempty"`
}

type Verdict struct {
	Disposition string `json:"disposition"`
	Reason      string `json:"reason"`
	Scope       string `json:"scope"`
}

func hold(reason string) Verdict {
	return Verdict{"HOLD", reason, "owner-pinned tests on the exact working tree; not general code correctness"}
}

func Hash(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func hashJSON(v any) string {
	data, err := json.Marshal(v)
	if err != nil {
		return ""
	}
	return Hash(data)
}

func ValidateContract(c Contract) error {
	if c.Protocol != ContractProtocol || c.Profile != Profile || !repoPattern.MatchString(c.Repository) ||
		!idPattern.MatchString(c.ActionID) || !shaPattern.MatchString(c.BaseSHA) {
		return errors.New("invalid contract identity, base SHA or fixed check profile")
	}
	u, err := url.Parse(c.IssueURL)
	if err != nil || u.Scheme != "https" || u.Host != "github.com" || u.User != nil || u.RawQuery != "" || u.Fragment != "" ||
		!regexp.MustCompile(`^/`+regexp.QuoteMeta(c.Repository)+`/issues/[1-9][0-9]*$`).MatchString(u.Path) {
		return errors.New("issue must identify the contract repository on GitHub")
	}
	if len(c.RequiredTests) == 0 || len(c.RequiredTests) > 64 {
		return errors.New("one to 64 owner-pinned tests required")
	}
	seen := map[string]bool{}
	for _, test := range c.RequiredTests {
		if test.File == "" || filepath.IsAbs(test.File) || strings.Contains(test.File, "\\") ||
			strings.HasPrefix(filepath.ToSlash(filepath.Clean(test.File)), "../") || filepath.ToSlash(filepath.Clean(test.File)) != test.File ||
			!strings.HasSuffix(test.File, "_test.go") || !digestPattern.MatchString(test.SHA256) ||
			!strings.HasPrefix(test.Package, "github.com/"+c.Repository+"/") || !regexp.MustCompile(`^Test[A-Za-z0-9_]+$`).MatchString(test.Name) {
			return errors.New("invalid owner-pinned test")
		}
		key := test.Package + ":" + test.Name
		if seen[key] {
			return errors.New("duplicate required test")
		}
		seen[key] = true
	}
	return nil
}

func pinnedTests(c Contract, s Snapshot) bool {
	files := map[string]string{}
	for _, f := range s.Files {
		files[f.Path] = f.SHA256
	}
	for _, test := range c.RequiredTests {
		if files[test.File] != test.SHA256 {
			return false
		}
	}
	return true
}

func suite() []Check {
	return []Check{
		{Name: "go-test", Args: []string{"test", "-json", "-count=1", "./..."}},
		{Name: "go-vet", Args: []string{"vet", "./..."}},
	}
}

// testEvents inspects actual go-test JSON, not text such as "PASS" in a log.
func testEvents(log string, required []RequiredTest) bool {
	passed := map[string]bool{}
	decoder := json.NewDecoder(strings.NewReader(log))
	for {
		var e struct{ Action, Package, Test string }
		err := decoder.Decode(&e)
		if err == io.EOF {
			break
		}
		if err != nil || e.Action == "fail" {
			return false
		}
		if e.Action == "pass" && e.Test != "" {
			passed[e.Package+":"+e.Test] = true
		}
	}
	for _, test := range required {
		if !passed[test.Package+":"+test.Name] {
			return false
		}
	}
	return true
}

// assess never considers AgentClaim when calculating acceptance.
func assess(p Proof, c Contract, current Snapshot) Verdict {
	if ValidateContract(c) != nil || p.Protocol != Protocol || p.ContractSHA256 != hashJSON(c) || !reflect.DeepEqual(p.Contract, c) {
		return hold("CONTRACT_MISMATCH")
	}
	if !reflect.DeepEqual(p.Before, p.After) || !reflect.DeepEqual(p.Before, current) {
		return hold("STALE_PATCH")
	}
	if !pinnedTests(c, p.Before) {
		return hold("ACCEPTANCE_CHANGED")
	}
	if p.Problem != "" {
		return hold(p.Problem)
	}
	checks := suite()
	for i, got := range p.Checks {
		if i >= len(checks) || got.Name != checks[i].Name || !reflect.DeepEqual(got.Args, checks[i].Args) ||
			got.StdoutSHA256 != Hash([]byte(got.Stdout)) || got.StderrSHA256 != Hash([]byte(got.Stderr)) {
			return hold("EVIDENCE_MISMATCH")
		}
		if got.TimedOut {
			return hold("TEST_TIMEOUT")
		}
		if !got.Started {
			return hold("CHECK_NOT_STARTED")
		}
		if got.Truncated {
			return hold("OUTPUT_LIMIT")
		}
		if got.ExitCode != 0 {
			return hold("TEST_FAILED")
		}
		if i == 0 && !testEvents(got.Stdout, c.RequiredTests) {
			return hold("MISSING_REQUIRED_TEST")
		}
	}
	if len(p.Checks) != len(checks) {
		return hold("MISSING_CHECK")
	}
	return Verdict{"VERIFIED", "REQUIRED_CHECKS_PASSED", hold("").Scope}
}

func resultFor(p Proof) map[string]any {
	return map[string]any{
		"head_sha":        p.Before.HeadSHA,
		"worktree_sha256": p.Before.WorktreeSHA256,
		"patch_sha256":    p.Before.PatchSHA256,
		"checks_sha256":   hashJSON(p.Checks),
		"contract_sha256": p.ContractSHA256,
	}
}

func inputFor(p Proof) map[string]any {
	return map[string]any{"issue_url": p.Contract.IssueURL, "contract_sha256": p.ContractSHA256,
		"base_sha": p.Contract.BaseSHA, "head_sha": p.Before.HeadSHA, "worktree_sha256": p.Before.WorktreeSHA256,
		"patch_sha256": p.Before.PatchSHA256}
}

// Evaluate binds the local test report to Metro, in addition to checking results.
func Evaluate(p Proof, c Contract, current Snapshot) Verdict {
	v := assess(p, c, current)
	if v.Disposition != "VERIFIED" {
		return v
	}
	if p.Packet.Protocol != metro.PacketProtocol || p.Packet.ActionID != c.ActionID || p.Packet.Action.Kind != "coding.go-check" ||
		p.Packet.SourceAgent != "metro-check" || !p.Packet.Constraints.SideEffect || p.Route.DecisionMode != "explicit-local-test-approval" || hashJSON(p.Packet.Action.Inputs) != hashJSON(inputFor(p)) ||
		!reflect.DeepEqual(p.Packet.AllowedTargets, []string{"local-go-verifier"}) ||
		p.Route.Protocol != metro.RouteProtocol || p.Route.ActionID != c.ActionID || p.Route.SelectedTarget != "local-go-verifier" ||
		p.Route.PolicyRef != "policy://coding/go-test-vet-v1" ||
		hashJSON(p.Result) != hashJSON(resultFor(p)) || p.Receipt == nil || p.Receipt.Status != "SUCCEEDED" ||
		p.Receipt.Protocol != metro.ReceiptProtocol || p.Receipt.HashAlgorithm != "sha256" {
		return hold("EVIDENCE_MISMATCH")
	}
	if err := metro.Verify(p.Packet, p.Route, p.Result, *p.Receipt); err != nil {
		return hold("EVIDENCE_MISMATCH")
	}
	return v
}

// Verify checks a digest pinned OUTSIDE the agent's workspace. It does not
// authenticate arbitrary self-supplied receipts or execute/re-run any tests.
func Verify(ctx context.Context, root string, c Contract, data []byte, pinnedDigest string) (Verdict, error) {
	if !digestPattern.MatchString(pinnedDigest) || Hash(data) != pinnedDigest || len(data) > MaxProofBytes {
		return hold("EVIDENCE_MISMATCH"), nil
	}
	var proof Proof
	if err := Decode(bytes.NewReader(data), &proof, MaxProofBytes); err != nil {
		return hold("EVIDENCE_MISMATCH"), nil
	}
	current, err := TakeSnapshot(ctx, root, c)
	if err != nil {
		return hold("STALE_PATCH"), err
	}
	return Evaluate(proof, c, current), nil
}

// Decode requires one bounded JSON object and rejects unknown struct fields.
// Evidence bytes are hash-pinned before this decoder runs in Verify.
func Decode(reader io.Reader, target any, limit int64) error {
	b, err := io.ReadAll(io.LimitReader(reader, limit+1))
	if err != nil {
		return err
	}
	if int64(len(b)) > limit || len(bytes.TrimSpace(b)) == 0 || bytes.TrimSpace(b)[0] != '{' {
		return errors.New("invalid bounded JSON object")
	}
	d := json.NewDecoder(bytes.NewReader(b))
	d.DisallowUnknownFields()
	if err := d.Decode(target); err != nil {
		return err
	}
	if d.Decode(new(any)) != io.EOF {
		return errors.New("expected one JSON object")
	}
	return nil
}

// Run executes the immutable profile exactly once. Caller must explicitly
// authorize tests; CLI enforces --allow-tests. Use an isolated, disposable host.
func Run(ctx context.Context, root string, c Contract, agentClaim string) (Proof, Verdict, error) {
	return run(ctx, root, c, agentClaim, execute)
}

type executor func(context.Context, string, Check) Check

func run(ctx context.Context, root string, c Contract, claim string, exec executor) (Proof, Verdict, error) {
	if runtime.GOOS != "linux" && runtime.GOOS != "darwin" {
		return Proof{}, hold("UNSUPPORTED_PLATFORM"), errors.New("v0.3 requires Linux/macOS; use a disposable Linux CI runner")
	}
	if err := ValidateContract(c); err != nil {
		return Proof{}, hold("CONTRACT_MISMATCH"), err
	}
	before, err := TakeSnapshot(ctx, root, c)
	if err != nil {
		return Proof{}, hold("PREFLIGHT_FAILED"), err
	}
	if status, err := git(ctx, root, "status", "--porcelain", "--untracked-files=all"); err != nil || strings.TrimSpace(status) != "" {
		return Proof{}, hold("DIRTY_WORKTREE"), errors.New("commit the candidate patch before verification")
	}
	p := Proof{Protocol: Protocol, Contract: c, ContractSHA256: hashJSON(c), Before: before, After: before,
		AgentClaim: claim, RunnerVersion: runtime.Version() + "/" + runtime.GOOS + "/" + runtime.GOARCH,
		StartedAt: time.Now().UTC().Format(time.RFC3339Nano)}
	p.Packet = metro.NewPacket(c.ActionID, "metro-check", "Run the owner-approved Go test profile",
		metro.Action{Kind: "coding.go-check", Inputs: inputFor(p)}, []string{"local-go-verifier"})
	// Tests execute code. This is explicit local approval, NOT an AUTO_ROUTE
	// decision or a claim that tests cannot have side effects.
	p.Packet.Constraints.SideEffect = true
	p.Route = metro.Route{Protocol: metro.RouteProtocol, RouteID: "route-" + c.ActionID, ActionID: c.ActionID,
		RouterID: "metro-check", DecisionMode: "explicit-local-test-approval", SelectedTarget: "local-go-verifier",
		PolicyRef: "policy://coding/go-test-vet-v1", DecidedAt: p.StartedAt}
	if !pinnedTests(c, before) {
		p.Problem = "ACCEPTANCE_CHANGED"
	}
	for _, check := range suite() {
		if p.Problem != "" || ctx.Err() != nil {
			if ctx.Err() != nil {
				p.Problem = "TEST_TIMEOUT"
			}
			break
		}
		fresh, snapErr := TakeSnapshot(ctx, root, c)
		if snapErr != nil || !reflect.DeepEqual(fresh, before) {
			p.Problem = "STALE_PATCH"
			break
		}
		check = exec(ctx, root, check)
		p.Checks = append(p.Checks, check)
		fresh, snapErr = TakeSnapshot(context.WithoutCancel(ctx), root, c)
		if snapErr != nil || !reflect.DeepEqual(fresh, before) {
			p.Problem = "STALE_PATCH"
			break
		}
		if check.TimedOut || !check.Started || check.Truncated || check.ExitCode != 0 {
			break
		}
		if check.Name == "go-test" && !testEvents(check.Stdout, c.RequiredTests) {
			p.Problem = "MISSING_REQUIRED_TEST"
			break
		}
	}
	p.After, err = TakeSnapshot(context.WithoutCancel(ctx), root, c)
	if err != nil {
		p.Problem = "STALE_PATCH"
	}
	p.FinishedAt = time.Now().UTC().Format(time.RFC3339Nano)
	v := assess(p, c, p.After)
	if v.Disposition == "VERIFIED" {
		p.Result = resultFor(p)
		r, err := metro.MakeSuccessReceipt(p.Packet, p.Route, p.Result, "")
		if err != nil {
			return p, hold("EVIDENCE_MISMATCH"), err
		}
		r.StartedAt, r.CompletedAt = p.StartedAt, p.FinishedAt
		p.Receipt = &r
		v = Evaluate(p, c, p.After)
	}
	return p, v, nil
}

// Summary is safe to print. Keep the raw proof/logs in the local proof directory.
func Summary(v Verdict, digest string) string {
	b, _ := json.Marshal(map[string]any{"disposition": v.Disposition, "reason": v.Reason,
		"scope": v.Scope, "evidence_sha256": digest})
	return fmt.Sprintln(string(b))
}
