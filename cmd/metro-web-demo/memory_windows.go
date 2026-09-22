package main

import (
	"fmt"
	"os"
	"runtime"
	"strings"
	"syscall"
	"unsafe"
)

var memoryNtCreateFile = syscall.NewLazyDLL("ntdll.dll").NewProc("NtCreateFile")

// Open-or-create relative to the pinned directory, without following a reparse
// point. No write/delete sharing: a second reader process must use another cache.
func openMemoryFile(parent *resourceSource) (*os.File, error) {
	name := parent.name
	if name == "" || name == "." || name == ".." || strings.ContainsAny(name, "\\/:") {
		return nil, fmt.Errorf("invalid memory filename")
	}
	utf, err := syscall.UTF16FromString(name)
	if err != nil || len(utf) > 32767 {
		return nil, fmt.Errorf("invalid memory filename")
	}
	if err := memoryNtCreateFile.Find(); err != nil {
		return nil, err
	}
	objectName := resourceUnicodeString{Length: uint16(2 * (len(utf) - 1)), MaximumLength: uint16(2 * len(utf)), Buffer: &utf[0]}
	attrs := resourceObjectAttributes{RootDirectory: syscall.Handle(parent.parent.Fd()), ObjectName: &objectName, Attributes: 0x40 | 0x1000}
	attrs.Length = uint32(unsafe.Sizeof(attrs))
	var handle syscall.Handle
	var status resourceIOStatus
	code, _, _ := memoryNtCreateFile.Call(
		uintptr(unsafe.Pointer(&handle)), 0x0012019f, uintptr(unsafe.Pointer(&attrs)), uintptr(unsafe.Pointer(&status)),
		0, syscall.FILE_ATTRIBUTE_NORMAL, syscall.FILE_SHARE_READ, 3, 0x20|0x200000|0x40, 0, 0,
	) // GENERIC_READ|WRITE; FILE_OPEN_IF; SYNCHRONOUS_IO_NONALERT|OPEN_REPARSE_POINT|NON_DIRECTORY_FILE
	runtime.KeepAlive(utf)
	runtime.KeepAlive(parent)
	if int32(code) < 0 {
		return nil, fmt.Errorf("memory open rejected (possibly already in use): NTSTATUS 0x%08x", uint32(code))
	}
	f, err := plainWindowsFile(handle, name, false)
	if err != nil {
		return nil, err
	}
	var info syscall.ByHandleFileInformation
	if err := syscall.GetFileInformationByHandle(handle, &info); err != nil || info.NumberOfLinks != 1 {
		_ = f.Close()
		return nil, fmt.Errorf("memory requires a file with one link")
	}
	return f, nil
}
