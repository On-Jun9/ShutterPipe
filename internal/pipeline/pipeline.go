package pipeline

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/On-Jun9/ShutterPipe/internal/config"
	"github.com/On-Jun9/ShutterPipe/internal/copier"
	"github.com/On-Jun9/ShutterPipe/internal/log"
	"github.com/On-Jun9/ShutterPipe/internal/metadata"
	"github.com/On-Jun9/ShutterPipe/internal/planner"
	"github.com/On-Jun9/ShutterPipe/internal/policy"
	"github.com/On-Jun9/ShutterPipe/internal/scanner"
	"github.com/On-Jun9/ShutterPipe/internal/state"
	"github.com/On-Jun9/ShutterPipe/pkg/types"
)

var (
	ErrRunCanceled      = errors.New("backup run canceled")
	ErrRunFailed        = errors.New("backup run completed with file failures")
	ErrRunAlreadyActive = errors.New("another backup run is already active")
)

type metadataExtractor interface {
	ExtractWithContext(context.Context, types.FileEntry) (types.MediaMetadata, error)
}

type copyExecutor interface {
	CopyAll(context.Context, []types.CopyTask, chan<- copier.CopyResult)
}

type sidecarIdentityResolver func(context.Context, types.FileEntry) (metadata.SidecarIdentityInfo, bool, error)

type Pipeline struct {
	cfg              *config.Config
	scanner          *scanner.Scanner
	meta             metadataExtractor
	sidecarIdentity  sidecarIdentityResolver
	planner          *planner.Planner
	dedup            *policy.DedupChecker
	conflict         *policy.ConflictResolver
	copier           copyExecutor
	state            *state.State
	logger           *log.Logger
	progressCallback ProgressCallback
	userDataManager  *config.UserDataManager
}

func New(cfg *config.Config) (*Pipeline, error) {
	logger, err := log.New(cfg.LogFile, cfg.LogJSON, true)
	if err != nil {
		return nil, err
	}
	keepLogger := false
	defer func() {
		if !keepLogger {
			_ = logger.Close()
		}
	}()

	st, err := state.Load(cfg.StateFile)
	if err != nil {
		return nil, err
	}

	quarantinePath := filepath.Join(cfg.Dest, cfg.QuarantineDir)

	userDataManager, err := config.NewUserDataManager()
	if err != nil {
		return nil, fmt.Errorf("failed to create user data manager: %w", err)
	}

	copyWorkers := effectiveCopyWorkers(cfg.Jobs, cfg.ConflictPolicy)

	p := &Pipeline{
		cfg:             cfg,
		scanner:         scanner.New(cfg.IncludeExtensions),
		meta:            metadata.New(),
		sidecarIdentity: metadata.SidecarIdentity,
		planner:         planner.New(cfg.Dest, cfg.UnclassifiedDir, cfg.OrganizeStrategy, cfg.EventName),
		dedup:           policy.NewDedupChecker(cfg.DedupMethod),
		conflict:        policy.NewConflictResolver(cfg.ConflictPolicy, quarantinePath),
		copier:          copier.New(copyWorkers, cfg.DryRun, cfg.HashVerify),
		state:           st,
		logger:          logger,
		userDataManager: userDataManager,
	}
	keepLogger = true
	return p, nil
}

func effectiveCopyWorkers(configured int, conflictPolicy types.ConflictPolicy) int {
	if conflictPolicy == types.ConflictPolicyOverwrite {
		// Different path spellings can address the same file on case-insensitive
		// or Unicode-normalizing filesystems. Preserve scan-order last-wins
		// semantics by committing overwrite tasks in their planned order.
		return 1
	}
	return configured
}

func (p *Pipeline) SetProgressCallback(cb ProgressCallback) {
	p.progressCallback = cb
}

// shouldIncludeByDate checks if a file should be included based on date filter.
// Uses EXIF capture time if available, otherwise falls back to file modification time.
// Compares dates only (YYYY-MM-DD), ignoring time and timezone.
func (p *Pipeline) shouldIncludeByDate(entry types.FileEntry, meta types.MediaMetadata) bool {
	// No filter configured
	if p.cfg.DateFilterStart == "" && p.cfg.DateFilterEnd == "" {
		return true
	}

	// Determine the date to check: EXIF capture time (preferred) or file mod time (fallback)
	var checkDate time.Time
	if meta.CaptureTime != nil {
		checkDate = *meta.CaptureTime
	} else {
		checkDate = entry.ModTime
	}

	// Format as YYYY-MM-DD for comparison (timezone-agnostic)
	checkDateStr := checkDate.Format("2006-01-02")

	// Check start date (inclusive)
	if p.cfg.DateFilterStart != "" {
		if checkDateStr < p.cfg.DateFilterStart {
			return false
		}
	}

	// Check end date (inclusive)
	if p.cfg.DateFilterEnd != "" {
		if checkDateStr > p.cfg.DateFilterEnd {
			return false
		}
	}

	return true
}

