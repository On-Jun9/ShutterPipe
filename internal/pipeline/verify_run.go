package pipeline

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/On-Jun9/ShutterPipe/internal/config"
	"github.com/On-Jun9/ShutterPipe/internal/log"
	"github.com/On-Jun9/ShutterPipe/internal/metadata"
	"github.com/On-Jun9/ShutterPipe/internal/scanner"
	"github.com/On-Jun9/ShutterPipe/internal/state"
	fileverify "github.com/On-Jun9/ShutterPipe/internal/verify"
	"github.com/On-Jun9/ShutterPipe/pkg/types"
)

const verifyProblemPreviewLimit = 200

type VerificationResult struct {
	Summary           *types.VerifySummary
	RequeueCandidates []RequeueCandidate
}

type verificationSource struct {
	entry             types.FileEntry
	sourceContext     state.SourceContext
	processedRecordID string
	processedPresent  bool
}

type VerificationPipeline struct {
	cfg              *config.Config
	mode             types.VerifyMode
	manifestPath     string
	manifestName     string
	sourceScanner    *scanner.Scanner
	destScanner      *scanner.Scanner
	meta             metadataExtractor
	sidecarIdentity  sidecarIdentityResolver
	state            *state.State
	logger           *log.Logger
	progressCallback ProgressCallback
	userDataManager  *config.UserDataManager
}

func NewVerification(cfg *config.Config, mode types.VerifyMode, manifestPath, manifestName string) (*VerificationPipeline, error) {
	if mode != types.VerifyModeQuick && mode != types.VerifyModeHash {
		return nil, fmt.Errorf("unsupported verify mode: %s", mode)
	}
	if mode != types.VerifyModeHash && manifestPath != "" {
		return nil, fmt.Errorf("hash manifest requires hash verify mode")
	}

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
	manager, err := config.NewUserDataManager()
	if err != nil {
		return nil, fmt.Errorf("failed to create user data manager: %w", err)
	}

	pipeline := &VerificationPipeline{
		cfg:             cfg,
		mode:            mode,
		manifestPath:    manifestPath,
		manifestName:    manifestName,
		sourceScanner:   scanner.New(cfg.IncludeExtensions),
		destScanner:     scanner.NewAll(),
		meta:            metadata.New(),
		sidecarIdentity: metadata.SidecarIdentity,
		state:           st,
		logger:          logger,
		userDataManager: manager,
	}
	keepLogger = true
	return pipeline, nil
}

func (p *VerificationPipeline) SetProgressCallback(callback ProgressCallback) {
	p.progressCallback = callback
}

