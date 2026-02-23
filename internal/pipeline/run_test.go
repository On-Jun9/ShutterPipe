package pipeline

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/On-Jun9/ShutterPipe/internal/config"
	"github.com/On-Jun9/ShutterPipe/internal/state"
	"github.com/On-Jun9/ShutterPipe/pkg/types"
)

// newTestConfig는 테스트 코드 동작을 검증하거나 보조합니다.
func newTestConfig(baseDir, sourceDir, destDir string) *config.Config {
	return &config.Config{
		Source:            sourceDir,
		Dest:              destDir,
		IncludeExtensions: []string{"jpg"},
		Jobs:              1,
		DedupMethod:       types.DedupMethodNameSize,
		ConflictPolicy:    types.ConflictPolicySkip,
		OrganizeStrategy:  types.OrganizeByDate,
		UnclassifiedDir:   "unclassified",
		QuarantineDir:     "quarantine",
		StateFile:         filepath.Join(baseDir, "state", "state.json"),
		LogFile:           filepath.Join(baseDir, "logs", "shutterpipe.log"),
	}
}

// TestPipelineNew_FailFastWhenUserDataManagerInitFails는 테스트 코드 동작을 검증하거나 보조합니다.
func TestPipelineNew_FailFastWhenUserDataManagerInitFails(t *testing.T) {
	// ~/.shutterpipe 초기화에 실패하면 Pipeline 생성이 즉시 실패해야 한다.
	tmpDir := t.TempDir()
	homeAsFile := filepath.Join(tmpDir, "home-file")
	if err := os.WriteFile(homeAsFile, []byte("not-a-dir"), 0644); err != nil {
		t.Fatalf("failed to create fake home file: %v", err)
	}
	t.Setenv("HOME", homeAsFile)

	sourceDir := filepath.Join(tmpDir, "src")
	destDir := filepath.Join(tmpDir, "dest")
	if err := os.MkdirAll(sourceDir, 0755); err != nil {
		t.Fatalf("failed to create source dir: %v", err)
	}
	if err := os.MkdirAll(destDir, 0755); err != nil {
		t.Fatalf("failed to create dest dir: %v", err)
	}

	cfg := newTestConfig(tmpDir, sourceDir, destDir)
	_, err := New(cfg)
	if err == nil {
		t.Fatal("expected fail-fast error from user data manager init")
	}
	if !strings.Contains(err.Error(), "failed to create user data manager") {
		t.Fatalf("unexpected error: %v", err)
	}
}

// TestPipelineNew_ReturnsErrorWhenLoggerInitFails는 테스트 코드 동작을 검증하거나 보조합니다.
func TestPipelineNew_ReturnsErrorWhenLoggerInitFails(t *testing.T) {
	// 로그 디렉터리 생성이 불가능하면 Pipeline 생성이 실패해야 한다.
	tmpDir := t.TempDir()
	t.Setenv("HOME", filepath.Join(tmpDir, "home"))

	sourceDir := filepath.Join(tmpDir, "src")
	destDir := filepath.Join(tmpDir, "dest")
	if err := os.MkdirAll(sourceDir, 0755); err != nil {
		t.Fatalf("failed to create source dir: %v", err)
	}
	if err := os.MkdirAll(destDir, 0755); err != nil {
		t.Fatalf("failed to create dest dir: %v", err)
	}

	parentAsFile := filepath.Join(tmpDir, "not-dir")
	if err := os.WriteFile(parentAsFile, []byte("x"), 0644); err != nil {
		t.Fatalf("failed to create blocking file: %v", err)
	}

	cfg := newTestConfig(tmpDir, sourceDir, destDir)
	cfg.LogFile = filepath.Join(parentAsFile, "app.log")

	_, err := New(cfg)
	if err == nil {
		t.Fatal("expected logger init error")
	}
}

