// metro-codex is a single-request JSON CLI for Codex's supported shell boundary.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/safal207/Liminal-Rail-Metro/internal/codexadapter"
)

func execute(args []string, stdin io.Reader, stdout io.Writer) error {
	if len(args) != 1 {
		return errors.New("usage: metro-codex run|verify < input.json")
	}
	var output any
	switch args[0] {
	case "run":
		var action codexadapter.Action
		if err := codexadapter.Decode(stdin, &action, codexadapter.MaxInputBytes); err != nil {
			return err
		}
		response, err := codexadapter.Run(context.Background(), action)
		if err != nil {
			return err
		}
		output = response
	case "verify":
		var response codexadapter.Response
		if err := codexadapter.Decode(stdin, &response, 4*codexadapter.MaxInputBytes); err != nil {
			return err
		}
		if err := codexadapter.Verify(response); err != nil {
			return err
		}
		output = map[string]any{"verified": true, "scope": "local_consistency_only", "evidence_hash": response.EvidenceHash}
	default:
		return errors.New("usage: metro-codex run|verify < input.json")
	}
	encoder := json.NewEncoder(stdout)
	encoder.SetIndent("", "  ")
	return encoder.Encode(output)
}

func main() {
	if err := execute(os.Args[1:], os.Stdin, os.Stdout); err != nil {
		// No success-shaped JSON or partial proof is emitted on failure.
		fmt.Fprintln(os.Stderr, "metro-codex:", err)
		os.Exit(1)
	}
}
