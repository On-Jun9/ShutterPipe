package pipeline

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/On-Jun9/ShutterPipe/internal/state"
)

func TestAcquireRunLock_RejectsConcurrentOwnerAndReleases(t *testing.T) {
	path := filepath.Join(t.TempDir(), "run.lock")
	first, err := acquireRunLock(path)
	if err != nil {
		t.Fatalf("acquire first run lock: %v", err)
	}

	if _, err := acquireRunLock(path); err == nil {
		t.Fatal("expected second concurrent lock acquisition to fail")
	}
	if err := first.Close(); err != nil {
		t.Fatalf("release first run lock: %v", err)
	}

	second, err := acquireRunLock(path)
	if err != nil {
		t.Fatalf("acquire run lock after release: %v", err)
	}
	if err := second.Close(); err != nil {
		t.Fatalf("release second run lock: %v", err)
	}
}

func TestPipelineRun_RejectsAnotherProcessLockOwner(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("HOME", filepath.Join(tmpDir, "home"))
	sourceDir := filepath.Join(tmpDir, "src")
	destDir := filepath.Join(tmpDir, "dest")
	if err := os.MkdirAll(sourceDir, 0755); err != nil {
		t.Fatalf("create source: %v", err)
	}
	if err := os.MkdirAll(destDir, 0755); err != nil {
		t.Fatalf("create destination: %v", err)
	}

	p, err := New(newTestConfig(tmpDir, sourceDir, destDir))
	if err != nil {
		t.Fatalf("create pipeline: %v", err)
	}
	defer p.Close()
	owner, err := acquireRunLock(p.userDataManager.RunLockPath())
	if err != nil {
		t.Fatalf("acquire external owner lock: %v", err)
	}
	defer owner.Close()

	summary, err := p.Run()
	if !errors.Is(err, ErrRunAlreadyActive) {
		t.Fatalf("expected ErrRunAlreadyActive, got summary=%+v err=%v", summary, err)
	}
}

func TestPipelineRun_ReloadsStateAfterLockAcquisition(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("HOME", filepath.Join(tmpDir, "home"))
	sourceDir := filepath.Join(tmpDir, "src")
	destDir := filepath.Join(tmpDir, "dest")
	if err := os.MkdirAll(sourceDir, 0755); err != nil {
		t.Fatalf("create source: %v", err)
	}
	if err := os.MkdirAll(destDir, 0755); err != nil {
		t.Fatalf("create destination: %v", err)
	}
	cfg := newTestConfig(tmpDir, sourceDir, destDir)
	p, err := New(cfg)
	if err != nil {
		t.Fatalf("create pipeline: %v", err)
	}
	defer p.Close()

	newerState := state.New(cfg.StateFile)
	newerState.MarkProcessed("/source/completed-by-another-process.jpg", 42, "/dest/completed.jpg")
	if err := newerState.Save(); err != nil {
		t.Fatalf("save newer process state: %v", err)
	}

	if _, err := p.Run(); err != nil {
		t.Fatalf("run pipeline: %v", err)
	}
	reloaded, err := state.Load(cfg.StateFile)
	if err != nil {
		t.Fatalf("reload final state: %v", err)
	}
	if _, ok := reloaded.Processed["/source/completed-by-another-process.jpg"]; !ok {
		t.Fatal("run overwrote state saved after Pipeline.New")
	}
}
