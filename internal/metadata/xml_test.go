package metadata

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/On-Jun9/ShutterPipe/pkg/types"
)

// TestXMLExtractor_Extract는 테스트 코드 동작을 검증하거나 보조합니다.
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

// TestXMLExtractor_NoXMLFile는 테스트 코드 동작을 검증하거나 보조합니다.
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

func TestSidecarIdentityIncludesActualPathAndContentHash(t *testing.T) {
	tmpDir := t.TempDir()
	videoPath := filepath.Join(tmpDir, "clip.mp4")
	sidecarPath := filepath.Join(tmpDir, "clipM01.xml")
	if err := os.WriteFile(videoPath, []byte("video"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(sidecarPath, []byte("metadata"), 0644); err != nil {
		t.Fatal(err)
	}
	identity, ok, err := SidecarIdentity(context.Background(), types.FileEntry{
		Path: videoPath, Name: "clip.mp4", Extension: "mp4", IsVideo: true,
	})
	if err != nil || !ok {
		t.Fatalf("sidecar identity failed: ok=%v err=%v", ok, err)
	}
	if filepath.Base(identity.Path) != "clipM01.xml" {
		t.Fatalf("sidecar identity lost actual directory-entry spelling: %q", identity.Path)
	}
	if identity.Hash == "" || identity.Size != int64(len("metadata")) {
		t.Fatalf("sidecar identity omitted content identity: %+v", identity)
	}
}

// TestExtractFromSidecarUsesIdentitySnapshot는 identity 캡처 후 sidecar 파일이
// 교체되어도 분류가 identity와 같은 내용 스냅샷에서 이루어지는지 검증한다.
// 파일을 다시 열면 state에는 이전 해시, 목적지 분류에는 이후 내용이 기록될 수 있다.
func TestExtractFromSidecarUsesIdentitySnapshot(t *testing.T) {
	tmpDir := t.TempDir()
	videoPath := filepath.Join(tmpDir, "clip.mp4")
	sidecarPath := filepath.Join(tmpDir, "clipM01.xml")
	originalXML := `<?xml version="1.0"?>
<NonRealTimeMeta xmlns="urn:schemas-professionalDisc:nonRealTimeMeta:ver.2.00">
	<CreationDate value="2025-12-31T19:47:25+09:00"/>
</NonRealTimeMeta>`
	swappedXML := `<?xml version="1.0"?>
<NonRealTimeMeta xmlns="urn:schemas-professionalDisc:nonRealTimeMeta:ver.2.00">
	<CreationDate value="2026-06-15T08:00:00+09:00"/>
</NonRealTimeMeta>`
	if err := os.WriteFile(videoPath, []byte("video"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(sidecarPath, []byte(originalXML), 0644); err != nil {
		t.Fatal(err)
	}

	identity, ok, err := SidecarIdentity(context.Background(), types.FileEntry{
		Path: videoPath, Name: "clip.mp4", Extension: "mp4", IsVideo: true,
	})
	if err != nil || !ok {
		t.Fatalf("sidecar identity failed: ok=%v err=%v", ok, err)
	}

	// identity 캡처와 분류 사이에 sidecar가 교체되는 상황을 재현한다.
	if err := os.WriteFile(sidecarPath, []byte(swappedXML), 0644); err != nil {
		t.Fatal(err)
	}

	meta := ExtractFromSidecar(identity)
	if meta.Error != "" {
		t.Fatalf("unexpected extract error: %s", meta.Error)
	}
	if meta.CaptureTime == nil || meta.CaptureTime.Year() != 2025 {
		t.Fatalf("classification did not use the identity snapshot: %+v", meta.CaptureTime)
	}
}

// TestXMLExtractor_Extract_ReturnsReadError는 테스트 코드 동작을 검증하거나 보조합니다.
func TestXMLExtractor_Extract_ReturnsReadError(t *testing.T) {
	// XML 경로가 디렉터리면 read 에러를 반환해야 한다.
	tmpDir := t.TempDir()
	videoPath := filepath.Join(tmpDir, "C1000.MP4")
	if err := os.WriteFile(videoPath, []byte("fake video"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(tmpDir, "C1000M01.XML"), 0755); err != nil {
		t.Fatal(err)
	}

	extractor := NewXMLExtractor()
	meta := extractor.Extract(types.FileEntry{
		Path:      videoPath,
		Name:      "C1000.MP4",
		Extension: "mp4",
		IsVideo:   true,
	})

	if !strings.Contains(meta.Error, "failed to read XML") {
		t.Fatalf("expected read error, got %s", meta.Error)
	}
}

// TestXMLExtractor_Extract_ReturnsParseError는 테스트 코드 동작을 검증하거나 보조합니다.
func TestXMLExtractor_Extract_ReturnsParseError(t *testing.T) {
	// XML 파싱 실패 시 parse 에러를 반환해야 한다.
	tmpDir := t.TempDir()
	videoPath := filepath.Join(tmpDir, "C1001.MP4")
	xmlPath := filepath.Join(tmpDir, "C1001M01.XML")

	if err := os.WriteFile(videoPath, []byte("fake video"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(xmlPath, []byte("<NonRealTimeMeta>"), 0644); err != nil {
		t.Fatal(err)
	}

	extractor := NewXMLExtractor()
	meta := extractor.Extract(types.FileEntry{
		Path:      videoPath,
		Name:      "C1001.MP4",
		Extension: "mp4",
		IsVideo:   true,
	})

	if !strings.Contains(meta.Error, "failed to parse XML") {
		t.Fatalf("expected parse error, got %s", meta.Error)
	}
}

// TestXMLExtractor_Extract_ReturnsCreationDateMissingError는 테스트 코드 동작을 검증하거나 보조합니다.
func TestXMLExtractor_Extract_ReturnsCreationDateMissingError(t *testing.T) {
	// CreationDate가 없으면 명시적인 에러를 반환해야 한다.
	tmpDir := t.TempDir()
	videoPath := filepath.Join(tmpDir, "C1002.MP4")
	xmlPath := filepath.Join(tmpDir, "C1002M01.XML")

	xmlContent := `<?xml version="1.0" encoding="UTF-8"?>
<NonRealTimeMeta xmlns="urn:schemas-professionalDisc:nonRealTimeMeta:ver.2.00">
</NonRealTimeMeta>`

	if err := os.WriteFile(videoPath, []byte("fake video"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(xmlPath, []byte(xmlContent), 0644); err != nil {
		t.Fatal(err)
	}

	extractor := NewXMLExtractor()
	meta := extractor.Extract(types.FileEntry{
		Path:      videoPath,
		Name:      "C1002.MP4",
		Extension: "mp4",
		IsVideo:   true,
	})

	if meta.Error != "CreationDate not found in XML" {
		t.Fatalf("unexpected error: %s", meta.Error)
	}
}

// TestXMLExtractor_Extract_ReturnsInvalidDateError는 테스트 코드 동작을 검증하거나 보조합니다.
func TestXMLExtractor_Extract_ReturnsInvalidDateError(t *testing.T) {
	// CreationDate 포맷이 RFC3339가 아니면 invalid date 에러여야 한다.
	tmpDir := t.TempDir()
	videoPath := filepath.Join(tmpDir, "C1003.MP4")
	xmlPath := filepath.Join(tmpDir, "C1003M01.XML")

	xmlContent := `<?xml version="1.0" encoding="UTF-8"?>
<NonRealTimeMeta xmlns="urn:schemas-professionalDisc:nonRealTimeMeta:ver.2.00">
	<CreationDate value="2025/12/31 19:47:25"/>
</NonRealTimeMeta>`

	if err := os.WriteFile(videoPath, []byte("fake video"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(xmlPath, []byte(xmlContent), 0644); err != nil {
		t.Fatal(err)
	}

	extractor := NewXMLExtractor()
	meta := extractor.Extract(types.FileEntry{
		Path:      videoPath,
		Name:      "C1003.MP4",
		Extension: "mp4",
		IsVideo:   true,
	})

	if !strings.Contains(meta.Error, "invalid date format") {
		t.Fatalf("expected invalid date error, got %s", meta.Error)
	}
}

// TestXMLExtractor_ExtractFromXMLFile_ReturnsReadError는 테스트 코드 동작을 검증하거나 보조합니다.
func TestXMLExtractor_ExtractFromXMLFile_ReturnsReadError(t *testing.T) {
	// direct XML 추출에서 파일이 없으면 read 에러를 반환해야 한다.
	extractor := NewXMLExtractor()
	meta := extractor.ExtractFromXMLFile(types.FileEntry{
		Path:      "/path/does/not/exist.xml",
		Name:      "missing.xml",
		Extension: "xml",
	})

	if !strings.Contains(meta.Error, "failed to read XML") {
		t.Fatalf("expected read error, got %s", meta.Error)
	}
}

// TestXMLExtractor_ExtractFromXMLFile_ReturnsParseError는 테스트 코드 동작을 검증하거나 보조합니다.
func TestXMLExtractor_ExtractFromXMLFile_ReturnsParseError(t *testing.T) {
	// direct XML 추출에서 파싱 실패 시 parse 에러를 반환해야 한다.
	tmpDir := t.TempDir()
	xmlPath := filepath.Join(tmpDir, "broken.xml")
	if err := os.WriteFile(xmlPath, []byte("<NonRealTimeMeta>"), 0644); err != nil {
		t.Fatal(err)
	}

	extractor := NewXMLExtractor()
	meta := extractor.ExtractFromXMLFile(types.FileEntry{
		Path:      xmlPath,
		Name:      "broken.xml",
		Extension: "xml",
	})

	if !strings.Contains(meta.Error, "failed to parse XML") {
		t.Fatalf("expected parse error, got %s", meta.Error)
	}
}

// TestXMLExtractor_ExtractFromXMLFile_ReturnsCreationDateMissingError는 테스트 코드 동작을 검증하거나 보조합니다.
func TestXMLExtractor_ExtractFromXMLFile_ReturnsCreationDateMissingError(t *testing.T) {
	// direct XML 추출에서 CreationDate가 없으면 명시 에러를 반환해야 한다.
	tmpDir := t.TempDir()
	xmlPath := filepath.Join(tmpDir, "no-date.xml")
	xmlContent := `<?xml version="1.0" encoding="UTF-8"?>
<NonRealTimeMeta xmlns="urn:schemas-professionalDisc:nonRealTimeMeta:ver.2.00">
</NonRealTimeMeta>`
	if err := os.WriteFile(xmlPath, []byte(xmlContent), 0644); err != nil {
		t.Fatal(err)
	}

	extractor := NewXMLExtractor()
	meta := extractor.ExtractFromXMLFile(types.FileEntry{
		Path:      xmlPath,
		Name:      "no-date.xml",
		Extension: "xml",
	})

	if meta.Error != "CreationDate not found in XML" {
		t.Fatalf("unexpected error: %s", meta.Error)
	}
}

// TestXMLExtractor_Extract_FindsLowercaseXMLPath는 테스트 코드 동작을 검증하거나 보조합니다.
func TestXMLExtractor_Extract_FindsLowercaseXMLPath(t *testing.T) {
	// M01.XML이 없고 M01.xml만 있을 때도 메타데이터를 찾아야 한다.
	tmpDir := t.TempDir()
	videoPath := filepath.Join(tmpDir, "C1004.MP4")
	xmlPath := filepath.Join(tmpDir, "C1004M01.xml")

	xmlContent := `<?xml version="1.0" encoding="UTF-8"?>
<NonRealTimeMeta xmlns="urn:schemas-professionalDisc:nonRealTimeMeta:ver.2.00">
	<CreationDate value="2026-01-01T00:00:00Z"/>
</NonRealTimeMeta>`

	if err := os.WriteFile(videoPath, []byte("fake video"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(xmlPath, []byte(xmlContent), 0644); err != nil {
		t.Fatal(err)
	}

	extractor := NewXMLExtractor()
	meta := extractor.Extract(types.FileEntry{
		Path:      videoPath,
		Name:      "C1004.MP4",
		Extension: "mp4",
		IsVideo:   true,
	})

	if meta.CaptureTime == nil {
		t.Fatalf("expected capture time, got error: %s", meta.Error)
	}
}
