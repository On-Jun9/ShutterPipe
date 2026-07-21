package state

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/On-Jun9/ShutterPipe/pkg/types"
)

func TestRebackupMarkerSaveAndLoadRoundTrip(t *testing.T) {
	statePath := filepath.Join(t.TempDir(), "state.json")
	st := New(statePath)
	st.SetRebackupMarker(RebackupMarker{
		VerificationRunID:     "verify-1",
		SourcePath:            "/source/photo.jpg",
		DestPath:              "/dest/photo.jpg",
		Verdict:               types.VerifyVerdictMismatch,
		ConfigFingerprint:     "fingerprint",
		SourceSize:            12,
		SourceModTimeUnixNano: 34,
		SourceHash:            "abc",
	})
	if err := st.Save(); err != nil {
		t.Fatal(err)
	}

	loaded, err := Load(statePath)
	if err != nil {
		t.Fatal(err)
	}
	marker, ok := loaded.RebackupMarker("/source/photo.jpg")
	if !ok || marker.VerificationRunID != "verify-1" || marker.DestPath != "/dest/photo.jpg" {
		t.Fatalf("unexpected marker: %+v, ok=%v", marker, ok)
	}
}

func TestProcessedSnapshotForRequeueUsesStableIdentityAndConditionalDestination(t *testing.T) {
	modTime := time.Unix(100, 200)
	entry := types.FileEntry{
		Path: "/source/photo.jpg", Size: 12, ModTime: modTime,
	}
	sctx := SourceContext{ConfigFingerprint: "fingerprint"}
	record := ProcessedFile{
		Path: entry.Path, Size: entry.Size, DestPath: "/dest/photo.jpg",
		Timestamp: modTime, IdentityVersion: 2,
		SourceModTimeUnixNano: entry.ModTime.UnixNano(),
		ConfigFingerprint:     sctx.ConfigFingerprint,
	}
	st := New(filepath.Join(t.TempDir(), "state.json"))
	st.Processed[entry.Path] = record

	recordID, present, destPath := st.ProcessedSnapshotForRequeue(entry, sctx)
	if !present || recordID == "" || destPath != record.DestPath {
		t.Fatalf("id=%q present=%v dest=%q", recordID, present, destPath)
	}
	if recordID != ProcessedRecordID(record) {
		t.Fatal("snapshot ID does not identify the stored record")
	}

	record.Timestamp = record.Timestamp.Add(time.Nanosecond)
	if ProcessedRecordID(record) == recordID {
		t.Fatal("changed record retained the same identity")
	}
}

func TestProcessedSnapshotForRequeueNeverTrustsSupersededDestination(t *testing.T) {
	modTime := time.Unix(100, 200)
	entry := types.FileEntry{Path: "/source/photo.jpg", Size: 12, ModTime: modTime}
	sctx := SourceContext{ConfigFingerprint: "fingerprint"}
	st := New(filepath.Join(t.TempDir(), "state.json"))
	st.Processed[entry.Path] = ProcessedFile{
		Path: entry.Path, Size: entry.Size, DestPath: "/dest/winner.jpg",
		IdentityVersion: 2, SourceModTimeUnixNano: modTime.UnixNano(),
		ConfigFingerprint: sctx.ConfigFingerprint, Superseded: true,
	}

	recordID, present, destPath := st.ProcessedSnapshotForRequeue(entry, sctx)
	if !present || recordID == "" {
		t.Fatal("the record must still participate in stale-result detection")
	}
	if destPath != "" {
		t.Fatalf("superseded destination was trusted: %s", destPath)
	}
}
