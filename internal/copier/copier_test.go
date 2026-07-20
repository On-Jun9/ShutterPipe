package copier

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/On-Jun9/ShutterPipe/pkg/types"
)

type stagedVerifierFunc func(context.Context, *os.File, int64, []byte) error

func (f stagedVerifierFunc) VerifyStagedFileWithContext(ctx context.Context, file *os.File, size int64, hash []byte) error {
	return f(ctx, file, size, hash)
}

type cancelAfterChecksContext struct {
	checksBeforeCancel int
	checks             int
}

type mutateAfterChecksContext struct {
	checks   int
	mutateAt int
	mutate   func()
}

func (c *mutateAfterChecksContext) Deadline() (time.Time, bool) { return time.Time{}, false }
func (c *mutateAfterChecksContext) Done() <-chan struct{}       { return nil }
func (c *mutateAfterChecksContext) Err() error {
	c.checks++
	if c.checks == c.mutateAt {
		c.mutate()
	}
	return nil
}
func (c *mutateAfterChecksContext) Value(any) any { return nil }

func (c *cancelAfterChecksContext) Deadline() (time.Time, bool) {
	return time.Time{}, false
}

func (c *cancelAfterChecksContext) Done() <-chan struct{} {
	return nil
}

func (c *cancelAfterChecksContext) Err() error {
	c.checks++
	if c.checks > c.checksBeforeCancel {
		return context.Canceled
	}
	return nil
}

func (c *cancelAfterChecksContext) Value(any) any {
	return nil
}

// TestCopierCopyAll_DryRunMarksCompletedWithoutFileIO는 테스트 코드 동작을 검증하거나 보조합니다.
func TestCopierCopyAll_DryRunMarksCompletedWithoutFileIO(t *testing.T) {
	// Dry-run 모드에서는 실제 파일 접근 없이 즉시 완료 상태가 되어야 한다.
	c := New(1, true, false)
	task := types.CopyTask{
		Source: types.FileEntry{
			Path: "/path/does/not/exist.jpg",
			Name: "missing.jpg",
		},
		DestPath: filepath.Join(t.TempDir(), "out.jpg"),
	}

	resultChan := make(chan CopyResult, 1)
	c.CopyAll(context.Background(), []types.CopyTask{task}, resultChan)
	result := <-resultChan

	if result.Error != nil {
		t.Fatalf("expected no error in dry-run, got %v", result.Error)
	}
	if result.Task.Status != types.TaskStatusCompleted {
		t.Fatalf("expected completed status, got %s", result.Task.Status)
	}
	if result.Task.Action != types.CopyActionCopied {
		t.Fatalf("expected copied action, got %s", result.Task.Action)
	}
}

func TestCopierCopyAll_DryRunPreservesPlannedConflictAction(t *testing.T) {
	c := New(1, true, false)
	task := types.CopyTask{
		Source:   types.FileEntry{Path: "/missing/photo.jpg", Name: "photo.jpg"},
		DestPath: filepath.Join(t.TempDir(), "photo_1.jpg"),
		Action:   types.CopyActionRenamed,
	}
	resultChan := make(chan CopyResult, 1)
	c.CopyAll(context.Background(), []types.CopyTask{task}, resultChan)
	result := <-resultChan
	if result.Error != nil || result.Task.Action != types.CopyActionRenamed {
		t.Fatalf("dry-run lost planned action: %+v", result)
	}
}

// TestCopierCopyAll_CopiesFileContent는 테스트 코드 동작을 검증하거나 보조합니다.
func TestCopierCopyAll_CopiesFileContent(t *testing.T) {
	// 실제 실행에서는 소스 파일이 목적지로 복사되어야 한다.
	tmpDir := t.TempDir()
	srcPath := filepath.Join(tmpDir, "src.jpg")
	destPath := filepath.Join(tmpDir, "dest", "out.jpg")

	if err := os.WriteFile(srcPath, []byte("photo-bytes"), 0644); err != nil {
		t.Fatalf("failed to create source file: %v", err)
	}

	c := New(1, false, false)
	task := types.CopyTask{
		Source: types.FileEntry{
			Path: srcPath,
			Name: "src.jpg",
			Size: int64(len("photo-bytes")),
		},
		DestPath: destPath,
	}

	resultChan := make(chan CopyResult, 1)
	c.CopyAll(context.Background(), []types.CopyTask{task}, resultChan)
	result := <-resultChan

	if result.Error != nil {
		t.Fatalf("expected no copy error, got %v", result.Error)
	}
	if result.Task.Status != types.TaskStatusCompleted {
		t.Fatalf("expected completed status, got %s", result.Task.Status)
	}

	data, err := os.ReadFile(destPath)
	if err != nil {
		t.Fatalf("failed to read destination file: %v", err)
	}
	if string(data) != "photo-bytes" {
		t.Fatalf("unexpected destination content: %q", string(data))
	}
}

