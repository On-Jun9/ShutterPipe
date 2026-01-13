package metadata

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/On-Jun9/ShutterPipe/pkg/types"
)

func TestXMLExtractor_Extract(t *testing.T) {
	tmpDir := t.TempDir()

	xmlContent := `<?xml version="1.0" encoding="UTF-8"?>
<NonRealTimeMeta xmlns="urn:schemas-professionalDisc:nonRealTimeMeta:ver.2.00">
	<CreationDate value="2025-12-31T19:47:25+09:00"/>
</NonRealTimeMeta>`

	xmlPath := filepath.Join(tmpDir, "C0005M01.XML")
	if err := os.WriteFile(xmlPath, []byte(xmlContent), 0644); err != nil {
		t.Fatal(err)
	}

	videoPath := filepath.Join(tmpDir, "C0005.MP4")
	if err := os.WriteFile(videoPath, []byte("fake video"), 0644); err != nil {
		t.Fatal(err)
	}

	extractor := NewXMLExtractor()
	entry := types.FileEntry{
		Path:      videoPath,
		Name:      "C0005.MP4",
		Extension: "mp4",
		IsVideo:   true,
	}

	meta := extractor.Extract(entry)

	if meta.CaptureTime == nil {
		t.Fatalf("expected capture time, got error: %s", meta.Error)
	}

	expected := time.Date(2025, 12, 31, 19, 47, 25, 0, time.FixedZone("", 9*3600))
	if !meta.CaptureTime.Equal(expected) {
		t.Errorf("expected %v, got %v", expected, *meta.CaptureTime)
	}

	if meta.Source != "XML:CreationDate" {
		t.Errorf("expected XML:CreationDate source, got %s", meta.Source)
	}
}

func TestXMLExtractor_NoXMLFile(t *testing.T) {
	tmpDir := t.TempDir()

	videoPath := filepath.Join(tmpDir, "C0005.MP4")
	if err := os.WriteFile(videoPath, []byte("fake video"), 0644); err != nil {
		t.Fatal(err)
	}

	extractor := NewXMLExtractor()
	entry := types.FileEntry{
		Path:      videoPath,
		Name:      "C0005.MP4",
		Extension: "mp4",
		IsVideo:   true,
	}

	meta := extractor.Extract(entry)

	if meta.CaptureTime != nil {
		t.Error("expected nil capture time when XML not found")
	}
	if meta.Error == "" {
		t.Error("expected error message")
	}
}