func (p *Pipeline) Run() (*types.RunSummary, error) {
	return p.RunWithContext(context.Background())
}

func (p *Pipeline) RunWithContext(ctx context.Context) (*types.RunSummary, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	runLock, err := acquireRunLock(p.userDataManager.RunLockPath())
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrRunAlreadyActive, err)
	}
	defer runLock.Close()
	// New may have loaded state before another process completed its run. Reload
	// only after owning the process-wide lock so this run cannot overwrite a
	// newer state snapshot with a stale in-memory map.
	freshState, err := state.Load(p.cfg.StateFile)
	if err != nil {
		return nil, fmt.Errorf("failed to reload state after acquiring run lock: %w", err)
	}
	p.state = freshState
	p.conflict.ResetReservations()

	startTime := time.Now()
	summary := &types.RunSummary{
		StartTime: startTime,
	}

	p.logger.Info("Starting scan: '" + p.cfg.Source + "'")

	if p.progressCallback != nil {
		p.progressCallback(ProgressUpdate{
			Type:    "status",
			Message: "파일 스캔 중... (시간이 걸릴 수 있습니다)",
		})
	}

	entries, err := p.scanner.ScanWithContext(ctx, p.cfg.Source)
	if err != nil {
		if cause := contextTermination(ctx, err); cause != nil {
			summary.ScannedFiles = len(entries)
			return p.finishCanceledRun(summary, 0, cause)
		}

		// Save failure history for scan errors
		endTime := time.Now()
		summary.EndTime = endTime
		summary.Duration = endTime.Sub(startTime)
		summary.Failed = 1

		historyEntry := types.BackupHistoryEntry{
			ID:        historyEntryID(startTime),
			Summary:   *summary,
			Config:    p.configToBackupConfig(),
			Status:    types.BackupStatusFailed,
			CreatedAt: startTime,
		}

		if saveErr := p.userDataManager.AddHistoryEntry(historyEntry); saveErr != nil {
			p.logger.Error("Failed to save backup history", saveErr)
		}

		return nil, err
	}

	summary.ScannedFiles = len(entries)

	p.logger.Info("Found " + strconv.Itoa(len(entries)) + " files")

	if p.progressCallback != nil {
		p.progressCallback(ProgressUpdate{
			Type:    "status",
			Message: "메타데이터 분석 및 계획 수립 중...",
			Total:   len(entries),
		})
	}

	var tasks []types.CopyTask
	taskIndexByDest := make(map[string]int)
	overwriteSourcesByDest := make(map[string][]types.FileEntry)
	overwriteDirtyCountByDest := make(map[string]int)
	overwriteSequenceByDest := make(map[string]int)
	var unclassifiedCount int
	var filteredCount int
	fingerprint := stateFingerprint(p.cfg)
	// Capture each source's SourceContext once, at the moment its state is
	// checked, and reuse the same value at commit time. Recomputing it at commit
	// would bind the record to a sidecar snapshot that differs from the one the
	// destination was planned against, letting a mid-run sidecar change be
	// silently frozen into a stale destination on the next run.
	contextBySource := make(map[string]state.SourceContext)

	for i, entry := range entries {
		if ctx.Err() != nil {
			summary.TotalFiles = filteredCount
			summary.Unclassified = unclassifiedCount
			return p.finishCanceledRun(summary, 0, ctx.Err())
		}

		if i%100 == 0 {
			if p.progressCallback != nil {
				p.progressCallback(ProgressUpdate{
					Type:    "analysis_progress",
					Message: "메타데이터 분석 중...",
					Current: i,
					Total:   len(entries),
				})
			}
		}

		sctx, sidecar, hasSidecar, err := p.sourceContext(ctx, fingerprint, entry)
		if cause := contextTermination(ctx, err); cause != nil {
			summary.TotalFiles = filteredCount
			summary.Unclassified = unclassifiedCount
			return p.finishCanceledRun(summary, 0, cause)
		}
		if err != nil {
			summary.TotalFiles = filteredCount + 1
			summary.Unclassified = unclassifiedCount
			summary.Failed++
			return p.finishFailedRun(
				summary,
				0,
				fmt.Errorf("failed to identify metadata sidecar for %s: %w", entry.Path, err),
			)
		}
		stateProcessed := !p.cfg.IgnoreState && p.state.IsEntryProcessed(ctx, entry, p.cfg.HashVerify, sctx)
		if stateProcessed && p.cfg.ConflictPolicy != types.ConflictPolicyOverwrite {
			continue
		}
		contextBySource[entry.Path] = sctx

		meta, err := p.extractMetadata(ctx, entry, sidecar, hasSidecar)
		if cause := contextTermination(ctx, err); cause != nil {
			summary.TotalFiles = filteredCount
			summary.Unclassified = unclassifiedCount
			return p.finishCanceledRun(summary, 0, cause)
		}

		// Date filter check (EXIF preferred, file mod time fallback)
		if !p.shouldIncludeByDate(entry, meta) {
			continue
		}

		task := p.planner.Plan(entry, meta)
		task.ConflictPolicy = p.cfg.ConflictPolicy
		task.QuarantineDir = filepath.Join(p.cfg.Dest, p.cfg.QuarantineDir)
		task.DestinationRoot = p.cfg.Dest
		needsProcessing := !stateProcessed
		if p.cfg.ConflictPolicy == types.ConflictPolicyOverwrite && stateProcessed {
			_, statErr := os.Stat(task.DestPath)
			needsProcessing = statErr != nil
		}
		if needsProcessing {
			filteredCount++
		}

		if meta.CaptureTime == nil && needsProcessing {
			unclassifiedCount++
		}

		// Skip duplicate check if IgnoreState is enabled
		if !p.cfg.IgnoreState && p.cfg.ConflictPolicy != types.ConflictPolicyOverwrite {
			isDup, err := p.dedup.IsDuplicateWithContext(ctx, entry, task.DestPath)
			if cause := contextTermination(ctx, err); cause != nil {
				summary.TotalFiles = filteredCount
				summary.Unclassified = unclassifiedCount
				return p.finishCanceledRun(summary, 0, cause)
			}
			if err != nil {
				// A dedup check that could not read the source or destination must
				// fail the file, not fall through and let a pre-existing destination
				// be recorded as a successful skip.
				task.Status = types.TaskStatusFailed
				task.Action = types.CopyActionFailed
				task.Error = err.Error()
				summary.Failed++
				p.logger.LogTask(task, 0)
				continue
			}
			if isDup {
				task.Status = types.TaskStatusSkipped
				task.Action = types.CopyActionSkipped
				summary.Skipped++
				// The source is already represented at the destination. Upgrade its
				// state record so later runs fast-skip instead of re-extracting
				// metadata and re-deduplicating every time (e.g. legacy records left
				// dirty by a state-schema change). markProcessed re-verifies hashes
				// under hash_verify, so a name/size-only match with differing content
				// is warned and left dirty rather than falsely recorded.
				if !p.cfg.DryRun {
					if err := p.markProcessed(ctx, entry, task.DestPath, nil, false, false, sctx); err != nil {
						if cause := contextTermination(ctx, err); cause != nil {
							summary.TotalFiles = filteredCount
							summary.Unclassified = unclassifiedCount
							return p.finishCanceledRun(summary, 0, cause)
						}
						summary.Warnings = append(summary.Warnings, "중복 파일의 처리 상태를 기록하지 못했습니다: "+err.Error())
					}
				}
				continue
			}
		}

		resolution := p.conflict.Resolve(&task)
		if resolution.Err != nil {
			task.Status = types.TaskStatusFailed
			task.Action = types.CopyActionFailed
			task.Error = resolution.Err.Error()
			summary.Failed++
			p.logger.LogTask(task, 0)
			continue
		}
		if resolution.Skip {
			task.Status = types.TaskStatusSkipped
			task.Action = resolution.Action
			summary.Skipped++
			continue
		}

		task.DestPath = resolution.DestPath
		task.Action = resolution.Action
		if p.cfg.ConflictPolicy == types.ConflictPolicyOverwrite {
			overwriteSourcesByDest[task.DestPath] = append(overwriteSourcesByDest[task.DestPath], entry)
			overwriteSequenceByDest[task.DestPath] = i
			if needsProcessing {
				overwriteDirtyCountByDest[task.DestPath]++
			}
		}
		if resolution.ReplaceReserved {
			if index, ok := taskIndexByDest[task.DestPath]; ok {
				tasks[index] = task
				continue
			}
		}
		taskIndexByDest[task.DestPath] = len(tasks)
		tasks = append(tasks, task)
	}
	if p.cfg.ConflictPolicy == types.ConflictPolicyOverwrite {
		tasks, overwriteSourcesByDest, overwriteDirtyCountByDest = selectDirtyOverwriteGroups(
			ctx, tasks, overwriteSourcesByDest, overwriteDirtyCountByDest, overwriteSequenceByDest,
		)
	}

	summary.TotalFiles = filteredCount
	summary.Unclassified = unclassifiedCount
	if ctx.Err() != nil {
		return p.finishCanceledRun(summary, 0, ctx.Err())
	}

	// Ensure 100% analysis progress is sent
	if p.progressCallback != nil {
		p.progressCallback(ProgressUpdate{
			Type:    "analysis_progress",
			Message: "메타데이터 분석 완료",
			Current: len(entries),
			Total:   len(entries),
		})
	}

	if len(tasks) == 0 {
		p.finalizeSummary(summary, 0)
		// Planning itself can fail files (e.g. an unreadable destination), so a
		// run with no runnable tasks is not automatically a success.
		status := types.BackupStatusSuccess
		var runErr error
		if summary.Failed > 0 {
			status = types.BackupStatusFailed
			runErr = fmt.Errorf("%w: %d file(s) failed", ErrRunFailed, summary.Failed)
		}
		p.persistRunResult(summary, status)

		// Wait a bit to ensure previous progress messages are sent
		time.Sleep(100 * time.Millisecond)

		if p.progressCallback != nil && runErr != nil {
			p.progressCallback(ProgressUpdate{
				Type:    "error",
				Summary: summary,
				Error:   runErr.Error(),
			})
		} else if p.progressCallback != nil {
			p.progressCallback(ProgressUpdate{
				Type:    "complete",
				Summary: summary,
			})
		}
		return summary, runErr
	}

	resultChan := make(chan copier.CopyResult, len(tasks))
	go p.copier.CopyAll(ctx, tasks, resultChan)

	var bytesCopied int64
	processed := 0
	var cancellationCause error
	overwriteWinnerByDest := make(map[string]verifiedCopyIdentity)

	for result := range resultChan {
		cancelled, countFailure := classifyCopyResultError(result.Error)
		if cancelled {
			if cancellationCause == nil {
				cancellationCause = contextTermination(nil, result.Error)
			}
		}
		if cancelled && !countFailure {
			continue
		}

		processed++
		p.logger.Progress(processed, len(tasks), result.Task.Source.Name)

		if p.progressCallback != nil {
			action := result.Task.Action
			errorMessage := ""
			if result.Error != nil {
				action = types.CopyActionFailed
				errorMessage = result.Error.Error()
			}
			p.progressCallback(ProgressUpdate{
				Type:     "progress",
				Current:  processed,
				Total:    len(tasks),
				Filename: result.Task.Source.Name,
				Action:   action,
				Error:    errorMessage,
			})
		}

		if result.Error != nil {
			summary.Failed += overwriteDispositionCount(p.cfg.ConflictPolicy, overwriteDirtyCountByDest, result.Task.DestPath)
			p.logger.LogTask(result.Task, 0)
			continue
		}
		if result.Warning != "" {
			summary.Warnings = append(summary.Warnings, result.Warning)
		}
		if p.cfg.ConflictPolicy == types.ConflictPolicyOverwrite {
			overwriteWinnerByDest[result.Task.DestPath] = verifiedCopyIdentity{
				sourcePath: result.Task.Source.Path,
				hash:       append([]byte(nil), result.VerifiedSourceHash...),
			}
		}

		switch result.Task.Action {
		case types.CopyActionCopied:
			summary.Copied++
			summary.Overwritten += overwriteSupersededCount(p.cfg.ConflictPolicy, overwriteDirtyCountByDest, result.Task.DestPath)
			bytesCopied += result.Task.Source.Size
		case types.CopyActionSkipped:
			summary.Skipped++
		case types.CopyActionRenamed:
			summary.Renamed++
			bytesCopied += result.Task.Source.Size
		case types.CopyActionOverwritten:
			summary.Overwritten++
			summary.Overwritten += overwriteSupersededCount(p.cfg.ConflictPolicy, overwriteDirtyCountByDest, result.Task.DestPath)
			bytesCopied += result.Task.Source.Size
		case types.CopyActionQuarantined:
			summary.Quarantined++
			bytesCopied += result.Task.Source.Size
		}

		if !p.cfg.DryRun && result.Task.Action != types.CopyActionSkipped && p.cfg.ConflictPolicy != types.ConflictPolicyOverwrite {
			if err := p.markProcessed(ctx, result.Task.Source, result.Task.DestPath, result.VerifiedSourceHash, p.cfg.HashVerify, false, contextBySource[result.Task.Source.Path]); err != nil {
				// A cancelled state commit must terminate the run as cancelled, not
				// be downgraded to a warning that lets it finish as complete.
				if cause := contextTermination(ctx, err); cause != nil {
					if cancellationCause == nil {
						cancellationCause = cause
					}
				} else {
					summary.Warnings = append(summary.Warnings, "파일 처리 상태를 기록하지 못했습니다: "+err.Error())
				}
			}
		}
		p.logger.LogTask(result.Task, 0)
	}

	if cancellationCause == nil && ctx.Err() != nil && processed < len(tasks) {
		cancellationCause = ctx.Err()
	}
	if cancellationCause == nil && !p.cfg.DryRun && p.cfg.ConflictPolicy == types.ConflictPolicyOverwrite && summary.Failed == 0 {
		// Overwrite replay establishes one ordered final state. Commit its source
		// state only after every winner succeeds so cancellation/failure leaves a
		// dirty source that forces the next run to converge again.
		// Re-resolve identities after publish as well. Two missing names such as
		// Photo.jpg and photo.jpg cannot be proven aliases during planning on a
		// case-insensitive or Unicode-normalizing filesystem. Once published,
		// both names resolve to the same directory entry and can share the real
		// final winner before state is committed.
		overwriteSourcesByDest, overwriteDirtyCountByDest, overwriteWinnerByDest = mergeCommittedOverwriteStateGroups(
			overwriteSourcesByDest, overwriteDirtyCountByDest, overwriteWinnerByDest, overwriteSequenceByDest,
		)
		for destPath, sources := range overwriteSourcesByDest {
			winner := overwriteWinnerByDest[destPath]
			if p.cfg.HashVerify && (winner.sourcePath == "" || len(winner.hash) == 0) {
				summary.Warnings = append(summary.Warnings, "덮어쓰기 결과의 검증 해시가 없어 처리 상태를 기록하지 않았습니다: "+destPath)
				continue
			}
			for _, source := range sources {
				requireVerifiedHash := p.cfg.HashVerify && source.Path == winner.sourcePath
				superseded := source.Path != winner.sourcePath
				if err := p.markProcessed(ctx, source, destPath, winner.hash, requireVerifiedHash, superseded, contextBySource[source.Path]); err != nil {
					if cause := contextTermination(ctx, err); cause != nil {
						cancellationCause = cause
						break
					}
					summary.Warnings = append(summary.Warnings, "파일 처리 상태를 기록하지 못했습니다: "+err.Error())
				}
			}
			if cancellationCause != nil {
				break
			}
		}
	}
	// Re-check after every state commit so a cancellation during the final file's
	// state hashing (copy loop or overwrite replay) still ends the run cancelled.
	if cancellationCause != nil {
		return p.finishCanceledRun(summary, bytesCopied, cancellationCause)
	}

	p.finalizeSummary(summary, bytesCopied)
	status := types.BackupStatusSuccess
	var runErr error
	if summary.Failed > 0 {
		status = types.BackupStatusFailed
		runErr = fmt.Errorf("%w: %d file(s) failed", ErrRunFailed, summary.Failed)
	}
	p.persistRunResult(summary, status)

	// Wait a bit to ensure previous progress messages are sent
	time.Sleep(100 * time.Millisecond)

	if p.progressCallback != nil && runErr != nil {
		p.progressCallback(ProgressUpdate{
			Type:    "error",
			Summary: summary,
			Error:   runErr.Error(),
		})
	} else if p.progressCallback != nil {
		p.progressCallback(ProgressUpdate{
			Type:    "complete",
			Summary: summary,
		})
	}

	return summary, runErr
}

