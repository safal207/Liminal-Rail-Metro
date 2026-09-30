package policyauthority

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// Lock the stable sidecar, never the head itself: the head inode changes on
// every atomic replacement. Cooperating writers keep this file in place.
func openChainHeadLock(path string) (*os.File, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, fmt.Errorf("create policy authority head directory: %w", err)
	}
	lockPath := path + ".lock"
	f, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, fmt.Errorf("open policy authority head lock: %w", err)
	}
	if err := lockHeadFile(f); err != nil {
		return nil, errors.Join(fmt.Errorf("acquire policy authority head lock: %w", err), f.Close())
	}
	return f, nil
}

func closeChainHeadLock(f *os.File) error {
	unlockErr := unlockHeadFile(f)
	closeErr := f.Close()
	if unlockErr != nil {
		unlockErr = fmt.Errorf("release policy authority head lock: %w", unlockErr)
	}
	if closeErr != nil {
		closeErr = fmt.Errorf("close policy authority head lock: %w", closeErr)
	}
	return errors.Join(unlockErr, closeErr)
}
