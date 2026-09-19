#!/usr/bin/env python3
"""Isolated real regression proof, not a mocked Codex/model run.

The baseline implementation is extracted from the pinned source commit by CI.
A tiny stdlib-only harness uses those exact implementation/test bytes. The full
repository is checked separately by metro-check in the same workflow.
"""
import argparse
import hashlib
import json
import os
from pathlib import Path
import shutil
import subprocess
import tempfile


def call(args, cwd=None, expected=0):
    proc = subprocess.run(args, cwd=cwd, text=True, capture_output=True, timeout=180)
    if proc.returncode != expected:
        raise AssertionError(f"command failed ({proc.returncode}, expected {expected}): {args}\n{proc.stdout}\n{proc.stderr}")
    return proc.stdout


def git(root, *args):
    return call(["git", "-c", "core.hooksPath=/dev/null", "-c", "commit.gpgsign=false", "-c", "user.name=Metro Proof", "-c", "user.email=metro@example.invalid", *args], cwd=root).strip()


def digest(path):
    return hashlib.sha256(path.read_bytes()).hexdigest()


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--checker", required=True)
    parser.add_argument("--base-impl", required=True)
    parser.add_argument("--source-root", required=True)
    parser.add_argument("--out", required=True)
    a = parser.parse_args()
    checker = str(Path(a.checker).resolve())
    source = Path(a.source_root).resolve()
    out = Path(a.out).resolve()
    out.mkdir(parents=True, exist_ok=True)
    with tempfile.TemporaryDirectory(prefix="metro-coding-fixture-") as tmp:
        root = Path(tmp) / "candidate"
        impl = root / "internal/metro/metro.go"
        test = root / "internal/metro/route_binding_test.go"
        impl.parent.mkdir(parents=True)
        (root / "go.mod").write_text("module github.com/safal207/Liminal-Rail-Metro\n\ngo 1.23.0\n")
        shutil.copyfile(a.base_impl, impl)
        shutil.copyfile(source / "internal/metro/route_binding_test.go", test)
        git(root, "init", "-q")
        git(root, "remote", "add", "origin", "https://github.com/safal207/Liminal-Rail-Metro.git")
        git(root, "add", ".")
        git(root, "commit", "-qm", "pinned baseline implementation plus owner regression")
        base = git(root, "rev-parse", "HEAD")
        contract = {
            "protocol": "metro.coding.contract.v0.3",
            "repository": "safal207/Liminal-Rail-Metro",
            "issue_url": "https://github.com/safal207/Liminal-Rail-Metro/issues/14",
            "action_id": "issue14-baseline-proof",
            "base_sha": base,
            "profile": "go-test-vet-v1",
            "required_tests": [{"file": "internal/metro/route_binding_test.go", "sha256": digest(test),
                                "package": "github.com/safal207/Liminal-Rail-Metro/internal/metro",
                                "name": "TestVerifyRejectsRouteActionMismatch"}],
        }
        cp = out / "baseline-contract.json"
        cp.write_text(json.dumps(contract, indent=2) + "\n")
        baseline = out / "baseline-proof.json"
        cmd = [checker, "check", "--repo", str(root), "--contract", str(cp), "--proof", str(baseline), "--allow-tests", "--agent-claim", "fixed"]
        failed = json.loads(call(cmd, expected=2))
        assert failed["reason"] == "TEST_FAILED" and failed["disposition"] == "HOLD"
        report = json.loads(baseline.read_text())
        assert len(report["checks"]) == 1 and "receipt" not in report
        assert 'accepted route.action_id=' in report["checks"][0]["stdout"]
        # Explicit second case on new code/commit, not an automatic retry.
        shutil.copyfile(source / "internal/metro/metro.go", impl)
        git(root, "add", ".")
        git(root, "commit", "-qm", "fix route action binding")
        contract["action_id"] = "issue14-patched-proof"
        cp = out / "patched-contract.json"
        cp.write_text(json.dumps(contract, indent=2) + "\n")
        proof = out / "patched-proof.json"
        check = [checker, "check", "--repo", str(root), "--contract", str(cp), "--proof", str(proof), "--allow-tests"]
        passed = json.loads(call(check))
        assert passed["disposition"] == "VERIFIED"
        pin = passed["evidence_sha256"]
        verify = [checker, "verify", "--repo", str(root), "--contract", str(cp), "--proof", str(proof), "--expected-evidence-sha256", pin]
        verified = json.loads(call(verify))
        assert verified["disposition"] == "VERIFIED"
        # An existing proof path cannot be reused to silently launch tests again.
        call(check, expected=1)
        with impl.open("a") as f:
            f.write("\n// mutation after successful verification\n")
        stale = json.loads(call(verify, expected=2))
        assert stale["reason"] == "STALE_PATCH"
        # Restore the exact bytes, then mutate evidence while keeping owner's pin.
        shutil.copyfile(source / "internal/metro/metro.go", impl)
        data = proof.read_bytes()
        proof.write_bytes(data + b" ")
        tampered = json.loads(call(verify, expected=2))
        assert tampered["reason"] == "EVIDENCE_MISMATCH"
        proof.write_bytes(data)
        summary = {"scope": "real Issue #14 implementation in an isolated stdlib Go regression harness",
                   "baseline_impl_sha256": digest(Path(a.base_impl)),
                   "patched_impl_sha256": digest(impl),
                   "required_test_sha256": digest(test),
                   "baseline_false_success": failed,
                   "patched": passed, "verify": verified,
                   "post_test_mutation": stale, "proof_tamper": tampered,
                   "overwrite_retry": "REJECTED", "live_codex_authoring": "NOT_RUN"}
        (out / "matrix.json").write_text(json.dumps(summary, indent=2) + "\n")
        print(json.dumps(summary, indent=2))


if __name__ == "__main__":
    main()