func TestCopierCopyOne_RejectsTruncatedSourceWithoutHashVerification(t *testing.T) {
	tmpDir := t.TempDir()
	srcPath := filepath.Join(tmpDir, "src.jpg")
	destDir := filepath.Join(tmpDir, "dest")
	destPath := filepath.Join(destDir, "out.jpg")
	if err := os.WriteFile(srcPath, []byte("short"), 0644); err != nil {
		t.Fatal(err)
	}

	result := New(1, false, false).copyOne(context.Background(), types.CopyTask{
		Source: types.FileEntry{
			Path: srcPath,
			Name: "src.jpg",
			Size: int64(len("originally-longer")),
		},
		DestinationRoot: destDir,
		DestPath:        destPath,
	})

	if result.Error == nil || !strings.Contains(result.Error.Error(), "source changed since scan") {
		t.Fatalf("expected source size change rejection, got %+v", result)
	}
	if result.Task.Status != types.TaskStatusFailed {
		t.Fatalf("expected failed task, got %s", result.Task.Status)
	}
	if _, err := os.Stat(destPath); !os.IsNotExist(err) {
		t.Fatalf("truncated source was published, stat err=%v", err)
	}
	parts, err := filepath.Glob(filepath.Join(destDir, ".out.jpg.*.part"))
	if err != nil {
		t.Fatal(err)
	}
	if len(parts) != 0 {
		t.Fatalf("staged files were not cleaned up: %v", parts)
	}
}

func TestCopierCopyOne_RejectsSourceModifiedDuringCopy(t *testing.T) {
	tmpDir := t.TempDir()
	srcPath := filepath.Join(tmpDir, "src.bin")
	destDir := filepath.Join(tmpDir, "dest")
	destPath := filepath.Join(destDir, "out.bin")
	data := make([]byte, 2*1024*1024)
	if err := os.WriteFile(srcPath, data, 0644); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(srcPath)
	if err != nil {
		t.Fatal(err)
	}
	ctx := &mutateAfterChecksContext{mutateAt: 3}
	ctx.mutate = func() {
		file, openErr := os.OpenFile(srcPath, os.O_WRONLY, 0)
		if openErr != nil {
			t.Fatal(openErr)
		}
		defer file.Close()
		if _, writeErr := file.WriteAt([]byte("changed"), 1024*1024); writeErr != nil {
			t.Fatal(writeErr)
		}
		future := info.ModTime().Add(2 * time.Second)
		if timeErr := os.Chtimes(srcPath, future, future); timeErr != nil {
			t.Fatal(timeErr)
		}
	}

	result := New(1, false, false).copyOne(ctx, types.CopyTask{
		Source:          types.FileEntry{Path: srcPath, Name: "src.bin", Size: info.Size(), ModTime: info.ModTime()},
		DestinationRoot: destDir,
		DestPath:        destPath,
	})
	if result.Error == nil || !strings.Contains(result.Error.Error(), "source changed during copy") {
		t.Fatalf("expected source stability error, got %+v", result)
	}
	if _, err := os.Stat(destPath); !os.IsNotExist(err) {
		t.Fatalf("unstable source was published, stat err=%v", err)
	}
}

func TestCopierCopyOne_HashVerifyRechecksSourceAfterCopy(t *testing.T) {
	tmpDir := t.TempDir()
	srcPath := filepath.Join(tmpDir, "src.bin")
	destDir := filepath.Join(tmpDir, "dest")
	destPath := filepath.Join(destDir, "out.bin")
	data := make([]byte, 2*1024*1024)
	if err := os.WriteFile(srcPath, data, 0644); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(srcPath)
	if err != nil {
		t.Fatal(err)
	}
	ctx := &mutateAfterChecksContext{mutateAt: 4}
	ctx.mutate = func() {
		file, openErr := os.OpenFile(srcPath, os.O_WRONLY, 0)
		if openErr != nil {
			t.Fatal(openErr)
		}
		defer file.Close()
		if _, writeErr := file.WriteAt([]byte("changed-after-read"), 0); writeErr != nil {
			t.Fatal(writeErr)
		}
		if timeErr := os.Chtimes(srcPath, info.ModTime(), info.ModTime()); timeErr != nil {
			t.Fatal(timeErr)
		}
	}

	result := New(1, false, true).copyOne(ctx, types.CopyTask{
		Source:          types.FileEntry{Path: srcPath, Name: "src.bin", Size: info.Size(), ModTime: info.ModTime()},
		DestinationRoot: destDir,
		DestPath:        destPath,
	})
	if result.Error == nil || !strings.Contains(result.Error.Error(), "source hash changed during copy") {
		t.Fatalf("expected post-copy source hash error, got %+v", result)
	}
	if _, err := os.Stat(destPath); !os.IsNotExist(err) {
		t.Fatalf("changed source was published, stat err=%v", err)
	}
}

