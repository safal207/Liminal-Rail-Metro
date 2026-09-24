//go:build !windows

package policyauthority

import (
	"os"
	"path/filepath"
)

func commitChainHead(tmpName, path string) error {
	if err := os.Rename(tmpName, path); err != nil {
		return err
	}
	d, err := os.Open(filepath.Dir(path))
	if err != nil {
		return err
	}
	defer d.Close()
	return d.Sync()
}
