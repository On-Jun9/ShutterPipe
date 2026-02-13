package copier

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/On-Jun9/ShutterPipe/pkg/types"
)

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
	c.CopyAll([]types.CopyTask{task}, resultChan)
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
		},
		DestPath: destPath,
	}

	resultChan := make(chan CopyResult, 1)
	c.CopyAll([]types.CopyTask{task}, resultChan)
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
	c.CopyAll([]types.CopyTask{task}, resultChan)
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
	c.CopyAll([]types.CopyTask{task}, resultChan)
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
	c.CopyAll([]types.CopyTask{task}, resultChan)
	result := <-resultChan

	if result.Error == nil {
		t.Fatal("expected source open error")
	}
	if result.Task.Status != types.TaskStatusFailed {
		t.Fatalf("expected failed status, got %s", result.Task.Status)
	}
}

// TestCopierAtomicCopy_ReturnsErrorWhenDestinationCreateFails는 테스트 코드 동작을 검증하거나 보조합니다.
func TestCopierAtomicCopy_ReturnsErrorWhenDestinationCreateFails(t *testing.T) {
	// atomicCopy에서 destination 생성이 실패하면 에러를 반환해야 한다.
	tmpDir := t.TempDir()
	srcPath := filepath.Join(tmpDir, "src.jpg")
	if err := os.WriteFile(srcPath, []byte("photo"), 0644); err != nil {
		t.Fatalf("failed to write source file: %v", err)
	}

	c := New(1, false, false)
	err := c.atomicCopy(srcPath, "\x00", filepath.Join(tmpDir, "out.jpg"))
	if err == nil {
		t.Fatal("expected destination create error")
	}
}
