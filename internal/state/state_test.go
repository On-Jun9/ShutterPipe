package state

import (
	"os"
	"path/filepath"
	"testing"
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