// TestPipelineNew_ReturnsErrorWhenStateLoadFails는 테스트 코드 동작을 검증하거나 보조합니다.
func TestPipelineNew_ReturnsErrorWhenStateLoadFails(t *testing.T) {
	// state 파일이 깨져 있으면 Pipeline 생성이 실패해야 한다.
	tmpDir := t.TempDir()
	t.Setenv("HOME", filepath.Join(tmpDir, "home"))

	sourceDir := filepath.Join(tmpDir, "src")
	destDir := filepath.Join(tmpDir, "dest")
	if err := os.MkdirAll(sourceDir, 0755); err != nil {
		t.Fatalf("failed to create source dir: %v", err)
	}
	if err := os.MkdirAll(destDir, 0755); err != nil {
		t.Fatalf("failed to create dest dir: %v", err)
	}

	cfg := newTestConfig(tmpDir, sourceDir, destDir)
	if err := os.MkdirAll(filepath.Dir(cfg.StateFile), 0755); err != nil {
		t.Fatalf("failed to create state dir: %v", err)
	}
	if err := os.WriteFile(cfg.StateFile, []byte("{"), 0644); err != nil {
		t.Fatalf("failed to write broken state: %v", err)
	}

	_, err := New(cfg)
	if err == nil {
		t.Fatal("expected state load error")
	}
}

// TestPipelineRun_CopiesFileAndPersistsStateAndHistory는 테스트 코드 동작을 검증하거나 보조합니다.
func TestPipelineRun_CopiesFileAndPersistsStateAndHistory(t *testing.T) {
	// 정상 실행 시 파일 복사/상태 저장/백업 이력이 모두 반영되어야 한다.
	tmpDir := t.TempDir()
	t.Setenv("HOME", filepath.Join(tmpDir, "home"))

	sourceDir := filepath.Join(tmpDir, "src")
	destDir := filepath.Join(tmpDir, "dest")
	if err := os.MkdirAll(sourceDir, 0755); err != nil {
		t.Fatalf("failed to create source dir: %v", err)
	}
	if err := os.MkdirAll(destDir, 0755); err != nil {
		t.Fatalf("failed to create dest dir: %v", err)
	}

	srcPath := filepath.Join(sourceDir, "photo.jpg")
	if err := os.WriteFile(srcPath, []byte("photo-bytes"), 0644); err != nil {
		t.Fatalf("failed to write source file: %v", err)
	}

	cfg := newTestConfig(tmpDir, sourceDir, destDir)
	p, err := New(cfg)
	if err != nil {
		t.Fatalf("failed to create pipeline: %v", err)
	}
	defer p.Close()

	sawComplete := false
	p.SetProgressCallback(func(update ProgressUpdate) {
		if update.Type == "complete" {
			sawComplete = true
		}
	})

	summary, err := p.Run()
	if err != nil {
		t.Fatalf("pipeline run failed: %v", err)
	}
	if summary == nil {
		t.Fatal("expected non-nil summary")
	}
	if summary.ScannedFiles != 1 || summary.TotalFiles != 1 {
		t.Fatalf("unexpected scan/total summary: %+v", *summary)
	}
	if summary.Copied != 1 || summary.Failed != 0 {
		t.Fatalf("unexpected copied/failed summary: %+v", *summary)
	}
	if summary.Unclassified != 1 {
		t.Fatalf("expected 1 unclassified file, got %d", summary.Unclassified)
	}
	if !sawComplete {
		t.Fatal("expected complete progress callback")
	}

	destPath := filepath.Join(destDir, "unclassified", "photo.jpg")
	data, err := os.ReadFile(destPath)
	if err != nil {
		t.Fatalf("failed to read copied file: %v", err)
	}
	if string(data) != "photo-bytes" {
		t.Fatalf("unexpected copied content: %q", string(data))
	}

	st, err := state.Load(cfg.StateFile)
	if err != nil {
		t.Fatalf("failed to load state: %v", err)
	}
	if !st.IsProcessed(srcPath, int64(len("photo-bytes"))) {
		t.Fatal("expected source file to be marked as processed")
	}

	m, err := config.NewUserDataManager()
	if err != nil {
		t.Fatalf("failed to create user data manager: %v", err)
	}
	history, err := m.LoadBackupHistory()
	if err != nil {
		t.Fatalf("failed to load backup history: %v", err)
	}
	if len(history.Entries) == 0 {
		t.Fatal("expected backup history entry")
	}
	if history.Entries[0].Status != types.BackupStatusSuccess {
		t.Fatalf("expected success history status, got %s", history.Entries[0].Status)
	}
}

