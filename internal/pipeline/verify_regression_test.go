package pipeline

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/On-Jun9/ShutterPipe/internal/state"
	"github.com/On-Jun9/ShutterPipe/pkg/types"
)

// P1: a trusted re-backup that reclaims a destination already reserved by a
// normal file must not drop that file. Under rename policy the displaced file
// has to be preserved with a renamed copy instead of silently disappearing.
func TestTrustedRebackupPreservesDisplacedNormalFile(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	sourceDir := filepath.Join(home, "source")
	destDir := filepath.Join(home, "dest")
	normalPath := filepath.Join(sourceDir, "a", "photo.jpg")
	rebackupPath := filepath.Join(sourceDir, "b", "photo.jpg")
	for _, dir := range []string{filepath.Dir(normalPath), filepath.Dir(rebackupPath)} {
		if err := os.MkdirAll(dir, 0755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(normalPath, []byte("aaa"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(rebackupPath, []byte("bbb"), 0644); err != nil {
		t.Fatal(err)
	}

	cfg := newTestConfig(home, sourceDir, destDir)
	cfg.ConflictPolicy = types.ConflictPolicyRename

	// Both files classify to the same unclassified destination (same basename).
	dest := filepath.Join(destDir, cfg.UnclassifiedDir, "photo.jpg")
	renamed := filepath.Join(destDir, cfg.UnclassifiedDir, "photo_1.jpg")

	info, err := os.Stat(rebackupPath)
	if err != nil {
		t.Fatal(err)
	}
	st := state.New(cfg.StateFile)
	st.SetRebackupMarker(state.RebackupMarker{
		VerificationRunID:     "verify-1",
		SourcePath:            rebackupPath,
		DestPath:              dest, // trusted historical destination, currently missing on disk
		Verdict:               types.VerifyVerdictMismatch,
		ConfigFingerprint:     stateFingerprint(cfg),
		SourceSize:            info.Size(),
		SourceModTimeUnixNano: info.ModTime().UnixNano(),
	})
	if err := st.Save(); err != nil {
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

	// The re-backup reclaims the destination.
	if got, err := os.ReadFile(dest); err != nil || string(got) != "bbb" {
		t.Fatalf("re-backup did not reclaim destination: content=%q err=%v", got, err)
	}
	// The displaced normal file must still be preserved under a renamed copy.
	if got, err := os.ReadFile(renamed); err != nil || string(got) != "aaa" {
		t.Fatalf("displaced normal file was lost: content=%q err=%v summary=%+v", got, err, summary)
	}
	if summary.Renamed != 1 {
		t.Fatalf("expected the displaced file to be renamed once: %+v", summary)
	}
}

// P2: a re-backup marker must be discarded when the sidecar that produced the
// recorded destination has changed, so the file is re-classified instead of
// being force-overwritten to a stale destination.
func TestApplicableRebackupMarker_DiscardedOnSidecarChange(t *testing.T) {
	st := state.New(filepath.Join(t.TempDir(), "state.json"))
	src := filepath.Join("src", "video.mp4")
	modTime := time.Unix(1_700_000_000, 0)
	marker := state.RebackupMarker{
		SourcePath:             src,
		DestPath:               filepath.Join("dest", "old", "video.mp4"),
		ConfigFingerprint:      "fp",
		SourceSize:             100,
		SourceModTimeUnixNano:  modTime.UnixNano(),
		SidecarPresent:         true,
		SidecarPath:            filepath.Join("src", "video.xml"),
		SidecarSize:            10,
		SidecarModTimeUnixNano: 111,
		SidecarHash:            "old",
	}
	st.SetRebackupMarker(marker)
	p := &Pipeline{state: st}
	entry := types.FileEntry{Path: src, Size: 100, ModTime: modTime}

	changed := state.SourceContext{
		ConfigFingerprint:      "fp",
		SidecarPresent:         true,
		SidecarPath:            marker.SidecarPath,
		SidecarSize:            10,
		SidecarModTimeUnixNano: 111,
		SidecarHash:            "new", // sidecar content changed since the marker was written
	}
	got, ok, err := p.applicableRebackupMarker(context.Background(), entry, changed)
	if err != nil {
		t.Fatal(err)
	}
	if ok {
		t.Fatalf("marker applied despite sidecar change: %+v", got)
	}
	if _, still := st.RebackupMarker(src); still {
		t.Fatal("stale marker was not discarded")
	}
}

// P2 positive control: an unchanged sidecar keeps the marker applicable.
func TestApplicableRebackupMarker_AppliesWhenSidecarUnchanged(t *testing.T) {
	st := state.New(filepath.Join(t.TempDir(), "state.json"))
	src := filepath.Join("src", "video.mp4")
	modTime := time.Unix(1_700_000_000, 0)
	dest := filepath.Join("dest", "old", "video.mp4")
	marker := state.RebackupMarker{
		SourcePath:             src,
		DestPath:               dest,
		ConfigFingerprint:      "fp",
		SourceSize:             100,
		SourceModTimeUnixNano:  modTime.UnixNano(),
		SidecarPresent:         true,
		SidecarPath:            filepath.Join("src", "video.xml"),
		SidecarSize:            10,
		SidecarModTimeUnixNano: 111,
		SidecarHash:            "same",
	}
	st.SetRebackupMarker(marker)
	p := &Pipeline{state: st}
	entry := types.FileEntry{Path: src, Size: 100, ModTime: modTime}
	unchanged := state.SourceContext{
		ConfigFingerprint:      "fp",
		SidecarPresent:         true,
		SidecarPath:            marker.SidecarPath,
		SidecarSize:            10,
		SidecarModTimeUnixNano: 111,
		SidecarHash:            "same",
	}
	got, ok, err := p.applicableRebackupMarker(context.Background(), entry, unchanged)
	if err != nil {
		t.Fatal(err)
	}
	if !ok || got.DestPath != dest {
		t.Fatalf("marker should stay applicable when sidecar is unchanged: ok=%v marker=%+v", ok, got)
	}
}

// P3: run-lock contention is reported through the errRunLockHeld sentinel, while
// a genuine I/O failure to create the lock must not be misclassified as it.
func TestAcquireRunLock_ContentionUsesHeldSentinel(t *testing.T) {
	path := filepath.Join(t.TempDir(), "run.lock")
	first, err := acquireRunLock(path)
	if err != nil {
		t.Fatalf("acquire first lock: %v", err)
	}
	defer first.Close()

	if _, err := acquireRunLock(path); !errors.Is(err, errRunLockHeld) {
		t.Fatalf("contention should map to errRunLockHeld, got %v", err)
	}
}

func TestAcquireRunLock_IOFailureIsNotHeldSentinel(t *testing.T) {
	dir := t.TempDir()
	blocker := filepath.Join(dir, "blocker")
	if err := os.WriteFile(blocker, []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}
	// The lock's parent directory cannot be created because a component is a file.
	path := filepath.Join(blocker, "sub", "run.lock")
	_, err := acquireRunLock(path)
	if err == nil {
		t.Fatal("expected an error when the lock directory cannot be created")
	}
	if errors.Is(err, errRunLockHeld) {
		t.Fatalf("I/O failure misclassified as lock contention: %v", err)
	}
}
