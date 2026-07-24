package pipeline

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/On-Jun9/ShutterPipe/internal/state"
	"github.com/On-Jun9/ShutterPipe/pkg/types"
)

func TestVerificationAndRequeueRoundTripRepairsSameSizeDamageDespiteNameSizeSkip(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	sourceDir := filepath.Join(home, "source")
	destDir := filepath.Join(home, "dest")
	if err := os.MkdirAll(sourceDir, 0755); err != nil {
		t.Fatal(err)
	}
	sourcePath := filepath.Join(sourceDir, "photo.jpg")
	if err := os.WriteFile(sourcePath, []byte("good"), 0644); err != nil {
		t.Fatal(err)
	}
	cfg := newTestConfig(home, sourceDir, destDir)
	cfg.DedupMethod = types.DedupMethodNameSize
	cfg.ConflictPolicy = types.ConflictPolicySkip

	first, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := first.RunWithContext(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := first.Close(); err != nil {
		t.Fatal(err)
	}
	destPath := filepath.Join(destDir, cfg.UnclassifiedDir, "photo.jpg")
	if err := os.WriteFile(destPath, []byte("evil"), 0644); err != nil {
		t.Fatal(err)
	}
	stateBeforeVerify, err := os.ReadFile(cfg.StateFile)
	if err != nil {
		t.Fatal(err)
	}

	verification, err := NewVerification(cfg, types.VerifyModeHash, "", "")
	if err != nil {
		t.Fatal(err)
	}
	verifyResult, err := verification.RunWithContext(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if err := verification.Close(); err != nil {
		t.Fatal(err)
	}
	if verifyResult.Summary.Mismatch != 1 || len(verifyResult.RequeueCandidates) != 1 {
		t.Fatalf("unexpected verification result: %+v candidates=%d", verifyResult.Summary, len(verifyResult.RequeueCandidates))
	}
	stateAfterVerify, err := os.ReadFile(cfg.StateFile)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(stateBeforeVerify, stateAfterVerify) {
		t.Fatal("verification changed state before requeue confirmation")
	}

	queueResult, err := QueueVerificationProblems(context.Background(), cfg.StateFile, "verify-1", verifyResult.RequeueCandidates)
	if err != nil {
		t.Fatal(err)
	}
	if queueResult.Applied != 1 {
		t.Fatalf("unexpected queue result: %+v", queueResult)
	}

	repair, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	repairSummary, err := repair.RunWithContext(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if err := repair.Close(); err != nil {
		t.Fatal(err)
	}
	if repairSummary.Overwritten != 1 {
		t.Fatalf("damaged destination was not replaced: %+v", repairSummary)
	}
	content, err := os.ReadFile(destPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != "good" {
		t.Fatalf("destination content = %q", content)
	}
	loaded, err := state.Load(cfg.StateFile)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := loaded.RebackupMarker(sourcePath); ok {
		t.Fatal("successful repair retained its rebackup marker")
	}
}

func TestRecordlessRequeuePreservesCollisionAndCreatesRenamedCopy(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	sourceDir := filepath.Join(home, "source")
	destDir := filepath.Join(home, "dest")
	if err := os.MkdirAll(sourceDir, 0755); err != nil {
		t.Fatal(err)
	}
	sourcePath := filepath.Join(sourceDir, "photo.jpg")
	if err := os.WriteFile(sourcePath, []byte("good"), 0644); err != nil {
		t.Fatal(err)
	}
	cfg := newTestConfig(home, sourceDir, destDir)
	cfg.DedupMethod = types.DedupMethodNameSize
	cfg.ConflictPolicy = types.ConflictPolicySkip
	collisionPath := filepath.Join(destDir, cfg.UnclassifiedDir, "photo.jpg")
	if err := os.MkdirAll(filepath.Dir(collisionPath), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(collisionPath, []byte("evil"), 0644); err != nil {
		t.Fatal(err)
	}

	verification, err := NewVerification(cfg, types.VerifyModeHash, "", "")
	if err != nil {
		t.Fatal(err)
	}
	verifyResult, err := verification.RunWithContext(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if err := verification.Close(); err != nil {
		t.Fatal(err)
	}
	if len(verifyResult.RequeueCandidates) != 1 || verifyResult.RequeueCandidates[0].ProcessedPresent {
		t.Fatalf("expected one recordless problem: %+v", verifyResult.RequeueCandidates)
	}
	if _, err := QueueVerificationProblems(context.Background(), cfg.StateFile, "verify-2", verifyResult.RequeueCandidates); err != nil {
		t.Fatal(err)
	}

	backup, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	summary, err := backup.RunWithContext(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if err := backup.Close(); err != nil {
		t.Fatal(err)
	}
	if summary.Renamed != 1 {
		t.Fatalf("recordless collision was not renamed: %+v", summary)
	}
	original, err := os.ReadFile(collisionPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(original) != "evil" {
		t.Fatalf("untrusted collision was overwritten: %q", original)
	}
	renamed, err := os.ReadFile(filepath.Join(destDir, cfg.UnclassifiedDir, "photo_1.jpg"))
	if err != nil {
		t.Fatal(err)
	}
	if string(renamed) != "good" {
		t.Fatalf("renamed copy content = %q", renamed)
	}
}

func TestUnreadableDestinationDisablesRequeue(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows의 Chmod는 디렉터리 접근 권한을 제거하지 못한다")
	}
	if os.Geteuid() == 0 {
		t.Skip("root ignores directory permissions")
	}
	home := t.TempDir()
	t.Setenv("HOME", home)
	sourceDir := filepath.Join(home, "source")
	destDir := filepath.Join(home, "dest")
	if err := os.MkdirAll(sourceDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sourceDir, "photo.jpg"), []byte("good"), 0644); err != nil {
		t.Fatal(err)
	}
	cfg := newTestConfig(home, sourceDir, destDir)

	backup, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := backup.RunWithContext(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := backup.Close(); err != nil {
		t.Fatal(err)
	}

	// 도착 폴더의 분류 폴더를 읽지 못하게 만들어 실스캔이 불완전한 상황을 재현한다.
	lockedDir := filepath.Join(destDir, cfg.UnclassifiedDir)
	if err := os.Chmod(lockedDir, 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(lockedDir, 0o755) })

	verification, err := NewVerification(cfg, types.VerifyModeQuick, "", "")
	if err != nil {
		t.Fatal(err)
	}
	result, err := verification.RunWithContext(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if err := verification.Close(); err != nil {
		t.Fatal(err)
	}

	// 잠긴 폴더 때문에 문제 판정이 실제로 발생했는지 먼저 확인한다. 이 확인이 없으면
	// 전부 OK로 끝난 검증(candidates 0)도 RequeueAllowed=false라 테스트가 자명 통과한다.
	if result.Summary.Missing+result.Summary.Unverifiable == 0 {
		t.Fatalf("locked destination did not produce problems: %+v", result.Summary)
	}
	if result.Summary.RequeueAllowed {
		t.Fatalf("requeue allowed despite incomplete destination scan: %+v", result.Summary)
	}
	if result.Summary.RequeueEligible != 0 || len(result.RequeueCandidates) != 0 {
		t.Fatalf("candidates survived incomplete destination scan: eligible=%d candidates=%d",
			result.Summary.RequeueEligible, len(result.RequeueCandidates))
	}
}
