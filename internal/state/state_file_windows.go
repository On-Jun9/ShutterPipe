//go:build windows

package state

import (
	"os"
	"syscall"
	"unsafe"
)

const (
	stateMoveFileReplaceExisting = 0x1
	stateMoveFileWriteThrough    = 0x8
)

var stateMoveFileExW = syscall.NewLazyDLL("kernel32.dll").NewProc("MoveFileExW")

func replaceStateFile(oldPath, newPath string) error {
	oldPtr, err := syscall.UTF16PtrFromString(oldPath)
	if err != nil {
		return err
	}
	newPtr, err := syscall.UTF16PtrFromString(newPath)
	if err != nil {
		return err
	}
	result, _, callErr := stateMoveFileExW.Call(
		uintptr(unsafe.Pointer(oldPtr)), uintptr(unsafe.Pointer(newPtr)),
		stateMoveFileReplaceExisting|stateMoveFileWriteThrough,
	)
	if result == 0 {
		return &os.LinkError{Op: "MoveFileEx", Old: oldPath, New: newPath, Err: callErr}
	}
	return nil
}

func syncStateDir(string) error { return nil }
