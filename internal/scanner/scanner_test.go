package scanner

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// TestScanner_Scan는 테스트 코드 동작을 검증하거나 보조합니다.
func TestScanner_Scan(t *testing.T) {
	tmpDir := t.TempDir()

	testFiles := []struct {
		name    string
		content string
	}{
		{"photo1.jpg", "fake jpg"},
		{"photo2.JPEG", "fake jpeg"},
		{"video1.mp4", "fake mp4"},
		{"document.pdf", "should be ignored"},
		{"subdir/photo3.heic", "nested photo"},
	}

	for _, tf := range testFiles {
		path := filepath.Join(tmpDir, tf.name)
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(tf.content), 0644); err != nil {
			t.Fatal(err)
		}
	}

	s := New([]string{"jpg", "jpeg", "heic", "mp4"})
	entries, err := s.Scan(tmpDir)
	if err != nil {
		t.Fatalf("Scan failed: %v", err)
	}

	if len(entries) != 4 {
		t.Errorf("expected 4 files, got %d", len(entries))
	}

	videoCount := 0
	for _, e := range entries {
		if e.IsVideo {
			videoCount++
		}
	}
	if videoCount != 1 {
		t.Errorf("expected 1 video, got %d", videoCount)
	}
}

// TestScanner_Scan_ReturnsWalkErrorForMissingRoot는 테스트 코드 동작을 검증하거나 보조합니다.
func TestScanner_Scan_ReturnsWalkErrorForMissingRoot(t *testing.T) {
	// 루트 경로가 없으면 WalkDir 에러를 그대로 반환해야 한다.
	s := New([]string{"jpg"})

	entries, err := s.Scan(filepath.Join(t.TempDir(), "missing"))
	if err == nil {
		t.Fatal("expected walk error for missing root")
	}
	if len(entries) != 0 {
		t.Fatalf("expected no entries on walk error, got %d", len(entries))
	}
}

// TestScanner_ScanWithContext_ReturnsCanceled는 테스트 코드 동작을 검증하거나 보조합니다.
func TestScanner_ScanWithContext_ReturnsCanceled(t *testing.T) {
	// 취소된 컨텍스트로 스캔하면 context.Canceled를 반환해야 한다.
	tmpDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(tmpDir, "photo.jpg"), []byte("x"), 0644); err != nil {
		t.Fatalf("failed to write test file: %v", err)
	}

	s := New([]string{"jpg"})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	entries, err := s.ScanWithContext(ctx, tmpDir)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled, got %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("expected no entries on canceled context, got %d", len(entries))
	}
}

func TestScanner_ScanReturnsEntryInfoError(t *testing.T) {
	tmpDir := t.TempDir()
	sourcePath := filepath.Join(tmpDir, "photo.jpg")
	if err := os.WriteFile(sourcePath, []byte("photo"), 0644); err != nil {
		t.Fatal(err)
	}
	infoErr := errors.New("injected stat failure")
	s := New([]string{"jpg"})
	s.entryInfo = func(os.DirEntry) (os.FileInfo, error) {
		return nil, infoErr
	}

	entries, err := s.Scan(tmpDir)
	if !errors.Is(err, infoErr) {
		t.Fatalf("expected source inspection error, got %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("failed source entry was silently included: %+v", entries)
	}
}

// TestScanner_SkipsNonRegularFiles는 테스트 코드 동작을 검증하거나 보조합니다.
func TestScanner_SkipsNonRegularFiles(t *testing.T) {
	tmpDir := t.TempDir()
	regularPath := filepath.Join(tmpDir, "photo.jpg")
	if err := os.WriteFile(regularPath, []byte("photo"), 0644); err != nil {
		t.Fatal(err)
	}
	linkPath := filepath.Join(tmpDir, "linked.jpg")
	if err := os.Symlink(regularPath, linkPath); err != nil {
		t.Skipf("symlink unsupported on this platform: %v", err)
	}

	s := New([]string{"jpg"})
	entries, err := s.Scan(tmpDir)
	if err != nil {
		t.Fatalf("Scan failed: %v", err)
	}
	if len(entries) != 1 || entries[0].Path != regularPath {
		t.Fatalf("symlink was not skipped: %+v", entries)
	}

	// 검증 원본 스캔 경로: 확장자 필터 스캐너의 수집 스캔도 비정규 파일을 스킵해야 한다.
	filtered, issues, err := s.ScanCollectingWithContext(context.Background(), tmpDir)
	if err != nil {
		t.Fatalf("filtered ScanCollectingWithContext failed: %v", err)
	}
	if len(issues) != 0 {
		t.Fatalf("unexpected scan issues: %+v", issues)
	}
	if len(filtered) != 1 || filtered[0].Path != regularPath {
		t.Fatalf("symlink was not skipped in filtered collecting scan: %+v", filtered)
	}

	all := NewAll()
	collected, issues, err := all.ScanCollectingWithContext(context.Background(), tmpDir)
	if err != nil {
		t.Fatalf("ScanCollectingWithContext failed: %v", err)
	}
	if len(issues) != 0 {
		t.Fatalf("unexpected scan issues: %+v", issues)
	}
	if len(collected) != 1 || collected[0].Path != regularPath {
		t.Fatalf("symlink was not skipped in collecting scan: %+v", collected)
	}
}
