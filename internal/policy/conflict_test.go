package policy

import (
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/On-Jun9/ShutterPipe/pkg/types"
)

// TestConflictResolver_StatErrorSurfacesAsFailureNotSkip는 목적지 stat 오류가
// "이미 존재함"으로 오판되어 조용히 skip 처리되지 않고 실패로 노출되는지 검증한다.
func TestConflictResolver_StatErrorSurfacesAsFailureNotSkip(t *testing.T) {
	tmpDir := t.TempDir()
	blocker := filepath.Join(tmpDir, "blocker")
	if err := os.WriteFile(blocker, []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}
	// 일반 파일 아래 경로를 stat하면 ENOENT가 아니라 ENOTDIR가 반환된다.
	destUnderFile := filepath.Join(blocker, "photo.jpg")
	resolver := NewConflictResolver(types.ConflictPolicySkip, filepath.Join(tmpDir, "quarantine"))

	res := resolver.Resolve(&types.CopyTask{
		Source:   types.FileEntry{Name: "photo.jpg"},
		DestPath: destUnderFile,
	})
	if res.Err == nil {
		t.Fatal("stat 오류는 resolution 오류로 노출되어야 한다")
	}
	if res.Skip {
		t.Fatal("stat 오류를 skip으로 처리하면 안 된다")
	}
}

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

func TestConflictResolver_RenameReservesDestinationsWithinRun(t *testing.T) {
	tmpDir := t.TempDir()
	resolver := NewConflictResolver(types.ConflictPolicyRename, filepath.Join(tmpDir, "quarantine"))
	destPath := filepath.Join(tmpDir, "photo.jpg")

	first := resolver.Resolve(&types.CopyTask{
		Source:   types.FileEntry{Path: "/card-a/photo.jpg", Name: "photo.jpg"},
		DestPath: destPath,
	})
	second := resolver.Resolve(&types.CopyTask{
		Source:   types.FileEntry{Path: "/card-b/photo.jpg", Name: "photo.jpg"},
		DestPath: destPath,
	})

	if first.Skip || first.Action != types.CopyActionCopied || first.DestPath != destPath {
		t.Fatalf("unexpected first resolution: %+v", first)
	}
	expectedSecond := filepath.Join(tmpDir, "photo_1.jpg")
	if second.Skip || second.Action != types.CopyActionRenamed || second.DestPath != expectedSecond {
		t.Fatalf("expected reserved destination to rename to %s, got %+v", expectedSecond, second)
	}
}

func TestConflictResolver_ResetReservationsStartsANewRunScope(t *testing.T) {
	tmpDir := t.TempDir()
	resolver := NewConflictResolver(types.ConflictPolicyRename, filepath.Join(tmpDir, "quarantine"))
	destPath := filepath.Join(tmpDir, "photo.jpg")
	task := &types.CopyTask{Source: types.FileEntry{Name: "photo.jpg"}, DestPath: destPath}

	first := resolver.Resolve(task)
	if first.Action != types.CopyActionCopied || first.DestPath != destPath {
		t.Fatalf("unexpected first resolution: %+v", first)
	}
	resolver.ResetReservations()
	second := resolver.Resolve(task)
	if second.Action != types.CopyActionCopied || second.DestPath != destPath {
		t.Fatalf("reservation leaked into the next run: %+v", second)
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

func TestConflictResolver_OverwriteDistinguishesReservedFromDiskConflict(t *testing.T) {
	tmpDir := t.TempDir()
	resolver := NewConflictResolver(types.ConflictPolicyOverwrite, filepath.Join(tmpDir, "quarantine"))
	destPath := filepath.Join(tmpDir, "photo.jpg")

	first := resolver.Resolve(&types.CopyTask{Source: types.FileEntry{Path: "/a/photo.jpg"}, DestPath: destPath})
	second := resolver.Resolve(&types.CopyTask{Source: types.FileEntry{Path: "/b/photo.jpg"}, DestPath: destPath})

	if first.ReplaceReserved || first.Action != types.CopyActionCopied {
		t.Fatalf("unexpected first resolution: %+v", first)
	}
	if !second.ReplaceReserved || second.Action != types.CopyActionCopied || second.DestPath != destPath {
		t.Fatalf("reserved overwrite must replace the planned task without claiming a disk overwrite: %+v", second)
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
	got, err := resolver.generateUniqueName(original)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if got != original {
		t.Fatalf("expected original path when candidates exhausted, got %s", got)
	}
}

// TestConflictResolver_QuarantineCandidateStatErrorSurfacesAsFailure는 격리 후보
// 경로(별도 quarantine 디렉터리)의 stat 오류가 조용한 skip이 아니라 실패로
// 노출되는지 검증한다. quarantine 후보는 목적지 디렉터리와 분리되어 있어 최초
// 목적지 stat과 독립적으로 오류를 재현할 수 있다.
func TestConflictResolver_QuarantineCandidateStatErrorSurfacesAsFailure(t *testing.T) {
	tmpDir := t.TempDir()
	destDir := filepath.Join(tmpDir, "dest")
	if err := os.MkdirAll(destDir, 0755); err != nil {
		t.Fatal(err)
	}
	dest := filepath.Join(destDir, "photo.jpg")
	if err := os.WriteFile(dest, []byte("x"), 0644); err != nil { // 존재 → 충돌 → quarantine 분기
		t.Fatal(err)
	}
	// quarantine 디렉터리를 일반 파일로 만들어 그 하위 후보 stat이 ENOTDIR가 되게 한다.
	quarantineAsFile := filepath.Join(tmpDir, "quarantine")
	if err := os.WriteFile(quarantineAsFile, []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}

	resolver := NewConflictResolver(types.ConflictPolicyQuarantine, quarantineAsFile)
	res := resolver.Resolve(&types.CopyTask{
		Source:   types.FileEntry{Name: "photo.jpg"},
		DestPath: dest,
	})
	if res.Err == nil {
		t.Fatal("격리 후보 stat 오류는 resolution 오류로 노출되어야 한다")
	}
	if res.Skip {
		t.Fatal("격리 후보 stat 오류를 skip으로 처리하면 안 된다")
	}
}
