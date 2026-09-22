package main

import (
	"fmt"
	"os"
	"syscall"
)

func openMemoryFile(parent *resourceSource) (*os.File, error) {
	fd, err := syscall.Openat(int(parent.parent.Fd()), parent.name, syscall.O_RDWR|syscall.O_CREAT|syscall.O_CLOEXEC|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0600)
	if err != nil {
		return nil, err
	}
	f := os.NewFile(uintptr(fd), parent.name)
	var stat syscall.Stat_t
	if err := syscall.Fstat(fd, &stat); err != nil || stat.Mode&syscall.S_IFMT != syscall.S_IFREG || stat.Nlink != 1 {
		_ = f.Close()
		return nil, fmt.Errorf("memory requires a plain file with one link")
	}
	if err := syscall.Flock(fd, syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		_ = f.Close()
		return nil, fmt.Errorf("route memory is already in use: %w", err)
	}
	return f, nil
}
