package pipeline

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
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
