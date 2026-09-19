//go:build !linux && !darwin

package codingworkflow

import "os/exec"

// Run refuses execution on these systems before this stub can be reached.
func configureProcess(cmd *exec.Cmd) {}
