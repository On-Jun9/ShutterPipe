package pipeline

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/On-Jun9/ShutterPipe/internal/config"
	"github.com/On-Jun9/ShutterPipe/internal/copier"
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

type truncatingMetadataExtractor struct {
	size int64
}

func (e truncatingMetadataExtractor) ExtractWithContext(_ context.Context, entry types.FileEntry) (types.MediaMetadata, error) {
	return types.MediaMetadata{}, os.Truncate(entry.Path, e.size)
}

func TestEffectiveCopyWorkers_OverwriteSerializesFilesystemEquivalentPaths(t *testing.T) {
	if got := effectiveCopyWorkers(8, types.ConflictPolicyOverwrite); got != 1 {
		t.Fatalf("overwrite commits must preserve planned order, got %d workers", got)
	}
	if got := effectiveCopyWorkers(8, types.ConflictPolicyRename); got != 8 {
		t.Fatalf("non-overwrite policy unexpectedly lost concurrency: got %d workers", got)
	}
}

func TestSelectDirtyOverwriteGroups_DoesNotCollapseDistinctHardLinks(t *testing.T) {
	dir := t.TempDir()
	firstDest := filepath.Join(dir, "first.jpg")
	secondDest := filepath.Join(dir, "second.jpg")
	if err := os.WriteFile(firstDest, []byte("shared"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.Link(firstDest, secondDest); err != nil {
		t.Skipf("hard links unavailable: %v", err)
	}
	firstSource := types.FileEntry{Path: "/source/first.jpg", Name: "first.jpg", Size: 6}
	secondSource := types.FileEntry{Path: "/source/second.jpg", Name: "second.jpg", Size: 6}
	tasks := []types.CopyTask{
		{Source: firstSource, DestPath: firstDest},
		{Source: secondSource, DestPath: secondDest},
	}
	selected, _, dirty := selectDirtyOverwriteGroups(
		context.Background(),
		tasks,
		map[string][]types.FileEntry{firstDest: {firstSource}, secondDest: {secondSource}},
		map[string]int{firstDest: 1},
		map[string]int{firstDest: 0, secondDest: 1},
	)
	if len(selected) != 1 || selected[0].DestPath != firstDest || dirty[firstDest] != 1 {
		t.Fatalf("distinct hard-link destinations were collapsed: selected=%+v dirty=%+v", selected, dirty)
	}
}

func TestSelectDirtyOverwriteGroups_DoesNotCollapseSymlinkWithTargetEntry(t *testing.T) {
	dir := t.TempDir()
	targetDest := filepath.Join(dir, "target.jpg")
	symlinkDest := filepath.Join(dir, "symlink.jpg")
	if err := os.WriteFile(targetDest, []byte("target"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Base(targetDest), symlinkDest); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	targetSource := types.FileEntry{Path: "/source/target.jpg", Name: "target.jpg", Size: 6}
	symlinkSource := types.FileEntry{Path: "/source/symlink.jpg", Name: "symlink.jpg", Size: 7}
	selected, _, dirty := selectDirtyOverwriteGroups(
		context.Background(),
		[]types.CopyTask{
			{Source: targetSource, DestPath: targetDest},
			{Source: symlinkSource, DestPath: symlinkDest},
		},
		map[string][]types.FileEntry{targetDest: {targetSource}, symlinkDest: {symlinkSource}},
		map[string]int{targetDest: 1, symlinkDest: 1},
		map[string]int{targetDest: 0, symlinkDest: 1},
	)
	if len(selected) != 2 || dirty[targetDest] != 1 || dirty[symlinkDest] != 1 {
		t.Fatalf("symlink entry was collapsed with its target: selected=%+v dirty=%+v", selected, dirty)
	}
}

func TestSelectDirtyOverwriteGroups_UnicodeAliasWithHardLinkKeepsLastAliasWinner(t *testing.T) {
	dir := t.TempDir()
	composedDest := filepath.Join(dir, "caf\u00e9.jpg")
	decomposedDest := filepath.Join(dir, "cafe\u0301.jpg")
	hardLinkDest := filepath.Join(dir, "separate-hard-link.jpg")
	if err := os.WriteFile(composedDest, []byte("shared"), 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(decomposedDest); err != nil {
		t.Skip("destination filesystem does not normalize Unicode path names")
	}
	if err := os.Link(composedDest, hardLinkDest); err != nil {
		t.Skipf("hard links unavailable: %v", err)
	}

	firstSource := types.FileEntry{Path: "/source/first.jpg", Name: "first.jpg", Size: 6}
	winnerSource := types.FileEntry{Path: "/source/winner.jpg", Name: "winner.jpg", Size: 6}
	hardLinkSource := types.FileEntry{Path: "/source/hard-link.jpg", Name: "hard-link.jpg", Size: 6}
	tasks := []types.CopyTask{
		{Source: firstSource, DestPath: composedDest},
		{Source: winnerSource, DestPath: decomposedDest},
		{Source: hardLinkSource, DestPath: hardLinkDest},
	}
	selected, _, dirty := selectDirtyOverwriteGroups(
		context.Background(),
		tasks,
		map[string][]types.FileEntry{
			composedDest:   {firstSource},
			decomposedDest: {winnerSource},
			hardLinkDest:   {hardLinkSource},
		},
		map[string]int{composedDest: 1, hardLinkDest: 1},
		map[string]int{composedDest: 0, decomposedDest: 1, hardLinkDest: 2},
	)
	if len(selected) != 2 {
		t.Fatalf("expected one alias winner plus distinct hard link, got %+v", selected)
	}
	if selected[0].DestPath != decomposedDest || dirty[decomposedDest] != 1 {
		t.Fatalf("Unicode aliases did not select their last planned winner: selected=%+v dirty=%+v", selected, dirty)
	}
	if selected[1].DestPath != hardLinkDest || dirty[hardLinkDest] != 1 {
		t.Fatalf("distinct hard link was collapsed into Unicode alias group: selected=%+v dirty=%+v", selected, dirty)
	}
}

func TestSelectDirtyOverwriteGroups_PreCanceledContextStopsSelection(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	selected, sources, dirty := selectDirtyOverwriteGroups(
		ctx,
		[]types.CopyTask{{DestPath: "/dest/photo.jpg"}},
		nil,
		map[string]int{"/dest/photo.jpg": 1},
		map[string]int{"/dest/photo.jpg": 0},
	)
	if selected != nil || sources != nil || dirty != nil {
		t.Fatalf("canceled selection returned work: selected=%+v sources=%+v dirty=%+v", selected, sources, dirty)
	}
}

func TestSelectDirtyOverwriteGroups_UsesFilesystemCanonicalEntryForCaseAlias(t *testing.T) {
	upperDest := filepath.FromSlash("/dest/Photo.jpg")
	lowerDest := filepath.FromSlash("/dest/photo.jpg")
	canonicalDest := filepath.FromSlash("/dest/PHOTO.jpg")
	firstSource := types.FileEntry{Path: "/source/first.jpg", Name: "first.jpg"}
	winnerSource := types.FileEntry{Path: "/source/winner.jpg", Name: "winner.jpg"}
	lookups := 0
	resolver := newDestinationIdentityResolver()
	resolver.canonical = func(path string) (string, bool) {
		lookups++
		if path == upperDest || path == lowerDest {
			return canonicalDest, true
		}
		return path, true
	}

	selected, _, dirty := selectDirtyOverwriteGroupsWithResolver(
		context.Background(),
		[]types.CopyTask{
			{Source: firstSource, DestPath: upperDest},
			{Source: winnerSource, DestPath: lowerDest},
		},
		map[string][]types.FileEntry{upperDest: {firstSource}, lowerDest: {winnerSource}},
		map[string]int{upperDest: 1},
		map[string]int{upperDest: 0, lowerDest: 1},
		resolver,
	)
	if lookups != 2 {
		t.Fatalf("canonical resolver called %d times, want once per unique path", lookups)
	}
	if len(selected) != 1 || selected[0].DestPath != lowerDest || dirty[lowerDest] != 1 {
		t.Fatalf("case aliases did not replay the clean last winner: selected=%+v dirty=%+v", selected, dirty)
	}
}

func BenchmarkSelectDirtyOverwriteGroups_SingleDirectory(b *testing.B) {
	const taskCount = 10_000
	tasks := make([]types.CopyTask, 0, taskCount)
	sources := make(map[string][]types.FileEntry, taskCount)
	dirty := make(map[string]int, taskCount)
	sequences := make(map[string]int, taskCount)
	for index := 0; index < taskCount; index++ {
		destPath := filepath.Join("/missing-destination", "photos", fmt.Sprintf("%06d.jpg", index))
		source := types.FileEntry{Path: fmt.Sprintf("/source/%06d.jpg", index)}
		tasks = append(tasks, types.CopyTask{Source: source, DestPath: destPath})
		sources[destPath] = []types.FileEntry{source}
		dirty[destPath] = 1
		sequences[destPath] = index
	}
	b.ResetTimer()
	for index := 0; index < b.N; index++ {
		selected, _, _ := selectDirtyOverwriteGroups(context.Background(), tasks, sources, dirty, sequences)
		if len(selected) != taskCount {
			b.Fatalf("selected %d tasks, want %d", len(selected), taskCount)
		}
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
	destPath := filepath.Join(destDir, "unclassified", "photo.jpg")
	content := []byte("photo-bytes")
	if err := os.WriteFile(srcPath, content, 0644); err != nil {
		t.Fatalf("failed to write source file: %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(destPath), 0755); err != nil {
		t.Fatalf("failed to create destination parent: %v", err)
	}
	if err := os.WriteFile(destPath, content, 0644); err != nil {
		t.Fatalf("failed to write destination file: %v", err)
	}
	srcInfo, err := os.Stat(srcPath)
	if err != nil {
		t.Fatalf("failed to stat source file: %v", err)
	}

	cfg := newTestConfig(tmpDir, sourceDir, destDir)
	st := state.New(cfg.StateFile)
	if err := st.MarkProcessedEntry(types.FileEntry{
		Path:    srcPath,
		Name:    filepath.Base(srcPath),
		Size:    srcInfo.Size(),
		ModTime: srcInfo.ModTime(),
	}, destPath, false); err != nil {
		t.Fatalf("failed to preload state entry: %v", err)
	}
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
	if summary.TotalFiles != 1 || summary.Copied != 0 || summary.Skipped != 1 {
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
	// 복사 실패 시 Failed 카운터/히스토리/terminal 상태가 모두 failed여야 한다.
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
	var terminal ProgressUpdate
	p.SetProgressCallback(func(update ProgressUpdate) {
		if update.Type == "complete" || update.Type == "error" {
			terminal = update
		}
	})

	summary, err := p.Run()
	if !errors.Is(err, ErrRunFailed) {
		t.Fatalf("expected failed run error, got %v", err)
	}
	if summary.Failed != 1 {
		t.Fatalf("expected failed=1, got %+v", *summary)
	}
	if terminal.Type != "error" || terminal.Summary != summary || !errors.Is(err, ErrRunFailed) {
		t.Fatalf("copy failure was not emitted as terminal error: %+v", terminal)
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

type partialFailureCopier struct{}

func (partialFailureCopier) CopyAll(_ context.Context, tasks []types.CopyTask, results chan<- copier.CopyResult) {
	defer close(results)
	for index, task := range tasks {
		if index == 0 {
			task.Status = types.TaskStatusCompleted
			task.Action = types.CopyActionCopied
			results <- copier.CopyResult{Task: task}
			continue
		}
		err := errors.New("injected copy failure")
		task.Status = types.TaskStatusFailed
		task.Error = err.Error()
		results <- copier.CopyResult{Task: task, Error: err}
	}
}

func TestPipelineRun_PartialCopyFailureReturnsTerminalErrorWithSummary(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("HOME", filepath.Join(tmpDir, "home"))
	sourceDir := filepath.Join(tmpDir, "src")
	destDir := filepath.Join(tmpDir, "dest")
	if err := os.MkdirAll(sourceDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(destDir, 0755); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"first.jpg", "second.jpg"} {
		if err := os.WriteFile(filepath.Join(sourceDir, name), []byte(name), 0644); err != nil {
			t.Fatal(err)
		}
	}

	cfg := newTestConfig(tmpDir, sourceDir, destDir)
	cfg.IgnoreState = true
	p, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	p.copier = partialFailureCopier{}
	var terminal ProgressUpdate
	p.SetProgressCallback(func(update ProgressUpdate) {
		if update.Type == "complete" || update.Type == "error" {
			terminal = update
		}
	})

	summary, err := p.Run()
	if !errors.Is(err, ErrRunFailed) {
		t.Fatalf("expected partial failure to fail the run, got %v", err)
	}
	if summary.Copied != 1 || summary.Failed != 1 {
		t.Fatalf("unexpected partial failure summary: %+v", summary)
	}
	if terminal.Type != "error" || terminal.Summary != summary || terminal.Error == "" {
		t.Fatalf("partial failure terminal did not preserve summary/error: %+v", terminal)
	}
}

// TestPipelineRun_StateAndHistorySaveFailuresAreIgnored는 테스트 코드 동작을 검증하거나 보조합니다.
func TestPipelineRun_StateAndHistorySaveFailuresAreReportedAsWarnings(t *testing.T) {
	// 파일 복사는 성공으로 유지하되 state/히스토리 저장 실패는 summary에 노출해야 한다.
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

	// state 저장 실패 유도: lock 이후의 복사 progress에서 state 부모 경로를 파일로 막는다.
	stateParent := filepath.Dir(cfg.StateFile)
	p.SetProgressCallback(func(update ProgressUpdate) {
		if update.Type == "progress" {
			if err := os.WriteFile(stateParent, []byte("block"), 0644); err != nil && !errors.Is(err, os.ErrExist) {
				t.Errorf("failed to create state parent blocker: %v", err)
			}
		}
	})

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
	if len(summary.Warnings) != 2 {
		t.Fatalf("expected state and history warnings, got %#v", summary.Warnings)
	}
	if !strings.Contains(summary.Warnings[0], "처리 상태") || !strings.Contains(summary.Warnings[1], "백업 이력") {
		t.Fatalf("unexpected persistence warnings: %#v", summary.Warnings)
	}
}

type sourceChangedBeforeStateCopier struct{}

func (sourceChangedBeforeStateCopier) CopyAll(_ context.Context, tasks []types.CopyTask, results chan<- copier.CopyResult) {
	defer close(results)
	for _, task := range tasks {
		original, err := os.ReadFile(task.Source.Path)
		if err != nil {
			results <- copier.CopyResult{Task: task, Error: err}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(task.DestPath), 0755); err != nil {
			results <- copier.CopyResult{Task: task, Error: err}
			continue
		}
		if err := os.WriteFile(task.DestPath, original, 0644); err != nil {
			results <- copier.CopyResult{Task: task, Error: err}
			continue
		}
		changed := append([]byte(nil), original...)
		for index := range changed {
			changed[index] ^= 0xff
		}
		if err := os.WriteFile(task.Source.Path, changed, 0644); err != nil {
			results <- copier.CopyResult{Task: task, Error: err}
			continue
		}
		if err := os.Chtimes(task.Source.Path, task.Source.ModTime, task.Source.ModTime); err != nil {
			results <- copier.CopyResult{Task: task, Error: err}
			continue
		}
		verifiedHash := sha256.Sum256(original)
		task.Status = types.TaskStatusCompleted
		task.Action = types.CopyActionCopied
		results <- copier.CopyResult{Task: task, VerifiedSourceHash: verifiedHash[:]}
	}
}

func TestPipelineRun_HashStateCommitRejectsChangedVerifiedSnapshot(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("HOME", filepath.Join(tmpDir, "home"))
	sourceDir := filepath.Join(tmpDir, "src")
	destDir := filepath.Join(tmpDir, "dest")
	if err := os.MkdirAll(sourceDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(destDir, 0755); err != nil {
		t.Fatal(err)
	}
	sourcePath := filepath.Join(sourceDir, "photo.jpg")
	if err := os.WriteFile(sourcePath, []byte("AAAA"), 0644); err != nil {
		t.Fatal(err)
	}

	cfg := newTestConfig(tmpDir, sourceDir, destDir)
	cfg.HashVerify = true
	p, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	p.copier = sourceChangedBeforeStateCopier{}

	summary, err := p.Run()
	if err != nil {
		t.Fatalf("copy outcome should remain successful, got %v", err)
	}
	if summary.Copied != 1 || len(summary.Warnings) == 0 || !strings.Contains(summary.Warnings[0], "verified snapshot changed") {
		t.Fatalf("state commit mismatch was not exposed: %+v", summary)
	}
	saved, err := state.Load(cfg.StateFile)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := saved.Processed[sourcePath]; ok {
		t.Fatal("changed source was permanently recorded as processed")
	}
}

// TestPipelineRunWithContext_CancelAfterAllTasksComplete_RecordsSuccess는 모든 작업이 완료된 후
// 취소 신호가 도착한 경우 성공으로 기록되어야 함을 검증한다.
// processed == len(tasks)이면 ctx.Err() 취소 신호를 무시해야 한다.
func TestPipelineRunWithContext_CancelAfterAllTasksComplete_RecordsSuccess(t *testing.T) {
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

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// 마지막 파일 복사 완료 직후 progress 콜백에서 취소:
	// 이 시점에 processed == len(tasks)이므로 취소 조건(processed < len(tasks))이 false
	p.SetProgressCallback(func(update ProgressUpdate) {
		if update.Type == "progress" {
			cancel()
		}
	})

	summary, err := p.RunWithContext(ctx)
	if err != nil {
		t.Fatalf("expected success when all tasks complete before cancel, got %v", err)
	}
	if summary == nil {
		t.Fatal("expected non-nil summary")
	}

	m, mErr := config.NewUserDataManager()
	if mErr != nil {
		t.Fatalf("failed to create user data manager: %v", mErr)
	}
	history, hErr := m.LoadBackupHistory()
	if hErr != nil {
		t.Fatalf("failed to load backup history: %v", hErr)
	}
	if len(history.Entries) == 0 {
		t.Fatal("expected history entry")
	}
	if history.Entries[0].Status != types.BackupStatusSuccess {
		t.Fatalf("expected success history status, got %s", history.Entries[0].Status)
	}
}

// TestPipelineRunWithContext_CanceledContextRecordsCanceledHistory는 테스트 코드 동작을 검증하거나 보조합니다.
func TestPipelineRunWithContext_CanceledContextRecordsCanceledHistory(t *testing.T) {
	// 취소된 실행은 ErrRunCanceled을 반환하고 이력 상태를 canceled로 저장해야 한다.
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

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	summary, err := p.RunWithContext(ctx)
	if !errors.Is(err, ErrRunCanceled) {
		t.Fatalf("expected ErrRunCanceled, got %v", err)
	}
	if summary == nil {
		t.Fatal("expected non-nil summary for canceled run")
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
		t.Fatal("expected canceled history entry")
	}
	if history.Entries[0].Status != types.BackupStatusCanceled {
		t.Fatalf("expected canceled history status, got %s", history.Entries[0].Status)
	}
}

type blockingMetadataExtractor struct {
	started chan struct{}
}

func (e *blockingMetadataExtractor) ExtractWithContext(ctx context.Context, _ types.FileEntry) (types.MediaMetadata, error) {
	close(e.started)
	<-ctx.Done()
	return types.MediaMetadata{}, ctx.Err()
}

type deadlineContext struct {
	expired bool
}

func (c *deadlineContext) Deadline() (time.Time, bool) { return time.Time{}, false }
func (c *deadlineContext) Done() <-chan struct{}       { return nil }
func (c *deadlineContext) Err() error {
	if c.expired {
		return context.DeadlineExceeded
	}
	return nil
}
func (c *deadlineContext) Value(any) any { return nil }

type expiringMetadataExtractor struct {
	ctx *deadlineContext
}

func (e expiringMetadataExtractor) ExtractWithContext(context.Context, types.FileEntry) (types.MediaMetadata, error) {
	e.ctx.expired = true
	return types.MediaMetadata{}, context.DeadlineExceeded
}

func TestPipelineRunWithContext_DeadlineDuringMetadataCannotComplete(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("HOME", filepath.Join(tmpDir, "home"))
	sourceDir := filepath.Join(tmpDir, "src")
	destDir := filepath.Join(tmpDir, "dest")
	if err := os.MkdirAll(sourceDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(destDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sourceDir, "photo.jpg"), []byte("photo"), 0644); err != nil {
		t.Fatal(err)
	}

	cfg := newTestConfig(tmpDir, sourceDir, destDir)
	p, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	ctx := &deadlineContext{}
	p.meta = expiringMetadataExtractor{ctx: ctx}

	summary, runErr := p.RunWithContext(ctx)
	if !errors.Is(runErr, ErrRunCanceled) || !errors.Is(runErr, context.DeadlineExceeded) {
		t.Fatalf("expected canceled deadline error, got %v", runErr)
	}
	if summary == nil || summary.Copied != 0 || summary.BytesCopied != 0 {
		t.Fatalf("deadline produced a successful copy summary: %+v", summary)
	}

	historyManager, err := config.NewUserDataManager()
	if err != nil {
		t.Fatal(err)
	}
	history, err := historyManager.LoadBackupHistory()
	if err != nil {
		t.Fatal(err)
	}
	if len(history.Entries) == 0 || history.Entries[0].Status != types.BackupStatusCanceled {
		t.Fatalf("deadline was not recorded as canceled: %+v", history.Entries)
	}
}

func TestPipelineRunWithContext_CancelDuringMetadataExtraction(t *testing.T) {
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
	if err := os.WriteFile(filepath.Join(sourceDir, "photo.jpg"), []byte("photo"), 0644); err != nil {
		t.Fatalf("failed to write source file: %v", err)
	}

	cfg := newTestConfig(tmpDir, sourceDir, destDir)
	p, err := New(cfg)
	if err != nil {
		t.Fatalf("failed to create pipeline: %v", err)
	}
	defer p.Close()

	extractor := &blockingMetadataExtractor{started: make(chan struct{})}
	p.meta = extractor

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		<-extractor.started
		cancel()
	}()

	summary, err := p.RunWithContext(ctx)
	if !errors.Is(err, ErrRunCanceled) {
		t.Fatalf("expected ErrRunCanceled, got %v", err)
	}
	if summary == nil {
		t.Fatal("expected canceled run summary")
	}

	historyManager, err := config.NewUserDataManager()
	if err != nil {
		t.Fatalf("failed to create user data manager: %v", err)
	}
	history, err := historyManager.LoadBackupHistory()
	if err != nil {
		t.Fatalf("failed to load backup history: %v", err)
	}
	if len(history.Entries) == 0 || history.Entries[0].Status != types.BackupStatusCanceled {
		t.Fatalf("expected canceled history entry, got %+v", history.Entries)
	}
}

func TestPipelineRun_RenamePolicyPreservesSameNamedFilesFromDifferentDirectories(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("HOME", filepath.Join(tmpDir, "home"))
	sourceDir := filepath.Join(tmpDir, "src")
	destDir := filepath.Join(tmpDir, "dest")

	for dir, content := range map[string]string{"card-a": "first-photo", "card-b": "second-photo"} {
		path := filepath.Join(sourceDir, dir, "photo.jpg")
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			t.Fatalf("failed to create source directory: %v", err)
		}
		if err := os.WriteFile(path, []byte(content), 0644); err != nil {
			t.Fatalf("failed to create source file: %v", err)
		}
	}
	if err := os.MkdirAll(destDir, 0755); err != nil {
		t.Fatalf("failed to create destination: %v", err)
	}

	cfg := newTestConfig(tmpDir, sourceDir, destDir)
	cfg.ConflictPolicy = types.ConflictPolicyRename
	cfg.IgnoreState = true
	cfg.Jobs = 2
	p, err := New(cfg)
	if err != nil {
		t.Fatalf("failed to create pipeline: %v", err)
	}
	defer p.Close()

	summary, err := p.Run()
	if err != nil {
		t.Fatalf("pipeline run failed: %v", err)
	}
	if summary.Copied != 1 || summary.Renamed != 1 || summary.Failed != 0 {
		t.Fatalf("unexpected summary: %+v", summary)
	}

	contents := map[string]bool{}
	for _, name := range []string{"photo.jpg", "photo_1.jpg"} {
		data, err := os.ReadFile(filepath.Join(destDir, "unclassified", name))
		if err != nil {
			t.Fatalf("failed to read %s: %v", name, err)
		}
		contents[string(data)] = true
	}
	if !contents["first-photo"] || !contents["second-photo"] {
		t.Fatalf("both source files were not preserved: %+v", contents)
	}
}

func TestPipelineRun_OverwriteReservedDestinationUsesDeterministicLastPlannedSource(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("HOME", filepath.Join(tmpDir, "home"))
	sourceDir := filepath.Join(tmpDir, "src")
	destDir := filepath.Join(tmpDir, "dest")
	for dir, content := range map[string]string{"card-a": "first-photo", "card-b": "second-photo"} {
		path := filepath.Join(sourceDir, dir, "photo.jpg")
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			t.Fatalf("failed to create source directory: %v", err)
		}
		if err := os.WriteFile(path, []byte(content), 0644); err != nil {
			t.Fatalf("failed to create source file: %v", err)
		}
	}
	if err := os.MkdirAll(destDir, 0755); err != nil {
		t.Fatalf("failed to create destination: %v", err)
	}

	cfg := newTestConfig(tmpDir, sourceDir, destDir)
	cfg.ConflictPolicy = types.ConflictPolicyOverwrite
	cfg.IgnoreState = false
	cfg.Jobs = 2
	p, err := New(cfg)
	if err != nil {
		t.Fatalf("failed to create pipeline: %v", err)
	}
	summary, err := p.Run()
	if err != nil {
		t.Fatalf("pipeline run failed: %v", err)
	}
	if summary.Copied != 1 || summary.Overwritten != 1 || summary.Failed != 0 {
		t.Fatalf("unexpected summary: %+v", summary)
	}
	if summary.TotalFiles != summary.Copied+summary.Skipped+summary.Renamed+summary.Overwritten+summary.Quarantined+summary.Failed {
		t.Fatalf("overwrite summary does not account for every input: %+v", summary)
	}
	data, err := os.ReadFile(filepath.Join(destDir, "unclassified", "photo.jpg"))
	if err != nil {
		t.Fatalf("failed to read final file: %v", err)
	}
	if string(data) != "second-photo" {
		t.Fatalf("expected last planned source to win, got %q", data)
	}
	if err := p.Close(); err != nil {
		t.Fatalf("failed to close first pipeline: %v", err)
	}

	// A repeated run must not reverse the winner because only the collapsed task
	// was recorded in state.
	p, err = New(cfg)
	if err != nil {
		t.Fatalf("failed to create second pipeline: %v", err)
	}
	if _, err := p.Run(); err != nil {
		t.Fatalf("second pipeline run failed: %v", err)
	}
	p.Close()
	data, err = os.ReadFile(filepath.Join(destDir, "unclassified", "photo.jpg"))
	if err != nil || string(data) != "second-photo" {
		t.Fatalf("repeated run reversed overwrite winner: data=%q err=%v", data, err)
	}

	// If an earlier source changes, the last planned source still wins and all
	// members of the destination group are re-recorded together.
	if err := os.WriteFile(filepath.Join(sourceDir, "card-a", "photo.jpg"), []byte("first-photo-changed"), 0644); err != nil {
		t.Fatalf("failed to change earlier source: %v", err)
	}
	p, err = New(cfg)
	if err != nil {
		t.Fatalf("failed to create third pipeline: %v", err)
	}
	defer p.Close()
	if _, err := p.Run(); err != nil {
		t.Fatalf("third pipeline run failed: %v", err)
	}
	data, err = os.ReadFile(filepath.Join(destDir, "unclassified", "photo.jpg"))
	if err != nil || string(data) != "second-photo" {
		t.Fatalf("changed earlier source reversed overwrite winner: data=%q err=%v", data, err)
	}
}

func TestPipelineRun_OverwriteAliasReplayKeepsLastWinnerOnCaseInsensitiveDestination(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("HOME", filepath.Join(tmpDir, "home"))
	sourceDir := filepath.Join(tmpDir, "src")
	destDir := filepath.Join(tmpDir, "dest")
	if err := os.MkdirAll(destDir, 0755); err != nil {
		t.Fatal(err)
	}
	probe := filepath.Join(destDir, "CaseProbe")
	if err := os.WriteFile(probe, []byte("probe"), 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(destDir, "caseprobe")); err != nil {
		os.Remove(probe)
		t.Skip("destination filesystem is case-sensitive")
	}
	if err := os.Remove(probe); err != nil {
		t.Fatal(err)
	}

	firstPath := filepath.Join(sourceDir, "card-a", "Photo.jpg")
	lastPath := filepath.Join(sourceDir, "card-b", "photo.jpg")
	for path, content := range map[string]string{firstPath: "first-photo", lastPath: "last-photo"} {
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
	}

	cfg := newTestConfig(tmpDir, sourceDir, destDir)
	cfg.ConflictPolicy = types.ConflictPolicyOverwrite
	cfg.IgnoreState = false
	cfg.Jobs = 8
	run := func() *types.RunSummary {
		p, err := New(cfg)
		if err != nil {
			t.Fatal(err)
		}
		summary, runErr := p.Run()
		closeErr := p.Close()
		if runErr != nil || closeErr != nil {
			t.Fatalf("pipeline run failed: run=%v close=%v", runErr, closeErr)
		}
		return summary
	}

	finalPath := filepath.Join(destDir, "unclassified", "photo.jpg")
	p, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	p.copier = cancelAfterFirstOverwriteCopier{}
	cancelledSummary, runErr := p.RunWithContext(context.Background())
	closeErr := p.Close()
	if !errors.Is(runErr, ErrRunCanceled) || closeErr != nil {
		t.Fatalf("expected injected cancellation: run=%v close=%v", runErr, closeErr)
	}
	if cancelledSummary.Overwritten != 1 {
		t.Fatalf("expected first alias to commit before cancellation: %+v", cancelledSummary)
	}
	data, err := os.ReadFile(finalPath)
	if err != nil || string(data) != "first-photo" {
		t.Fatalf("test did not establish the partial earlier winner: data=%q err=%v", data, err)
	}

	recovered := run()
	data, err = os.ReadFile(finalPath)
	if err != nil || string(data) != "last-photo" {
		t.Fatalf("next run did not restore the last alias winner: data=%q err=%v", data, err)
	}
	if recovered.TotalFiles != 2 || recovered.Overwritten != 2 || recovered.Failed != 0 {
		t.Fatalf("recovery replay was not fully accounted for: %+v", recovered)
	}

	if err := os.WriteFile(firstPath, []byte("first-photo-changed"), 0644); err != nil {
		t.Fatal(err)
	}
	summary := run()
	data, err = os.ReadFile(finalPath)
	if err != nil || string(data) != "last-photo" {
		t.Fatalf("dirty earlier alias reversed clean last winner: data=%q err=%v", data, err)
	}
	if summary.TotalFiles != 1 || summary.Overwritten != 1 || summary.Failed != 0 {
		t.Fatalf("dirty alias group was not narrowed to one final winner: %+v", summary)
	}
}

func TestPipelineRun_OverwriteReplaysOnlyDirtyDestinationGroup(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("HOME", filepath.Join(tmpDir, "home"))
	sourceDir := filepath.Join(tmpDir, "src")
	destDir := filepath.Join(tmpDir, "dest")
	firstPath := filepath.Join(sourceDir, "first.jpg")
	secondPath := filepath.Join(sourceDir, "second.jpg")
	if err := os.MkdirAll(sourceDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(firstPath, []byte("first"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(secondPath, []byte("second"), 0644); err != nil {
		t.Fatal(err)
	}
	cfg := newTestConfig(tmpDir, sourceDir, destDir)
	cfg.ConflictPolicy = types.ConflictPolicyOverwrite
	cfg.IgnoreState = false
	cfg.Jobs = 8
	run := func() *types.RunSummary {
		p, err := New(cfg)
		if err != nil {
			t.Fatal(err)
		}
		summary, runErr := p.Run()
		closeErr := p.Close()
		if runErr != nil || closeErr != nil {
			t.Fatalf("pipeline run failed: run=%v close=%v", runErr, closeErr)
		}
		return summary
	}
	run()

	if err := os.WriteFile(firstPath, []byte("first-changed"), 0644); err != nil {
		t.Fatal(err)
	}
	summary := run()
	if summary.TotalFiles != 1 || summary.Overwritten != 1 || summary.Failed != 0 {
		t.Fatalf("unrelated clean destination was replayed: %+v", summary)
	}
	data, err := os.ReadFile(filepath.Join(destDir, "unclassified", "second.jpg"))
	if err != nil || string(data) != "second" {
		t.Fatalf("unrelated destination changed: data=%q err=%v", data, err)
	}

	secondDest := filepath.Join(destDir, "unclassified", "second.jpg")
	if err := os.Remove(secondDest); err != nil {
		t.Fatal(err)
	}
	recovered := run()
	if recovered.TotalFiles != 1 || recovered.Copied != 1 || recovered.Failed != 0 {
		t.Fatalf("missing processed destination was not recovered: %+v", recovered)
	}
	data, err = os.ReadFile(secondDest)
	if err != nil || string(data) != "second" {
		t.Fatalf("missing destination recovery failed: data=%q err=%v", data, err)
	}
}

func TestPipelineRun_OverwriteReplaysSameSizeSourceAndDestinationChanges(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("HOME", filepath.Join(tmpDir, "home"))
	sourceDir := filepath.Join(tmpDir, "src")
	destDir := filepath.Join(tmpDir, "dest")
	if err := os.MkdirAll(sourceDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(destDir, 0755); err != nil {
		t.Fatal(err)
	}
	sourcePath := filepath.Join(sourceDir, "photo.jpg")
	if err := os.WriteFile(sourcePath, []byte("first1"), 0644); err != nil {
		t.Fatal(err)
	}
	cfg := newTestConfig(tmpDir, sourceDir, destDir)
	cfg.ConflictPolicy = types.ConflictPolicyOverwrite
	p, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	if _, err := p.Run(); err != nil {
		t.Fatal(err)
	}
	destPath := filepath.Join(destDir, cfg.UnclassifiedDir, "photo.jpg")

	if err := os.WriteFile(sourcePath, []byte("second"), 0644); err != nil {
		t.Fatal(err)
	}
	future := time.Now().Add(2 * time.Second)
	if err := os.Chtimes(sourcePath, future, future); err != nil {
		t.Fatal(err)
	}
	summary, err := p.Run()
	if err != nil {
		t.Fatal(err)
	}
	if summary.Overwritten != 1 {
		t.Fatalf("same-size source change was not replayed: %+v", summary)
	}
	data, err := os.ReadFile(destPath)
	if err != nil || string(data) != "second" {
		t.Fatalf("source change not reflected: data=%q err=%v", data, err)
	}

	if err := os.WriteFile(destPath, []byte("damage"), 0644); err != nil {
		t.Fatal(err)
	}
	later := future.Add(2 * time.Second)
	if err := os.Chtimes(destPath, later, later); err != nil {
		t.Fatal(err)
	}
	summary, err = p.Run()
	if err != nil {
		t.Fatal(err)
	}
	if summary.Overwritten != 1 {
		t.Fatalf("same-size destination damage was not replayed: %+v", summary)
	}
	data, err = os.ReadFile(destPath)
	if err != nil || string(data) != "second" {
		t.Fatalf("destination damage not repaired: data=%q err=%v", data, err)
	}
}

func TestClassifyCopyResultError_CancelWithCleanupFailureCountsFailure(t *testing.T) {
	cleanupErr := errors.New("failed to remove partial file")
	cancelled, countFailure := classifyCopyResultError(errors.Join(context.Canceled, cleanupErr))
	if !cancelled || !countFailure {
		t.Fatalf("expected cancelled failure, got cancelled=%v countFailure=%v", cancelled, countFailure)
	}

	cancelled, countFailure = classifyCopyResultError(context.Canceled)
	if !cancelled || countFailure {
		t.Fatalf("pure cancellation must not count as failure: cancelled=%v countFailure=%v", cancelled, countFailure)
	}
}

type joinedCancellationCopier struct {
	err error
}

func (c joinedCancellationCopier) CopyAll(_ context.Context, tasks []types.CopyTask, results chan<- copier.CopyResult) {
	defer close(results)
	task := tasks[0]
	task.Status = types.TaskStatusFailed
	task.Error = c.err.Error()
	results <- copier.CopyResult{Task: task, Error: c.err}
}

type lateSkipCopier struct{}

func (lateSkipCopier) CopyAll(_ context.Context, tasks []types.CopyTask, results chan<- copier.CopyResult) {
	defer close(results)
	task := tasks[0]
	task.Status = types.TaskStatusSkipped
	task.Action = types.CopyActionSkipped
	results <- copier.CopyResult{Task: task}
}

type cancelAfterFirstOverwriteCopier struct{}

func (cancelAfterFirstOverwriteCopier) CopyAll(_ context.Context, tasks []types.CopyTask, results chan<- copier.CopyResult) {
	defer close(results)
	first := tasks[0]
	data, err := os.ReadFile(first.Source.Path)
	if err != nil {
		results <- copier.CopyResult{Task: first, Error: err}
		return
	}
	if err := os.MkdirAll(filepath.Dir(first.DestPath), 0755); err != nil {
		results <- copier.CopyResult{Task: first, Error: err}
		return
	}
	if err := os.WriteFile(first.DestPath, data, 0644); err != nil {
		results <- copier.CopyResult{Task: first, Error: err}
		return
	}
	first.Status = types.TaskStatusCompleted
	first.Action = types.CopyActionOverwritten
	results <- copier.CopyResult{Task: first}

	last := tasks[1]
	last.Status = types.TaskStatusFailed
	last.Error = context.Canceled.Error()
	results <- copier.CopyResult{Task: last, Error: context.Canceled}
}

func TestPipelineRun_LateSkipDoesNotMarkSourceProcessed(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("HOME", filepath.Join(tmpDir, "home"))
	sourceDir := filepath.Join(tmpDir, "src")
	destDir := filepath.Join(tmpDir, "dest")
	if err := os.MkdirAll(sourceDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(destDir, 0755); err != nil {
		t.Fatal(err)
	}
	sourcePath := filepath.Join(sourceDir, "photo.jpg")
	if err := os.WriteFile(sourcePath, []byte("photo"), 0644); err != nil {
		t.Fatal(err)
	}

	cfg := newTestConfig(tmpDir, sourceDir, destDir)
	cfg.IgnoreState = false
	p, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	p.copier = lateSkipCopier{}

	summary, err := p.Run()
	if err != nil {
		t.Fatal(err)
	}
	if summary.Skipped != 1 || summary.Failed != 0 {
		t.Fatalf("unexpected late-skip summary: %+v", summary)
	}
	if p.state.IsProcessed(sourcePath, int64(len("photo"))) {
		t.Fatal("late conflict skip marked an uncopied source as processed")
	}
}

func TestPipelineRun_SourceTruncatedAfterScanFailsWithoutPublishingOrState(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("HOME", filepath.Join(tmpDir, "home"))
	sourceDir := filepath.Join(tmpDir, "src")
	destDir := filepath.Join(tmpDir, "dest")
	if err := os.MkdirAll(sourceDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(destDir, 0755); err != nil {
		t.Fatal(err)
	}
	sourcePath := filepath.Join(sourceDir, "photo.jpg")
	original := []byte("originally-longer")
	if err := os.WriteFile(sourcePath, original, 0644); err != nil {
		t.Fatal(err)
	}

	cfg := newTestConfig(tmpDir, sourceDir, destDir)
	cfg.HashVerify = false
	p, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	p.meta = truncatingMetadataExtractor{size: int64(len("short"))}

	summary, runErr := p.Run()
	if !errors.Is(runErr, ErrRunFailed) {
		t.Fatalf("expected failed run after source truncation, got %v", runErr)
	}
	if summary == nil || summary.Failed != 1 || summary.Copied != 0 || summary.BytesCopied != 0 {
		t.Fatalf("truncated source was counted as copied: %+v", summary)
	}
	if p.state.IsProcessed(sourcePath, int64(len(original))) {
		t.Fatal("truncated source was marked processed")
	}
	destPath := filepath.Join(destDir, cfg.UnclassifiedDir, "photo.jpg")
	if _, err := os.Stat(destPath); !os.IsNotExist(err) {
		t.Fatalf("truncated source was published, stat err=%v", err)
	}
	parts, err := filepath.Glob(filepath.Join(filepath.Dir(destPath), ".photo.jpg.*.part"))
	if err != nil {
		t.Fatal(err)
	}
	if len(parts) != 0 {
		t.Fatalf("staged files were not cleaned up: %v", parts)
	}
}

func TestPipelineRun_CancelCleanupFailureIsLoggedAndRecordedAsCanceledFailure(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("HOME", filepath.Join(tmpDir, "home"))
	sourceDir := filepath.Join(tmpDir, "src")
	destDir := filepath.Join(tmpDir, "dest")
	if err := os.MkdirAll(sourceDir, 0755); err != nil {
		t.Fatalf("failed to create source: %v", err)
	}
	if err := os.MkdirAll(destDir, 0755); err != nil {
		t.Fatalf("failed to create destination: %v", err)
	}
	if err := os.WriteFile(filepath.Join(sourceDir, "photo.jpg"), []byte("photo"), 0644); err != nil {
		t.Fatalf("failed to create source file: %v", err)
	}

	cfg := newTestConfig(tmpDir, sourceDir, destDir)
	cfg.IgnoreState = true
	p, err := New(cfg)
	if err != nil {
		t.Fatalf("failed to create pipeline: %v", err)
	}
	defer p.Close()
	cleanupErr := errors.New("failed to remove task part file")
	p.copier = joinedCancellationCopier{err: errors.Join(context.Canceled, cleanupErr)}
	var failedProgress ProgressUpdate
	p.SetProgressCallback(func(update ProgressUpdate) {
		if update.Type == "progress" {
			failedProgress = update
		}
	})

	summary, err := p.RunWithContext(context.Background())
	if !errors.Is(err, ErrRunCanceled) {
		t.Fatalf("expected canceled run, got %v", err)
	}
	if summary == nil || summary.Failed != 1 || summary.Copied != 0 || summary.Renamed != 0 || summary.BytesCopied != 0 {
		t.Fatalf("expected one failed cleanup in canceled summary, got %+v", summary)
	}
	if failedProgress.Action != types.CopyActionFailed || !strings.Contains(failedProgress.Error, cleanupErr.Error()) {
		t.Fatalf("failed progress was not exposed to the UI: %+v", failedProgress)
	}

	logData, err := os.ReadFile(cfg.LogFile)
	if err != nil {
		t.Fatalf("failed to read log: %v", err)
	}
	if !strings.Contains(string(logData), cleanupErr.Error()) {
		t.Fatalf("cleanup failure missing from log: %s", logData)
	}

	historyManager, err := config.NewUserDataManager()
	if err != nil {
		t.Fatalf("failed to create history manager: %v", err)
	}
	history, err := historyManager.LoadBackupHistory()
	if err != nil {
		t.Fatalf("failed to load history: %v", err)
	}
	if len(history.Entries) == 0 || history.Entries[0].Status != types.BackupStatusCanceled || history.Entries[0].Summary.Failed != 1 {
		t.Fatalf("expected canceled history with cleanup failure, got %+v", history.Entries)
	}
}
