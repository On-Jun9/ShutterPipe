package pipeline

import (
	"context"
	"errors"
	"fmt"

	"github.com/On-Jun9/ShutterPipe/internal/config"
	"github.com/On-Jun9/ShutterPipe/internal/state"
	"github.com/On-Jun9/ShutterPipe/pkg/types"
)

type RequeueCandidate struct {
	Entry             types.FileEntry
	SourceContext     state.SourceContext
	Verdict           types.VerifyVerdict
	Mode              types.VerifyMode
	SourceHash        string
	ProcessedRecordID string
	ProcessedPresent  bool
}

type RequeueResult struct {
	Applied int
	Stale   int
}

// QueueVerificationProblems atomically records force-rebackup markers after
// confirming that the processed state has not changed since verification.
func QueueVerificationProblems(ctx context.Context, stateFile, runID string, candidates []RequeueCandidate) (RequeueResult, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	manager, err := config.NewUserDataManager()
	if err != nil {
		return RequeueResult{}, err
	}
	lock, err := acquireRunLock(manager.RunLockPath())
	if err != nil {
		if errors.Is(err, errRunLockHeld) {
			return RequeueResult{}, fmt.Errorf("%w: %v", ErrRunAlreadyActive, err)
		}
		return RequeueResult{}, err
	}
	defer lock.Close()

	st, err := state.Load(stateFile)
	if err != nil {
		return RequeueResult{}, fmt.Errorf("failed to load state for requeue: %w", err)
	}

	var result RequeueResult
	for _, candidate := range candidates {
		if err := ctx.Err(); err != nil {
			return RequeueResult{}, err
		}
		recordID, present, trustedDestPath := st.ProcessedSnapshotForRequeue(candidate.Entry, candidate.SourceContext)
		if present != candidate.ProcessedPresent || present && recordID != candidate.ProcessedRecordID {
			result.Stale++
			continue
		}
		if existing, ok := st.RebackupMarker(candidate.Entry.Path); ok && existing.VerificationRunID == runID {
			result.Stale++
			continue
		}
		if candidate.Mode == types.VerifyModeHash && candidate.SourceHash == "" {
			result.Stale++
			continue
		}
		st.SetRebackupMarker(state.RebackupMarker{
			VerificationRunID:      runID,
			SourcePath:             candidate.Entry.Path,
			DestPath:               trustedDestPath,
			Verdict:                candidate.Verdict,
			ConfigFingerprint:      candidate.SourceContext.ConfigFingerprint,
			SourceSize:             candidate.Entry.Size,
			SourceModTimeUnixNano:  candidate.Entry.ModTime.UnixNano(),
			SourceHash:             candidate.SourceHash,
			SidecarPresent:         candidate.SourceContext.SidecarPresent,
			SidecarPath:            candidate.SourceContext.SidecarPath,
			SidecarSize:            candidate.SourceContext.SidecarSize,
			SidecarModTimeUnixNano: candidate.SourceContext.SidecarModTimeUnixNano,
			SidecarHash:            candidate.SourceContext.SidecarHash,
		})
		result.Applied++
	}
	if result.Applied == 0 {
		return result, nil
	}
	if err := st.Save(); err != nil {
		return RequeueResult{}, fmt.Errorf("failed to save requeue state: %w", err)
	}
	return result, nil
}
