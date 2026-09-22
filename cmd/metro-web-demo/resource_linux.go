package main

import (
	"os"
	"path/filepath"
	"strings"
	"syscall"
)

// openResourceParent walks from the root with no-follow directory opens, then
// retains the final descriptor so later pathname replacements cannot redirect it.
func openResourceParent(abs string) (*resourceSource, error) {
	fd, err := syscall.Open("/", syscall.O_RDONLY|syscall.O_DIRECTORY|syscall.O_CLOEXEC, 0)
	if err != nil {
		return nil, err
	}
	parent := os.NewFile(uintptr(fd), "/")
	for _, part := range strings.Split(strings.TrimPrefix(filepath.Dir(abs), "/"), "/") {
		if part == "" {
			continue
		}
		fd, err = syscall.Openat(int(parent.Fd()), part, syscall.O_RDONLY|syscall.O_DIRECTORY|syscall.O_CLOEXEC|syscall.O_NOFOLLOW, 0)
		_ = parent.Close()
		if err != nil {
			return nil, err
		}
		parent = os.NewFile(uintptr(fd), part)
	}
	return &resourceSource{parent: parent, name: filepath.Base(abs), parents: []*os.File{parent}}, nil
}

// openFile anchors each read to the retained directory, refusing symlinks and
// avoiding a blocking open if a publisher replaces the menu with a FIFO.
func (s *resourceSource) openFile() (*os.File, error) {
	fd, err := syscall.Openat(int(s.parent.Fd()), s.name, syscall.O_RDONLY|syscall.O_CLOEXEC|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
	if err != nil {
		return nil, err
	}
	return os.NewFile(uintptr(fd), s.name), nil
}
