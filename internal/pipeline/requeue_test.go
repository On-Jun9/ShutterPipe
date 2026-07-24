package pipeline

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/On-Jun9/ShutterPipe/internal/state"
	"github.com/On-Jun9/ShutterPipe/pkg/types"
)

func TestQueueVerificationProblemsIsAtomicAndIdempotent(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	sourcePath := filepath.Join(home, "source.jpg")
	if err := os.WriteFile(sourcePath, []byte("source"), 0644); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(sourcePath)
	if err != nil {
		t.Fatal(err)
	}
	entry := types.FileEntry{Path: sourcePath, Name: "source.jpg", Size: info.Size(), ModTime: info.ModTime()}
	sctx := state.SourceContext{ConfigFingerprint: "fingerprint"}
	statePath := filepath.Join(home, "state", "state.json")
	st := state.New(statePath)
	st.Processed[sourcePath] = state.ProcessedFile{
		Path: sourcePath, Size: entry.Size, DestPath: filepath.Join(home, "dest.jpg"),
		Timestamp: time.Now(), IdentityVersion: 2,
		SourceModTimeUnixNano: entry.ModTime.UnixNano(),
		ConfigFingerprint:     sctx.ConfigFingerprint,
	}
	recordID, present, _ := st.ProcessedSnapshotForRequeue(entry, sctx)
	if err := st.Save(); err != nil {
		t.Fatal(err)
	}
	candidate := RequeueCandidate{
		Entry: entry, SourceContext: sctx, Verdict: types.VerifyVerdictMismatch,
		Mode: types.VerifyModeHash, SourceHash: "abc",
		ProcessedRecordID: recordID, ProcessedPresent: present,
	}

	result, err := QueueVerificationProblems(context.Background(), statePath, "verify-1", []RequeueCandidate{candidate})
	if err != nil {
		t.Fatal(err)
	}
	if result.Applied != 1 || result.Stale != 0 {
		t.Fatalf("unexpected result: %+v", result)
	}
	loaded, err := state.Load(statePath)
	if err != nil {
		t.Fatal(err)
	}
	marker, ok := loaded.RebackupMarker(sourcePath)
	if !ok || marker.DestPath != st.Processed[sourcePath].DestPath {
		t.Fatalf("trusted destination was not recorded: %+v, ok=%v", marker, ok)
	}

	result, err = QueueVerificationProblems(context.Background(), statePath, "verify-1", []RequeueCandidate{candidate})
	if err != nil {
		t.Fatal(err)
	}
	if result.Applied != 0 || result.Stale != 1 {
		t.Fatalf("second call was not idempotent: %+v", result)
	}
}

func TestQueueVerificationProblemsRejectsChangedProcessedRecordAsStale(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	sourcePath := filepath.Join(home, "source.jpg")
	if err := os.WriteFile(sourcePath, []byte("source"), 0644); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(sourcePath)
	if err != nil {
		t.Fatal(err)
	}
	entry := types.FileEntry{Path: sourcePath, Name: "source.jpg", Size: info.Size(), ModTime: info.ModTime()}
	sctx := state.SourceContext{ConfigFingerprint: "fingerprint"}
	statePath := filepath.Join(home, "state.json")
	st := state.New(statePath)
	record := state.ProcessedFile{
		Path: sourcePath, Size: entry.Size, DestPath: filepath.Join(home, "dest.jpg"),
		Timestamp: time.Now(), IdentityVersion: 2,
		SourceModTimeUnixNano: entry.ModTime.UnixNano(), ConfigFingerprint: sctx.ConfigFingerprint,
	}
	st.Processed[sourcePath] = record
	recordID, _, _ := st.ProcessedSnapshotForRequeue(entry, sctx)
	record.Timestamp = record.Timestamp.Add(time.Second)
	st.Processed[sourcePath] = record
	if err := st.Save(); err != nil {
		t.Fatal(err)
	}

	result, err := QueueVerificationProblems(context.Background(), statePath, "verify-1", []RequeueCandidate{{
		Entry: entry, SourceContext: sctx, Verdict: types.VerifyVerdictMissing,
		Mode: types.VerifyModeQuick, ProcessedPresent: true, ProcessedRecordID: recordID,
	}})
	if err != nil {
		t.Fatal(err)
	}
	if result.Applied != 0 || result.Stale != 1 {
		t.Fatalf("unexpected result: %+v", result)
	}
	loaded, err := state.Load(statePath)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := loaded.RebackupMarker(sourcePath); ok {
		t.Fatal("stale result created a marker")
	}
}