// TestPipelineRun_DryRunSkipsFileAndStateWrite는 테스트 코드 동작을 검증하거나 보조합니다.
func TestPipelineRun_DryRunSkipsFileAndStateWrite(t *testing.T) {
	// dry-run 실행 시 대상 파일/상태 파일은 생성되지 않아야 한다.
	tmpDir := t.TempDir()
	t.Setenv("HOME", filepath.Join(tmpDir, "home"))

	sourceDir := filepath.Join(tmpDir, "src")
	destDir := filepath.Join(tmpDir, "dest")
	if err := os.MkdirAll(sourceDir, 0755); err != nil {
		t.Fatalf("failed to create source dir: %v", err)
	}
	if err := os.MkdirAll(destDir, 0755); err != nil {
		t.Fatalf("failed to create dest dir: %v", err)
	}

	srcPath := filepath.Join(sourceDir, "photo.jpg")
	if err := os.WriteFile(srcPath, []byte("photo-bytes"), 0644); err != nil {
		t.Fatalf("failed to write source file: %v", err)
	}

	cfg := newTestConfig(tmpDir, sourceDir, destDir)
	cfg.DryRun = true

	p, err := New(cfg)
	if err != nil {
		t.Fatalf("failed to create pipeline: %v", err)
	}
	defer p.Close()

	summary, err := p.Run()
	if err != nil {
		t.Fatalf("pipeline dry-run failed: %v", err)
	}
	if summary.Copied != 1 {
		t.Fatalf("expected copied=1 in dry-run summary, got %d", summary.Copied)
	}

	destPath := filepath.Join(destDir, "unclassified", "photo.jpg")
	if _, err := os.Stat(destPath); !os.IsNotExist(err) {
		t.Fatalf("expected no destination file in dry-run, stat err=%v", err)
	}
	if _, err := os.Stat(cfg.StateFile); !os.IsNotExist(err) {
		t.Fatalf("expected no state file in dry-run, stat err=%v", err)
	}
}

// TestPipelineRun_ScanFailureRecordsFailedHistory는 테스트 코드 동작을 검증하거나 보조합니다.
func TestPipelineRun_ScanFailureRecordsFailedHistory(t *testing.T) {
	// 스캔 실패 시 실패 이력이 남고 Run은 에러를 반환해야 한다.
	tmpDir := t.TempDir()
	t.Setenv("HOME", filepath.Join(tmpDir, "home"))

	missingSource := filepath.Join(tmpDir, "missing-source")
	destDir := filepath.Join(tmpDir, "dest")
	if err := os.MkdirAll(destDir, 0755); err != nil {
		t.Fatalf("failed to create dest dir: %v", err)
	}

	cfg := newTestConfig(tmpDir, missingSource, destDir)
	p, err := New(cfg)
	if err != nil {
		t.Fatalf("failed to create pipeline: %v", err)
	}
	defer p.Close()

	summary, err := p.Run()
	if err == nil {
		t.Fatal("expected scan error")
	}
	if summary != nil {
		t.Fatal("expected nil summary on scan failure")
	}

	m, err := config.NewUserDataManager()
	if err != nil {
		t.Fatalf("failed to create user data manager: %v", err)
	}
	history, err := m.LoadBackupHistory()
	if err != nil {
		t.Fatalf("failed to load backup history: %v", err)
	}
	if len(history.Entries) == 0 {
		t.Fatal("expected failed history entry")
	}
	if history.Entries[0].Status != types.BackupStatusFailed {
		t.Fatalf("expected failed history status, got %s", history.Entries[0].Status)
	}
	if history.Entries[0].Summary.Failed != 1 {
		t.Fatalf("expected failed count 1, got %d", history.Entries[0].Summary.Failed)
	}
}

