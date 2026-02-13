package verify

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestVerifierVerify_SizeOnlySuccessWhenHashDisabled는 테스트 코드 동작을 검증하거나 보조합니다.
func TestVerifierVerify_SizeOnlySuccessWhenHashDisabled(t *testing.T) {
	// 해시 검증이 꺼져 있으면 크기만 같을 때 성공해야 한다.
	tmpDir := t.TempDir()
	srcPath := filepath.Join(tmpDir, "src.bin")
	destPath := filepath.Join(tmpDir, "dest.bin")

	if err := os.WriteFile(srcPath, []byte("abc"), 0644); err != nil {
		t.Fatalf("failed to write source file: %v", err)
	}
	if err := os.WriteFile(destPath, []byte("xyz"), 0644); err != nil {
		t.Fatalf("failed to write dest file: %v", err)
	}

	v := New(false)
	if err := v.Verify(srcPath, destPath, 3); err != nil {
		t.Fatalf("expected verify success, got %v", err)
	}
}

// TestVerifierVerify_SizeMismatch는 테스트 코드 동작을 검증하거나 보조합니다.
func TestVerifierVerify_SizeMismatch(t *testing.T) {
	// 목적지 크기가 기대값과 다르면 즉시 실패해야 한다.
	tmpDir := t.TempDir()
	srcPath := filepath.Join(tmpDir, "src.bin")
	destPath := filepath.Join(tmpDir, "dest.bin")

	if err := os.WriteFile(srcPath, []byte("abcd"), 0644); err != nil {
		t.Fatalf("failed to write source file: %v", err)
	}
	if err := os.WriteFile(destPath, []byte("abc"), 0644); err != nil {
		t.Fatalf("failed to write dest file: %v", err)
	}

	v := New(false)
	err := v.Verify(srcPath, destPath, 4)
	if err == nil {
		t.Fatal("expected size mismatch error")
	}
	if !strings.Contains(err.Error(), "size mismatch") {
		t.Fatalf("unexpected error: %v", err)
	}
}

// TestVerifierVerify_HashMismatchWhenEnabled는 테스트 코드 동작을 검증하거나 보조합니다.
func TestVerifierVerify_HashMismatchWhenEnabled(t *testing.T) {
	// 해시 검증이 켜져 있으면 동일 크기라도 내용이 다르면 실패해야 한다.
	tmpDir := t.TempDir()
	srcPath := filepath.Join(tmpDir, "src.bin")
	destPath := filepath.Join(tmpDir, "dest.bin")

	if err := os.WriteFile(srcPath, []byte("abc"), 0644); err != nil {
		t.Fatalf("failed to write source file: %v", err)
	}
	if err := os.WriteFile(destPath, []byte("xyz"), 0644); err != nil {
		t.Fatalf("failed to write dest file: %v", err)
	}

	v := New(true)
	err := v.Verify(srcPath, destPath, 3)
	if err == nil {
		t.Fatal("expected hash mismatch error")
	}
	if !strings.Contains(err.Error(), "hash mismatch") {
		t.Fatalf("unexpected error: %v", err)
	}
}

// TestVerifierVerify_DestinationMissing는 테스트 코드 동작을 검증하거나 보조합니다.
func TestVerifierVerify_DestinationMissing(t *testing.T) {
	// 목적지 파일이 없으면 명확한 에러를 반환해야 한다.
	tmpDir := t.TempDir()
	srcPath := filepath.Join(tmpDir, "src.bin")
	missingDestPath := filepath.Join(tmpDir, "missing.bin")

	if err := os.WriteFile(srcPath, []byte("abc"), 0644); err != nil {
		t.Fatalf("failed to write source file: %v", err)
	}

	v := New(true)
	err := v.Verify(srcPath, missingDestPath, 3)
	if err == nil {
		t.Fatal("expected destination not found error")
	}
	if !strings.Contains(err.Error(), "destination file not found") {
		t.Fatalf("unexpected error: %v", err)
	}
}

// TestVerifierVerify_HashSuccessWhenContentsMatch는 테스트 코드 동작을 검증하거나 보조합니다.
func TestVerifierVerify_HashSuccessWhenContentsMatch(t *testing.T) {
	// 해시 검증이 켜져 있고 내용이 동일하면 성공해야 한다.
	tmpDir := t.TempDir()
	srcPath := filepath.Join(tmpDir, "src.bin")
	destPath := filepath.Join(tmpDir, "dest.bin")

	if err := os.WriteFile(srcPath, []byte("same"), 0644); err != nil {
		t.Fatalf("failed to write source file: %v", err)
	}
	if err := os.WriteFile(destPath, []byte("same"), 0644); err != nil {
		t.Fatalf("failed to write dest file: %v", err)
	}

	v := New(true)
	if err := v.Verify(srcPath, destPath, 4); err != nil {
		t.Fatalf("expected verify success, got %v", err)
	}
}

// TestVerifierVerify_ReturnsSourceHashError는 테스트 코드 동작을 검증하거나 보조합니다.
func TestVerifierVerify_ReturnsSourceHashError(t *testing.T) {
	// 소스 해시 계산 실패 시 failed to hash source 에러를 반환해야 한다.
	tmpDir := t.TempDir()
	srcDir := filepath.Join(tmpDir, "src-dir")
	destPath := filepath.Join(tmpDir, "dest.bin")

	if err := os.MkdirAll(srcDir, 0755); err != nil {
		t.Fatalf("failed to create src dir: %v", err)
	}
	if err := os.WriteFile(destPath, []byte(""), 0644); err != nil {
		t.Fatalf("failed to write dest file: %v", err)
	}

	v := New(true)
	err := v.Verify(srcDir, destPath, 0)
	if err == nil {
		t.Fatal("expected source hash error")
	}
	if !strings.Contains(err.Error(), "failed to hash source") {
		t.Fatalf("unexpected error: %v", err)
	}
}

// TestVerifierVerify_ReturnsDestinationHashError는 테스트 코드 동작을 검증하거나 보조합니다.
func TestVerifierVerify_ReturnsDestinationHashError(t *testing.T) {
	// 목적지 해시 계산 실패 시 failed to hash destination 에러를 반환해야 한다.
	tmpDir := t.TempDir()
	srcPath := filepath.Join(tmpDir, "src.bin")
	destDir := filepath.Join(tmpDir, "dest-dir")

	if err := os.WriteFile(srcPath, []byte("abc"), 0644); err != nil {
		t.Fatalf("failed to write source file: %v", err)
	}
	if err := os.MkdirAll(destDir, 0755); err != nil {
		t.Fatalf("failed to create dest dir: %v", err)
	}

	info, err := os.Stat(destDir)
	if err != nil {
		t.Fatalf("failed to stat dest dir: %v", err)
	}

	v := New(true)
	err = v.Verify(srcPath, destDir, info.Size())
	if err == nil {
		t.Fatal("expected destination hash error")
	}
	if !strings.Contains(err.Error(), "failed to hash destination") {
		t.Fatalf("unexpected error: %v", err)
	}
}