type verifiedCopyIdentity struct {
	sourcePath string
	hash       []byte
}

func (p *Pipeline) markProcessed(ctx context.Context, source types.FileEntry, destPath string, verifiedHash []byte, requireVerifiedHash, superseded bool, sctx state.SourceContext) error {
	var err error
	if requireVerifiedHash {
		err = p.state.MarkProcessedEntryWithVerifiedHash(ctx, source, destPath, verifiedHash, sctx)
	} else if superseded {
		err = p.state.MarkSupersededEntry(ctx, source, destPath, p.cfg.HashVerify, sctx)
	} else {
		err = p.state.MarkProcessedEntry(ctx, source, destPath, p.cfg.HashVerify, sctx)
	}
	if err != nil {
		p.logger.Error("Failed to record processed file", err)
		return err
	}
	return nil
}

// stateFingerprint captures the configuration inputs that determine where a
// source file is published. When any of them changes, prior state records must
// not shortcut the run, or a re-pointed destination would be silently skipped.
func stateFingerprint(cfg *config.Config) string {
	return strings.Join([]string{
		cfg.Dest,
		string(cfg.OrganizeStrategy),
		cfg.EventName,
		cfg.UnclassifiedDir,
		cfg.QuarantineDir,
		// Conflict policy changes the publish outcome (rename → overwrite, etc.)
		// and dedup method changes whether a file counts as already-present, so a
		// change in either must re-evaluate files the previous policy skipped.
		string(cfg.ConflictPolicy),
		string(cfg.DedupMethod),
	}, "\x00")
}

