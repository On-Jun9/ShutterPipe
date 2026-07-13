//go:build linux && !amd64 && !arm64

package copier

import "syscall"

func renameNoReplace(_, _ string) error {
	return syscall.ENOTSUP
}
