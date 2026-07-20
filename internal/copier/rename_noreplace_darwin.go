//go:build darwin

package copier

import (
	"os"
	"syscall"
	"unsafe"
)

const (
	darwinRenameAtXNP = 488
	darwinAtFDCWD     = ^uintptr(1) // -2
	darwinRenameExcl  = 0x00000004
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
		darwinRenameAtXNP,
		darwinAtFDCWD,
		uintptr(unsafe.Pointer(oldPtr)),
		darwinAtFDCWD,
		uintptr(unsafe.Pointer(newPtr)),
		darwinRenameExcl,
		0,
	)
	if errno != 0 {
		return &os.LinkError{Op: "renameatx_np", Old: oldPath, New: newPath, Err: errno}
	}
	return nil
}