func (p *VerificationPipeline) RunWithContext(ctx context.Context) (*VerificationResult, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	lock, err := acquireRunLock(p.userDataManager.RunLockPath())
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrRunAlreadyActive, err)
	}
	defer lock.Close()

	freshState, err := state.Load(p.cfg.StateFile)
	if err != nil {
		return nil, fmt.Errorf("failed to reload state after acquiring run lock: %w", err)
	}
	p.state = freshState

	summary := &types.VerifySummary{
		Mode:      p.mode,
		Source:    p.cfg.Source,
		Dest:      p.cfg.Dest,
		StartTime: time.Now(),
	}
	p.emit(ProgressUpdate{
		Type: "status", Kind: types.RunKindVerify,
		Message: "검증할 원본 파일을 스캔하는 중...",
	})
	p.logger.Info("Starting verification source scan: '" + p.cfg.Source + "'")

	entries, scanIssues, err := p.sourceScanner.ScanCollectingWithContext(ctx, p.cfg.Source)
	if err != nil {
		if cause := contextTermination(ctx, err); cause != nil {
			return p.finish(summary, nil, types.BackupStatusCanceled, errors.Join(ErrRunCanceled, cause))
		}
		return p.finish(summary, nil, types.BackupStatusFailed, fmt.Errorf("failed to scan verification source: %w", err))
	}

	fingerprint := stateFingerprint(p.cfg)
	sources := make([]verificationSource, 0, len(entries))
	for _, issue := range scanIssues {
		problem := types.VerifyProblem{
			Verdict:    types.VerifyVerdictUnverifiable,
			SourcePath: issue.Path,
			Name:       filepath.Base(issue.Path),
			Reason:     "원본 경로를 읽지 못했습니다: " + issue.Err.Error(),
		}
		summary.SourceFiles++
		p.recordProblem(summary, problem)
		p.logger.Info(fmt.Sprintf("Verification %s: %s - %s", problem.Verdict, problem.SourcePath, problem.Reason))
	}

	p.emit(ProgressUpdate{
		Type: "status", Kind: types.RunKindVerify,
		Message: "원본 메타데이터와 날짜 필터를 확인하는 중...",
		Total:   len(entries),
	})
	for index, entry := range entries {
		if err := ctx.Err(); err != nil {
			return p.finish(summary, nil, types.BackupStatusCanceled, errors.Join(ErrRunCanceled, err))
		}
		sctx, sidecar, hasSidecar, sourceErr := sourceContextWithResolver(ctx, fingerprint, entry, p.sidecarIdentity)
		if sourceErr == nil {
			var mediaMetadata types.MediaMetadata
			mediaMetadata, sourceErr = extractMetadataWithExtractor(ctx, entry, sidecar, hasSidecar, p.meta)
			if sourceErr == nil && !includesDateFilter(p.cfg, entry, mediaMetadata) {
				continue
			}
		}
		if sourceErr != nil {
			if cause := contextTermination(ctx, sourceErr); cause != nil {
				return p.finish(summary, nil, types.BackupStatusCanceled, errors.Join(ErrRunCanceled, cause))
			}
			summary.SourceFiles++
			summary.SourceBytes += entry.Size
			problem := types.VerifyProblem{
				Verdict:    types.VerifyVerdictUnverifiable,
				SourcePath: entry.Path,
				Name:       entry.Name,
				Size:       entry.Size,
				Reason:     "원본 메타데이터를 확인하지 못했습니다: " + sourceErr.Error(),
			}
			p.recordProblem(summary, problem)
			p.logger.Info(fmt.Sprintf("Verification %s: %s - %s", problem.Verdict, problem.SourcePath, problem.Reason))
			continue
		}

		recordID, present, _ := p.state.ProcessedSnapshotForRequeue(entry, sctx)
		sources = append(sources, verificationSource{
			entry: entry, sourceContext: sctx,
			processedRecordID: recordID, processedPresent: present,
		})
		summary.SourceFiles++
		summary.SourceBytes += entry.Size
		if index%100 == 0 {
			p.emit(ProgressUpdate{
				Type: "analysis_progress", Kind: types.RunKindVerify,
				Message: "원본 분석 중...", Current: index, Total: len(entries),
			})
		}
	}

	var destinationIndex *fileverify.DestinationIndex
	var manifest *fileverify.Manifest
	if p.manifestPath != "" {
		file, openErr := os.Open(p.manifestPath)
		if openErr != nil {
			return p.finish(summary, nil, types.BackupStatusFailed, fmt.Errorf("failed to open hash manifest: %w", openErr))
		}
		manifest, err = fileverify.ParseManifest(file)
		closeErr := file.Close()
		if err == nil {
			err = closeErr
		}
		if err != nil {
			return p.finish(summary, nil, types.BackupStatusFailed, fmt.Errorf("failed to parse hash manifest: %w", err))
		}
		summary.Manifest = &types.HashManifestSummary{
			Filename: p.manifestName, Entries: len(manifest.Entries), ParseErrors: manifest.ParseErrors,
		}
		summary.IncompleteManifest = manifest.ParseErrors > 0
	} else {
		p.emit(ProgressUpdate{
			Type: "status", Kind: types.RunKindVerify,
			Message: "도착 폴더 전체를 스캔하는 중...",
		})
		destEntries, destIssues, scanErr := p.destScanner.ScanCollectingWithContext(ctx, p.cfg.Dest)
		if scanErr != nil {
			if cause := contextTermination(ctx, scanErr); cause != nil {
				return p.finish(summary, nil, types.BackupStatusCanceled, errors.Join(ErrRunCanceled, cause))
			}
			return p.finish(summary, nil, types.BackupStatusFailed, fmt.Errorf("failed to scan verification destination: %w", scanErr))
		}
		for _, issue := range destIssues {
			p.logger.Info(fmt.Sprintf("Verification destination unreadable: %s - %v", issue.Path, issue.Err))
		}
		if len(destIssues) > 0 {
			summary.Warnings = append(summary.Warnings, "읽지 못한 도착 경로가 있어 일부 파일은 검증 불가로 판정될 수 있습니다.")
		}
		destinationIndex = fileverify.NewDestinationIndex(destEntries, len(destIssues) > 0)
	}

	p.emit(ProgressUpdate{
		Type: "status", Kind: types.RunKindVerify,
		Message: "원본과 도착 파일을 대조하는 중...",
		Total:   len(sources),
	})
	candidates := make([]RequeueCandidate, 0)
	ambiguousQuickMatch := false
	for index, source := range sources {
		if err := ctx.Err(); err != nil {
			return p.finish(summary, nil, types.BackupStatusCanceled, errors.Join(ErrRunCanceled, err))
		}

		var compared fileverify.CompareResult
		switch {
		case manifest != nil:
			compared = manifest.Compare(ctx, source.entry)
		case p.mode == types.VerifyModeHash:
			compared = destinationIndex.CompareHash(ctx, source.entry)
		default:
			compared = destinationIndex.CompareQuick(source.entry)
		}
		if err := ctx.Err(); err != nil {
			return p.finish(summary, nil, types.BackupStatusCanceled, errors.Join(ErrRunCanceled, err))
		}

		ambiguousQuickMatch = ambiguousQuickMatch || p.mode == types.VerifyModeQuick && compared.Ambiguous
		p.logger.Info(fmt.Sprintf("Verification %s: %s - %s", compared.Verdict, source.entry.Path, compared.Reason))
		if compared.Verdict == types.VerifyVerdictOK {
			p.recordVerdict(summary, compared.Verdict)
		} else {
			p.recordProblem(summary, types.VerifyProblem{
				Verdict: compared.Verdict, SourcePath: source.entry.Path,
				Name: source.entry.Name, Size: source.entry.Size, Reason: compared.Reason,
			})
			if p.mode == types.VerifyModeQuick || compared.SourceHash != "" {
				candidates = append(candidates, RequeueCandidate{
					Entry: source.entry, SourceContext: source.sourceContext,
					Verdict: compared.Verdict, Mode: p.mode, SourceHash: compared.SourceHash,
					ProcessedRecordID: source.processedRecordID,
					ProcessedPresent:  source.processedPresent,
				})
			}
		}
		p.emit(ProgressUpdate{
			Type: "progress", Kind: types.RunKindVerify,
			Message: "검증 중", Current: index + 1, Total: len(sources),
			Filename: source.entry.Name, VerifyVerdict: compared.Verdict,
		})
	}
	if ambiguousQuickMatch {
		summary.Warnings = append(summary.Warnings, "빠른 모드는 동명 사본이 여러 개일 때 이름과 크기만으로 오매칭할 수 있습니다. 정확한 판정은 정밀 모드를 사용하세요.")
	}
	if summary.IncompleteManifest {
		candidates = nil
	}
	summary.RequeueEligible = len(candidates)
	summary.RequeueAllowed = !summary.IncompleteManifest && len(candidates) > 0
	return p.finish(summary, candidates, types.BackupStatusSuccess, nil)
}

