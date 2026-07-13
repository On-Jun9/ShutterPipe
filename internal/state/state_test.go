package state

import (
	"crypto/sha256"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/On-Jun9/ShutterPipe/pkg/types"
)

// TestLoad_ReturnsEmptyStateWhenFileMissing는 테스트 코드 동작을 검증하거나 보조합니다.
func TestLoad_ReturnsEmptyStateWhenFileMissing(t *testing.T) {
	// 상태 파일이 없으면 에러 대신 빈 상태가 반환되어야 한다.
	filePath := filepath.Join(t.TempDir(), "state", "state.json")

	st, err := Load(filePath)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if st == nil {
		t.Fatal("expected non-nil state")
	}
	if len(st.Processed) != 0 {
		t.Fatalf("expected empty processed map, got %d", len(st.Processed))
	}
}

// TestStateMarkProcessedAndIsProcessed는 테스트 코드 동작을 검증하거나 보조합니다.
func TestStateMarkProcessedAndIsProcessed(t *testing.T) {
	// MarkProcessed 후 동일 경로/크기에 대해 IsProcessed가 true를 반환해야 한다.
	st := New(filepath.Join(t.TempDir(), "state.json"))
	st.MarkProcessed("/src/a.jpg", 123, "/dest/a.jpg")

	if !st.IsProcessed("/src/a.jpg", 123) {
		t.Fatal("expected file to be marked as processed")
	}
	if st.IsProcessed("/src/a.jpg", 124) {
		t.Fatal("expected size mismatch to return false")
	}
	if st.LastRun.IsZero() {
		t.Fatal("expected LastRun to be set")
	}
}

// TestStateSaveAndLoad_RoundTrip는 테스트 코드 동작을 검증하거나 보조합니다.
func TestStateSaveAndLoad_RoundTrip(t *testing.T) {
	// 저장한 상태를 다시 로드했을 때 핵심 정보가 유지되어야 한다.
	filePath := filepath.Join(t.TempDir(), "nested", "state.json")
	st := New(filePath)
	st.MarkProcessed("/src/a.jpg", 321, "/dest/a.jpg")

	if err := st.Save(); err != nil {
		t.Fatalf("failed to save state: %v", err)
	}

	loaded, err := Load(filePath)
	if err != nil {
		t.Fatalf("failed to load state: %v", err)
	}
	if !loaded.IsProcessed("/src/a.jpg", 321) {
		t.Fatal("expected loaded state to include processed file")
	}
	if loaded.Processed["/src/a.jpg"].DestPath != "/dest/a.jpg" {
		t.Fatalf("unexpected dest path: %s", loaded.Processed["/src/a.jpg"].DestPath)
	}
}

// TestLoad_ReturnsErrorOnInvalidJSON는 테스트 코드 동작을 검증하거나 보조합니다.
func TestLoad_ReturnsErrorOnInvalidJSON(t *testing.T) {
	// 상태 파일 JSON이 깨져 있으면 Load는 에러를 반환해야 한다.
	filePath := filepath.Join(t.TempDir(), "state.json")
	if err := os.WriteFile(filePath, []byte("{"), 0644); err != nil {
		t.Fatalf("failed to write broken state file: %v", err)
	}

	_, err := Load(filePath)
	if err == nil {
		t.Fatal("expected unmarshal error")
	}
}

// TestLoad_ReturnsErrorOnReadFailure는 테스트 코드 동작을 검증하거나 보조합니다.
func TestLoad_ReturnsErrorOnReadFailure(t *testing.T) {
	// 파일 대신 디렉터리 경로를 읽으면 Load는 read 에러를 반환해야 한다.
	dirPath := filepath.Join(t.TempDir(), "state-dir")
	if err := os.MkdirAll(dirPath, 0755); err != nil {
		t.Fatalf("failed to create dir path: %v", err)
	}

	_, err := Load(dirPath)
	if err == nil {
		t.Fatal("expected read error when loading from directory path")
	}
}

// TestStateSave_ReturnsErrorWhenParentIsFile는 테스트 코드 동작을 검증하거나 보조합니다.
func TestStateSave_ReturnsErrorWhenParentIsFile(t *testing.T) {
	// 부모 경로가 파일이면 Save의 MkdirAll 단계에서 실패해야 한다.
	tmpDir := t.TempDir()
	parentAsFile := filepath.Join(tmpDir, "not-dir")
	if err := os.WriteFile(parentAsFile, []byte("x"), 0644); err != nil {
		t.Fatalf("failed to create blocking file: %v", err)
	}

	st := New(filepath.Join(parentAsFile, "state.json"))
	st.MarkProcessed("/src/a.jpg", 1, "/dest/a.jpg")

	if err := st.Save(); err == nil {
		t.Fatal("expected save error")
	}
}

