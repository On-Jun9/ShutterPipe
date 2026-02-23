package metadata

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/On-Jun9/ShutterPipe/pkg/types"
)

// TestExtractorExtract_UsesXMLForVideo는 테스트 코드 동작을 검증하거나 보조합니다.
func TestExtractorExtract_UsesXMLForVideo(t *testing.T) {
	// 비디오 파일은 XMLExtractor 경로를 타고 캡처 시간을 읽어야 한다.
	tmpDir := t.TempDir()
	videoPath := filepath.Join(tmpDir, "C0007.MP4")
	xmlPath := filepath.Join(tmpDir, "C0007M01.XML")

	xmlContent := `<?xml version="1.0" encoding="UTF-8"?>
<NonRealTimeMeta xmlns="urn:schemas-professionalDisc:nonRealTimeMeta:ver.2.00">
	<CreationDate value="2025-12-31T19:47:25+09:00"/>
</NonRealTimeMeta>`

	if err := os.WriteFile(videoPath, []byte("video"), 0644); err != nil {
		t.Fatalf("failed to write video file: %v", err)
	}
	if err := os.WriteFile(xmlPath, []byte(xmlContent), 0644); err != nil {
		t.Fatalf("failed to write xml file: %v", err)
	}

	extractor := New()
	meta := extractor.Extract(types.FileEntry{
		Path:      videoPath,
		Name:      "C0007.MP4",
		Extension: "mp4",
		IsVideo:   true,
	})

	if meta.CaptureTime == nil {
		t.Fatalf("expected capture time, got error: %s", meta.Error)
	}
	if meta.Source != "XML:CreationDate" {
		t.Fatalf("expected XML source, got %s", meta.Source)
	}
}

// TestExtractorExtract_UsesDirectXMLPathForXMLFile는 테스트 코드 동작을 검증하거나 보조합니다.
func TestExtractorExtract_UsesDirectXMLPathForXMLFile(t *testing.T) {
	// XML 파일 자체는 direct XML 추출 경로를 타야 한다.
	tmpDir := t.TempDir()
	xmlPath := filepath.Join(tmpDir, "meta.xml")
	xmlContent := `<?xml version="1.0" encoding="UTF-8"?>
<NonRealTimeMeta xmlns="urn:schemas-professionalDisc:nonRealTimeMeta:ver.2.00">
	<CreationDate value="2026-01-01T00:00:00Z"/>
</NonRealTimeMeta>`

	if err := os.WriteFile(xmlPath, []byte(xmlContent), 0644); err != nil {
		t.Fatalf("failed to write xml file: %v", err)
	}

	extractor := New()
	meta := extractor.Extract(types.FileEntry{
		Path:      xmlPath,
		Name:      "meta.xml",
		Extension: "xml",
		IsVideo:   false,
	})

	if meta.CaptureTime == nil {
		t.Fatalf("expected capture time, got error: %s", meta.Error)
	}
	if meta.Source != "XML:CreationDate(direct)" {
		t.Fatalf("expected direct XML source, got %s", meta.Source)
	}
}

// TestExtractorExtract_UsesEXIFForPhotoAndReturnsErrorOnMissingFile는 테스트 코드 동작을 검증하거나 보조합니다.
func TestExtractorExtract_UsesEXIFForPhotoAndReturnsErrorOnMissingFile(t *testing.T) {
	// 일반 사진은 EXIF 경로를 타며 파일이 없으면 에러를 반환해야 한다.
	extractor := New()
	meta := extractor.Extract(types.FileEntry{
		Path:      "/path/does/not/exist.jpg",
		Name:      "missing.jpg",
		Extension: "jpg",
		IsVideo:   false,
	})

	if meta.Error == "" {
		t.Fatal("expected EXIF extraction error")
	}
}

// TestXMLExtractorExtractFromXMLFile_InvalidDate는 테스트 코드 동작을 검증하거나 보조합니다.
func TestXMLExtractorExtractFromXMLFile_InvalidDate(t *testing.T) {
	// direct XML 추출에서 날짜 포맷이 잘못되면 invalid date 에러여야 한다.
	tmpDir := t.TempDir()
	xmlPath := filepath.Join(tmpDir, "invalid.xml")
	xmlContent := `<?xml version="1.0" encoding="UTF-8"?>
<NonRealTimeMeta xmlns="urn:schemas-professionalDisc:nonRealTimeMeta:ver.2.00">
	<CreationDate value="not-a-date"/>
</NonRealTimeMeta>`

	if err := os.WriteFile(xmlPath, []byte(xmlContent), 0644); err != nil {
		t.Fatalf("failed to write invalid xml file: %v", err)
	}

	extractor := NewXMLExtractor()
	meta := extractor.ExtractFromXMLFile(types.FileEntry{
		Path:      xmlPath,
		Name:      "invalid.xml",
		Extension: "xml",
	})

	if meta.Error == "" {
		t.Fatal("expected invalid date error")
	}
	if !strings.Contains(meta.Error, "invalid date format") {
		t.Fatalf("unexpected error: %s", meta.Error)
	}
}
