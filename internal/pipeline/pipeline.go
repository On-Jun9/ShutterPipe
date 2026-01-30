package pipeline

import (
	"fmt"
	"path/filepath"
	"strconv"
	"time"

	"github.com/On-Jun9/ShutterPipe/internal/config"
	"github.com/On-Jun9/ShutterPipe/internal/copier"
	"github.com/On-Jun9/ShutterPipe/internal/log"
	"github.com/On-Jun9/ShutterPipe/internal/metadata"
	"github.com/On-Jun9/ShutterPipe/internal/planner"
	"github.com/On-Jun9/ShutterPipe/internal/policy"
	"github.com/On-Jun9/ShutterPipe/internal/scanner"
	"github.com/On-Jun9/ShutterPipe/internal/state"
	"github.com/On-Jun9/ShutterPipe/internal/verify"
	"github.com/On-Jun9/ShutterPipe/pkg/types"
)

type Pipeline struct {
	cfg              *config.Config
	scanner          *scanner.Scanner
	meta             *metadata.Extractor
	planner          *planner.Planner
	dedup            *policy.DedupChecker
	conflict         *policy.ConflictResolver
	copier           *copier.Copier
	verifier         *verify.Verifier
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

	st, err := state.Load(cfg.StateFile)
	if err != nil {
		return nil, err
	}

	quarantinePath := filepath.Join(cfg.Dest, cfg.QuarantineDir)

	userDataManager, err := config.NewUserDataManager()
	if err != nil {
		return nil, fmt.Errorf("failed to create user data manager: %w", err)
	}

	return &Pipeline{
		cfg:             cfg,
		scanner:         scanner.New(cfg.IncludeExtensions),
		meta:            metadata.New(),
		planner:         planner.New(cfg.Dest, cfg.UnclassifiedDir, cfg.OrganizeStrategy, cfg.EventName),
		dedup:           policy.NewDedupChecker(cfg.DedupMethod),
		conflict:        policy.NewConflictResolver(cfg.ConflictPolicy, quarantinePath),
		copier:          copier.New(cfg.Jobs, cfg.DryRun, cfg.HashVerify),
		verifier:        verify.New(cfg.HashVerify),
		state:           st,
		logger:          logger,
		userDataManager: userDataManager,
	}, nil
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
	startTime := time.Now()

	p.logger.Info("Starting scan: '" + p.cfg.Source + "'")

	if p.progressCallback != nil {
		p.progressCallback(ProgressUpdate{
			Type:    "status",
			Message: "파일 스캔 중... (시간이 걸릴 수 있습니다)",
		})
	}

	entries, err := p.scanner.Scan(p.cfg.Source)
	if err != nil {
		// Save failure history for scan errors
		summary := &types.RunSummary{
			StartTime: startTime,
			Duration:  time.Since(startTime),
		}

		historyEntry := types.BackupHistoryEntry{
			ID:        strconv.FormatInt(startTime.Unix(), 10),
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

	p.logger.Info("Found " + strconv.Itoa(len(entries)) + " files")

	if p.progressCallback != nil {
		p.progressCallback(ProgressUpdate{
			Type:    "status",
			Message: "메타데이터 분석 및 계획 수립 중...",
			Total:   len(entries),
		})
	}

	var tasks []types.CopyTask
	var unclassifiedCount int
	var filteredCount int

	for i, entry := range entries {
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

		if !p.cfg.IgnoreState && p.state.IsProcessed(entry.Path, entry.Size) {
			continue
		}

		meta := p.meta.Extract(entry)

		// Date filter check (EXIF preferred, file mod time fallback)
		if !p.shouldIncludeByDate(entry, meta) {
			continue
		}

		filteredCount++
		task := p.planner.Plan(entry, meta)

		if meta.CaptureTime == nil {
			unclassifiedCount++
		}

		// Skip duplicate check if IgnoreState is enabled
		if !p.cfg.IgnoreState {
			isDup, err := p.dedup.IsDuplicate(entry, task.DestPath)
			if err == nil && isDup {
				task.Status = types.TaskStatusSkipped
				task.Action = types.CopyActionSkipped
				continue
			}
		}

		resolution := p.conflict.Resolve(&task)
		if resolution.Skip {
			task.Status = types.TaskStatusSkipped
			task.Action = resolution.Action
			continue
		}

		task.DestPath = resolution.DestPath
		task.Action = resolution.Action
		tasks = append(tasks, task)
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

	summary := &types.RunSummary{
		ScannedFiles: len(entries),
		TotalFiles:   filteredCount,
		Unclassified: unclassifiedCount,
		StartTime:    startTime,
	}

	if len(tasks) == 0 {
		summary.EndTime = time.Now()
		summary.Duration = summary.EndTime.Sub(startTime)
		p.logger.Summary(*summary)

		// Save backup history
		status := types.BackupStatusSuccess
		if summary.Failed > 0 {
			status = types.BackupStatusFailed
		}

		historyEntry := types.BackupHistoryEntry{
			ID:        strconv.FormatInt(summary.StartTime.Unix(), 10),
			Summary:   *summary,
			Config:    p.configToBackupConfig(),
			Status:    status,
			CreatedAt: summary.StartTime,
		}

		if err := p.userDataManager.AddHistoryEntry(historyEntry); err != nil {
			p.logger.Error("Failed to save backup history", err)
			// Don't fail the backup if history save fails
		}

		// Wait a bit to ensure previous progress messages are sent
		time.Sleep(100 * time.Millisecond)

		if p.progressCallback != nil {
			p.progressCallback(ProgressUpdate{
				Type:    "complete",
				Summary: summary,
			})
		}
		return summary, nil
	}

	resultChan := make(chan copier.CopyResult, len(tasks))
	go p.copier.CopyAll(tasks, resultChan)

	var bytesCopied int64
	processed := 0

	for result := range resultChan {
		processed++
		p.logger.Progress(processed, len(tasks), result.Task.Source.Name)

		if p.progressCallback != nil {
			p.progressCallback(ProgressUpdate{
				Type:     "progress",
				Current:  processed,
				Total:    len(tasks),
				Filename: result.Task.Source.Name,
				Action:   result.Task.Action,
			})
		}

		switch result.Task.Action {
		case types.CopyActionCopied:
			summary.Copied++
			bytesCopied += result.Task.Source.Size
		case types.CopyActionSkipped:
			summary.Skipped++
		case types.CopyActionRenamed:
			summary.Renamed++
			bytesCopied += result.Task.Source.Size
		case types.CopyActionOverwritten:
			summary.Overwritten++
			bytesCopied += result.Task.Source.Size
		case types.CopyActionQuarantined:
			summary.Quarantined++
			bytesCopied += result.Task.Source.Size
		}

		if result.Error != nil {
			summary.Failed++
			p.logger.LogTask(result.Task, 0)
		} else {
			if !p.cfg.DryRun {
				p.state.MarkProcessed(result.Task.Source.Path, result.Task.Source.Size, result.Task.DestPath)
			}
			p.logger.LogTask(result.Task, 0)
		}
	}

	summary.EndTime = time.Now()
	summary.Duration = summary.EndTime.Sub(startTime)
	summary.BytesCopied = bytesCopied
	if summary.Duration.Seconds() > 0 {
		summary.BytesPerSecond = float64(bytesCopied) / summary.Duration.Seconds()
	}

	if !p.cfg.DryRun {
		if err := p.state.Save(); err != nil {
			p.logger.Error("Failed to save state", err)
		}
	}

	p.logger.Summary(*summary)

	// Save backup history
	status := types.BackupStatusSuccess
	if summary.Failed > 0 {
		status = types.BackupStatusFailed
	}

	historyEntry := types.BackupHistoryEntry{
		ID:        strconv.FormatInt(summary.StartTime.Unix(), 10),
		Summary:   *summary,
		Config:    p.configToBackupConfig(),
		Status:    status,
		CreatedAt: summary.StartTime,
	}

	if err := p.userDataManager.AddHistoryEntry(historyEntry); err != nil {
		p.logger.Error("Failed to save backup history", err)
		// Don't fail the backup if history save fails
	}

	// Wait a bit to ensure previous progress messages are sent
	time.Sleep(100 * time.Millisecond)

	if p.progressCallback != nil {
		p.progressCallback(ProgressUpdate{
			Type:    "complete",
			Summary: summary,
		})
	}

	return summary, nil
}

func (p *Pipeline) Close() error {
	return p.logger.Close()
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
