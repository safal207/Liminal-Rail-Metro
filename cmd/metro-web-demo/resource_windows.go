package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
)

// openWindowsResource opens the final component without following any reparse
// point. Directory handles omit delete sharing, pinning their names until close.
func openWindowsResource(path string, directory bool) (*os.File, error) {
	name, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return nil, err
	}
	flags := uint32(syscall.FILE_FLAG_OPEN_REPARSE_POINT)
	share := uint32(syscall.FILE_SHARE_READ | syscall.FILE_SHARE_WRITE)
	access := uint32(syscall.GENERIC_READ)
	if directory {
		flags |= syscall.FILE_FLAG_BACKUP_SEMANTICS
		// FILE_TRAVERSE | FILE_READ_ATTRIBUTES; directory listing is unnecessary.
		access = 0x20 | 0x80
	} else {
		share |= syscall.FILE_SHARE_DELETE
	}
	h, err := syscall.CreateFile(name, access, share, nil, syscall.OPEN_EXISTING, flags, 0)
	if err != nil {
		return nil, err
	}
	var info syscall.ByHandleFileInformation
	err = syscall.GetFileInformationByHandle(h, &info)
	if err != nil || info.FileAttributes&syscall.FILE_ATTRIBUTE_REPARSE_POINT != 0 || (info.FileAttributes&syscall.FILE_ATTRIBUTE_DIRECTORY != 0) != directory {
		_ = syscall.CloseHandle(h)
		return nil, fmt.Errorf("resource component is not a plain file or directory")
	}
	return os.NewFile(uintptr(h), path), nil
}

// openResourceParent pins every local-drive ancestor before resolving its child,
// preventing directory/junction swaps while preserving replacement of the leaf.
func openResourceParent(abs string) (*resourceSource, error) {
	volume := filepath.VolumeName(abs)
	if len(volume) != 2 || volume[1] != ':' || strings.ContainsAny(abs[2:], ":") {
		return nil, fmt.Errorf("resource requires a local drive path")
	}
	current := volume + `\`
	s := &resourceSource{name: filepath.Base(abs)}
	parts := []string{""}
	rel := strings.TrimPrefix(filepath.Dir(abs), current)
	if rel != "" {
		parts = append(parts, strings.Split(rel, `\`)...)
	}
	for _, part := range parts {
		if part != "" {
			current = filepath.Join(current, part)
		}
		f, err := openWindowsResource(current, true)
		if err != nil {
			s.close()
			return nil, fmt.Errorf("%s: %w", current, err)
		}
		s.parents = append(s.parents, f)
		s.parent = f
	}
	return s, nil
}

// openFile uses ancestors locked at startup and rejects a replacement reparse
// point in the configured leaf before any file bytes are read.
func (s *resourceSource) openFile() (*os.File, error) {
	return openWindowsResource(filepath.Join(s.parent.Name(), s.name), false)
}