// TestCopierCopyAll_RemovesPartFileWhenCopyFails는 테스트 코드 동작을 검증하거나 보조합니다.
func TestCopierCopyAll_RemovesPartFileWhenCopyFails(t *testing.T) {
	// 복사 도중 실패하면 .part 임시 파일이 남지 않아야 한다.
	tmpDir := t.TempDir()
	srcDir := filepath.Join(tmpDir, "src-dir")
	if err := os.MkdirAll(srcDir, 0755); err != nil {
		t.Fatalf("failed to create source dir: %v", err)
	}

	destPath := filepath.Join(tmpDir, "dest", "out.jpg")
	partPath := destPath + ".part"

	c := New(1, false, false)
	task := types.CopyTask{
		Source: types.FileEntry{
			Path: srcDir, // 디렉터리를 파일처럼 복사해서 실패를 유도한다.
			Name: "src-dir",
		},
		DestPath: destPath,
	}

	resultChan := make(chan CopyResult, 1)
	c.CopyAll(context.Background(), []types.CopyTask{task}, resultChan)
	result := <-resultChan

	if result.Error == nil {
		t.Fatal("expected copy error")
	}
	if result.Task.Status != types.TaskStatusFailed {
		t.Fatalf("expected failed status, got %s", result.Task.Status)
	}
	if _, err := os.Stat(partPath); !os.IsNotExist(err) {
		t.Fatalf("expected no part file, stat error=%v", err)
	}
	parts, err := filepath.Glob(filepath.Join(filepath.Dir(destPath), "."+filepath.Base(destPath)+".*.part"))
	if err != nil || len(parts) != 0 {
		t.Fatalf("expected no hidden part files, parts=%v err=%v", parts, err)
	}
}

// TestCopierCopyAll_ReturnsErrorWhenDestinationDirCannotBeCreated는 테스트 코드 동작을 검증하거나 보조합니다.
func TestCopierCopyAll_ReturnsErrorWhenDestinationDirCannotBeCreated(t *testing.T) {
	// 목적지 디렉터리 생성이 불가능하면 즉시 실패 상태를 반환해야 한다.
	tmpDir := t.TempDir()
	srcPath := filepath.Join(tmpDir, "src.jpg")
	if err := os.WriteFile(srcPath, []byte("photo"), 0644); err != nil {
		t.Fatalf("failed to write source file: %v", err)
	}

	parentAsFile := filepath.Join(tmpDir, "not-dir")
	if err := os.WriteFile(parentAsFile, []byte("x"), 0644); err != nil {
		t.Fatalf("failed to create parent blocker file: %v", err)
	}

	task := types.CopyTask{
		Source: types.FileEntry{
			Path: srcPath,
			Name: "src.jpg",
		},
		DestPath: filepath.Join(parentAsFile, "out.jpg"),
	}

	c := New(1, false, false)
	resultChan := make(chan CopyResult, 1)
	c.CopyAll(context.Background(), []types.CopyTask{task}, resultChan)
	result := <-resultChan

	if result.Error == nil {
		t.Fatal("expected mkdirall error")
	}
	if result.Task.Status != types.TaskStatusFailed {
		t.Fatalf("expected failed status, got %s", result.Task.Status)
	}
}

// TestCopierCopyAll_ReturnsErrorWhenSourceOpenFails는 테스트 코드 동작을 검증하거나 보조합니다.
func TestCopierCopyAll_ReturnsErrorWhenSourceOpenFails(t *testing.T) {
	// source 파일 오픈이 실패하면 copy는 실패 상태여야 한다.
	tmpDir := t.TempDir()
	destPath := filepath.Join(tmpDir, "dest", "out.jpg")

	task := types.CopyTask{
		Source: types.FileEntry{
			Path: filepath.Join(tmpDir, "missing.jpg"),
			Name: "missing.jpg",
		},
		DestPath: destPath,
	}

	c := New(1, false, false)
	resultChan := make(chan CopyResult, 1)
	c.CopyAll(context.Background(), []types.CopyTask{task}, resultChan)
	result := <-resultChan

	if result.Error == nil {
		t.Fatal("expected source open error")
	}
	if result.Task.Status != types.TaskStatusFailed {
		t.Fatalf("expected failed status, got %s", result.Task.Status)
	}
}

