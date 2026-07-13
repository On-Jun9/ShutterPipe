//go:build !darwin && !dragonfly && !freebsd && !linux && !netbsd && !openbsd && !windows

package pipeline

import (
	"fmt"
	"os"
	"path/filepath"
)

// Platforms without a supported advisory-lock primitive use an atomic lock
// directory. The directory is removed on normal shutdown; if a process is
// killed without cleanup, the error names the stale path so an operator can
// inspect and remove it rather than risking concurrent state mutation.
type runLock struct {
	path string
}

func acquireRunLock(path string) (*runLock, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return nil, fmt.Errorf("create run lock directory: %w", err)
	}
	lockDir := path + ".d"
	if err := os.Mkdir(lockDir, 0700); err != nil {
		return nil, fmt.Errorf("lock %s: %w", lockDir, err)
	}
	return &runLock{path: lockDir}, nil
}

func (l *runLock) Close() error {
	return os.Remove(l.path)
}
