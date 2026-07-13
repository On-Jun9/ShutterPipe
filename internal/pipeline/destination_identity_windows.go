//go:build windows

package pipeline

import (
	"syscall"
	"unsafe"
)

const (
	fileShareRead          = 0x00000001
	fileShareWrite         = 0x00000002
	fileShareDelete        = 0x00000004
	openExisting           = 3
	fileFlagOpenReparse    = 0x00200000
	fileFlagBackupSemantic = 0x02000000
)

var getFinalPathNameByHandle = syscall.NewLazyDLL("kernel32.dll").NewProc("GetFinalPathNameByHandleW")

// canonicalOpenedPath resolves case aliases and short-name aliases through the
// filesystem handle. OPEN_REPARSE_POINT keeps a symlink/junction entry distinct
// from its target; separate hard-link names remain separate opened paths.
func canonicalOpenedPath(path string) (string, bool) {
	pathPtr, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return "", false
	}
	handle, err := syscall.CreateFile(
		pathPtr,
		0,
		fileShareRead|fileShareWrite|fileShareDelete,
		nil,
		openExisting,
		fileFlagOpenReparse|fileFlagBackupSemantic,
		0,
	)
	if err != nil {
		return "", false
	}
	defer syscall.CloseHandle(handle)

	buffer := make([]uint16, syscall.MAX_PATH)
	for {
		length, _, callErr := getFinalPathNameByHandle.Call(
			uintptr(handle),
			uintptr(unsafe.Pointer(&buffer[0])),
			uintptr(len(buffer)),
			0,
		)
		if length == 0 {
			_ = callErr
			return "", false
		}
		if int(length) < len(buffer) {
			return syscall.UTF16ToString(buffer[:length]), true
		}
		buffer = make([]uint16, int(length)+1)
	}
}
