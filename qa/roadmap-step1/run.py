#!/usr/bin/env python3
"""Independent QA only. No source fix, GitHub write, service call, or deployment.

This file is intended to execute from an immutable trusted commit, never from the
candidate PR checkout. Public source refs are pinned and fetched independently.
A red result means at least one safety assertion failed, not a repaired product.
"""
import hashlib
import json
import os
from pathlib import Path
import shutil
import subprocess
import sys
import tempfile

PR15 = "19a42678cbf6bd1132876b0b67493b279a9fbb7e"
MAIN = "706c05b9c0d5a3ee63078ec2db44e0f76184f157"
BASE = "3600fae8cf1f7bf1ae9ade429e154aab38ff0f2c"
REPO = "https://github.com/safal207/Liminal-Rail-Metro.git"
out = Path(sys.argv[1]).resolve()
out.mkdir(parents=True, exist_ok=True)
work = Path(tempfile.mkdtemp(prefix="liminal-independent-qa-"))
home = work / "home"
home.mkdir()
env = {
    "PATH": os.environ["PATH"], "HOME": str(home), "TMPDIR": str(work),
    "GOCACHE": str(work / "gocache"), "GOMODCACHE": str(work / "gomodcache"),
    "GOENV": "off", "GOWORK": "off", "GOTOOLCHAIN": "local",
    "GOFLAGS": "-mod=readonly", "LC_ALL": "C", "GIT_TERMINAL_PROMPT": "0",
    "GIT_CONFIG_NOSYSTEM": "1", "GIT_CONFIG_GLOBAL": "/dev/null",
}
records = []


def run(name, args, cwd=None, safety=False, timeout=180):
    """Run one bounded command and preserve stdout/stderr digests in audit records."""
    try:
        p = subprocess.run(args, cwd=cwd, env=env, capture_output=True, timeout=timeout)
        code, stdout, stderr = p.returncode, p.stdout, p.stderr
    except subprocess.TimeoutExpired as e:
        code, stdout, stderr = 124, e.stdout or b"", e.stderr or b""
    (out / (name + ".stdout.log")).write_bytes(stdout)
    (out / (name + ".stderr.log")).write_bytes(stderr)
    records.append({"name": name, "command": args, "cwd": str(cwd) if cwd else None,
                    "exit_code": code, "safety_assertion": safety,
                    "stdout_sha256": hashlib.sha256(stdout).hexdigest(),
                    "stderr_sha256": hashlib.sha256(stderr).hexdigest()})
    print(json.dumps(records[-1]), flush=True)
    return code, stdout.decode("utf-8", errors="replace")


def require(name, args, cwd=None, timeout=180):
    """Run a required command, recording evidence before propagating failure."""
    code, text = run(name, args, cwd, timeout=timeout)
    if code:
        raise RuntimeError(name + " failed; inspect saved raw logs")
    return text


def archive(name, cwd, destination, timeout=180):
    """Create a source archive and record command, stderr, bytes, and archive digest."""
    args = ["git", "archive", "--format=tar.gz", "HEAD"]
    try:
        p = subprocess.run(args, cwd=cwd, env=env, capture_output=True, timeout=timeout)
        code, data, stderr = p.returncode, p.stdout, p.stderr
    except subprocess.TimeoutExpired as e:
        code, data, stderr = 124, e.stdout or b"", e.stderr or b""
    destination.write_bytes(data)
    (out / (name + ".stderr.log")).write_bytes(stderr)
    record = {
        "name": name,
        "command": args,
        "cwd": str(cwd),
        "exit_code": code,
        "safety_assertion": False,
        "stdout_sha256": hashlib.sha256(data).hexdigest(),
        "stderr_sha256": hashlib.sha256(stderr).hexdigest(),
        "archive_path": destination.name,
        "archive_bytes": len(data),
        "archive_sha256": hashlib.sha256(data).hexdigest(),
    }
    records.append(record)
    print(json.dumps(record), flush=True)
    if code:
        raise RuntimeError(name + " failed; inspect saved raw logs")
    return record