// TestCopierCopyAll_CancelledContextStopsBeforeScheduling는 테스트 코드 동작을 검증하거나 보조합니다.
func TestCopierCopyAll_CancelledContextStopsBeforeScheduling(t *testing.T) {
	// 컨텍스트가 이미 취소된 경우 작업을 스케줄링하지 않고 종료되어야 한다.
	tmpDir := t.TempDir()
	srcPath := filepath.Join(tmpDir, "src.jpg")
	destPath := filepath.Join(tmpDir, "dest", "out.jpg")

	if err := os.WriteFile(srcPath, []byte("photo"), 0644); err != nil {
		t.Fatalf("failed to write source file: %v", err)
	}

	task := types.CopyTask{
		Source: types.FileEntry{
			Path: srcPath,
			Name: "src.jpg",
		},
		DestPath: destPath,
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	c := New(1, false, false)
	resultChan := make(chan CopyResult, 1)
	c.CopyAll(ctx, []types.CopyTask{task}, resultChan)

	if result, ok := <-resultChan; ok {
		t.Fatalf("expected closed result channel without result, got %+v", result)
	}
	if _, err := os.Stat(destPath); !os.IsNotExist(err) {
		t.Fatalf("expected destination not to be created, stat err=%v", err)
	}
}

func TestCopierCopyOne_CancelDuringCopyRemovesPartFile(t *testing.T) {
	tmpDir := t.TempDir()
	srcPath := filepath.Join(tmpDir, "src.bin")
	destPath := filepath.Join(tmpDir, "dest", "out.bin")
	partPath := destPath + ".part"

	data := make([]byte, 2*1024*1024)
	if err := os.WriteFile(srcPath, data, 0644); err != nil {
		t.Fatalf("failed to write source file: %v", err)
	}

	ctx := &cancelAfterChecksContext{checksBeforeCancel: 2}
	c := New(1, false, false)
	c.partFileFactory = func(string) (*os.File, error) {
		return os.OpenFile(partPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	}
	result := c.copyOne(ctx, types.CopyTask{
		Source: types.FileEntry{
			Path: srcPath,
			Name: "src.bin",
			Size: int64(len(data)),
		},
		DestPath: destPath,
	})

	if !errors.Is(result.Error, context.Canceled) {
		t.Fatalf("expected context.Canceled, got %v", result.Error)
	}
	if _, err := os.Stat(partPath); !os.IsNotExist(err) {
		t.Fatalf("expected part file to be removed, stat err=%v", err)
	}
	if _, err := os.Stat(destPath); !os.IsNotExist(err) {
		t.Fatalf("expected destination not to be created, stat err=%v", err)
	}
}

func TestCopierCopyOne_CancelBeforeRenameDoesNotCreateFinalFile(t *testing.T) {
	tmpDir := t.TempDir()
	srcPath := filepath.Join(tmpDir, "src.bin")
	destPath := filepath.Join(tmpDir, "dest", "out.bin")
	partPath := destPath + ".part"

	if err := os.WriteFile(srcPath, []byte("photo-bytes"), 0644); err != nil {
		t.Fatalf("failed to write source file: %v", err)
	}

	// The first three checks allow copyOne and the read loop to finish. The next
	// check must happen immediately before the final rename.
	ctx := &cancelAfterChecksContext{checksBeforeCancel: 3}
	c := New(1, false, false)
	c.partFileFactory = func(string) (*os.File, error) {
		return os.OpenFile(partPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	}
	result := c.copyOne(ctx, types.CopyTask{
		Source: types.FileEntry{
			Path: srcPath,
			Name: "src.bin",
			Size: int64(len("photo-bytes")),
		},
		DestPath: destPath,
	})

	if !errors.Is(result.Error, context.Canceled) {
		t.Fatalf("expected context.Canceled before rename, got %v", result.Error)
	}
	if _, err := os.Stat(partPath); !os.IsNotExist(err) {
		t.Fatalf("expected part file to be removed, stat err=%v", err)
	}
	if _, err := os.Stat(destPath); !os.IsNotExist(err) {
		t.Fatalf("expected final file not to be created, stat err=%v", err)
	}
}

func TestCopierCopyOne_ReturnsCopyAndPartCleanupErrors(t *testing.T) {
	tmpDir := t.TempDir()
	srcPath := filepath.Join(tmpDir, "src.bin")
	destPath := filepath.Join(tmpDir, "dest", "out.bin")
	partPath := destPath + ".part"

	if err := os.WriteFile(srcPath, []byte("photo-bytes"), 0644); err != nil {
		t.Fatalf("failed to write source file: %v", err)
	}
	if err := os.MkdirAll(partPath, 0755); err != nil {
		t.Fatalf("failed to create blocking part directory: %v", err)
	}
	if err := os.WriteFile(filepath.Join(partPath, "keep"), []byte("x"), 0644); err != nil {
		t.Fatalf("failed to make part directory non-empty: %v", err)
	}

	expectedCleanupErr := os.Remove(partPath)
	if expectedCleanupErr == nil {
		t.Fatal("expected setup operations to fail")
	}

	c := New(1, false, false)
	c.partFileFactory = func(string) (*os.File, error) { return os.Open(partPath) }
	result := c.copyOne(context.Background(), types.CopyTask{
		Source:   types.FileEntry{Path: srcPath, Name: "src.bin"},
		DestPath: destPath,
	})

	if result.Error == nil {
		t.Fatal("expected copy and cleanup error")
	}
	joined, ok := result.Error.(interface{ Unwrap() []error })
	if !ok || len(joined.Unwrap()) != 2 {
		t.Fatalf("expected joined copy and cleanup errors, got %T: %v", result.Error, result.Error)
	}
	if !strings.Contains(result.Error.Error(), "directory not empty") {
		t.Fatalf("expected part cleanup error equivalent to %q, got %v", expectedCleanupErr, result.Error)
	}
}

func TestCopierPartPathsAreUniquePerTask(t *testing.T) {
	tmpDir := t.TempDir()
	destPath := filepath.Join(tmpDir, "photo.jpg")
	root, err := os.OpenRoot(tmpDir)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()

	first, firstPath, err := newPartFileInRoot(root, filepath.Base(destPath))
	if err != nil {
		t.Fatalf("failed to create first part path: %v", err)
	}
	defer first.Close()
	defer root.Remove(firstPath)
	second, secondPath, err := newPartFileInRoot(root, filepath.Base(destPath))
	if err != nil {
		t.Fatalf("failed to create second part path: %v", err)
	}
	defer second.Close()
	defer root.Remove(secondPath)

	if firstPath == secondPath {
		t.Fatalf("expected task-specific part paths, both were %s", firstPath)
	}
}

func TestCopierSkipPolicyDoesNotReplaceLateDestination(t *testing.T) {
	tmpDir := t.TempDir()
	srcPath := filepath.Join(tmpDir, "src.jpg")
	destPath := filepath.Join(tmpDir, "dest", "photo.jpg")
	if err := os.WriteFile(srcPath, []byte("source"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(destPath), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(destPath, []byte("late-external-file"), 0644); err != nil {
		t.Fatal(err)
	}

	result := New(1, false, false).copyOne(context.Background(), types.CopyTask{
		Source:         types.FileEntry{Path: srcPath, Name: "src.jpg", Size: int64(len("source"))},
		DestPath:       destPath,
		Action:         types.CopyActionCopied,
		ConflictPolicy: types.ConflictPolicySkip,
	})
	if result.Error != nil || result.Task.Action != types.CopyActionSkipped || result.Task.Status != types.TaskStatusSkipped {
		t.Fatalf("expected late conflict to be skipped, got %+v", result)
	}
	data, err := os.ReadFile(destPath)
	if err != nil || string(data) != "late-external-file" {
		t.Fatalf("late destination was overwritten: data=%q err=%v", data, err)
	}
}

func TestCopierRenamedActionRetriesLateDestinationCollision(t *testing.T) {
	tmpDir := t.TempDir()
	srcPath := filepath.Join(tmpDir, "src.jpg")
	destPath := filepath.Join(tmpDir, "dest", "photo_1.jpg")
	if err := os.WriteFile(srcPath, []byte("source"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(destPath), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(destPath, []byte("late-external-file"), 0644); err != nil {
		t.Fatal(err)
	}

	result := New(1, false, false).copyOne(context.Background(), types.CopyTask{
		Source:         types.FileEntry{Path: srcPath, Name: "src.jpg", Size: int64(len("source"))},
		DestPath:       destPath,
		Action:         types.CopyActionRenamed,
		ConflictPolicy: types.ConflictPolicyRename,
	})
	if result.Error != nil {
		t.Fatalf("rename retry failed: %v", result.Error)
	}
	expected := filepath.Join(filepath.Dir(destPath), "photo_1_1.jpg")
	if result.Task.DestPath != expected {
		t.Fatalf("expected committed retry path %s, got %s", expected, result.Task.DestPath)
	}
	data, err := os.ReadFile(expected)
	if err != nil || string(data) != "source" {
		t.Fatalf("renamed source missing: data=%q err=%v", data, err)
	}
	lateData, err := os.ReadFile(destPath)
	if err != nil || string(lateData) != "late-external-file" {
		t.Fatalf("late destination was overwritten: data=%q err=%v", lateData, err)
	}
}

func TestCopierOverwritePolicyReplacesLateDestination(t *testing.T) {
	tmpDir := t.TempDir()
	srcPath := filepath.Join(tmpDir, "src.jpg")
	destPath := filepath.Join(tmpDir, "dest", "photo.jpg")
	if err := os.WriteFile(srcPath, []byte("source"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(destPath), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(destPath, []byte("late-external-file"), 0644); err != nil {
		t.Fatal(err)
	}

	result := New(1, false, false).copyOne(context.Background(), types.CopyTask{
		Source:         types.FileEntry{Path: srcPath, Name: "src.jpg", Size: int64(len("source"))},
		DestPath:       destPath,
		Action:         types.CopyActionCopied,
		ConflictPolicy: types.ConflictPolicyOverwrite,
	})
	if result.Error != nil || result.Task.Action != types.CopyActionOverwritten {
		t.Fatalf("late overwrite failed: %+v", result)
	}
	data, err := os.ReadFile(destPath)
	if err != nil || string(data) != "source" {
		t.Fatalf("late destination was not overwritten: data=%q err=%v", data, err)
	}
}

func TestCopierQuarantinePolicyMovesLateConflictWithoutReplacingEitherFile(t *testing.T) {
	tmpDir := t.TempDir()
	srcPath := filepath.Join(tmpDir, "src.jpg")
	destPath := filepath.Join(tmpDir, "dest", "photo.jpg")
	quarantineDir := filepath.Join(tmpDir, "quarantine")
	if err := os.WriteFile(srcPath, []byte("source"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(destPath), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(destPath, []byte("late-external-file"), 0644); err != nil {
		t.Fatal(err)
	}

	result := New(1, false, false).copyOne(context.Background(), types.CopyTask{
		Source:          types.FileEntry{Path: srcPath, Name: "src.jpg", Size: int64(len("source"))},
		DestinationRoot: tmpDir,
		DestPath:        destPath,
		Action:          types.CopyActionCopied,
		ConflictPolicy:  types.ConflictPolicyQuarantine,
		QuarantineDir:   quarantineDir,
	})
	if result.Error != nil || result.Task.Action != types.CopyActionQuarantined {
		t.Fatalf("late quarantine failed: %+v", result)
	}
	expected := filepath.Join(quarantineDir, "src.jpg")
	if result.Task.DestPath != expected {
		t.Fatalf("expected quarantine destination %s, got %s", expected, result.Task.DestPath)
	}
	data, err := os.ReadFile(expected)
	if err != nil || string(data) != "source" {
		t.Fatalf("quarantined source missing: data=%q err=%v", data, err)
	}
	lateData, err := os.ReadFile(destPath)
	if err != nil || string(lateData) != "late-external-file" {
		t.Fatalf("late destination was overwritten: data=%q err=%v", lateData, err)
	}
}

func TestCopierHashVerifyPublishesOnlyVerifiedPart(t *testing.T) {
	tmpDir := t.TempDir()
	sourceData := []byte("verified-content")
	srcPath := filepath.Join(tmpDir, "source.jpg")
	destPath := filepath.Join(tmpDir, "dest", "photo.jpg")
	if err := os.WriteFile(srcPath, sourceData, 0644); err != nil {
		t.Fatal(err)
	}

	c := New(1, false, true)
	result := c.copyOne(context.Background(), types.CopyTask{
		Source:   types.FileEntry{Path: srcPath, Name: "source.jpg", Size: int64(len(sourceData))},
		DestPath: destPath, Action: types.CopyActionCopied, ConflictPolicy: types.ConflictPolicySkip,
	})
	if result.Error != nil {
		t.Fatalf("verified copy failed: %v", result.Error)
	}
	expectedHash := sha256.Sum256(sourceData)
	if !bytes.Equal(result.VerifiedSourceHash, expectedHash[:]) {
		t.Fatalf("copy result did not preserve verified source hash: got=%x want=%x", result.VerifiedSourceHash, expectedHash)
	}
	data, err := os.ReadFile(destPath)
	if err != nil || string(data) != string(sourceData) {
		t.Fatalf("verified final file missing: data=%q err=%v", data, err)
	}
}

func TestCopierHashVerifyFailureRemovesPartAndPreservesFinal(t *testing.T) {
	for _, verifyErr := range []error{errors.New("hash mismatch"), context.Canceled} {
		t.Run(verifyErr.Error(), func(t *testing.T) {
			tmpDir := t.TempDir()
			sourceData := []byte("source-content")
			srcPath := filepath.Join(tmpDir, "source.jpg")
			destPath := filepath.Join(tmpDir, "dest", "photo.jpg")
			if err := os.WriteFile(srcPath, sourceData, 0644); err != nil {
				t.Fatal(err)
			}

			c := New(1, false, true)
			c.verifier = stagedVerifierFunc(func(context.Context, *os.File, int64, []byte) error { return verifyErr })
			result := c.copyOne(context.Background(), types.CopyTask{
				Source:   types.FileEntry{Path: srcPath, Name: "source.jpg", Size: int64(len(sourceData))},
				DestPath: destPath, Action: types.CopyActionCopied, ConflictPolicy: types.ConflictPolicySkip,
			})
			if !errors.Is(result.Error, verifyErr) {
				t.Fatalf("expected verification error %v, got %v", verifyErr, result.Error)
			}
			if _, err := os.Stat(destPath); !os.IsNotExist(err) {
				t.Fatalf("unverified final file was published: %v", err)
			}
			parts, err := filepath.Glob(filepath.Join(filepath.Dir(destPath), ".photo.jpg.*.part"))
			if err != nil || len(parts) != 0 {
				t.Fatalf("unverified part was not cleaned: parts=%v err=%v", parts, err)
			}
		})
	}
}

func TestCopierRejectsDestinationSubdirSymlinkEscape(t *testing.T) {
	tmpDir := t.TempDir()
	root := filepath.Join(tmpDir, "destination")
	outside := filepath.Join(tmpDir, "outside")
	if err := os.MkdirAll(root, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(outside, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "unclassified")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	source := filepath.Join(tmpDir, "source.jpg")
	if err := os.WriteFile(source, []byte("source"), 0644); err != nil {
		t.Fatal(err)
	}
	result := New(1, false, false).copyOne(context.Background(), types.CopyTask{
		Source:   types.FileEntry{Path: source, Name: "source.jpg", Size: 6},
		DestPath: filepath.Join(root, "unclassified", "source.jpg"), DestinationRoot: root,
		ConflictPolicy: types.ConflictPolicySkip, Action: types.CopyActionCopied,
	})
	if result.Error == nil || !strings.Contains(result.Error.Error(), "escapes configured root") {
		t.Fatalf("expected symlink escape rejection, got %v", result.Error)
	}
	if _, err := os.Stat(filepath.Join(outside, "source.jpg")); !os.IsNotExist(err) {
		t.Fatalf("file escaped destination root: %v", err)
	}
}

func TestCopierRejectsDestinationSymlinkSwapAfterRootOpen(t *testing.T) {
	tmpDir := t.TempDir()
	root := filepath.Join(tmpDir, "destination")
	destinationDir := filepath.Join(root, "unclassified")
	movedDir := filepath.Join(root, "moved")
	outside := filepath.Join(tmpDir, "outside")
	for _, dir := range []string{destinationDir, outside} {
		if err := os.MkdirAll(dir, 0755); err != nil {
			t.Fatal(err)
		}
	}
	source := filepath.Join(tmpDir, "source.jpg")
	if err := os.WriteFile(source, []byte("source"), 0644); err != nil {
		t.Fatal(err)
	}

	copier := New(1, false, false)
	copier.beforeDestinationCreate = func(types.CopyTask) {
		if err := os.Rename(destinationDir, movedDir); err != nil {
			t.Fatalf("failed to move validated destination directory: %v", err)
		}
		if err := os.Symlink(outside, destinationDir); err != nil {
			t.Skipf("symlinks unavailable: %v", err)
		}
	}
	result := copier.copyOne(context.Background(), types.CopyTask{
		Source:          types.FileEntry{Path: source, Name: "source.jpg", Size: 6},
		DestPath:        filepath.Join(destinationDir, "source.jpg"),
		DestinationRoot: root,
		ConflictPolicy:  types.ConflictPolicySkip,
		Action:          types.CopyActionCopied,
	})

	if result.Error == nil || !strings.Contains(result.Error.Error(), "escapes configured root") {
		t.Fatalf("expected post-open symlink swap rejection, got %v", result.Error)
	}
	if _, err := os.Stat(filepath.Join(outside, "source.jpg")); !os.IsNotExist(err) {
		t.Fatalf("file escaped destination root after symlink swap: %v", err)
	}
}

func TestCopierRejectsSymlinkEscapeBeforeCreatingOutsideDirectories(t *testing.T) {
	tmpDir := t.TempDir()
	root := filepath.Join(tmpDir, "destination")
	outside := filepath.Join(tmpDir, "outside")
	if err := os.MkdirAll(root, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(outside, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "link")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	source := filepath.Join(tmpDir, "source.jpg")
	if err := os.WriteFile(source, []byte("source"), 0644); err != nil {
		t.Fatal(err)
	}

	result := New(1, false, false).copyOne(context.Background(), types.CopyTask{
		Source:          types.FileEntry{Path: source, Name: "source.jpg", Size: 6},
		DestPath:        filepath.Join(root, "link", "new", "subdir", "source.jpg"),
		DestinationRoot: root,
		ConflictPolicy:  types.ConflictPolicySkip,
		Action:          types.CopyActionCopied,
	})
	if result.Error == nil || !strings.Contains(result.Error.Error(), "escapes configured root") {
		t.Fatalf("expected pre-create symlink escape rejection, got %v", result.Error)
	}
	if _, err := os.Stat(filepath.Join(outside, "new")); !os.IsNotExist(err) {
		t.Fatalf("outside directory was created before containment rejection: %v", err)
	}
}

func TestCopierRejectsQuarantineSymlinkEscapeBeforeCreatingOutsideDirectories(t *testing.T) {
	tmpDir := t.TempDir()
	root := filepath.Join(tmpDir, "destination")
	outside := filepath.Join(tmpDir, "outside")
	if err := os.MkdirAll(root, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(outside, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "link")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	source := filepath.Join(tmpDir, "source.jpg")
	if err := os.WriteFile(source, []byte("source"), 0644); err != nil {
		t.Fatal(err)
	}
	destPath := filepath.Join(root, "source.jpg")
	if err := os.WriteFile(destPath, []byte("existing"), 0644); err != nil {
		t.Fatal(err)
	}

	result := New(1, false, false).copyOne(context.Background(), types.CopyTask{
		Source:          types.FileEntry{Path: source, Name: "source.jpg", Size: 6},
		DestPath:        destPath,
		DestinationRoot: root,
		QuarantineDir:   filepath.Join(root, "link", "new", "subdir"),
		ConflictPolicy:  types.ConflictPolicyQuarantine,
		Action:          types.CopyActionQuarantined,
	})
	if result.Error == nil || !strings.Contains(result.Error.Error(), "escapes configured root") {
		t.Fatalf("expected quarantine symlink escape rejection, got %v", result.Error)
	}
	if _, err := os.Stat(filepath.Join(outside, "new")); !os.IsNotExist(err) {
		t.Fatalf("outside quarantine directory was created before rejection: %v", err)
	}
}

func TestMovePartNoReplaceInRootPreservesNoClobber(t *testing.T) {
	dir := t.TempDir()
	root, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()

	// 신규 대상: part를 finalDest로 publish하고 part는 제거되어야 한다.
	partPath := ".photo.part"
	finalDest := "photo.jpg"
	if err := root.WriteFile(partPath, []byte("new"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := movePartNoReplaceInRoot(root, partPath, finalDest); err != nil {
		t.Fatalf("신규 대상 publish 실패: %v", err)
	}
	if _, err := root.Stat(partPath); !os.IsNotExist(err) {
		t.Fatalf("publish 후 part 파일이 남아 있음: %v", err)
	}
	if data, err := root.ReadFile(finalDest); err != nil || string(data) != "new" {
		t.Fatalf("publish 내용 불일치: data=%q err=%v", data, err)
	}

	// 기존 파일 존재: 덮어쓰지 않고 os.ErrExist를 반환해야 한다.
	part2 := ".photo2.part"
	if err := root.WriteFile(part2, []byte("intruder"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := movePartNoReplaceInRoot(root, part2, finalDest); !errors.Is(err, os.ErrExist) {
		t.Fatalf("기존 파일이 있을 때 os.ErrExist를 기대했으나 got %v", err)
	}
	if data, err := root.ReadFile(finalDest); err != nil || string(data) != "new" {
		t.Fatalf("기존 파일이 덮어써짐: data=%q err=%v", data, err)
	}
}
