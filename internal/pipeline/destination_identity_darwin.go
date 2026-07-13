//go:build darwin

package pipeline

import (
	"bytes"
	"os"
	"syscall"
	"unsafe"
)

func canonicalOpenedPath(path string) (string, bool) {
	info, err := os.Lstat(path)
	if err != nil {
		return "", false
	}
	flags := syscall.O_EVTONLY
	if info.Mode()&os.ModeSymlink != 0 {
		flags |= syscall.O_SYMLINK
	}
	fd, err := syscall.Open(path, flags, 0)
	if err != nil {
		return "", false
	}
	file := os.NewFile(uintptr(fd), path)
	defer file.Close()

	buffer := make([]byte, 4096)
	_, _, errno := syscall.Syscall(
		syscall.SYS_FCNTL,
		file.Fd(),
		uintptr(syscall.F_GETPATH),
		uintptr(unsafe.Pointer(&buffer[0])),
	)
	if errno != 0 {
		return "", false
	}
	if end := bytes.IndexByte(buffer, 0); end >= 0 {
		buffer = buffer[:end]
	}
	if len(buffer) == 0 {
		return "", false
	}
	return string(buffer), true
}