PUBLIC_TESTS = r'''package codingworkflow

import (
    "context"
    "encoding/json"
    "testing"
)

// The issue/PR/check fixtures come from the tested source, not the live API.
type qaMovingReader struct {
    GitHubEvidenceReader
    reads int
    moved bool
}
func (r *qaMovingReader) GetPullRequest(ctx context.Context, repo string, n int) (GitHubPullRequest, error) {
    r.reads++
    p, err := r.GitHubEvidenceReader.GetPullRequest(ctx, repo, n)
    if r.moved { p.Head.SHA = "eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee" }
    return p, err
}
func (r *qaMovingReader) GetPullRequestFiles(ctx context.Context, repo string, n int) ([]GitHubPRFile, error) {
    files, err := r.GitHubEvidenceReader.GetPullRequestFiles(ctx, repo, n)
    r.moved = true // Deterministic force-push after the initial PR read.
    return files, err
}
func TestIndependentQARejectsHeadMovedDuringVerification(t *testing.T) {
    client, closeServer := fixtureClient(t, defaultFixture()); defer closeServer()
    signer := testSigner(t, 19)
    c, err := Start(context.Background(), client, signer, testProvider(), testStartInput())
    if err != nil { t.Fatal(err) }
    reader := &qaMovingReader{GitHubEvidenceReader: client}
    v, err := Verify(context.Background(), reader, signer, c, 17)
    if err != nil { t.Fatal(err) }
    t.Logf("PR reads=%d moved=%v verdict=%s reason=%s", reader.reads, reader.moved, v.Status, v.ReasonCode)
    if v.Status != StatusHold || v.Receipt != nil {
        t.Fatalf("STALE_HEAD_ACCEPTED: current head moved but verdict=%s receipt=%+v", v.Status, v.Receipt)
    }
}

func TestIndependentQARejectsRenameFromOutsideScope(t *testing.T) {
    fixture := defaultFixture()
    // This is the JSON field GitHub supplies for a rename. It must not disappear
    // before scope checking: removing the old file is also a change.
    raw := `[{"sha":"1234567890123456789012345678901234567890","filename":"internal/codingworkflow/moved.go","previous_filename":"outside/payroll.go","status":"renamed","changes":0}]`
    if err := json.Unmarshal([]byte(raw), &fixture.files); err != nil { t.Fatal(err) }
    client, closeServer := fixtureClient(t, fixture); defer closeServer()
    signer := testSigner(t, 19)
    c, err := Start(context.Background(), client, signer, testProvider(), testStartInput())
    if err != nil { t.Fatal(err) }
    v, err := Verify(context.Background(), client, signer, c, 17)
    if err != nil { t.Fatal(err) }
    t.Logf("rename outside/payroll.go -> internal/codingworkflow/moved.go verdict=%s", v.Status)
    if v.Status != StatusHold || v.Receipt != nil {
        t.Fatalf("OUT_OF_SCOPE_RENAME_ACCEPTED: verdict=%s receipt=%+v", v.Status, v.Receipt)
    }
}

// Positive control: every explicitly non-successful check must remain HOLD.
func TestIndependentQANonSuccessCheckMatrix(t *testing.T) {
    for _, conclusion := range []string{"failure", "cancelled", "skipped", "neutral", "timed_out", "action_required", ""} {
        t.Run(conclusion, func(t *testing.T) {
            fixture := defaultFixture(); fixture.checkRuns[0].Conclusion = conclusion
            client, closeServer := fixtureClient(t, fixture); defer closeServer()
            signer := testSigner(t, 19)
            c, err := Start(context.Background(), client, signer, testProvider(), testStartInput())
            if err != nil { t.Fatal(err) }
            v, err := Verify(context.Background(), client, signer, c, 17)
            if err != nil { t.Fatal(err) }
            if v.Status != StatusHold || v.Receipt != nil { t.Fatalf("non-success accepted: %+v", v) }
        })
    }
}
'''

