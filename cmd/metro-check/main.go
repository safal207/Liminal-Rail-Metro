// metro-check runs an owner-approved fixed Go verification profile, never an
// agent-provided shell command. Run it in a trusted disposable runner.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/safal207/Liminal-Rail-Metro/internal/codingworkflow"
)

func main() { os.Exit(run(os.Args[1:], os.Stdout, os.Stderr)) }

func externalPath(root, path string) (string, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	parent, err := filepath.EvalSymlinks(filepath.Dir(abs))
	if err != nil {
		return "", err
	}
	abs = filepath.Join(parent, filepath.Base(abs))
	if abs == root || strings.HasPrefix(abs, root+string(os.PathSeparator)) {
		return "", fmt.Errorf("proof/contract files must be outside candidate checkout")
	}
	if info, err := os.Lstat(abs); err == nil && info.Mode()&os.ModeSymlink != 0 {
		return "", fmt.Errorf("symlink evidence/contract refused")
	}
	return abs, nil
}

func run(args []string, out, errs io.Writer) int {
	if len(args) == 0 || (args[0] != "check" && args[0] != "verify") {
		fmt.Fprintln(errs, "usage: metro-check check|verify --repo DIR --contract FILE --proof FILE [--allow-tests|--expected-evidence-sha256 SHA256]")
		return 1
	}
	fs := flag.NewFlagSet(args[0], flag.ContinueOnError)
	fs.SetOutput(errs)
	root := fs.String("repo", ".", "candidate Git root")
	contractPath := fs.String("contract", "", "owner-approved contract outside checkout")
	proofPath := fs.String("proof", "", "proof file outside checkout; check refuses overwrite")
	allow := fs.Bool("allow-tests", false, "explicitly permit the fixed test profile in this disposable runner")
	pin := fs.String("expected-evidence-sha256", "", "digest captured by owner/CI after check; never obtain from agent-edited evidence")
	claim := fs.String("agent-claim", "", "optional untrusted agent statement (does not affect verdict)")
	if fs.Parse(args[1:]) != nil {
		return 1
	}
	if fs.NArg() != 0 || *contractPath == "" || *proofPath == "" || len(*claim) > 4096 {
		fmt.Fprintln(errs, "contract/proof required; no positional commands allowed")
		return 1
	}
	abs, err := filepath.Abs(*root)
	if err == nil {
		abs, err = filepath.EvalSymlinks(abs)
	}
	if err != nil {
		fmt.Fprintln(errs, err)
		return 1
	}
	cp, err := externalPath(abs, *contractPath)
	if err != nil {
		fmt.Fprintln(errs, err)
		return 1
	}
	pp, err := externalPath(abs, *proofPath)
	if err != nil {
		fmt.Fprintln(errs, err)
		return 1
	}
	f, err := os.Open(cp)
	if err != nil {
		fmt.Fprintln(errs, err)
		return 1
	}
	var contract codingworkflow.Contract
	err = codingworkflow.Decode(f, &contract, 64<<10)
	_ = f.Close()
	if err != nil {
		fmt.Fprintln(errs, err)
		return 1
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	var verdict codingworkflow.Verdict
	var digest string
	if args[0] == "check" {
		if !*allow {
			fmt.Fprintln(errs, "tests execute code: --allow-tests is required in a disposable trusted runner")
			return 1
		}
		// Reserve before executing anything: a second invocation cannot silently retry.
		file, err := os.OpenFile(pp, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
		if err != nil {
			fmt.Fprintln(errs, err)
			return 1
		}
		defer file.Close()
		proof, v, err := codingworkflow.Run(ctx, abs, contract, *claim)
		if err != nil {
			fmt.Fprintln(errs, err)
			return 1
		}
		data, err := json.MarshalIndent(proof, "", "  ")
		if err != nil {
			fmt.Fprintln(errs, err)
			return 1
		}
		data = append(data, '\n')
		if len(data) > codingworkflow.MaxProofBytes {
			fmt.Fprintln(errs, "proof exceeds limit")
			return 1
		}
		if _, err = file.Write(data); err != nil {
			fmt.Fprintln(errs, err)
			return 1
		}
		if err = file.Sync(); err != nil {
			fmt.Fprintln(errs, err)
			return 1
		}
		verdict, digest = v, codingworkflow.Hash(data)
	} else {
		if *pin == "" {
			fmt.Fprintln(errs, "owner/CI-pinned --expected-evidence-sha256 is required")
			return 1
		}
		file, err := os.Open(pp)
		if err != nil {
			fmt.Fprintln(errs, err)
			return 1
		}
		data, err := io.ReadAll(io.LimitReader(file, codingworkflow.MaxProofBytes+1))
		_ = file.Close()
		if err != nil {
			fmt.Fprintln(errs, err)
			return 1
		}
		verdict, err = codingworkflow.Verify(ctx, abs, contract, data, *pin)
		if err != nil {
			fmt.Fprintln(errs, err)
		}
		digest = codingworkflow.Hash(data)
	}
	fmt.Fprint(out, codingworkflow.Summary(verdict, digest))
	if verdict.Disposition != "VERIFIED" {
		return 2
	}
	return 0
}
