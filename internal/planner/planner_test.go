package planner

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/On-Jun9/ShutterPipe/pkg/types"
)

// TestPlanner_Plan_WithCaptureTime는 테스트 코드 동작을 검증하거나 보조합니다.
func TestPlanner_Plan_WithCaptureTime(t *testing.T) {
	p := New("/dest", "unclassified", types.OrganizeByDate, "")

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

// TestPlanner_Plan_WithoutCaptureTime는 테스트 코드 동작을 검증하거나 보조합니다.
func TestPlanner_Plan_WithoutCaptureTime(t *testing.T) {
	p := New("/dest", "unclassified", types.OrganizeByDate, "")

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

// TestPlanner_Plan_ByEvent_WithEventNameAndRawType는 테스트 코드 동작을 검증하거나 보조합니다.
func TestPlanner_Plan_ByEvent_WithEventNameAndRawType(t *testing.T) {
	// event 전략에서는 YYMMDD-이벤트명/파일타입 구조로 경로가 생성되어야 한다.
	p := New("/dest", "unclassified", types.OrganizeByEvent, "wedding")

	captureTime := time.Date(2026, 1, 2, 9, 0, 0, 0, time.UTC)
	entry := types.FileEntry{
		Path:      "/source/photo.arw",
		Name:      "photo.arw",
		Extension: "arw",
	}
	meta := types.MediaMetadata{CaptureTime: &captureTime}

	task := p.Plan(entry, meta)
	expectedDir := filepath.Join("/dest", "2026", "260102-wedding", "RAW")

	if task.DestDir != expectedDir {
		t.Fatalf("expected %s, got %s", expectedDir, task.DestDir)
	}
	if task.DestPath != filepath.Join(expectedDir, "photo.arw") {
		t.Fatalf("unexpected dest path: %s", task.DestPath)
	}
}

// TestPlanner_Plan_ByEvent_WithoutEventNameAndVideoType는 테스트 코드 동작을 검증하거나 보조합니다.
func TestPlanner_Plan_ByEvent_WithoutEventNameAndVideoType(t *testing.T) {
	// 이벤트명이 없으면 YYMMDD 폴더만 사용하고 영상 확장자는 MP4 폴더로 분류해야 한다.
	p := New("/dest", "unclassified", types.OrganizeByEvent, "")

	captureTime := time.Date(2026, 7, 8, 12, 0, 0, 0, time.UTC)
	entry := types.FileEntry{
		Path:      "/source/clip.xml",
		Name:      "clip.xml",
		Extension: "xml",
	}
	meta := types.MediaMetadata{CaptureTime: &captureTime}

	task := p.Plan(entry, meta)
	expectedDir := filepath.Join("/dest", "2026", "260708", "MP4")

	if task.DestDir != expectedDir {
		t.Fatalf("expected %s, got %s", expectedDir, task.DestDir)
	}
}

// TestPlanner_GetFileTypeFolder_DefaultJPG는 테스트 코드 동작을 검증하거나 보조합니다.
func TestPlanner_GetFileTypeFolder_DefaultJPG(t *testing.T) {
	// RAW/VIDEO 외 확장자는 기본 JPG 폴더로 분류되어야 한다.
	p := New("/dest", "unclassified", types.OrganizeByEvent, "")

	task := p.Plan(types.FileEntry{
		Path:      "/source/image.heic",
		Name:      "image.heic",
		Extension: "heic",
	}, types.MediaMetadata{
		CaptureTime: func() *time.Time {
			tm := time.Date(2026, 8, 9, 0, 0, 0, 0, time.UTC)
			return &tm
		}(),
	})

	if filepath.Base(task.DestDir) != "JPG" {
		t.Fatalf("expected JPG folder, got %s", task.DestDir)
	}
}