func (p *VerificationPipeline) recordVerdict(summary *types.VerifySummary, verdict types.VerifyVerdict) {
	switch verdict {
	case types.VerifyVerdictOK:
		summary.Normal++
	case types.VerifyVerdictMissing:
		summary.Missing++
	case types.VerifyVerdictMismatch:
		summary.Mismatch++
	case types.VerifyVerdictUnverifiable:
		summary.Unverifiable++
	}
}

func (p *VerificationPipeline) recordProblem(summary *types.VerifySummary, problem types.VerifyProblem) {
	p.recordVerdict(summary, problem.Verdict)
	summary.ProblemCount++
	if len(summary.Problems) < verifyProblemPreviewLimit {
		summary.Problems = append(summary.Problems, problem)
	} else {
		summary.ProblemsTruncated = true
	}
}

func (p *VerificationPipeline) finish(summary *types.VerifySummary, candidates []RequeueCandidate, status types.BackupStatus, runErr error) (*VerificationResult, error) {
	summary.EndTime = time.Now()
	summary.Duration = summary.EndTime.Sub(summary.StartTime)
	entry := types.BackupHistoryEntry{
		ID: historyEntryID(summary.StartTime), Kind: types.RunKindVerify,
		VerifySummary: summary, Config: backupConfigFromConfig(p.cfg),
		Status: status, CreatedAt: summary.StartTime,
	}
	if err := p.userDataManager.AddHistoryEntry(entry); err != nil {
		p.logger.Error("Failed to save verification history", err)
		summary.Warnings = append(summary.Warnings, "검증 이력을 저장하지 못했습니다: "+err.Error())
	}

	update := ProgressUpdate{Kind: types.RunKindVerify, VerifySummary: summary}
	switch status {
	case types.BackupStatusCanceled:
		update.Type = "cancelled"
		update.Message = "검증이 취소되었습니다."
	case types.BackupStatusFailed:
		update.Type = "error"
		if runErr != nil {
			update.Error = runErr.Error()
		}
	default:
		update.Type = "complete"
	}
	p.emit(update)

	result := &VerificationResult{Summary: summary}
	if status == types.BackupStatusSuccess {
		result.RequeueCandidates = append([]RequeueCandidate(nil), candidates...)
	}
	return result, runErr
}

func (p *VerificationPipeline) emit(update ProgressUpdate) {
	if p.progressCallback != nil {
		p.progressCallback(update)
	}
}

func (p *VerificationPipeline) Close() error {
	return p.logger.Close()
}
