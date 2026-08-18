package metadata

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/On-Jun9/ShutterPipe/pkg/types"
	"github.com/bep/imagemeta"
)

type EXIFExtractor struct{}

func NewEXIFExtractor() *EXIFExtractor {
	return &EXIFExtractor{}
}

func (e *EXIFExtractor) Extract(entry types.FileEntry) types.MediaMetadata {
	return e.ExtractWithContext(context.Background(), entry)
}

func (e *EXIFExtractor) ExtractWithContext(ctx context.Context, entry types.FileEntry) (meta types.MediaMetadata) {
	ctx = normalizeContext(ctx)
	if err := ctx.Err(); err != nil {
		return types.MediaMetadata{Error: err.Error()}
	}

	f, err := os.Open(entry.Path)
	if err != nil {
		return types.MediaMetadata{Error: err.Error()}
	}
	defer f.Close()

	format, err := detectEXIFImageFormat(f, entry)
	if err != nil {
		return types.MediaMetadata{Error: err.Error()}
	}
	defer func() {
		if recovered := recover(); recovered != nil {
			meta = types.MediaMetadata{Error: fmt.Sprintf("no EXIF data: parser panic: %v", recovered)}
		}
	}()

	dateValues := make(map[string]captureDateValue, 3)
	_, err = imagemeta.Decode(imagemeta.Options{
		R:           contextReadSeeker{ctx: ctx, r: f},
		ImageFormat: format,
		Sources:     imagemeta.EXIF,
		ShouldHandleTag: func(tag imagemeta.TagInfo) bool {
			return isPrimaryCaptureNamespace(tag.Namespace) && isCaptureTimeTag(tag.Tag)
		},
		HandleTag: func(tag imagemeta.TagInfo) error {
			current, exists := dateValues[tag.Tag]
			priority := captureDateNamespacePriority(tag.Tag, tag.Namespace)
			if !exists || priority > current.priority {
				dateValues[tag.Tag] = captureDateValue{priority: priority}
			}
			if value, ok := tag.Value.(string); ok {
				candidate := captureDateValue{
					value:    strings.TrimRight(value, "\x00"),
					priority: priority,
				}
				if !exists || candidate.priority > current.priority {
					dateValues[tag.Tag] = candidate
				} else if candidate.priority == current.priority {
					dateValues[tag.Tag] = candidate
				}
			}
			return nil
		},
	})
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return types.MediaMetadata{Error: ctxErr.Error()}
		}
		return types.MediaMetadata{Error: "no EXIF data: " + err.Error()}
	}

	original, hasOriginal := dateValues["DateTimeOriginal"]
	if hasOriginal {
		if captureTime, ok := parseEXIFCaptureTime(original.value); ok {
			return types.MediaMetadata{CaptureTime: &captureTime, Source: "EXIF:DateTimeOriginal"}
		}
		if captureTime, ok := parseEXIFCaptureTime(dateValues["CreateDate"].value); ok {
			return types.MediaMetadata{CaptureTime: &captureTime, Source: "EXIF:DateTimeDigitized"}
		}
		return types.MediaMetadata{Error: "no capture time found in EXIF"}
	}

	if captureTime, ok := parseEXIFCaptureTime(dateValues["ModifyDate"].value); ok {
		return types.MediaMetadata{CaptureTime: &captureTime, Source: "EXIF:DateTimeOriginal"}
	}
	if captureTime, ok := parseEXIFCaptureTime(dateValues["CreateDate"].value); ok {
		return types.MediaMetadata{CaptureTime: &captureTime, Source: "EXIF:DateTimeDigitized"}
	}

	return types.MediaMetadata{Error: "no capture time found in EXIF"}
}

func parseEXIFCaptureTime(value string) (time.Time, bool) {
	if value == "" {
		return time.Time{}, false
	}
	captureTime, err := time.ParseInLocation("2006:01:02 15:04:05", value, time.Local)
	return captureTime, err == nil
}

