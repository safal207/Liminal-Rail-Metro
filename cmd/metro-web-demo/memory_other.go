//go:build !linux && !windows

package main

import (
	"fmt"
	"os"
)

func openMemoryFile(*resourceSource) (*os.File, error) {
	return nil, fmt.Errorf("route memory files are supported on Linux and Windows")
}
