//go:build windows

package copier

import (
	"os"
	"syscall"
	"time"
)

func setFileTimes(file *os.File, modTime time.Time) error {
	timestamp := syscall.NsecToFiletime(modTime.UnixNano())
	return syscall.SetFileTime(
		syscall.Handle(file.Fd()),
		nil,
		&timestamp,
		&timestamp,
	)
}