// sourceContext combines the run-level configuration fingerprint with the
// per-file metadata sidecar identity. A later-appearing or changed sidecar (e.g.
// a video's M01.XML) can move the file to a different destination, so it must be
// part of the reprocessing decision alongside the configuration.
func (p *Pipeline) sourceContext(ctx context.Context, fingerprint string, entry types.FileEntry) (state.SourceContext, metadata.SidecarIdentityInfo, bool, error) {
	sctx := state.SourceContext{ConfigFingerprint: fingerprint}
	resolveIdentity := p.sidecarIdentity
	if resolveIdentity == nil {
		resolveIdentity = metadata.SidecarIdentity
	}
	identity, ok, err := resolveIdentity(ctx, entry)
	if err != nil {
		return state.SourceContext{}, metadata.SidecarIdentityInfo{}, false, err
	}
	if ok {
		sctx.SidecarPresent = true
		sctx.SidecarPath = identity.Path
		sctx.SidecarSize = identity.Size
		sctx.SidecarModTimeUnixNano = identity.ModTimeUnixNano
		sctx.SidecarHash = identity.Hash
	}
	return sctx, identity, ok, nil
}

// extractMetadata classifies videos from the sidecar snapshot captured for the
// state identity, so the recorded hash and the planned destination always come
// from the same sidecar content. Reopening the sidecar here would let a swap
// between hashing and parsing bind one content's identity to another content's
// destination. Non-video entries keep the regular extractor path.
func (p *Pipeline) extractMetadata(ctx context.Context, entry types.FileEntry, sidecar metadata.SidecarIdentityInfo, hasSidecar bool) (types.MediaMetadata, error) {
	if !entry.IsVideo {
		return p.meta.ExtractWithContext(ctx, entry)
	}
	if err := ctx.Err(); err != nil {
		return types.MediaMetadata{}, err
	}
	if !hasSidecar {
		return types.MediaMetadata{Error: "XML metadata file not found"}, nil
	}
	return metadata.ExtractFromSidecar(sidecar), nil
}

