package policy

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/On-Jun9/ShutterPipe/pkg/types"
)

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