func isPrimaryCaptureNamespace(namespace string) bool {
	return namespace == "IFD0" || namespace == "IFD0/ExifIFDP"
}

type captureDateValue struct {
	value    string
	priority int
}

func captureDateNamespacePriority(tag, namespace string) int {
	if tag == "DateTimeOriginal" || tag == "CreateDate" {
		if strings.Contains(namespace, "/ExifIFD") {
			return 2
		}
	}
	return 1
}

func isCaptureTimeTag(tag string) bool {
	switch tag {
	case "DateTimeOriginal", "ModifyDate", "CreateDate":
		return true
	default:
		return false
	}
}

func detectEXIFImageFormat(reader io.ReadSeeker, entry types.FileEntry) (imagemeta.ImageFormat, error) {
	if format, ok := exifImageFormatByExtension(entry); ok {
		return format, nil
	}

	header := make([]byte, 32)
	n, err := io.ReadFull(reader, header)
	if err != nil && err != io.EOF && err != io.ErrUnexpectedEOF {
		return imagemeta.ImageFormatAuto, fmt.Errorf("failed to inspect image format: %w", err)
	}
	if _, err := reader.Seek(0, io.SeekStart); err != nil {
		return imagemeta.ImageFormatAuto, fmt.Errorf("failed to reset image reader: %w", err)
	}
	header = header[:n]

	switch {
	case len(header) >= 3 && bytes.Equal(header[:3], []byte{0xff, 0xd8, 0xff}):
		return imagemeta.JPEG, nil
	case len(header) >= 8 && bytes.Equal(header[:8], []byte("\x89PNG\r\n\x1a\n")):
		return imagemeta.PNG, nil
	case len(header) >= 12 && bytes.Equal(header[:4], []byte("RIFF")) && bytes.Equal(header[8:12], []byte("WEBP")):
		return imagemeta.WebP, nil
	case isISOBaseMediaImage(header):
		return imagemeta.HEIF, nil
	case isTIFFHeader(header):
		return imagemeta.TIFF, nil
	default:
		return imagemeta.ImageFormatAuto, fmt.Errorf("unsupported EXIF image format")
	}
}

func exifImageFormatByExtension(entry types.FileEntry) (imagemeta.ImageFormat, bool) {
	extension := strings.ToLower(strings.TrimPrefix(entry.Extension, "."))
	if extension == "" {
		extension = strings.ToLower(strings.TrimPrefix(filepath.Ext(entry.Path), "."))
	}
	switch extension {
	case "jpg", "jpeg":
		return imagemeta.JPEG, true
	case "tif", "tiff", "raw":
		return imagemeta.TIFF, true
	case "arw":
		return imagemeta.ARW, true
	case "cr2":
		return imagemeta.CR2, true
	case "nef":
		return imagemeta.NEF, true
	case "dng":
		return imagemeta.DNG, true
	case "pef":
		return imagemeta.PEF, true
	case "png":
		return imagemeta.PNG, true
	case "webp":
		return imagemeta.WebP, true
	case "heic", "heif":
		return imagemeta.HEIF, true
	case "avif":
		return imagemeta.AVIF, true
	default:
		return imagemeta.ImageFormatAuto, false
	}
}

func isTIFFHeader(header []byte) bool {
	return len(header) >= 4 && (bytes.Equal(header[:4], []byte{'I', 'I', 0x2a, 0x00}) ||
		bytes.Equal(header[:4], []byte{'M', 'M', 0x00, 0x2a}))
}

func isISOBaseMediaImage(header []byte) bool {
	if len(header) < 12 || !bytes.Equal(header[4:8], []byte("ftyp")) {
		return false
	}
	for offset := 8; offset+4 <= len(header); offset += 4 {
		brand := string(header[offset : offset+4])
		switch brand {
		case "heic", "heix", "hevc", "hevx", "heim", "heis", "mif1", "msf1", "avif", "avis":
			return true
		}
	}
	return false
}