// TestPipelineRun_NoTasksPathWhenFileAlreadyProcessed는 테스트 코드 동작을 검증하거나 보조합니다.
func TestPipelineRun_NoTasksPathWhenFileAlreadyProcessed(t *testing.T) {
	// state에 이미 처리된 파일은 건너뛰고 len(tasks)==0 경로로 종료되어야 한다.
	tmpDir := t.TempDir()
	t.Setenv("HOME", filepath.Join(tmpDir, "home"))

	sourceDir := filepath.Join(tmpDir, "src")
	destDir := filepath.Join(tmpDir, "dest")
	if err := os.MkdirAll(sourceDir, 0755); err != nil {
		t.Fatalf("failed to create source dir: %v", err)
	}
	if err := os.MkdirAll(destDir, 0755); err != nil {
		t.Fatalf("failed to create dest dir: %v", err)
	}

	srcPath := filepath.Join(sourceDir, "photo.jpg")
	content := []byte("photo-bytes")
	if err := os.WriteFile(srcPath, content, 0644); err != nil {
		t.Fatalf("failed to write source file: %v", err)
	}

	cfg := newTestConfig(tmpDir, sourceDir, destDir)
	st := state.New(cfg.StateFile)
	st.MarkProcessed(srcPath, int64(len(content)), filepath.Join(destDir, "unclassified", "photo.jpg"))
	if err := st.Save(); err != nil {
		t.Fatalf("failed to save preloaded state: %v", err)
	}

	p, err := New(cfg)
	if err != nil {
		t.Fatalf("failed to create pipeline: %v", err)
	}
	defer p.Close()

	summary, err := p.Run()
	if err != nil {
		t.Fatalf("pipeline run failed: %v", err)
	}
	if summary.ScannedFiles != 1 {
		t.Fatalf("expected scanned files 1, got %d", summary.ScannedFiles)
	}
	if summary.TotalFiles != 0 || summary.Copied != 0 {
		t.Fatalf("expected no runnable tasks, got summary %+v", *summary)
	}
}

// TestPipelineRun_ScanFailureIgnoresHistorySaveFailure는 테스트 코드 동작을 검증하거나 보조합니다.
func TestPipelineRun_ScanFailureIgnoresHistorySaveFailure(t *testing.T) {
	// 스캔 실패 경로에서 히스토리 저장이 실패해도 원래 스캔 에러를 반환해야 한다.
	tmpDir := t.TempDir()
	homeDir := filepath.Join(tmpDir, "home")
	t.Setenv("HOME", homeDir)

	destDir := filepath.Join(tmpDir, "dest")
	if err := os.MkdirAll(destDir, 0755); err != nil {
		t.Fatalf("failed to create dest dir: %v", err)
	}

	cfg := newTestConfig(tmpDir, filepath.Join(tmpDir, "missing"), destDir)
	p, err := New(cfg)
	if err != nil {
		t.Fatalf("failed to create pipeline: %v", err)
	}
	defer p.Close()

	blockPath := filepath.Join(homeDir, ".shutterpipe", "backup-history.json")
	if err := os.MkdirAll(blockPath, 0755); err != nil {
		t.Fatalf("failed to create history blocking dir: %v", err)
	}

	summary, err := p.Run()
	if err == nil {
		t.Fatal("expected scan error")
	}
	if summary != nil {
		t.Fatal("expected nil summary on scan error")
	}
}

