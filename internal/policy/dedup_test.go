package policy

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/On-Jun9/ShutterPipe/pkg/types"
)

// TestDedupChecker_NameSize_ReturnsTrueWhenSizeMatches는 테스트 코드 동작을 검증하거나 보조합니다.
func TestDedupChecker_NameSize_ReturnsTrueWhenSizeMatches(t *testing.T) {
	// name-size 모드에서는 목적지 파일 크기와 소스 크기가 같으면 중복으로 본다.
	tmpDir := t.TempDir()
	destPath := filepath.Join(tmpDir, "dest.jpg")
	if err := os.WriteFile(destPath, []byte("abcd"), 0644); err != nil {
		t.Fatalf("failed to write dest file: %v", err)
	}

	checker := NewDedupChecker(types.DedupMethodNameSize)
	src := types.FileEntry{Size: 4}

	isDup, err := checker.IsDuplicate(src, destPath)
	if err != nil {
		t.Fatalf("is duplicate failed: %v", err)
	}
	if !isDup {
		t.Fatal("expected duplicate=true for same size")
	}
}

// TestDedupChecker_NameSize_ReturnsFalseWhenSizeDiffers는 테스트 코드 동작을 검증하거나 보조합니다.
func TestDedupChecker_NameSize_ReturnsFalseWhenSizeDiffers(t *testing.T) {
	// name-size 모드에서는 크기가 다르면 중복이 아니다.
	tmpDir := t.TempDir()
	destPath := filepath.Join(tmpDir, "dest.jpg")
	if err := os.WriteFile(destPath, []byte("abcd"), 0644); err != nil {
		t.Fatalf("failed to write dest file: %v", err)
	}

	checker := NewDedupChecker(types.DedupMethodNameSize)
	src := types.FileEntry{Size: 5}

	isDup, err := checker.IsDuplicate(src, destPath)
	if err != nil {
		t.Fatalf("is duplicate failed: %v", err)
	}
	if isDup {
		t.Fatal("expected duplicate=false for different size")
	}
}

// TestDedupChecker_Hash_ReturnsTrueForSameContent는 테스트 코드 동작을 검증하거나 보조합니다.
func TestDedupChecker_Hash_ReturnsTrueForSameContent(t *testing.T) {
	// hash 모드에서는 내용이 같으면 중복으로 판단해야 한다.
	tmpDir := t.TempDir()
	srcPath := filepath.Join(tmpDir, "src.jpg")
	destPath := filepath.Join(tmpDir, "dest.jpg")

	if err := os.WriteFile(srcPath, []byte("same-content"), 0644); err != nil {
		t.Fatalf("failed to write src file: %v", err)
	}
	if err := os.WriteFile(destPath, []byte("same-content"), 0644); err != nil {
		t.Fatalf("failed to write dest file: %v", err)
	}

	checker := NewDedupChecker(types.DedupMethodHash)
	src := types.FileEntry{Path: srcPath, Size: 12}

	isDup, err := checker.IsDuplicate(src, destPath)
	if err != nil {
		t.Fatalf("is duplicate failed: %v", err)
	}
	if !isDup {
		t.Fatal("expected duplicate=true for same hash")
	}
}

// TestDedupChecker_Hash_ReturnsFalseForDifferentContent는 테스트 코드 동작을 검증하거나 보조합니다.
func TestDedupChecker_Hash_ReturnsFalseForDifferentContent(t *testing.T) {
	// hash 모드에서는 크기가 같아도 내용이 다르면 중복이 아니다.
	tmpDir := t.TempDir()
	srcPath := filepath.Join(tmpDir, "src.jpg")
	destPath := filepath.Join(tmpDir, "dest.jpg")

	if err := os.WriteFile(srcPath, []byte("abc"), 0644); err != nil {
		t.Fatalf("failed to write src file: %v", err)
	}
	if err := os.WriteFile(destPath, []byte("xyz"), 0644); err != nil {
		t.Fatalf("failed to write dest file: %v", err)
	}

	checker := NewDedupChecker(types.DedupMethodHash)
	src := types.FileEntry{Path: srcPath, Size: 3}

	isDup, err := checker.IsDuplicate(src, destPath)
	if err != nil {
		t.Fatalf("is duplicate failed: %v", err)
	}
	if isDup {
		t.Fatal("expected duplicate=false for different hash")
	}
}

// TestDedupChecker_ReturnsFalseWhenDestinationMissing는 테스트 코드 동작을 검증하거나 보조합니다.
func TestDedupChecker_ReturnsFalseWhenDestinationMissing(t *testing.T) {
	// 목적지 파일이 없으면 중복이 아니며 에러도 없어야 한다.
	checker := NewDedupChecker(types.DedupMethodNameSize)
	isDup, err := checker.IsDuplicate(types.FileEntry{Size: 1}, "/path/does/not/exist")
	if err != nil {
		t.Fatalf("expected no error for missing destination, got %v", err)
	}
	if isDup {
		t.Fatal("expected duplicate=false when destination missing")
	}
}

// TestDedupChecker_ReturnsStatErrorForInvalidDestinationPath는 테스트 코드 동작을 검증하거나 보조합니다.
func TestDedupChecker_ReturnsStatErrorForInvalidDestinationPath(t *testing.T) {
	// destination stat 자체가 실패하면 에러를 그대로 반환해야 한다.
	checker := NewDedupChecker(types.DedupMethodNameSize)
	_, err := checker.IsDuplicate(types.FileEntry{Size: 1}, "\x00")
	if err == nil {
		t.Fatal("expected destination stat error")
	}
}

// TestDedupChecker_Hash_ReturnsSourceHashError는 테스트 코드 동작을 검증하거나 보조합니다.
func TestDedupChecker_Hash_ReturnsSourceHashError(t *testing.T) {
	// hash 모드에서 source 해시 계산 실패 시 에러를 반환해야 한다.
	tmpDir := t.TempDir()
	destPath := filepath.Join(tmpDir, "dest.jpg")
	if err := os.WriteFile(destPath, []byte("abc"), 0644); err != nil {
		t.Fatalf("failed to write dest file: %v", err)
	}

	checker := NewDedupChecker(types.DedupMethodHash)
	_, err := checker.IsDuplicate(types.FileEntry{Path: filepath.Join(tmpDir, "missing.jpg"), Size: 3}, destPath)
	if err == nil {
		t.Fatal("expected source hash error")
	}
}

// TestDedupChecker_Hash_ReturnsDestinationHashError는 테스트 코드 동작을 검증하거나 보조합니다.
func TestDedupChecker_Hash_ReturnsDestinationHashError(t *testing.T) {
	// hash 모드에서 destination 해시 계산 실패 시 에러를 반환해야 한다.
	tmpDir := t.TempDir()
	srcPath := filepath.Join(tmpDir, "src.jpg")
	destDir := filepath.Join(tmpDir, "dest-dir")

	if err := os.WriteFile(srcPath, []byte("abc"), 0644); err != nil {
		t.Fatalf("failed to write src file: %v", err)
	}
	if err := os.MkdirAll(destDir, 0755); err != nil {
		t.Fatalf("failed to create dest dir: %v", err)
	}

	checker := NewDedupChecker(types.DedupMethodHash)
	_, err := checker.IsDuplicate(types.FileEntry{Path: srcPath, Size: 3}, destDir)
	if err == nil {
		t.Fatal("expected destination hash error")
	}
}
