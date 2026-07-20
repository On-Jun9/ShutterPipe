//go:build windows

package copier

import (
	"os"
	"syscall"
)

func renameNoReplace(oldPath, newPath string) error {
	oldPtr, err := syscall.UTF16PtrFromString(oldPath)
	if err != nil {
		return err
	}
	newPtr, err := syscall.UTF16PtrFromString(newPath)
	if err != nil {
		return err
	}
	if err := syscall.MoveFile(oldPtr, newPtr); err != nil {
		return &os.LinkError{Op: "MoveFile", Old: oldPath, New: newPath, Err: err}
	}
	return nil
}
