//go:build !darwin && !linux && !windows

package pipeline

import (
	"path/filepath"
)

func canonicalOpenedPath(path string) (string, bool) {
	parent := filepath.Dir(path)
	resolvedParent, err := filepath.EvalSymlinks(parent)
	if err != nil {
		resolvedParent, err = filepath.Abs(parent)
	}
	if err != nil {
		return "", false
	}
	absPath := filepath.Join(filepath.Clean(resolvedParent), filepath.Base(path))
	return absPath, true
}
