//go:build !darwin && !dragonfly && !freebsd && !linux && !netbsd && !openbsd && !windows

package policyauthority

import (
	"errors"
	"os"
)

func lockHeadFile(*os.File) error {
	return errors.New("policy authority head locking is unsupported on this platform")
}

func unlockHeadFile(*os.File) error {
	return errors.New("policy authority head locking is unsupported on this platform")
}