func overwriteDispositionCount(policy types.ConflictPolicy, dirtyCountByDest map[string]int, destPath string) int {
	if policy == types.ConflictPolicyOverwrite && dirtyCountByDest[destPath] > 0 {
		return dirtyCountByDest[destPath]
	}
	return 1
}

func overwriteSupersededCount(policy types.ConflictPolicy, dirtyCountByDest map[string]int, destPath string) int {
	count := overwriteDispositionCount(policy, dirtyCountByDest, destPath)
	if count > 1 {
		return count - 1
	}
	return 0
}

type overwriteCandidate struct {
	task     types.CopyTask
	sequence int
	identity string
}

type overwriteGroup struct {
	candidates []overwriteCandidate
}

type committedOverwriteStateGroup struct {
	destPath string
	sequence int
	sources  []types.FileEntry
	dirty    int
	winner   verifiedCopyIdentity
}

func mergeCommittedOverwriteStateGroups(
	sourcesByDest map[string][]types.FileEntry,
	dirtyCountByDest map[string]int,
	winnerByDest map[string]verifiedCopyIdentity,
	sequenceByDest map[string]int,
) (map[string][]types.FileEntry, map[string]int, map[string]verifiedCopyIdentity) {
	resolver := newDestinationIdentityResolver()
	paths := make([]string, 0, len(sourcesByDest))
	for destPath := range sourcesByDest {
		paths = append(paths, destPath)
	}
	sort.Slice(paths, func(i, j int) bool {
		left, right := sequenceByDest[paths[i]], sequenceByDest[paths[j]]
		if left == right {
			return paths[i] < paths[j]
		}
		return left < right
	})

	groups := make(map[string]*committedOverwriteStateGroup, len(paths))
	order := make([]string, 0, len(paths))
	for _, destPath := range paths {
		identity := resolver.Identity(destPath)
		group, exists := groups[identity]
		if !exists {
			group = &committedOverwriteStateGroup{sequence: -1}
			groups[identity] = group
			order = append(order, identity)
		}
		group.sources = append(group.sources, sourcesByDest[destPath]...)
		group.dirty += dirtyCountByDest[destPath]
		sequence := sequenceByDest[destPath]
		if sequence >= group.sequence {
			group.destPath = destPath
			group.sequence = sequence
			group.winner = winnerByDest[destPath]
		}
	}

	mergedSources := make(map[string][]types.FileEntry, len(groups))
	mergedDirty := make(map[string]int, len(groups))
	mergedWinners := make(map[string]verifiedCopyIdentity, len(groups))
	for _, identity := range order {
		group := groups[identity]
		mergedSources[group.destPath] = group.sources
		mergedDirty[group.destPath] = group.dirty
		mergedWinners[group.destPath] = group.winner
	}
	return mergedSources, mergedDirty, mergedWinners
}

