package policy

import (
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/On-Jun9/ShutterPipe/pkg/types"
)

// TestConflictResolver_NoConflict는 테스트 코드 동작을 검증하거나 보조합니다.
func TestConflictResolver_NoConflict(t *testing.T) {
	tmpDir := t.TempDir()
	resolver := NewConflictResolver(types.ConflictPolicySkip, filepath.Join(tmpDir, "quarantine"))

	task := &types.CopyTask{
		Source:   types.FileEntry{Name: "photo.jpg"},
		DestPath: filepath.Join(tmpDir, "photo.jpg"),
	}

	res := resolver.Resolve(task)

	if res.Skip {
		t.Error("should not skip when no conflict")
	}
	if res.Action != types.CopyActionCopied {
		t.Errorf("expected copied action, got %s", res.Action)
	}
}

// TestConflictResolver_Skip는 테스트 코드 동작을 검증하거나 보조합니다.
func TestConflictResolver_Skip(t *testing.T) {
	tmpDir := t.TempDir()
	existingFile := filepath.Join(tmpDir, "photo.jpg")
	os.WriteFile(existingFile, []byte("existing"), 0644)

	resolver := NewConflictResolver(types.ConflictPolicySkip, filepath.Join(tmpDir, "quarantine"))

	task := &types.CopyTask{
		Source:   types.FileEntry{Name: "photo.jpg"},
		DestPath: existingFile,
	}

	res := resolver.Resolve(task)

	if !res.Skip {
		t.Error("should skip on conflict with skip policy")
	}
	if res.Action != types.CopyActionSkipped {
		t.Errorf("expected skipped action, got %s", res.Action)
	}
}

// TestConflictResolver_Rename는 테스트 코드 동작을 검증하거나 보조합니다.
func TestConflictResolver_Rename(t *testing.T) {
	tmpDir := t.TempDir()
	existingFile := filepath.Join(tmpDir, "photo.jpg")
	os.WriteFile(existingFile, []byte("existing"), 0644)

	resolver := NewConflictResolver(types.ConflictPolicyRename, filepath.Join(tmpDir, "quarantine"))

	task := &types.CopyTask{
		Source:   types.FileEntry{Name: "photo.jpg"},
		DestPath: existingFile,
	}

	res := resolver.Resolve(task)

	if res.Skip {
		t.Error("should not skip on rename policy")
	}
	if res.Action != types.CopyActionRenamed {
		t.Errorf("expected renamed action, got %s", res.Action)
	}

	expected := filepath.Join(tmpDir, "photo_1.jpg")
	if res.DestPath != expected {
		t.Errorf("expected %s, got %s", expected, res.DestPath)
	}
}

// TestConflictResolver_Overwrite는 테스트 코드 동작을 검증하거나 보조합니다.
func TestConflictResolver_Overwrite(t *testing.T) {
	// overwrite 정책은 같은 경로를 유지하고 overwrite 액션을 반환해야 한다.
	tmpDir := t.TempDir()
	existingFile := filepath.Join(tmpDir, "photo.jpg")
	os.WriteFile(existingFile, []byte("existing"), 0644)

	resolver := NewConflictResolver(types.ConflictPolicyOverwrite, filepath.Join(tmpDir, "quarantine"))
	task := &types.CopyTask{
		Source:   types.FileEntry{Name: "photo.jpg"},
		DestPath: existingFile,
	}

	res := resolver.Resolve(task)
	if res.Skip {
		t.Fatal("should not skip on overwrite policy")
	}
	if res.Action != types.CopyActionOverwritten {
		t.Fatalf("expected overwritten action, got %s", res.Action)
	}
	if res.DestPath != existingFile {
		t.Fatalf("expected same destination path, got %s", res.DestPath)
	}
}

// TestConflictResolver_Quarantine는 테스트 코드 동작을 검증하거나 보조합니다.
func TestConflictResolver_Quarantine(t *testing.T) {
	// quarantine 정책은 quarantine 디렉터리로 목적지를 이동해야 한다.
	tmpDir := t.TempDir()
	existingFile := filepath.Join(tmpDir, "photo.jpg")
	os.WriteFile(existingFile, []byte("existing"), 0644)

	quarantineDir := filepath.Join(tmpDir, "quarantine")
	resolver := NewConflictResolver(types.ConflictPolicyQuarantine, quarantineDir)
	task := &types.CopyTask{
		Source:   types.FileEntry{Name: "photo.jpg"},
		DestPath: existingFile,
	}

	res := resolver.Resolve(task)
	if res.Skip {
		t.Fatal("should not skip on quarantine policy")
	}
	if res.Action != types.CopyActionQuarantined {
		t.Fatalf("expected quarantined action, got %s", res.Action)
	}
	if filepath.Dir(res.DestPath) != quarantineDir {
		t.Fatalf("expected quarantine destination, got %s", res.DestPath)
	}
}

// TestConflictResolver_DefaultPolicyFallsBackToSkip는 테스트 코드 동작을 검증하거나 보조합니다.
func TestConflictResolver_DefaultPolicyFallsBackToSkip(t *testing.T) {
	// 알 수 없는 정책 값은 안전하게 skip으로 처리해야 한다.
	tmpDir := t.TempDir()
	existingFile := filepath.Join(tmpDir, "photo.jpg")
	os.WriteFile(existingFile, []byte("existing"), 0644)

	resolver := NewConflictResolver(types.ConflictPolicy("unknown"), filepath.Join(tmpDir, "quarantine"))
	task := &types.CopyTask{
		Source:   types.FileEntry{Name: "photo.jpg"},
		DestPath: existingFile,
	}

	res := resolver.Resolve(task)
	if !res.Skip {
		t.Fatal("expected skip for unknown policy")
	}
	if res.Action != types.CopyActionSkipped {
		t.Fatalf("expected skipped action, got %s", res.Action)
	}
}

// TestConflictResolver_GenerateUniqueName_ReturnsOriginalWhenExhausted는 테스트 코드 동작을 검증하거나 보조합니다.
func TestConflictResolver_GenerateUniqueName_ReturnsOriginalWhenExhausted(t *testing.T) {
	// _1~_9999 후보가 모두 존재하면 generateUniqueName은 원본 경로를 반환해야 한다.
	tmpDir := t.TempDir()
	original := filepath.Join(tmpDir, "photo.jpg")

	for i := 1; i < 10000; i++ {
		candidate := filepath.Join(tmpDir, "photo_"+strconv.Itoa(i)+".jpg")
		if err := os.WriteFile(candidate, []byte("x"), 0644); err != nil {
			t.Fatalf("failed to create candidate file %d: %v", i, err)
		}
	}

	resolver := NewConflictResolver(types.ConflictPolicyRename, filepath.Join(tmpDir, "quarantine"))
	got := resolver.generateUniqueName(original)

	if got != original {
		t.Fatalf("expected original path when candidates exhausted, got %s", got)
	}
}
