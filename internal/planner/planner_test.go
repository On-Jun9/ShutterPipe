package planner

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/On-Jun9/ShutterPipe/pkg/types"
)

func TestPlanner_Plan_WithCaptureTime(t *testing.T) {
	p := New("/dest", "unclassified")

	captureTime := time.Date(2025, 12, 31, 15, 30, 0, 0, time.Local)
	entry := types.FileEntry{
		Path: "/source/photo.jpg",
		Name: "photo.jpg",
	}
	meta := types.MediaMetadata{
		CaptureTime: &captureTime,
		Source:      "EXIF:DateTimeOriginal",
	}

	task := p.Plan(entry, meta)

	expectedDir := filepath.Join("/dest", "2025", "12", "31")
	if task.DestDir != expectedDir {
		t.Errorf("expected %s, got %s", expectedDir, task.DestDir)
	}

	expectedPath := filepath.Join(expectedDir, "photo.jpg")
	if task.DestPath != expectedPath {
		t.Errorf("expected %s, got %s", expectedPath, task.DestPath)
	}
}

func TestPlanner_Plan_WithoutCaptureTime(t *testing.T) {
	p := New("/dest", "unclassified")

	entry := types.FileEntry{
		Path: "/source/photo.jpg",
		Name: "photo.jpg",
	}
	meta := types.MediaMetadata{
		Error: "no EXIF data",
	}

	task := p.Plan(entry, meta)

	expectedDir := filepath.Join("/dest", "unclassified")
	if task.DestDir != expectedDir {
		t.Errorf("expected %s, got %s", expectedDir, task.DestDir)
	}
}