func selectDirtyOverwriteGroups(
	ctx context.Context,
	tasks []types.CopyTask,
	sourcesByDest map[string][]types.FileEntry,
	dirtyCountByDest map[string]int,
	sequenceByDest map[string]int,
) ([]types.CopyTask, map[string][]types.FileEntry, map[string]int) {
	return selectDirtyOverwriteGroupsWithResolver(
		ctx, tasks, sourcesByDest, dirtyCountByDest, sequenceByDest, newDestinationIdentityResolver(),
	)
}

func selectDirtyOverwriteGroupsWithResolver(
	ctx context.Context,
	tasks []types.CopyTask,
	sourcesByDest map[string][]types.FileEntry,
	dirtyCountByDest map[string]int,
	sequenceByDest map[string]int,
	resolver *destinationIdentityResolver,
) ([]types.CopyTask, map[string][]types.FileEntry, map[string]int) {
	candidates := make([]overwriteCandidate, 0, len(tasks))
	for _, task := range tasks {
		if ctx.Err() != nil {
			return nil, nil, nil
		}
		candidates = append(candidates, overwriteCandidate{
			task: task, sequence: sequenceByDest[task.DestPath], identity: resolver.Identity(task.DestPath),
		})
	}
	sort.SliceStable(candidates, func(i, j int) bool { return candidates[i].sequence < candidates[j].sequence })

	groups := make([]overwriteGroup, 0, len(candidates))
	groupByIdentity := make(map[string]int, len(candidates))
	groupByRawPath := make(map[string]int, len(candidates))
	for _, candidate := range candidates {
		if ctx.Err() != nil {
			return nil, nil, nil
		}
		groupIndex, found := groupByRawPath[candidate.task.DestPath]
		if !found && candidate.identity != "" {
			groupIndex, found = groupByIdentity[candidate.identity]
		}
		if !found {
			groupIndex = len(groups)
			groups = append(groups, overwriteGroup{candidates: []overwriteCandidate{candidate}})
		} else {
			groups[groupIndex].candidates = append(groups[groupIndex].candidates, candidate)
		}
		groupByRawPath[candidate.task.DestPath] = groupIndex
		if candidate.identity != "" {
			groupByIdentity[candidate.identity] = groupIndex
		}
	}

	selectedTasks := make([]types.CopyTask, 0, len(groups))
	selectedSources := make(map[string][]types.FileEntry, len(groups))
	selectedDirtyCounts := make(map[string]int, len(groups))
	for _, group := range groups {
		if ctx.Err() != nil {
			return nil, nil, nil
		}
		dirtyCount := 0
		var sources []types.FileEntry
		for _, candidate := range group.candidates {
			dirtyCount += dirtyCountByDest[candidate.task.DestPath]
			sources = append(sources, sourcesByDest[candidate.task.DestPath]...)
		}
		if dirtyCount == 0 {
			continue
		}
		winner := group.candidates[len(group.candidates)-1].task
		selectedTasks = append(selectedTasks, winner)
		selectedSources[winner.DestPath] = sources
		selectedDirtyCounts[winner.DestPath] = dirtyCount
	}
	return selectedTasks, selectedSources, selectedDirtyCounts
}

