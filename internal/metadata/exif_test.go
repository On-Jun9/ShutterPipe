package metadata

import (
	"context"
	"encoding/binary"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/On-Jun9/ShutterPipe/pkg/types"
	"github.com/bep/imagemeta"
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

func TestEXIFExtractor_Extract_IgnoresThumbnailDate(t *testing.T) {
	filePath := filepath.Join(t.TempDir(), "thumbnail-date.tiff")
	writeTIFFWithIFD0AndIFD1Dates(t, filePath, "2025:12:31 12:34:56", "2000:01:01 00:00:00")

	meta := NewEXIFExtractor().Extract(types.FileEntry{Path: filePath, Extension: "tiff"})
	if meta.CaptureTime == nil {
		t.Fatalf("expected capture time, got error: %s", meta.Error)
	}
	want := time.Date(2025, 12, 31, 12, 34, 56, 0, time.Local)
	if !meta.CaptureTime.Equal(want) {
		t.Fatalf("thumbnail date replaced primary date: want=%v got=%v", want, *meta.CaptureTime)
	}
}

func TestEXIFExtractor_Extract_IgnoresSubIFDDate(t *testing.T) {
	filePath := filepath.Join(t.TempDir(), "subifd-date.tiff")
	writeTIFFWithPrimaryAndNestedIFDDate(
		t, filePath, 0x014a, "2025:12:31 12:34:56", "2000:01:01 00:00:00",
	)

	meta := NewEXIFExtractor().Extract(types.FileEntry{Path: filePath, Extension: "tiff"})
	if meta.CaptureTime == nil {
		t.Fatalf("expected capture time, got error: %s", meta.Error)
	}
	if got := meta.CaptureTime.Format("2006:01:02 15:04:05"); got != "2025:12:31 12:34:56" {
		t.Fatalf("SubIFD date replaced primary date: %s", got)
	}
}

func TestEXIFExtractor_Extract_InvalidOriginalFallsBackToDigitizedBeforeModifyDate(t *testing.T) {
	filePath := filepath.Join(t.TempDir(), "invalid-original.tiff")
	writeTIFFWithDateCandidates(t, filePath,
		"0000:00:00 00:00:00",
		"2030:01:02 03:04:05",
		"2020:02:03 04:05:06",
	)

	meta := NewEXIFExtractor().Extract(types.FileEntry{Path: filePath, Extension: "tiff"})
	if meta.CaptureTime == nil {
		t.Fatalf("expected capture time, got error: %s", meta.Error)
	}
	if got := meta.CaptureTime.Format("2006:01:02 15:04:05"); got != "2020:02:03 04:05:06" {
		t.Fatalf("invalid original fallback chose wrong date: %s", got)
	}
	if meta.Source != "EXIF:DateTimeDigitized" {
		t.Fatalf("unexpected source: %s", meta.Source)
	}
}

func TestEXIFExtractor_Extract_MissingOriginalFallsBackToModifyDateBeforeDigitized(t *testing.T) {
	filePath := filepath.Join(t.TempDir(), "missing-original.tiff")
	writeTIFFWithoutOriginalWithFallbacks(t, filePath,
		"2030:01:02 03:04:05",
		"2020:02:03 04:05:06",
	)

	meta := NewEXIFExtractor().Extract(types.FileEntry{Path: filePath, Extension: "tiff"})
	if meta.CaptureTime == nil {
		t.Fatalf("expected capture time, got error: %s", meta.Error)
	}
	if got := meta.CaptureTime.Format("2006:01:02 15:04:05"); got != "2030:01:02 03:04:05" {
		t.Fatalf("missing original fallback chose wrong date: %s", got)
	}
	if meta.Source != "EXIF:DateTimeOriginal" {
		t.Fatalf("unexpected source: %s", meta.Source)
	}
}

func TestEXIFExtractor_Extract_NonStringOriginalFallsBackToDigitizedBeforeModifyDate(t *testing.T) {
	filePath := filepath.Join(t.TempDir(), "non-string-original.tiff")
	writeTIFFWithDateCandidates(t, filePath,
		"0000:00:00 00:00:00",
		"2030:01:02 03:04:05",
		"2020:02:03 04:05:06",
	)
	makeTIFFOriginalNonString(t, filePath)

	meta := NewEXIFExtractor().Extract(types.FileEntry{Path: filePath, Extension: "tiff"})
	if meta.CaptureTime == nil {
		t.Fatalf("expected capture time, got error: %s", meta.Error)
	}
	if got := meta.CaptureTime.Format("2006:01:02 15:04:05"); got != "2020:02:03 04:05:06" {
		t.Fatalf("non-string original fallback chose wrong date: %s", got)
	}
	if meta.Source != "EXIF:DateTimeDigitized" {
		t.Fatalf("unexpected source: %s", meta.Source)
	}
}

func TestEXIFExtractor_Extract_InvalidOriginalWithoutDigitizedDoesNotUseModifyDate(t *testing.T) {
	filePath := filepath.Join(t.TempDir(), "invalid-original-without-digitized.tiff")
	writeTIFFWithDateCandidates(t, filePath,
		"0000:00:00 00:00:00",
		"2030:01:02 03:04:05",
		"2020:02:03 04:05:06",
	)
	removeTIFFDigitizedDateTag(t, filePath)

	meta := NewEXIFExtractor().Extract(types.FileEntry{Path: filePath, Extension: "tiff"})
	if meta.CaptureTime != nil {
		t.Fatalf("invalid original unexpectedly fell back to ModifyDate: %v", *meta.CaptureTime)
	}
	if meta.Error != "no capture time found in EXIF" {
		t.Fatalf("unexpected error: %q", meta.Error)
	}
}

func TestEXIFExtractor_Extract_NonStringOriginalWithoutDigitizedDoesNotUseModifyDate(t *testing.T) {
	filePath := filepath.Join(t.TempDir(), "non-string-original-without-digitized.tiff")
	writeTIFFWithDateCandidates(t, filePath,
		"0000:00:00 00:00:00",
		"2030:01:02 03:04:05",
		"2020:02:03 04:05:06",
	)
	makeTIFFOriginalNonString(t, filePath)
	removeTIFFDigitizedDateTag(t, filePath)

	meta := NewEXIFExtractor().Extract(types.FileEntry{Path: filePath, Extension: "tiff"})
	if meta.CaptureTime != nil {
		t.Fatalf("non-string original unexpectedly fell back to ModifyDate: %v", *meta.CaptureTime)
	}
	if meta.Error != "no capture time found in EXIF" {
		t.Fatalf("unexpected error: %q", meta.Error)
	}
}

func writeTIFFWithoutOriginalWithFallbacks(t *testing.T, path, modifyDate, digitizedDate string) {
	t.Helper()
	const (
		ifd0Offset       = 8
		ifd0Entries      = 2
		ifd0Size         = 2 + ifd0Entries*12 + 4
		modifyData       = ifd0Offset + ifd0Size
		dateByteCount    = 20
		exifIFDOffset    = modifyData + dateByteCount
		exifIFDSize      = 2 + 12 + 4
		digitizedData    = exifIFDOffset + exifIFDSize
		exifIFDPointer   = 0x8769
		modifyDateTag    = 0x0132
		digitizedDateTag = 0x9004
	)
	data := make([]byte, digitizedData+dateByteCount)
	copy(data[:4], []byte{0x49, 0x49, 0x2A, 0x00})
	binary.LittleEndian.PutUint32(data[4:8], ifd0Offset)
	binary.LittleEndian.PutUint16(data[ifd0Offset:ifd0Offset+2], ifd0Entries)
	writeEntry := func(offset int, tag uint16, typ uint16, count, value uint32) {
		entry := data[offset : offset+12]
		binary.LittleEndian.PutUint16(entry[0:2], tag)
		binary.LittleEndian.PutUint16(entry[2:4], typ)
		binary.LittleEndian.PutUint32(entry[4:8], count)
		binary.LittleEndian.PutUint32(entry[8:12], value)
	}
	writeEntry(ifd0Offset+2, modifyDateTag, 2, dateByteCount, modifyData)
	writeEntry(ifd0Offset+14, exifIFDPointer, 4, 1, exifIFDOffset)
	copy(data[modifyData:modifyData+dateByteCount], append([]byte(modifyDate), 0))

	binary.LittleEndian.PutUint16(data[exifIFDOffset:exifIFDOffset+2], 1)
	writeEntry(exifIFDOffset+2, digitizedDateTag, 2, dateByteCount, digitizedData)
	copy(data[digitizedData:digitizedData+dateByteCount], append([]byte(digitizedDate), 0))
	if err := os.WriteFile(path, data, 0644); err != nil {
		t.Fatal(err)
	}
}

func TestEXIFExtractor_ExtractWithContext_StopsBeforeReading(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	meta := NewEXIFExtractor().ExtractWithContext(ctx, types.FileEntry{
		Path:      filepath.Join(t.TempDir(), "missing.arw"),
		Extension: "arw",
	})
	if !strings.Contains(meta.Error, context.Canceled.Error()) {
		t.Fatalf("expected cancellation error, got %q", meta.Error)
	}
}

func TestEXIFImageFormat_SupportsPEF(t *testing.T) {
	format, ok := exifImageFormatByExtension(types.FileEntry{Extension: "pef"})
	if !ok {
		t.Fatal("expected PEF extension to be recognized")
	}
	if format != imagemeta.PEF {
		t.Fatalf("PEF format = %v, want %v", format, imagemeta.PEF)
	}
}

func TestEXIFExtractor_CustomExtensionUsesHeaderFallback(t *testing.T) {
	filePath := filepath.Join(t.TempDir(), "photo.custom")
	writeTIFFWithASCIITag(t, filePath, 0x0132, "2025:12:31 12:34:56")

	meta := NewEXIFExtractor().Extract(types.FileEntry{Path: filePath, Extension: "custom"})
	if meta.CaptureTime == nil {
		t.Fatalf("custom extension metadata was not detected from header: %s", meta.Error)
	}
	if got := meta.CaptureTime.Format("2006:01:02 15:04:05"); got != "2025:12:31 12:34:56" {
		t.Fatalf("unexpected capture time: %s", got)
	}
}

type failOnReadSeeker struct{}

func (failOnReadSeeker) Read([]byte) (int, error)       { return 0, errors.New("unexpected read") }
func (failOnReadSeeker) Seek(int64, int) (int64, error) { return 0, errors.New("unexpected seek") }

func TestDetectEXIFImageFormat_KnownExtensionDoesNotReadHeader(t *testing.T) {
	format, err := detectEXIFImageFormat(failOnReadSeeker{}, types.FileEntry{Extension: "arw"})
	if err != nil {
		t.Fatal(err)
	}
	if format != imagemeta.ARW {
		t.Fatalf("format = %v, want %v", format, imagemeta.ARW)
	}
}

func TestEXIFExtractor_PrefersExifIFDDateTimeOriginal(t *testing.T) {
	filePath := filepath.Join(t.TempDir(), "duplicate-datetime.tiff")
	writeTIFFWithPrimaryAndExifIFDDates(t, filePath, "2030:01:01 00:00:00", "2020:02:03 04:05:06")

	meta := NewEXIFExtractor().Extract(types.FileEntry{Path: filePath, Extension: "tiff"})
	if meta.CaptureTime == nil {
		t.Fatalf("expected capture time, got error: %s", meta.Error)
	}
	if got := meta.CaptureTime.Format("2006:01:02 15:04:05"); got != "2020:02:03 04:05:06" {
		t.Fatalf("ExifIFD date did not win: got %s", got)
	}
}

func TestEXIFExtractor_SonyARWIntegration(t *testing.T) {
	filePath := os.Getenv("SHUTTERPIPE_EXIF_TEST_FILE")
	if filePath == "" {
		t.Skip("SHUTTERPIPE_EXIF_TEST_FILE is not set")
	}
	expectedDate := os.Getenv("SHUTTERPIPE_EXIF_TEST_DATE")

	meta := NewEXIFExtractor().Extract(types.FileEntry{Path: filePath, Extension: "arw"})
	if meta.CaptureTime == nil {
		t.Fatalf("failed to extract Sony ARW capture time: %s", meta.Error)
	}
	if meta.Source != "EXIF:DateTimeOriginal" {
		t.Fatalf("unexpected source: %s", meta.Source)
	}
	if expectedDate != "" && meta.CaptureTime.Format("2006:01:02 15:04:05") != expectedDate {
		t.Fatalf("unexpected capture time: want=%s got=%s", expectedDate, meta.CaptureTime.Format("2006:01:02 15:04:05"))
	}
}

func BenchmarkEXIFExtractor_SonyARW(b *testing.B) {
	filePath := os.Getenv("SHUTTERPIPE_EXIF_TEST_FILE")
	if filePath == "" {
		b.Skip("SHUTTERPIPE_EXIF_TEST_FILE is not set")
	}
	entry := types.FileEntry{Path: filePath, Extension: "arw"}
	extractor := NewEXIFExtractor()

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		meta := extractor.Extract(entry)
		if meta.CaptureTime == nil {
			b.Fatalf("failed to extract capture time: %s", meta.Error)
		}
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

func writeTIFFWithIFD0AndIFD1Dates(t *testing.T, path, primaryDate, thumbnailDate string) {
	t.Helper()
	const (
		ifd0Offset          = 8
		ifd0DataOffset      = 26
		ifd1Offset          = 46
		ifd1DataOffset      = 64
		dateByteCount       = 20
		dateTimeOriginalTag = 0x0132
	)
	data := make([]byte, ifd1DataOffset+dateByteCount)
	copy(data[:4], []byte{0x49, 0x49, 0x2A, 0x00})
	binary.LittleEndian.PutUint32(data[4:8], ifd0Offset)
	writeIFDDate := func(offset, valueOffset, nextOffset int, value string) {
		binary.LittleEndian.PutUint16(data[offset:offset+2], 1)
		entry := data[offset+2 : offset+14]
		binary.LittleEndian.PutUint16(entry[0:2], dateTimeOriginalTag)
		binary.LittleEndian.PutUint16(entry[2:4], 2)
		binary.LittleEndian.PutUint32(entry[4:8], dateByteCount)
		binary.LittleEndian.PutUint32(entry[8:12], uint32(valueOffset))
		binary.LittleEndian.PutUint32(data[offset+14:offset+18], uint32(nextOffset))
		copy(data[valueOffset:valueOffset+dateByteCount], append([]byte(value), 0))
	}
	writeIFDDate(ifd0Offset, ifd0DataOffset, ifd1Offset, primaryDate)
	writeIFDDate(ifd1Offset, ifd1DataOffset, 0, thumbnailDate)
	if err := os.WriteFile(path, data, 0644); err != nil {
		t.Fatal(err)
	}
}

func writeTIFFWithPrimaryAndExifIFDDates(t *testing.T, path, primaryDate, exifDate string) {
	writeTIFFWithPrimaryAndNestedIFDDate(t, path, 0x8769, primaryDate, exifDate)
}

func writeTIFFWithPrimaryAndNestedIFDDate(t *testing.T, path string, pointerTag uint16, primaryDate, nestedDate string) {
	t.Helper()
	const (
		ifd0Offset      = 8
		ifd0EntryCount  = 2
		ifd0Size        = 2 + ifd0EntryCount*12 + 4
		primaryData     = ifd0Offset + ifd0Size
		dateByteCount   = 20
		exifIFDOffset   = primaryData + dateByteCount
		exifIFDSize     = 2 + 12 + 4
		exifDateData    = exifIFDOffset + exifIFDSize
		dateOriginalTag = 0x9003
	)
	data := make([]byte, exifDateData+dateByteCount)
	copy(data[:4], []byte{0x49, 0x49, 0x2A, 0x00})
	binary.LittleEndian.PutUint32(data[4:8], ifd0Offset)
	binary.LittleEndian.PutUint16(data[ifd0Offset:ifd0Offset+2], ifd0EntryCount)

	direct := data[ifd0Offset+2 : ifd0Offset+14]
	binary.LittleEndian.PutUint16(direct[0:2], dateOriginalTag)
	binary.LittleEndian.PutUint16(direct[2:4], 2)
	binary.LittleEndian.PutUint32(direct[4:8], dateByteCount)
	binary.LittleEndian.PutUint32(direct[8:12], primaryData)

	pointer := data[ifd0Offset+14 : ifd0Offset+26]
	binary.LittleEndian.PutUint16(pointer[0:2], pointerTag)
	binary.LittleEndian.PutUint16(pointer[2:4], 4)
	binary.LittleEndian.PutUint32(pointer[4:8], 1)
	binary.LittleEndian.PutUint32(pointer[8:12], exifIFDOffset)
	copy(data[primaryData:primaryData+dateByteCount], append([]byte(primaryDate), 0))

	binary.LittleEndian.PutUint16(data[exifIFDOffset:exifIFDOffset+2], 1)
	exifEntry := data[exifIFDOffset+2 : exifIFDOffset+14]
	binary.LittleEndian.PutUint16(exifEntry[0:2], dateOriginalTag)
	binary.LittleEndian.PutUint16(exifEntry[2:4], 2)
	binary.LittleEndian.PutUint32(exifEntry[4:8], dateByteCount)
	binary.LittleEndian.PutUint32(exifEntry[8:12], exifDateData)
	copy(data[exifDateData:exifDateData+dateByteCount], append([]byte(nestedDate), 0))

	if err := os.WriteFile(path, data, 0644); err != nil {
		t.Fatal(err)
	}
}

func writeTIFFWithDateCandidates(t *testing.T, path, originalDate, modifyDate, digitizedDate string) {
	t.Helper()
	const (
		ifd0Offset       = 8
		ifd0Entries      = 2
		ifd0Size         = 2 + ifd0Entries*12 + 4
		modifyData       = ifd0Offset + ifd0Size
		dateByteCount    = 20
		exifIFDOffset    = modifyData + dateByteCount
		exifEntries      = 2
		exifIFDSize      = 2 + exifEntries*12 + 4
		originalData     = exifIFDOffset + exifIFDSize
		digitizedData    = originalData + dateByteCount
		exifIFDPointer   = 0x8769
		modifyDateTag    = 0x0132
		originalDateTag  = 0x9003
		digitizedDateTag = 0x9004
	)
	data := make([]byte, digitizedData+dateByteCount)
	copy(data[:4], []byte{0x49, 0x49, 0x2A, 0x00})
	binary.LittleEndian.PutUint32(data[4:8], ifd0Offset)
	binary.LittleEndian.PutUint16(data[ifd0Offset:ifd0Offset+2], ifd0Entries)
	writeEntry := func(offset int, tag uint16, typ uint16, count, value uint32) {
		entry := data[offset : offset+12]
		binary.LittleEndian.PutUint16(entry[0:2], tag)
		binary.LittleEndian.PutUint16(entry[2:4], typ)
		binary.LittleEndian.PutUint32(entry[4:8], count)
		binary.LittleEndian.PutUint32(entry[8:12], value)
	}
	writeEntry(ifd0Offset+2, modifyDateTag, 2, dateByteCount, modifyData)
	writeEntry(ifd0Offset+14, exifIFDPointer, 4, 1, exifIFDOffset)
	copy(data[modifyData:modifyData+dateByteCount], append([]byte(modifyDate), 0))

	binary.LittleEndian.PutUint16(data[exifIFDOffset:exifIFDOffset+2], exifEntries)
	writeEntry(exifIFDOffset+2, originalDateTag, 2, dateByteCount, originalData)
	writeEntry(exifIFDOffset+14, digitizedDateTag, 2, dateByteCount, digitizedData)
	copy(data[originalData:originalData+dateByteCount], append([]byte(originalDate), 0))
	copy(data[digitizedData:digitizedData+dateByteCount], append([]byte(digitizedDate), 0))

	if err := os.WriteFile(path, data, 0644); err != nil {
		t.Fatal(err)
	}
}

func makeTIFFOriginalNonString(t *testing.T, path string) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	const (
		ifd0Offset    = 8
		ifd0Entries   = 2
		ifd0Size      = 2 + ifd0Entries*12 + 4
		modifyData    = ifd0Offset + ifd0Size
		dateByteCount = 20
		exifIFDOffset = modifyData + dateByteCount
	)
	originalEntry := data[exifIFDOffset+2 : exifIFDOffset+14]
	binary.LittleEndian.PutUint16(originalEntry[2:4], 4)
	binary.LittleEndian.PutUint32(originalEntry[4:8], 1)
	binary.LittleEndian.PutUint32(originalEntry[8:12], 1234)
	if err := os.WriteFile(path, data, 0644); err != nil {
		t.Fatal(err)
	}
}

func removeTIFFDigitizedDateTag(t *testing.T, path string) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	const (
		ifd0Offset    = 8
		ifd0Entries   = 2
		ifd0Size      = 2 + ifd0Entries*12 + 4
		modifyData    = ifd0Offset + ifd0Size
		dateByteCount = 20
		exifIFDOffset = modifyData + dateByteCount
	)
	binary.LittleEndian.PutUint16(data[exifIFDOffset:exifIFDOffset+2], 1)
	for index := exifIFDOffset + 14; index < exifIFDOffset+18; index++ {
		data[index] = 0
	}
	if err := os.WriteFile(path, data, 0644); err != nil {
		t.Fatal(err)
	}
}
