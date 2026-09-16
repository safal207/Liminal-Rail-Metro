package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/safal207/Liminal-Rail-Metro/internal/mirror"
)

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		printUsage(stderr)
		return 2
	}

	switch args[0] {
	case "verify":
		return runVerify(args[1:], stdout, stderr)
	case "fixture":
		return runFixture(args[1:], stdout, stderr)
	case "help", "-h", "--help":
		printUsage(stdout)
		return 0
	default:
		fmt.Fprintf(stderr, "unknown command %q\n", args[0])
		printUsage(stderr)
		return 2
	}
}

func runVerify(args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("verify", flag.ContinueOnError)
	flags.SetOutput(stderr)
	bundlePath := flags.String("bundle", "", "path to mirror.evidence-bundle.v0.1 JSON")
	casRoot := flags.String("cas", "", "filesystem CAS root")
	if err := flags.Parse(args); err != nil {
		return 2
	}
	if flags.NArg() != 0 {
		fmt.Fprintln(stderr, "verify does not accept positional arguments")
		return 2
	}
	if *bundlePath == "" || *casRoot == "" {
		fmt.Fprintln(stderr, "verify requires both -bundle and -cas")
		return 2
	}

	bundleFile, err := os.Open(*bundlePath)
	if err != nil {
		fmt.Fprintf(stderr, "open evidence bundle: %v\n", err)
		return 1
	}
	defer bundleFile.Close()

	var bundle mirror.EvidenceBundle
	decoder := json.NewDecoder(bundleFile)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&bundle); err != nil {
		fmt.Fprintf(stderr, "decode evidence bundle: %v\n", err)
		return 1
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		if err == nil {
			fmt.Fprintln(stderr, "decode evidence bundle: trailing JSON value is not allowed")
		} else {
			fmt.Fprintf(stderr, "decode evidence bundle trailing content: %v\n", err)
		}
		return 1
	}
	if err := bundle.Validate(); err != nil {
		fmt.Fprintf(stderr, "validate evidence bundle: %v\n", err)
		return 1
	}

	resolver, err := mirror.NewFilesystemCAS(*casRoot)
	if err != nil {
		fmt.Fprintf(stderr, "open filesystem CAS: %v\n", err)
		return 1
	}

	replay, err := mirror.VerifyEvidenceBundleFromCAS(bundle, resolver)
	if err != nil {
		fmt.Fprintf(stderr, "verification failed: %v\n", err)
		fmt.Fprintln(stderr, "VERDICT: REJECTED")
		return 1
	}

	fmt.Fprintf(stdout, "✓ bundle root %s\n", bundle.BundleHash)
	fmt.Fprintf(stdout, "✓ %d/%d artifacts resolved and digests matched\n", len(bundle.Artifacts), len(bundle.Artifacts))
	for _, check := range replay.Checks {
		if check.Passed {
			fmt.Fprintf(stdout, "✓ replay %s\n", check.Name)
		}
	}
	fmt.Fprintf(stdout, "✓ replay status %s\n", replay.Status)
	fmt.Fprintln(stdout, "VERDICT: REPRODUCED")
	return 0
}

func printUsage(w io.Writer) {
	fmt.Fprintln(w, "usage:")
	fmt.Fprintln(w, "  liminal-proof verify  -bundle <evidence-bundle.json> -cas <cas-root>")
	fmt.Fprintln(w, "  liminal-proof fixture -out <proof-package-dir>")
}
