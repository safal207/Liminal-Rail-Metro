//go:build darwin || dragonfly || freebsd || linux || netbsd || openbsd

package policyauthority

import (
	"os"
	"syscall"
)

func lockHeadFile(f *os.File) error {
	return syscall.Flock(int(f.Fd()), syscall.LOCK_EX)
}

func unlockHeadFile(f *os.File) error {
	return syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
}