// TestPipelineRun_DateFilterExcludesFile는 테스트 코드 동작을 검증하거나 보조합니다.
func TestPipelineRun_DateFilterExcludesFile(t *testing.T) {
	// 날짜 필터에 의해 제외되면 filtered/task가 0으로 종료되어야 한다.
	tmpDir := t.TempDir()
	t.Setenv("HOME", filepath.Join(tmpDir, "home"))

	sourceDir := filepath.Join(tmpDir, "src")
	destDir := filepath.Join(tmpDir, "dest")
	if err := os.MkdirAll(sourceDir, 0755); err != nil {
		t.Fatalf("failed to create source dir: %v", err)
	}
	if err := os.MkdirAll(destDir, 0755); err != nil {
		t.Fatalf("failed to create dest dir: %v", err)
	}

	srcPath := filepath.Join(sourceDir, "photo.jpg")
	if err := os.WriteFile(srcPath, []byte("photo"), 0644); err != nil {
		t.Fatalf("failed to write source file: %v", err)
	}
	old := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
	if err := os.Chtimes(srcPath, old, old); err != nil {
		t.Fatalf("failed to set file time: %v", err)
	}

	cfg := newTestConfig(tmpDir, sourceDir, destDir)
	cfg.DateFilterStart = "2025-01-01"
	cfg.DateFilterEnd = "2025-12-31"

	p, err := New(cfg)
	if err != nil {
		t.Fatalf("failed to create pipeline: %v", err)
	}
	defer p.Close()

	summary, err := p.Run()
	if err != nil {
		t.Fatalf("pipeline run failed: %v", err)
	}
	if summary.ScannedFiles != 1 || summary.TotalFiles != 0 {
		t.Fatalf("unexpected summary for filtered file: %+v", *summary)
	}
}

