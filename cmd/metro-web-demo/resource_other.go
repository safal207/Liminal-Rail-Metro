//go:build !linux && !windows

package main

import (
	"fmt"
	"os"
)

// openResourceParent fails closed where a pinned no-follow adapter is unavailable.
func openResourceParent(string) (*resourceSource, error) {
	return nil, fmt.Errorf("file resources are supported on Linux and Windows")
}

// openFile cannot be reached without a supported parent-directory adapter.
func (*resourceSource) openFile() (*os.File, error) {
	return nil, fmt.Errorf("file resources are unsupported on this platform")
}
