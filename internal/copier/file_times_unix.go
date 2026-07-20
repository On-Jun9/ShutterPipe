//go:build darwin || linux

package copier

import (
	"os"
	"syscall"
	"time"
)

func setFileTimes(file *os.File, modTime time.Time) error {
	timestamp := syscall.NsecToTimeval(modTime.UnixNano())
	return syscall.Futimes(int(file.Fd()), []syscall.Timeval{timestamp, timestamp})
}