LOCAL_TESTS = r'''package codingworkflow

import (
    "context"
    "encoding/json"
    "os"
    "os/exec"
    "path/filepath"
    "testing"
)

func TestIndependentQAExternalReplaceDependencyIsBound(t *testing.T) {
    root, c := fixture(t)
    external := t.TempDir()
    write := func(p, s string) { t.Helper(); if err := os.WriteFile(p, []byte(s), 0600); err != nil { t.Fatal(err) } }
    write(filepath.Join(external, "go.mod"), "module example.org/localdep\n\ngo 1.23.0\n")
    write(filepath.Join(external, "value.go"), "package localdep\nfunc Value() int { return 1 }\n")
    write(filepath.Join(root, "go.mod"), "module github.com/safal207/Liminal-Rail-Metro\n\ngo 1.23.0\nrequire example.org/localdep v0.0.0\nreplace example.org/localdep => "+filepath.ToSlash(external)+"\n")
    test := "package sample\nimport (\"testing\"; \"example.org/localdep\")\nfunc TestApproved(t *testing.T) { if localdep.Value()!=1 { t.Fatal(\"external dependency changed\") } }\n"
    write(filepath.Join(root, c.RequiredTests[0].File), test)
    c.RequiredTests[0].SHA256 = Hash([]byte(test)) // Owner-pinned before the run.
    commitTest(t, root)
    p, v, err := Run(context.Background(), root, c, "fixed")
    if err != nil || v.Disposition != "VERIFIED" { t.Fatalf("initial control failed: %+v %v", v, err) }
    data, err := json.Marshal(p); if err != nil { t.Fatal(err) }
    pin := Hash(data)
    write(filepath.Join(external, "value.go"), "package localdep\nfunc Value() int { return 2 }\n")
    after, err := Verify(context.Background(), root, c, data, pin)
    if err != nil { t.Fatal(err) }
    // Separate explicit diagnostic test, not an automatic checker retry.
    cmd := exec.Command("go", "test", "-count=1", "./..."); cmd.Dir = root
    raw, testErr := cmd.CombinedOutput()
    t.Logf("after dependency mutation: read-only verification=%+v; diagnostic test error=%v; output=%s", after, testErr, raw)
    if testErr == nil { t.Fatal("fixture invalid: changed dependency did not make test fail") }
    if after.Disposition != "HOLD" {
        t.Fatalf("UNBOUND_EXTERNAL_INPUT: old proof accepted while actual required test now fails: %+v", after)
    }
}
'''

