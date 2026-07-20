package state

import (
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
	if err := st.MarkProcessedEntry(context.Background(), entry, destPath, false, SourceContext{}); err != nil {
		t.Fatal(err)
	}
	if !st.IsEntryProcessed(context.Background(), entry, false, SourceContext{}) {
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
	if st.IsEntryProcessed(context.Background(), changedSource, false, SourceContext{}) {
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
	if st.IsEntryProcessed(context.Background(), entry, false, SourceContext{}) {
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
	if err := st.MarkProcessedEntry(context.Background(), entry, destPath, true, SourceContext{}); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(sourcePath, []byte("change"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(sourcePath, entry.ModTime, entry.ModTime); err != nil {
		t.Fatal(err)
	}
	if st.IsEntryProcessed(context.Background(), entry, true, SourceContext{}) {
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
	err = st.MarkProcessedEntryWithVerifiedHash(context.Background(), entry, destPath, verifiedHash[:], SourceContext{})
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
	if err := st.MarkProcessedEntry(context.Background(), entry, destPath, true, SourceContext{}); err == nil || !strings.Contains(err.Error(), "source and destination differ") {
		t.Fatalf("ordinary hash state commit accepted different content: %v", err)
	}
	if err := st.MarkSupersededEntry(context.Background(), entry, destPath, true, SourceContext{}); err != nil {
		t.Fatalf("explicit overwrite supersession was rejected: %v", err)
	}
	if record := st.Processed[sourcePath]; !record.Superseded {
		t.Fatalf("supersession semantics were not persisted: %#v", record)
	}
}

// TestStateConfigFingerprintChangeForcesReprocess는 목적지/분류 설정이 바뀌면
// 과거 레코드가 새 목적지 백업을 건너뛰게 만들지 않는지 검증한다.
func TestStateConfigFingerprintChangeForcesReprocess(t *testing.T) {
	tmpDir := t.TempDir()
	sourcePath := filepath.Join(tmpDir, "source.jpg")
	destPath := filepath.Join(tmpDir, "dest.jpg")
	if err := os.WriteFile(sourcePath, []byte("data"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(destPath, []byte("data"), 0644); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(sourcePath)
	if err != nil {
		t.Fatal(err)
	}
	entry := types.FileEntry{Path: sourcePath, Size: info.Size(), ModTime: info.ModTime()}
	st := New(filepath.Join(tmpDir, "state.json"))
	if err := st.MarkProcessedEntry(context.Background(), entry, destPath, false, SourceContext{ConfigFingerprint: "dest-A"}); err != nil {
		t.Fatal(err)
	}
	if !st.IsEntryProcessed(context.Background(), entry, false, SourceContext{ConfigFingerprint: "dest-A"}) {
		t.Fatal("동일한 설정 fingerprint는 처리 완료로 인식되어야 한다")
	}
	if st.IsEntryProcessed(context.Background(), entry, false, SourceContext{ConfigFingerprint: "dest-B"}) {
		t.Fatal("목적지/설정 fingerprint가 바뀌면 재처리가 강제되어야 한다")
	}
}

func TestStateSidecarPathAndHashChangesForceReprocess(t *testing.T) {
	tmpDir := t.TempDir()
	sourcePath := filepath.Join(tmpDir, "source.mp4")
	destPath := filepath.Join(tmpDir, "dest.mp4")
	for _, path := range []string{sourcePath, destPath} {
		if err := os.WriteFile(path, []byte("video"), 0644); err != nil {
			t.Fatal(err)
		}
	}
	info, err := os.Stat(sourcePath)
	if err != nil {
		t.Fatal(err)
	}
	entry := types.FileEntry{Path: sourcePath, Size: info.Size(), ModTime: info.ModTime()}
	original := SourceContext{
		ConfigFingerprint: "same",
		SidecarPresent:    true,
		SidecarPath:       filepath.Join(tmpDir, "clipM01.XML"),
		SidecarSize:       10,
		SidecarHash:       "hash-a",
	}
	st := New(filepath.Join(tmpDir, "state.json"))
	if err := st.MarkProcessedEntry(context.Background(), entry, destPath, false, original); err != nil {
		t.Fatal(err)
	}
	if !st.IsEntryProcessed(context.Background(), entry, false, original) {
		t.Fatal("identical sidecar identity must remain processed")
	}
	changedHash := original
	changedHash.SidecarHash = "hash-b"
	if st.IsEntryProcessed(context.Background(), entry, false, changedHash) {
		t.Fatal("sidecar content hash change did not force reprocessing")
	}
	changedPath := original
	changedPath.SidecarPath = filepath.Join(tmpDir, "clipM01.xml")
	if st.IsEntryProcessed(context.Background(), entry, false, changedPath) {
		t.Fatal("sidecar path change did not force reprocessing")
	}
}

// TestStateHashingHonorsContextCancellation은 hash_verify 재해싱이 취소 신호를
// 전파해 느린 NAS/대용량 파일에서 취소가 지연되지 않는지 검증한다.
func TestStateHashingHonorsContextCancellation(t *testing.T) {
	tmpDir := t.TempDir()
	sourcePath := filepath.Join(tmpDir, "source.jpg")
	destPath := filepath.Join(tmpDir, "dest.jpg")
	// 1MB 버퍼보다 큰 파일이라야 취소 확인이 여러 반복에 걸쳐 유효하다.
	blob := make([]byte, 2*1024*1024)
	if err := os.WriteFile(sourcePath, blob, 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(destPath, blob, 0644); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(sourcePath)
	if err != nil {
		t.Fatal(err)
	}
	entry := types.FileEntry{Path: sourcePath, Size: info.Size(), ModTime: info.ModTime()}
	st := New(filepath.Join(tmpDir, "state.json"))
	if err := st.MarkProcessedEntry(context.Background(), entry, destPath, true, SourceContext{}); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if st.IsEntryProcessed(ctx, entry, true, SourceContext{}) {
		t.Fatal("취소된 context에서 재해싱 확인은 완료로 판정되면 안 된다")
	}
	if err := st.MarkSupersededEntry(ctx, entry, destPath, true, SourceContext{}); !errors.Is(err, context.Canceled) {
		t.Fatalf("취소된 context는 상태 커밋 해싱을 중단해야 한다, got %v", err)
	}
}

func TestStateLegacyRecordIsDirtyForIdentityMigration(t *testing.T) {
	st := New(filepath.Join(t.TempDir(), "state.json"))
	st.MarkProcessed("/src/legacy.jpg", 7, "/dest/legacy.jpg")
	entry := types.FileEntry{Path: "/src/legacy.jpg", Size: 7, ModTime: time.Now()}
	if st.IsEntryProcessed(context.Background(), entry, false, SourceContext{}) {
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