func classifyCopyResultError(err error) (cancelled, countFailure bool) {
	if err == nil {
		return false, false
	}
	cancelled = isContextTermination(err)
	if !cancelled {
		return false, true
	}
	return true, !containsOnlyContextTermination(err)
}

func containsOnlyContextTermination(err error) bool {
	if joined, ok := err.(interface{ Unwrap() []error }); ok {
		children := joined.Unwrap()
		if len(children) == 0 {
			return isContextTermination(err)
		}
		for _, child := range children {
			if !containsOnlyContextTermination(child) {
				return false
			}
		}
		return true
	}
	if wrapped, ok := err.(interface{ Unwrap() error }); ok {
		if child := wrapped.Unwrap(); child != nil {
			return containsOnlyContextTermination(child)
		}
	}
	return isContextTermination(err)
}

func isContextTermination(err error) bool {
	return errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded)
}

func contextTermination(ctx context.Context, err error) error {
	if ctx != nil && ctx.Err() != nil {
		return ctx.Err()
	}
	if errors.Is(err, context.Canceled) {
		return context.Canceled
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return context.DeadlineExceeded
	}
	return nil
}

func (p *Pipeline) finishCanceledRun(summary *types.RunSummary, bytesCopied int64, causes ...error) (*types.RunSummary, error) {
	p.finalizeSummary(summary, bytesCopied)
	p.persistRunResult(summary, types.BackupStatusCanceled)
	for _, cause := range causes {
		if cause != nil {
			return summary, errors.Join(ErrRunCanceled, cause)
		}
	}
	return summary, ErrRunCanceled
}