try:
    repo = work / "repo"
    require("clone", ["git", "clone", "--no-checkout", REPO, str(repo)])
    observed_main = require("observed-main", ["git", "rev-parse", "origin/main"], repo).strip()
    pr15, main = work / "pr15", work / "main"
    require("checkout-pr15", ["git", "worktree", "add", "--detach", str(pr15), PR15], repo)
    require("checkout-main", ["git", "worktree", "add", "--detach", str(main), MAIN], repo)
    trusted_script = Path(__file__).resolve()
    trusted_script_sha256 = hashlib.sha256(trusted_script.read_bytes()).hexdigest()
    (out / "provenance.json").write_text(json.dumps({
        "pr15_head": PR15,
        "main_tested": MAIN,
        "observed_origin_main": observed_main,
        "original_base": BASE,
        "trusted_runner_commit": os.environ.get("TRUSTED_QA_COMMIT", ""),
        "trusted_runner_path": str(trusted_script),
        "trusted_runner_sha256": trusted_script_sha256,
        "workflow_source_head": os.environ.get("QA_WORKFLOW_SOURCE_HEAD", ""),
        "live_codex_authoring": "NOT_RUN",
        "production_mutations": False,
        "network_calls": "public clone and Go module download only"
    }, indent=2)+"\n")
    for label, root in [("pr15", pr15), ("main", main)]:
        archive(label+"-source-archive", root, out / ("source-"+label+".tar.gz"))
        require(label+"-modules", ["go", "mod", "download"], root)
        run(label+"-original-tests", ["go", "test", "-count=1", "./..."], root, safety=True)
        run(label+"-original-vet", ["go", "vet", "./..."], root, safety=True)
        run(label+"-original-race", ["go", "test", "-race", "-count=1", "./internal/codingworkflow"], root, safety=True)
    env["GOPROXY"] = "off"
    checker = work / "metro-check"
    require("checker-build", ["go", "build", "-o", str(checker), "./cmd/metro-check"], pr15)
    _, original = run("baseline-source", ["git", "show", BASE+":internal/metro/metro.go"], repo)
    base_impl = work / "baseline-metro.go"; base_impl.write_text(original)
    run("pr15-isolated-regression", [sys.executable, str(pr15 / "scripts/coding-workflow-proof.py"),
        "--checker", str(checker), "--base-impl", str(base_impl), "--source-root", str(pr15),
        "--out", str(out / "regression")], pr15, safety=True, timeout=300)
    contract = out / "issue14.contract.json"
    shutil.copyfile(pr15 / "examples/coding-issue14.contract.json", contract)
    proof = out / "full-repository-proof.json"
    code, text = run("pr15-full-check", [str(checker), "check", "--repo", str(pr15), "--contract", str(contract),
        "--proof", str(proof), "--allow-tests"], pr15, safety=True, timeout=300)
    if code == 0:
        pin = json.loads(text)["evidence_sha256"]
        run("pr15-full-readonly-verify", [str(checker), "verify", "--repo", str(pr15), "--contract", str(contract),
            "--proof", str(proof), "--expected-evidence-sha256", pin], pr15, safety=True)
    # Dry integration, with no source fix or remote write.
    code, _ = run("merge-pr15-into-main", ["git", "-c", "user.name=QA", "-c", "user.email=qa@example.invalid",
        "merge", "--no-commit", "--no-ff", PR15], main, safety=True)
    run("merge-conflicted-paths", ["git", "diff", "--name-only", "--diff-filter=U"], main)
    main_git_dir = Path(require("main-git-dir", ["git", "rev-parse", "--absolute-git-dir"], main).strip())
    if (main_git_dir / "MERGE_HEAD").exists():
        require("abort-dry-merge", ["git", "merge", "--abort"], main)
    else:
        run("abort-dry-merge", ["git", "merge", "--abort"], main)
    # The exact owner-pinned regression must also hold in current main.
    shutil.copyfile(pr15 / "internal/metro/route_binding_test.go", main / "internal/metro/qa_route_binding_test.go")
    run("main-route-action-binding", ["go", "test", "-count=1", "-run", "^TestVerifyRejectsRouteActionMismatch$", "-v", "./internal/metro"], main, safety=True)
    (main / "internal/codingworkflow/independent_qa_test.go").write_text(PUBLIC_TESTS)
    (pr15 / "internal/codingworkflow/independent_qa_test.go").write_text(LOCAL_TESTS)
    (out / "public-adversarial_test.go").write_text(PUBLIC_TESTS)
    (out / "local-adversarial_test.go").write_text(LOCAL_TESTS)
    run("main-independent-adversarial", ["go", "test", "-count=1", "-v", "-run", "^TestIndependentQA", "./internal/codingworkflow"], main, safety=True)
    run("pr15-independent-external-replace", ["go", "test", "-count=1", "-v", "-run", "^TestIndependentQAExternalReplace", "./internal/codingworkflow"], pr15, safety=True)
except Exception as e:
    (out / "runner-error.txt").write_text(str(e)+"\n")
    print("QA_RUNNER_ERROR:", str(e), flush=True)
finally:
    findings = [r["name"] for r in records if r["safety_assertion"] and r["exit_code"] != 0]
    summary = {"status": "HOLD" if findings or (out / "runner-error.txt").exists() else "QA_PASS",
               "failed_safety_cases": findings, "records": records,
               "live_codex_authoring": "NOT_RUN", "production_mutations": False}
    (out / "summary.json").write_text(json.dumps(summary, indent=2)+"\n")
    print(json.dumps({"status": summary["status"], "failed_safety_cases": findings}), flush=True)
    sys.exit(1 if summary["status"] == "HOLD" else 0)
