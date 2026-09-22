package main

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"unsafe"
)

var resourceNtOpenFile = syscall.NewLazyDLL("ntdll.dll").NewProc("NtOpenFile")

// These structures mirror the native Windows ABI used by NtOpenFile. Go 1.23
// has no public directory-relative open, so keep this read-only adapter local.
type resourceUnicodeString struct {
	Length        uint16
	MaximumLength uint16
	Buffer        *uint16
}
type resourceObjectAttributes struct {
	Length                   uint32
	RootDirectory            syscall.Handle
	ObjectName               *resourceUnicodeString
	Attributes               uint32
	SecurityDescriptor       uintptr
	SecurityQualityOfService uintptr
}
type resourceIOStatus struct {
	Status      uintptr
	Information uintptr
}

// plainWindowsFile rejects every reparse point using the opened handle itself.
func plainWindowsFile(h syscall.Handle, name string, directory bool) (*os.File, error) {
	var info syscall.ByHandleFileInformation
	err := syscall.GetFileInformationByHandle(h, &info)
	if err != nil || info.FileAttributes&syscall.FILE_ATTRIBUTE_REPARSE_POINT != 0 || (info.FileAttributes&syscall.FILE_ATTRIBUTE_DIRECTORY != 0) != directory {
		_ = syscall.CloseHandle(h)
		return nil, fmt.Errorf("resource component is not a plain file or directory")
	}
	return os.NewFile(uintptr(h), name), nil
}

// openWindowsResourceAt resolves one leaf against an existing directory handle.
// It never reparses a pathname to that parent, even after the parent is renamed.
func openWindowsResourceAt(parent *os.File, name string, directory bool) (*os.File, error) {
	if name == "" || name == "." || name == ".." || strings.ContainsAny(name, "\\/:") {
		return nil, fmt.Errorf("invalid resource component")
	}
	utf, err := syscall.UTF16FromString(name)
	if err != nil || len(utf) > 32767 {
		return nil, fmt.Errorf("invalid resource component")
	}
	if err := resourceNtOpenFile.Find(); err != nil {
		return nil, err
	}
	objectName := resourceUnicodeString{Length: uint16(2 * (len(utf) - 1)), MaximumLength: uint16(2 * len(utf)), Buffer: &utf[0]}
	attrs := resourceObjectAttributes{RootDirectory: syscall.Handle(parent.Fd()), ObjectName: &objectName, Attributes: 0x40 | 0x1000} // OBJ_CASE_INSENSITIVE | OBJ_DONT_REPARSE
	attrs.Length = uint32(unsafe.Sizeof(attrs))
	access := uint32(0x00120089)              // FILE_GENERIC_READ, including SYNCHRONIZE
	options := uint32(0x20 | 0x200000 | 0x40) // SYNCHRONOUS_IO_NONALERT | OPEN_REPARSE_POINT | NON_DIRECTORY_FILE
	if directory {
		access = 0x00100000 | 0x20 | 0x80 // SYNCHRONIZE | FILE_TRAVERSE | FILE_READ_ATTRIBUTES
		options = 0x20 | 0x200000 | 0x1   // DIRECTORY_FILE
	}
	var handle syscall.Handle
	var status resourceIOStatus
	code, _, _ := resourceNtOpenFile.Call(
		uintptr(unsafe.Pointer(&handle)), uintptr(access), uintptr(unsafe.Pointer(&attrs)), uintptr(unsafe.Pointer(&status)),
		uintptr(syscall.FILE_SHARE_READ|syscall.FILE_SHARE_WRITE|syscall.FILE_SHARE_DELETE), uintptr(options),
	)
	runtime.KeepAlive(utf)
	runtime.KeepAlive(parent)
	if int32(code) < 0 {
		return nil, fmt.Errorf("resource open rejected: NTSTATUS 0x%08x", uint32(code))
	}
	return plainWindowsFile(handle, name, directory)
}

// openResourceParent walks a local drive using no-reparse relative opens and
// retains only the final directory handle. Ordinary leaf replacement stays free.
func openResourceParent(abs string) (*resourceSource, error) {
	volume := filepath.VolumeName(abs)
	if len(volume) != 2 || volume[1] != ':' || strings.Contains(abs[2:], ":") {
		return nil, fmt.Errorf("resource requires a local drive path")
	}
	root := volume + "\\"
	name, err := syscall.UTF16PtrFromString(root)
	if err != nil {
		return nil, err
	}
	h, err := syscall.CreateFile(name, 0x20|0x80, syscall.FILE_SHARE_READ|syscall.FILE_SHARE_WRITE|syscall.FILE_SHARE_DELETE, nil, syscall.OPEN_EXISTING, syscall.FILE_FLAG_BACKUP_SEMANTICS|syscall.FILE_FLAG_OPEN_REPARSE_POINT, 0)
	if err != nil {
		return nil, err
	}
	parent, err := plainWindowsFile(h, root, true)
	if err != nil {
		return nil, err
	}
	for _, part := range strings.Split(strings.TrimPrefix(filepath.Dir(abs), root), "\\") {
		if part == "" {
			continue
		}
		next, err := openWindowsResourceAt(parent, part, true)
		_ = parent.Close()
		if err != nil {
			return nil, err
		}
		parent = next
	}
	return &resourceSource{parent: parent, name: filepath.Base(abs), parents: []*os.File{parent}}, nil
}

// openFile reopens only the configured leaf inside the retained directory.
func (s *resourceSource) openFile() (*os.File, error) {
	return openWindowsResourceAt(s.parent, s.name, false)
}