func (p *Pipeline) finishFailedRun(summary *types.RunSummary, bytesCopied int64, err error) (*types.RunSummary, error) {
	p.finalizeSummary(summary, bytesCopied)
	p.persistRunResult(summary, types.BackupStatusFailed)
	return summary, err
}

func (p *Pipeline) finalizeSummary(summary *types.RunSummary, bytesCopied int64) {
	summary.EndTime = time.Now()
	summary.Duration = summary.EndTime.Sub(summary.StartTime)
	summary.BytesCopied = bytesCopied
	if summary.Duration.Seconds() > 0 {
		summary.BytesPerSecond = float64(bytesCopied) / summary.Duration.Seconds()
	}
}

func (p *Pipeline) persistRunResult(summary *types.RunSummary, status types.BackupStatus) {
	if !p.cfg.DryRun {
		if err := p.state.Save(); err != nil {
			p.logger.Error("Failed to save state", err)
			summary.Warnings = append(summary.Warnings, "처리 상태를 저장하지 못했습니다: "+err.Error())
		}
	}

	p.logger.Summary(*summary)

	historyEntry := types.BackupHistoryEntry{
		ID:        historyEntryID(summary.StartTime),
		Summary:   *summary,
		Config:    p.configToBackupConfig(),
		Status:    status,
		CreatedAt: summary.StartTime,
	}

	if err := p.userDataManager.AddHistoryEntry(historyEntry); err != nil {
		p.logger.Error("Failed to save backup history", err)
		summary.Warnings = append(summary.Warnings, "백업 이력을 저장하지 못했습니다: "+err.Error())
	}
}

func (p *Pipeline) Close() error {
	return p.logger.Close()
}

func historyEntryID(t time.Time) string {
	return strconv.FormatInt(t.UnixNano(), 10)
}

// configToBackupConfig converts Config to BackupConfig for history entry.
func (p *Pipeline) configToBackupConfig() types.BackupConfig {
	return types.BackupConfig{
		Source:            p.cfg.Source,
		Dest:              p.cfg.Dest,
		OrganizeStrategy:  p.cfg.OrganizeStrategy,
		EventName:         p.cfg.EventName,
		ConflictPolicy:    p.cfg.ConflictPolicy,
		DedupMethod:       p.cfg.DedupMethod,
		DryRun:            p.cfg.DryRun,
		HashVerify:        p.cfg.HashVerify,
		IgnoreState:       p.cfg.IgnoreState,
		DateFilterStart:   p.cfg.DateFilterStart,
		DateFilterEnd:     p.cfg.DateFilterEnd,
		Jobs:              p.cfg.Jobs,
		IncludeExtensions: p.cfg.IncludeExtensions,
		UnclassifiedDir:   p.cfg.UnclassifiedDir,
		QuarantineDir:     p.cfg.QuarantineDir,
	}
}
