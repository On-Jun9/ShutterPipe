//go:build !windows

package state

import "os"

func replaceStateFile(oldPath, newPath string) error {
	return os.Rename(oldPath, newPath)
}

func syncStateDir(path string) error {
	dir, err := os.Open(path)
	if err != nil {
		return err
	}
	defer dir.Close()
	return dir.Sync()
}
