//go:build !darwin && !linux && !windows

package copier

import (
	"os"
	"time"
)

// setFileTimes falls back to the path-based os.Chtimes because these platforms
// have no handle-based implementation here. Unconditionally erroring instead
// would stamp every copied file with a "수정 시각을 보존하지 못했습니다" warning.
func setFileTimes(file *os.File, modTime time.Time) error {
	return os.Chtimes(file.Name(), modTime, modTime)
}
