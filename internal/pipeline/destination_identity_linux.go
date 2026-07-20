//go:build linux

package pipeline

import (
	"fmt"
	"os"
	"syscall"
)

const linuxOPath = 0x200000

// canonicalOpenedPath asks the kernel for the dentry path selected by the
// lookup. Unlike the request string, this reflects the actual entry spelling on
// casefold ext4 and case-insensitive SMB/CIFS mounts. O_PATH|O_NOFOLLOW keeps a
// symlink entry distinct from its target and preserves distinct hard-link names.
func canonicalOpenedPath(path string) (string, bool) {
	fd, err := syscall.Open(path, linuxOPath|syscall.O_NOFOLLOW, 0)
	if err != nil {
		return "", false
	}
	defer syscall.Close(fd)

	canonical, err := os.Readlink(fmt.Sprintf("/proc/self/fd/%d", fd))
	if err != nil || canonical == "" {
		return "", false
	}
	return canonical, true
}

func newCanonicalOpenedPathResolver() func(string) (string, bool) {
	return canonicalOpenedPath
}
