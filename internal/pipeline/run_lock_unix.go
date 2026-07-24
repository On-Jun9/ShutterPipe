//go:build darwin || dragonfly || freebsd || linux || netbsd || openbsd

package pipeline

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
)

type runLock struct {
	file *os.File
}

func acquireRunLock(path string) (*runLock, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return nil, fmt.Errorf("create run lock directory: %w", err)
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, fmt.Errorf("open run lock: %w", err)
	}
	if err := syscall.Flock(int(file.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		_ = file.Close()
		if errors.Is(err, syscall.EWOULDBLOCK) {
			return nil, fmt.Errorf("lock %s: %w", path, errRunLockHeld)
		}
		return nil, fmt.Errorf("lock %s: %w", path, err)
	}
	return &runLock{file: file}, nil
}

func (l *runLock) Close() error {
	unlockErr := syscall.Flock(int(l.file.Fd()), syscall.LOCK_UN)
	closeErr := l.file.Close()
	if unlockErr != nil {
		return unlockErr
	}
	return closeErr
}
