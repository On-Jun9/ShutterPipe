//go:build !darwin && !linux && !windows

package copier

import "errors"

var errRenameNoReplaceUnsupported = errors.New("atomic no-replace rename is not supported on this platform")

func renameNoReplace(_, _ string) error {
	return errRenameNoReplaceUnsupported
}
