package codingworkflow

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func fixture(t *testing.T) (string, Contract) {
	t.Helper()
	root := t.TempDir()
	write := func(name, text string) {
		t.Helper()
		p := filepath.Join(root, name)
		if err := os.MkdirAll(filepath.Dir(p), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(text), 0600); err != nil {
			t.Fatal(err)
		}
	}
	write("go.mod", "module github.com/safal207/Liminal-Rail-Metro\n\ngo 1.23.0\n")
	test := "package sample\nimport \"testing\"\nfunc TestApproved(t *testing.T) {}\n"
	write("internal/sample/approved_test.go", test)
	write(".gitignore", "ignored.txt\n")
	gitTest(t, root, "init", "-q")
	gitTest(t, root, "remote", "add", "origin", "https://github.com/safal207/Liminal-Rail-Metro.git")
	commitTest(t, root)
	head := strings.TrimSpace(gitTest(t, root, "rev-parse", "HEAD"))
	return root, Contract{Protocol: ContractProtocol, Repository: "safal207/Liminal-Rail-Metro", IssueURL: "https://github.com/safal207/Liminal-Rail-Metro/issues/14", ActionID: "coding-check-14", BaseSHA: head, Profile: Profile,
		RequiredTests: []RequiredTest{{File: "internal/sample/approved_test.go", SHA256: Hash([]byte(test)), Package: "github.com/safal207/Liminal-Rail-Metro/internal/sample", Name: "TestApproved"}}}
}

func gitTest(t *testing.T, root string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-c", "core.hooksPath=/dev/null", "-c", "commit.gpgsign=false", "-c", "user.name=Metro Test", "-c", "user.email=metro@example.invalid"}, args...)...)
	cmd.Dir = root
	cmd.Env = []string{"PATH=" + os.Getenv("PATH"), "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=/dev/null"}
	data, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git failed: %v %s", err, data)
	}
	return string(data)
}
func commitTest(t *testing.T, root string) {
	t.Helper()
	gitTest(t, root, "add", ".")
	gitTest(t, root, "commit", "-qm", "fixture")
}

func passing(c Contract, check Check) Check {
	check.Started = true
	if check.Name == "go-test" {
		event := map[string]string{"Action": "pass", "Test": c.RequiredTests[0].Name, "Package": c.RequiredTests[0].Package}
		b, _ := json.Marshal(event)
		return finishCheck(check, string(b)+"\n", "")
	}
	return finishCheck(check, "", "")
}

func good(t *testing.T, root string, c Contract) Proof {
	t.Helper()
	p, v, err := run(context.Background(), root, c, "fixed", func(_ context.Context, _ string, ch Check) Check { return passing(c, ch) })
	if err != nil || v.Disposition != "VERIFIED" {
		t.Fatalf("%v %#v", err, v)
	}
	return p
}

func TestPassHasPatchBoundReceipt(t *testing.T) {
	root, c := fixture(t)
	p := good(t, root, c)
	if len(p.Checks) != 2 || p.Receipt == nil || p.Receipt.Status != "SUCCEEDED" {
		t.Fatalf("bad proof %#v", p)
	}
	b, _ := json.Marshal(p)
	v, err := Verify(context.Background(), root, c, b, Hash(b))
	if err != nil || v.Disposition != "VERIFIED" {
		t.Fatalf("%v %#v", err, v)
	}
}

func TestFalseAgentSuccessCannotOverrideFailedTests(t *testing.T) {
	root, c := fixture(t)
	calls := 0
	p, v, err := run(context.Background(), root, c, "fixed", func(_ context.Context, _ string, ch Check) Check {
		calls++
		ch = passing(c, ch)
		ch.ExitCode = 1
		return ch
	})
	if err != nil || v.Reason != "TEST_FAILED" || calls != 1 || p.Receipt != nil {
		t.Fatalf("%v %#v calls=%d", err, v, calls)
	}
}

func TestPostTestChangesAreStaleIncludingIgnoredFiles(t *testing.T) {
	for _, path := range []string{"internal/sample/approved_test.go", "new.txt", "ignored.txt"} {
		t.Run(path, func(t *testing.T) {
			root, c := fixture(t)
			p := good(t, root, c)
			b, _ := json.Marshal(p)
			if err := os.WriteFile(filepath.Join(root, path), []byte("changed"), 0600); err != nil {
				t.Fatal(err)
			}
			v, err := Verify(context.Background(), root, c, b, Hash(b))
			if err != nil || v.Reason != "STALE_PATCH" {
				t.Fatalf("%v %#v", err, v)
			}
		})
	}
}

func TestMutationDuringTestsStopsWithoutRerun(t *testing.T) {
	root, c := fixture(t)
	calls := 0
	p, v, err := run(context.Background(), root, c, "fixed", func(_ context.Context, root string, ch Check) Check {
		calls++
		if err := os.WriteFile(filepath.Join(root, "ignored.txt"), []byte("mutation"), 0600); err != nil {
			t.Fatal(err)
		}
		return passing(c, ch)
	})
	if err != nil || v.Reason != "STALE_PATCH" || calls != 1 || p.Receipt != nil {
		t.Fatalf("%v %#v calls=%d", err, v, calls)
	}
}

func TestPinnedAcceptanceCannotBeChanged(t *testing.T) {
	root, c := fixture(t)
	if err := os.WriteFile(filepath.Join(root, c.RequiredTests[0].File), []byte("package sample\n"), 0600); err != nil {
		t.Fatal(err)
	}
	commitTest(t, root)
	calls := 0
	p, v, err := run(context.Background(), root, c, "fixed", func(_ context.Context, _ string, ch Check) Check { calls++; return passing(c, ch) })
	if err != nil || v.Reason != "ACCEPTANCE_CHANGED" || calls != 0 || p.Receipt != nil {
		t.Fatalf("%v %#v calls=%d", err, v, calls)
	}
}

