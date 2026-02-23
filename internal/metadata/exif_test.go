package metadata

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/On-Jun9/ShutterPipe/pkg/types"
)

// TestEXIFExtractor_Extract_ReturnsErrorWhenSourceMissing는 테스트 코드 동작을 검증하거나 보조합니다.
func TestEXIFExtractor_Extract_ReturnsErrorWhenSourceMissing(t *testing.T) {
	// 파일 오픈 자체가 실패하면 에러 메시지를 반환해야 한다.
	extractor := NewEXIFExtractor()
	meta := extractor.Extract(types.FileEntry{
		Path:      "/path/does/not/exist.jpg",
		Name:      "missing.jpg",
		Extension: "jpg",
	})

	if meta.Error == "" {
		t.Fatal("expected error for missing source file")
	}
}

// TestEXIFExtractor_Extract_ReturnsNoEXIFDataForPlainFile는 테스트 코드 동작을 검증하거나 보조합니다.
func TestEXIFExtractor_Extract_ReturnsNoEXIFDataForPlainFile(t *testing.T) {
	// EXIF 없는 일반 파일은 "no EXIF data" 에러 경로를 타야 한다.
	tmpDir := t.TempDir()
	filePath := filepath.Join(tmpDir, "plain.jpg")
	if err := os.WriteFile(filePath, []byte("not-a-real-jpeg-with-exif"), 0644); err != nil {
		t.Fatalf("failed to write plain file: %v", err)
	}

	extractor := NewEXIFExtractor()
	meta := extractor.Extract(types.FileEntry{
		Path:      filePath,
		Name:      "plain.jpg",
		Extension: "jpg",
	})

	if meta.Error == "" {
		t.Fatal("expected no EXIF data error")
	}
	if !strings.Contains(meta.Error, "no EXIF data") {
		t.Fatalf("unexpected error message: %s", meta.Error)
	}
}

// TestEXIFExtractor_Extract_UsesDateTimeTag는 테스트 코드 동작을 검증하거나 보조합니다.
func TestEXIFExtractor_Extract_UsesDateTimeTag(t *testing.T) {
	// DateTime 태그가 있으면 EXIF:DateTimeOriginal 경로로 캡처 시간이 반환되어야 한다.
	filePath := filepath.Join(t.TempDir(), "datetime.tiff")
	writeTIFFWithASCIITag(t, filePath, 0x0132, "2025:12:31 12:34:56")

	extractor := NewEXIFExtractor()
	meta := extractor.Extract(types.FileEntry{
		Path:      filePath,
		Name:      "datetime.tiff",
		Extension: "tiff",
	})

	if meta.CaptureTime == nil {
		t.Fatalf("expected capture time, got error: %s", meta.Error)
	}
	if meta.Source != "EXIF:DateTimeOriginal" {
		t.Fatalf("expected EXIF:DateTimeOriginal, got %s", meta.Source)
	}

	expected := time.Date(2025, 12, 31, 12, 34, 56, 0, time.Local)
	if !meta.CaptureTime.Equal(expected) {
		t.Fatalf("unexpected capture time: want=%v got=%v", expected, *meta.CaptureTime)
	}
}

// TestEXIFExtractor_Extract_FallsBackToDateTimeDigitized는 테스트 코드 동작을 검증하거나 보조합니다.
func TestEXIFExtractor_Extract_FallsBackToDateTimeDigitized(t *testing.T) {
	// DateTime 태그가 없고 DateTimeDigitized만 있으면 fallback 분기를 타야 한다.
	filePath := filepath.Join(t.TempDir(), "digitized.tiff")
	writeTIFFWithASCIITag(t, filePath, 0x9004, "2024:01:02 03:04:05")

	extractor := NewEXIFExtractor()
	meta := extractor.Extract(types.FileEntry{
		Path:      filePath,
		Name:      "digitized.tiff",
		Extension: "tiff",
	})

	if meta.CaptureTime == nil {
		t.Fatalf("expected capture time, got error: %s", meta.Error)
	}
	if meta.Source != "EXIF:DateTimeDigitized" {
		t.Fatalf("expected EXIF:DateTimeDigitized, got %s", meta.Source)
	}
}

// TestEXIFExtractor_Extract_NoCaptureTimeFound는 테스트 코드 동작을 검증하거나 보조합니다.
func TestEXIFExtractor_Extract_NoCaptureTimeFound(t *testing.T) {
	// EXIF는 읽히지만 날짜 태그가 없으면 no capture time 에러를 반환해야 한다.
	filePath := filepath.Join(t.TempDir(), "no-date.tiff")
	writeMinimalTIFFWithoutTags(t, filePath)

	extractor := NewEXIFExtractor()
	meta := extractor.Extract(types.FileEntry{
		Path:      filePath,
		Name:      "no-date.tiff",
		Extension: "tiff",
	})

	if meta.Error != "no capture time found in EXIF" {
		t.Fatalf("unexpected error: %s", meta.Error)
	}
}

// writeMinimalTIFFWithoutTags는 테스트 코드 동작을 검증하거나 보조합니다.
func writeMinimalTIFFWithoutTags(t *testing.T, path string) {
	t.Helper()

	data := []byte{
		0x49, 0x49, 0x2A, 0x00, // little-endian TIFF header
		0x08, 0x00, 0x00, 0x00, // first IFD offset
		0x00, 0x00, // number of IFD entries
		0x00, 0x00, 0x00, 0x00, // next IFD offset
	}
	if err := os.WriteFile(path, data, 0644); err != nil {
		t.Fatalf("failed to write minimal tiff: %v", err)
	}
}

// writeTIFFWithASCIITag는 테스트 코드 동작을 검증하거나 보조합니다.
func writeTIFFWithASCIITag(t *testing.T, path string, tagID uint16, value string) {
	t.Helper()

	ascii := append([]byte(value), 0x00)
	count := len(ascii)
	dataOffset := uint32(26) // header(8) + count(2) + entry(12) + nextIFD(4)

	data := []byte{
		0x49, 0x49, 0x2A, 0x00, // little-endian TIFF header
		0x08, 0x00, 0x00, 0x00, // first IFD offset
		0x01, 0x00, // number of IFD entries
		byte(tagID & 0xFF), byte(tagID >> 8), // tag ID
		0x02, 0x00, // ASCII type
		byte(count & 0xFF), byte((count >> 8) & 0xFF), byte((count >> 16) & 0xFF), byte((count >> 24) & 0xFF), // count
		byte(dataOffset & 0xFF), byte((dataOffset >> 8) & 0xFF), byte((dataOffset >> 16) & 0xFF), byte((dataOffset >> 24) & 0xFF), // data offset
		0x00, 0x00, 0x00, 0x00, // next IFD offset
	}
	data = append(data, ascii...)

	if err := os.WriteFile(path, data, 0644); err != nil {
		t.Fatalf("failed to write tiff with exif tag: %v", err)
	}
}