// TestPipelineRun_DedupDuplicateSkipsTask는 테스트 코드 동작을 검증하거나 보조합니다.
func TestPipelineRun_DedupDuplicateSkipsTask(t *testing.T) {
	// dedup 중복으로 판정되면 conflict/copy 단계로 가지 않고 건너뛰어야 한다.
	tmpDir := t.TempDir()
	t.Setenv("HOME", filepath.Join(tmpDir, "home"))

	sourceDir := filepath.Join(tmpDir, "src")
	destDir := filepath.Join(tmpDir, "dest")
	if err := os.MkdirAll(sourceDir, 0755); err != nil {
		t.Fatalf("failed to create source dir: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(destDir, "unclassified"), 0755); err != nil {
		t.Fatalf("failed to create unclassified dir: %v", err)
	}

	srcContent := []byte("same-size")
	srcPath := filepath.Join(sourceDir, "photo.jpg")
	if err := os.WriteFile(srcPath, srcContent, 0644); err != nil {
		t.Fatalf("failed to write source file: %v", err)
	}
	destPath := filepath.Join(destDir, "unclassified", "photo.jpg")
	if err := os.WriteFile(destPath, []byte("same-size"), 0644); err != nil {
		t.Fatalf("failed to write destination duplicate file: %v", err)
	}

	cfg := newTestConfig(tmpDir, sourceDir, destDir)
	p, err := New(cfg)
	if err != nil {
		t.Fatalf("failed to create pipeline: %v", err)
	}
	defer p.Close()

	completeCount := 0
	p.SetProgressCallback(func(update ProgressUpdate) {
		if update.Type == "complete" {
			completeCount++
		}
	})

	summary, err := p.Run()
	if err != nil {
		t.Fatalf("pipeline run failed: %v", err)
	}
	if summary.TotalFiles != 1 || summary.Copied != 0 {
		t.Fatalf("unexpected summary for dedup duplicate: %+v", *summary)
	}
	if completeCount == 0 {
		t.Fatal("expected complete callback in no-task path")
	}
}

// TestPipelineRun_ConflictSkipSkipsTask는 테스트 코드 동작을 검증하거나 보조합니다.
func TestPipelineRun_ConflictSkipSkipsTask(t *testing.T) {
	// conflict skip 정책이면 충돌 파일은 작업 리스트에 들어가지 않아야 한다.
	tmpDir := t.TempDir()
	t.Setenv("HOME", filepath.Join(tmpDir, "home"))

	sourceDir := filepath.Join(tmpDir, "src")
	destDir := filepath.Join(tmpDir, "dest")
	if err := os.MkdirAll(sourceDir, 0755); err != nil {
		t.Fatalf("failed to create source dir: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(destDir, "unclassified"), 0755); err != nil {
		t.Fatalf("failed to create unclassified dir: %v", err)
	}

	srcPath := filepath.Join(sourceDir, "photo.jpg")
	if err := os.WriteFile(srcPath, []byte("source-content"), 0644); err != nil {
		t.Fatalf("failed to write source file: %v", err)
	}
	destPath := filepath.Join(destDir, "unclassified", "photo.jpg")
	if err := os.WriteFile(destPath, []byte("x"), 0644); err != nil {
		t.Fatalf("failed to write conflict file: %v", err)
	}

	cfg := newTestConfig(tmpDir, sourceDir, destDir)
	cfg.ConflictPolicy = types.ConflictPolicySkip

	p, err := New(cfg)
	if err != nil {
		t.Fatalf("failed to create pipeline: %v", err)
	}
	defer p.Close()

	summary, err := p.Run()
	if err != nil {
		t.Fatalf("pipeline run failed: %v", err)
	}
	if summary.TotalFiles != 1 || summary.Copied != 0 || summary.Skipped != 0 {
		t.Fatalf("unexpected summary for conflict skip path: %+v", *summary)
	}
}

// TestPipelineRun_ConflictPolicyActionsAreCounted는 테스트 코드 동작을 검증하거나 보조합니다.
func TestPipelineRun_ConflictPolicyActionsAreCounted(t *testing.T) {
	// rename/overwrite/quarantine 정책별 카운터가 증가해야 한다.
	tests := []struct {
		name          string
		policy        types.ConflictPolicy
		expectRenamed int
		expectOver    int
		expectQuar    int
	}{
		{
			name:          "rename",
			policy:        types.ConflictPolicyRename,
			expectRenamed: 1,
		},
		{
			name:       "overwrite",
			policy:     types.ConflictPolicyOverwrite,
			expectOver: 1,
		},
		{
			name:       "quarantine",
			policy:     types.ConflictPolicyQuarantine,
			expectQuar: 1,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			tmpDir := t.TempDir()
			t.Setenv("HOME", filepath.Join(tmpDir, "home"))

			sourceDir := filepath.Join(tmpDir, "src")
			destDir := filepath.Join(tmpDir, "dest")
			if err := os.MkdirAll(sourceDir, 0755); err != nil {
				t.Fatalf("failed to create source dir: %v", err)
			}
			if err := os.MkdirAll(filepath.Join(destDir, "unclassified"), 0755); err != nil {
				t.Fatalf("failed to create unclassified dir: %v", err)
			}

			srcPath := filepath.Join(sourceDir, "photo.jpg")
			if err := os.WriteFile(srcPath, []byte("new-content"), 0644); err != nil {
				t.Fatalf("failed to write source file: %v", err)
			}
			destPath := filepath.Join(destDir, "unclassified", "photo.jpg")
			if err := os.WriteFile(destPath, []byte("x"), 0644); err != nil {
				t.Fatalf("failed to write conflict file: %v", err)
			}

			cfg := newTestConfig(tmpDir, sourceDir, destDir)
			cfg.ConflictPolicy = tc.policy

			p, err := New(cfg)
			if err != nil {
				t.Fatalf("failed to create pipeline: %v", err)
			}
			defer p.Close()

			summary, err := p.Run()
			if err != nil {
				t.Fatalf("pipeline run failed: %v", err)
			}
			if summary.Renamed != tc.expectRenamed || summary.Overwritten != tc.expectOver || summary.Quarantined != tc.expectQuar {
				t.Fatalf("unexpected summary counters: %+v", *summary)
			}
		})
	}
}

// TestPipelineRun_CopyFailureMarksFailedAndFailedStatus는 테스트 코드 동작을 검증하거나 보조합니다.
func TestPipelineRun_CopyFailureMarksFailedAndFailedStatus(t *testing.T) {
	// 복사 실패 시 Failed 카운터/히스토리 상태가 failed로 기록되어야 한다.
	tmpDir := t.TempDir()
	t.Setenv("HOME", filepath.Join(tmpDir, "home"))

	sourceDir := filepath.Join(tmpDir, "src")
	if err := os.MkdirAll(sourceDir, 0755); err != nil {
		t.Fatalf("failed to create source dir: %v", err)
	}
	srcPath := filepath.Join(sourceDir, "photo.jpg")
	if err := os.WriteFile(srcPath, []byte("photo"), 0644); err != nil {
		t.Fatalf("failed to write source file: %v", err)
	}

	destAsFile := filepath.Join(tmpDir, "dest-file")
	if err := os.WriteFile(destAsFile, []byte("not-dir"), 0644); err != nil {
		t.Fatalf("failed to write destination blocker file: %v", err)
	}

	cfg := newTestConfig(tmpDir, sourceDir, destAsFile)
	cfg.ConflictPolicy = types.ConflictPolicyOverwrite

	p, err := New(cfg)
	if err != nil {
		t.Fatalf("failed to create pipeline: %v", err)
	}
	defer p.Close()

	summary, err := p.Run()
	if err != nil {
		t.Fatalf("pipeline run should not return copy error directly: %v", err)
	}
	if summary.Failed != 1 {
		t.Fatalf("expected failed=1, got %+v", *summary)
	}

	m, err := config.NewUserDataManager()
	if err != nil {
		t.Fatalf("failed to create user data manager: %v", err)
	}
	history, err := m.LoadBackupHistory()
	if err != nil {
		t.Fatalf("failed to load backup history: %v", err)
	}
	if len(history.Entries) == 0 || history.Entries[0].Status != types.BackupStatusFailed {
		t.Fatalf("expected failed history entry, got %+v", history.Entries)
	}
}

// TestPipelineRun_StateAndHistorySaveFailuresAreIgnored는 테스트 코드 동작을 검증하거나 보조합니다.
func TestPipelineRun_StateAndHistorySaveFailuresAreIgnored(t *testing.T) {
	// state 저장/히스토리 저장 실패가 발생해도 Run은 summary를 반환해야 한다.
	tmpDir := t.TempDir()
	homeDir := filepath.Join(tmpDir, "home")
	t.Setenv("HOME", homeDir)

	sourceDir := filepath.Join(tmpDir, "src")
	destDir := filepath.Join(tmpDir, "dest")
	if err := os.MkdirAll(sourceDir, 0755); err != nil {
		t.Fatalf("failed to create source dir: %v", err)
	}
	if err := os.MkdirAll(destDir, 0755); err != nil {
		t.Fatalf("failed to create dest dir: %v", err)
	}
	srcPath := filepath.Join(sourceDir, "photo.jpg")
	if err := os.WriteFile(srcPath, []byte("photo"), 0644); err != nil {
		t.Fatalf("failed to write source file: %v", err)
	}

	cfg := newTestConfig(tmpDir, sourceDir, destDir)
	p, err := New(cfg)
	if err != nil {
		t.Fatalf("failed to create pipeline: %v", err)
	}
	defer p.Close()

	// state 저장 실패 유도: state 파일의 부모 경로를 파일로 막는다.
	stateParent := filepath.Dir(cfg.StateFile)
	if err := os.WriteFile(stateParent, []byte("block"), 0644); err != nil {
		t.Fatalf("failed to create state parent blocker: %v", err)
	}

	// 히스토리 저장 실패 유도: backup-history.json 경로를 디렉터리로 점유한다.
	blockPath := filepath.Join(homeDir, ".shutterpipe", "backup-history.json")
	if err := os.MkdirAll(blockPath, 0755); err != nil {
		t.Fatalf("failed to create history blocker dir: %v", err)
	}

	summary, err := p.Run()
	if err != nil {
		t.Fatalf("pipeline run failed: %v", err)
	}
	if summary == nil || summary.Copied != 1 {
		t.Fatalf("unexpected summary while save failures are ignored: %+v", summary)
	}
}