func TestMissingOrSkippedNamedTestIsNotSuccess(t *testing.T) {
	for _, output := range []string{"", `{"Action":"skip","Package":"github.com/safal207/Liminal-Rail-Metro/internal/sample","Test":"TestApproved"}` + "\n", "PASS\n"} {
		root, c := fixture(t)
		p, v, err := run(context.Background(), root, c, "fixed", func(_ context.Context, _ string, ch Check) Check {
			ch.Started = true
			return finishCheck(ch, output, "")
		})
		if err != nil || v.Reason != "MISSING_REQUIRED_TEST" || len(p.Checks) != 1 || p.Receipt != nil {
			t.Fatalf("%v %#v", err, v)
		}
	}
}

func TestTimeoutNotStartedOutputLimitAreHold(t *testing.T) {
	for _, reason := range []string{"TEST_TIMEOUT", "CHECK_NOT_STARTED", "OUTPUT_LIMIT"} {
		t.Run(reason, func(t *testing.T) {
			root, c := fixture(t)
			p, v, err := run(context.Background(), root, c, "fixed", func(_ context.Context, _ string, ch Check) Check {
				ch = passing(c, ch)
				switch reason {
				case "TEST_TIMEOUT":
					ch.TimedOut = true
				case "CHECK_NOT_STARTED":
					ch.Started = false
				case "OUTPUT_LIMIT":
					ch.Truncated = true
				}
				return ch
			})
			if err != nil || v.Reason != reason || len(p.Checks) != 1 || p.Receipt != nil {
				t.Fatalf("%v %#v", err, v)
			}
		})
	}
}

func TestUntrustedEvidenceCannotSupplyItsOwnPin(t *testing.T) {
	root, c := fixture(t)
	p := good(t, root, c)
	b, _ := json.Marshal(p)
	for _, pin := range []string{"", strings.Repeat("0", 64)} {
		v, _ := Verify(context.Background(), root, c, b, pin)
		if v.Reason != "EVIDENCE_MISMATCH" {
			t.Fatal(v)
		}
	}
	originalPin := Hash(b)
	p.Checks[0].Stdout = "forged PASS"
	changed, _ := json.Marshal(p)
	v, _ := Verify(context.Background(), root, c, changed, originalPin)
	if v.Reason != "EVIDENCE_MISMATCH" {
		t.Fatal(v)
	}
}

func TestBindingAndIncompleteProofAreRejected(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*Proof)
	}{
		{"route-action", func(p *Proof) { p.Route.ActionID = "other" }},
		{"packet-inputs", func(p *Proof) { p.Packet.Action.Inputs["head_sha"] = "other" }},
		{"receipt-unknown", func(p *Proof) { p.Receipt.Status = "UNKNOWN" }},
		{"result", func(p *Proof) { p.Result["checks_sha256"] = "other" }},
		{"missing-check", func(p *Proof) { p.Checks = p.Checks[:1] }},
		{"command", func(p *Proof) { p.Checks[0].Args = []string{"test", "-run", "^$"} }},
		{"log-hash", func(p *Proof) { p.Checks[0].StdoutSHA256 = strings.Repeat("a", 64) }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			root, c := fixture(t)
			p := good(t, root, c)
			tc.mutate(&p)
			if Evaluate(p, c, p.After).Disposition != "HOLD" {
				t.Fatal("mutation accepted")
			}
		})
	}
}

func TestContractRepositoryAndProfileAreNotAgentSelectable(t *testing.T) {
	root, c := fixture(t)
	p := good(t, root, c)
	c.IssueURL = "https://github.com/other/repo/issues/14"
	if ValidateContract(c) == nil || Evaluate(p, c, p.After).Disposition != "HOLD" {
		t.Fatal("foreign contract accepted")
	}
	c = p.Contract
	c.Profile = "skip-tests"
	if ValidateContract(c) == nil {
		t.Fatal("unapproved profile")
	}
	c = p.Contract
	c.RequiredTests = nil
	if ValidateContract(c) == nil {
		t.Fatal("missing required tests")
	}
}

func TestSnapshotRejectsSymlink(t *testing.T) {
	root, c := fixture(t)
	if err := os.Symlink("/etc/passwd", filepath.Join(root, "source-link")); err != nil {
		t.Fatal(err)
	}
	if _, err := TakeSnapshot(context.Background(), root, c); err == nil {
		t.Fatal("symlink accepted")
	}
}

func TestDirtyCandidateDoesNotDispatchTests(t *testing.T) {
	root, c := fixture(t)
	if err := os.WriteFile(filepath.Join(root, "untracked.txt"), []byte("x"), 0600); err != nil {
		t.Fatal(err)
	}
	calls := 0
	_, v, err := run(context.Background(), root, c, "fixed", func(_ context.Context, _ string, ch Check) Check { calls++; return passing(c, ch) })
	if err == nil || v.Reason != "DIRTY_WORKTREE" || calls != 0 {
		t.Fatalf("%v %#v %d", err, v, calls)
	}
}

func TestLogBufferIsBounded(t *testing.T) {
	var b logBuffer
	data := strings.Repeat("x", MaxLogBytes+99)
	n, err := b.Write([]byte(data))
	if err != nil || n != len(data) || !b.truncated || len(b.String()) != MaxLogBytes {
		t.Fatal("unbounded output")
	}
}
