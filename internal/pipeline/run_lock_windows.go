//go:build windows

package pipeline

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
)

const errorSharingViolation syscall.Errno = 32

type runLock struct {
	file *os.File
}

func acquireRunLock(path string) (*runLock, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return nil, fmt.Errorf("create run lock directory: %w", err)
	}
	pathPtr, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return nil, err
	}
	handle, err := syscall.CreateFile(
		pathPtr,
		syscall.GENERIC_READ|syscall.GENERIC_WRITE,
		0,
		nil,
		syscall.OPEN_ALWAYS,
		syscall.FILE_ATTRIBUTE_NORMAL,
		0,
	)
	if err != nil {
		if errors.Is(err, errorSharingViolation) {
			return nil, fmt.Errorf("lock %s: %w", path, errRunLockHeld)
		}
		return nil, fmt.Errorf("lock %s: %w", path, err)
	}
	return &runLock{file: os.NewFile(uintptr(handle), path)}, nil
}

func (l *runLock) Close() error {
	return l.file.Close()
}
