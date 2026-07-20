//go:build linux && arm64

package copier

import (
	"os"
	"syscall"
	"unsafe"
)

const (
	linuxRenameAt2       = 276
	linuxAtFDCWD         = ^uintptr(99) // -100
	linuxRenameNoReplace = 1
)

func renameNoReplace(oldPath, newPath string) error {
	oldPtr, err := syscall.BytePtrFromString(oldPath)
	if err != nil {
		return err
	}
	newPtr, err := syscall.BytePtrFromString(newPath)
	if err != nil {
		return err
	}
	_, _, errno := syscall.Syscall6(
		linuxRenameAt2,
		linuxAtFDCWD,
		uintptr(unsafe.Pointer(oldPtr)),
		linuxAtFDCWD,
		uintptr(unsafe.Pointer(newPtr)),
		linuxRenameNoReplace,
		0,
	)
	if errno != 0 {
		return &os.LinkError{Op: "renameat2", Old: oldPath, New: newPath, Err: errno}
	}
	return nil
}
