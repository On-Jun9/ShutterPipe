package pipeline

import "errors"

// errRunLockHeld marks the specific failure where the run lock is already held
// by another process. Only this cause should surface as ErrRunAlreadyActive;
// every other acquireRunLock failure (permission denied, directory creation
// failure, lock file open failure) is a genuine I/O fault that must not be
// masked as a concurrent run.
var errRunLockHeld = errors.New("run lock is held by another process")