func TestStateEntryIdentityDetectsSameSizeSourceAndDestinationChanges(t *testing.T) {
	tmpDir := t.TempDir()
	sourcePath := filepath.Join(tmpDir, "source.jpg")
	destPath := filepath.Join(tmpDir, "dest.jpg")
	if err := os.WriteFile(sourcePath, []byte("source"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(destPath, []byte("source"), 0644); err != nil {
		t.Fatal(err)
	}
	sourceInfo, err := os.Stat(sourcePath)
	if err != nil {
		t.Fatal(err)
	}
	entry := types.FileEntry{Path: sourcePath, Size: sourceInfo.Size(), ModTime: sourceInfo.ModTime()}
	st := New(filepath.Join(tmpDir, "state.json"))
	if err := st.MarkProcessedEntry(entry, destPath, false); err != nil {
		t.Fatal(err)
	}
	if !st.IsEntryProcessed(entry, false) {
		t.Fatal("fresh source and destination were not recognized")
	}

	if err := os.WriteFile(sourcePath, []byte("change"), 0644); err != nil {
		t.Fatal(err)
	}
	future := sourceInfo.ModTime().Add(2 * time.Second)
	if err := os.Chtimes(sourcePath, future, future); err != nil {
		t.Fatal(err)
	}
	changedSource := entry
	changedSource.ModTime = future
	if st.IsEntryProcessed(changedSource, false) {
		t.Fatal("same-size source modification remained processed")
	}

	if err := os.Chtimes(sourcePath, entry.ModTime, entry.ModTime); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(destPath, []byte("damage"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(destPath, future, future); err != nil {
		t.Fatal(err)
	}
	if st.IsEntryProcessed(entry, false) {
		t.Fatal("same-size destination modification remained processed")
	}
}

func TestStateHashIdentityDetectsPreservedTimestampContentChange(t *testing.T) {
	tmpDir := t.TempDir()
	sourcePath := filepath.Join(tmpDir, "source.jpg")
	destPath := filepath.Join(tmpDir, "dest.jpg")
	if err := os.WriteFile(sourcePath, []byte("source"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(destPath, []byte("source"), 0644); err != nil {
		t.Fatal(err)
	}
	sourceInfo, err := os.Stat(sourcePath)
	if err != nil {
		t.Fatal(err)
	}
	entry := types.FileEntry{Path: sourcePath, Size: sourceInfo.Size(), ModTime: sourceInfo.ModTime()}
	st := New(filepath.Join(tmpDir, "state.json"))
	if err := st.MarkProcessedEntry(entry, destPath, true); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(sourcePath, []byte("change"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(sourcePath, entry.ModTime, entry.ModTime); err != nil {
		t.Fatal(err)
	}
	if st.IsEntryProcessed(entry, true) {
		t.Fatal("hash mode missed same-size source content change with preserved mtime")
	}
}

func TestStateVerifiedHashRejectsSourceChangedBeforeCommitWithPreservedIdentity(t *testing.T) {
	tmpDir := t.TempDir()
	sourcePath := filepath.Join(tmpDir, "source.jpg")
	destPath := filepath.Join(tmpDir, "dest.jpg")
	original := []byte("AAAA")
	if err := os.WriteFile(sourcePath, original, 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(destPath, original, 0644); err != nil {
		t.Fatal(err)
	}
	sourceInfo, err := os.Stat(sourcePath)
	if err != nil {
		t.Fatal(err)
	}
	entry := types.FileEntry{Path: sourcePath, Size: sourceInfo.Size(), ModTime: sourceInfo.ModTime()}
	verifiedHash := sha256.Sum256(original)

	if err := os.WriteFile(sourcePath, []byte("BBBB"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(sourcePath, entry.ModTime, entry.ModTime); err != nil {
		t.Fatal(err)
	}

	st := New(filepath.Join(tmpDir, "state.json"))
	err = st.MarkProcessedEntryWithVerifiedHash(entry, destPath, verifiedHash[:])
	if err == nil || !strings.Contains(err.Error(), "verified snapshot changed") {
		t.Fatalf("expected verified snapshot mismatch, got %v", err)
	}
	if len(st.Processed) != 0 {
		t.Fatalf("mismatched snapshot was recorded: %#v", st.Processed)
	}
}

func TestStateHashCommitAllowsMismatchOnlyForExplicitOverwriteSupersession(t *testing.T) {
	tmpDir := t.TempDir()
	sourcePath := filepath.Join(tmpDir, "source.jpg")
	destPath := filepath.Join(tmpDir, "dest.jpg")
	if err := os.WriteFile(sourcePath, []byte("AAAA"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(destPath, []byte("BBBB"), 0644); err != nil {
		t.Fatal(err)
	}
	sourceInfo, err := os.Stat(sourcePath)
	if err != nil {
		t.Fatal(err)
	}
	entry := types.FileEntry{Path: sourcePath, Size: sourceInfo.Size(), ModTime: sourceInfo.ModTime()}
	st := New(filepath.Join(tmpDir, "state.json"))
	if err := st.MarkProcessedEntry(entry, destPath, true); err == nil || !strings.Contains(err.Error(), "source and destination differ") {
		t.Fatalf("ordinary hash state commit accepted different content: %v", err)
	}
	if err := st.MarkSupersededEntry(entry, destPath, true); err != nil {
		t.Fatalf("explicit overwrite supersession was rejected: %v", err)
	}
	if record := st.Processed[sourcePath]; !record.Superseded {
		t.Fatalf("supersession semantics were not persisted: %#v", record)
	}
}

func TestStateLegacyRecordIsDirtyForIdentityMigration(t *testing.T) {
	st := New(filepath.Join(t.TempDir(), "state.json"))
	st.MarkProcessed("/src/legacy.jpg", 7, "/dest/legacy.jpg")
	entry := types.FileEntry{Path: "/src/legacy.jpg", Size: 7, ModTime: time.Now()}
	if st.IsEntryProcessed(entry, false) {
		t.Fatal("legacy path-size record bypassed identity migration")
	}
}

func TestStateSaveUsesAtomicTemporaryFile(t *testing.T) {
	dir := t.TempDir()
	filePath := filepath.Join(dir, "state.json")
	st := New(filePath)
	st.MarkProcessed("/src/a.jpg", 1, "/dest/a.jpg")
	if err := st.Save(); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(filePath); err != nil {
		t.Fatalf("atomic state was not readable: %v", err)
	}
	temps, err := filepath.Glob(filepath.Join(dir, ".state.json.*.tmp"))
	if err != nil {
		t.Fatal(err)
	}
	if len(temps) != 0 {
		t.Fatalf("temporary state files were left behind: %v", temps)
	}
}
