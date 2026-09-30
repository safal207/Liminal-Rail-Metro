package policyauthority

import (
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/sys/windows"
)

func commitChainHead(tmpName, path string) error {
	from, err := moveFileExPath(tmpName)
	if err != nil {
		return err
	}
	to, err := moveFileExPath(path)
	if err != nil {
		return err
	}
	// The temp file was created in the target directory and synced before this
	// call. os.Open(dir).Sync fails on Windows, so request a write-through
	// replacement of its directory entry instead.
	if err := windows.MoveFileEx(from, to, windows.MOVEFILE_REPLACE_EXISTING|windows.MOVEFILE_WRITE_THROUGH); err != nil {
		return &os.LinkError{Op: "MoveFileEx", Old: tmpName, New: path, Err: err}
	}
	return nil
}

func moveFileExPath(path string) (*uint16, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	// os.Rename extends long paths before calling MoveFileEx. Preserve that
	// behavior when calling the Windows API directly.
	if len(abs) >= 248 && !strings.HasPrefix(abs, `\\?\`) && !strings.HasPrefix(abs, `\??\`) {
		if strings.HasPrefix(abs, `\\.\`) {
			return windows.UTF16PtrFromString(abs)
		}
		if strings.HasPrefix(abs, `\\`) {
			abs = `\\?\UNC\` + abs[2:]
		} else {
			abs = `\\?\` + abs
		}
		return windows.UTF16PtrFromString(abs)
	}
	return windows.UTF16PtrFromString(path)
}
