package metadata

import (
	"context"
	"encoding/xml"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/On-Jun9/ShutterPipe/pkg/types"
)

type XMLExtractor struct{}

func NewXMLExtractor() *XMLExtractor {
	return &XMLExtractor{}
}

type nonRealTimeMeta struct {
	XMLName      xml.Name `xml:"NonRealTimeMeta"`
	CreationDate struct {
		Value string `xml:"value,attr"`
	} `xml:"CreationDate"`
}

func (e *XMLExtractor) Extract(entry types.FileEntry) types.MediaMetadata {
	return e.ExtractWithContext(context.Background(), entry)
}

func (e *XMLExtractor) ExtractWithContext(ctx context.Context, entry types.FileEntry) types.MediaMetadata {
	ctx = normalizeContext(ctx)
	if err := ctx.Err(); err != nil {
		return types.MediaMetadata{Error: err.Error()}
	}

	xmlPath := e.findXMLPath(entry.Path)
	if xmlPath == "" {
		return types.MediaMetadata{Error: "XML metadata file not found"}
	}

	return extractXMLFileWithContext(ctx, xmlPath, "XML:CreationDate")
}

// ExtractFromXMLFile extracts metadata directly from an XML file
func (e *XMLExtractor) ExtractFromXMLFile(entry types.FileEntry) types.MediaMetadata {
	return e.ExtractFromXMLFileWithContext(context.Background(), entry)
}

func (e *XMLExtractor) ExtractFromXMLFileWithContext(ctx context.Context, entry types.FileEntry) types.MediaMetadata {
	return extractXMLFileWithContext(normalizeContext(ctx), entry.Path, "XML:CreationDate(direct)")
}

func extractXMLFileWithContext(ctx context.Context, path, source string) types.MediaMetadata {
	f, err := os.Open(path)
	if err != nil {
		return types.MediaMetadata{Error: "failed to read XML: " + err.Error()}
	}
	defer f.Close()

	data, err := io.ReadAll(contextReader{ctx: ctx, r: f})
	if err != nil {
		return types.MediaMetadata{Error: "failed to read XML: " + err.Error()}
	}

	var meta nonRealTimeMeta
	if err := xml.Unmarshal(data, &meta); err != nil {
		return types.MediaMetadata{Error: "failed to parse XML: " + err.Error()}
	}

	if meta.CreationDate.Value == "" {
		return types.MediaMetadata{Error: "CreationDate not found in XML"}
	}

	t, err := time.Parse(time.RFC3339, meta.CreationDate.Value)
	if err != nil {
		return types.MediaMetadata{Error: "invalid date format: " + err.Error()}
	}

	return types.MediaMetadata{
		CaptureTime: &t,
		Source:      source,
	}
}

// SidecarIdentity returns the identity of the XML sidecar that supplies the
// capture time for a video entry, if one is present. The sidecar can appear or
// change after an earlier run, moving the file to a different destination, so
// callers fold this identity into their reprocessing decision. It only stats the
// candidate sidecar path, so it stays cheap on the state fast path.
func SidecarIdentity(entry types.FileEntry) (size, modTimeUnixNano int64, ok bool) {
	if !entry.IsVideo {
		return 0, 0, false
	}
	xmlPath := (&XMLExtractor{}).findXMLPath(entry.Path)
	if xmlPath == "" {
		return 0, 0, false
	}
	info, err := os.Stat(xmlPath)
	if err != nil {
		return 0, 0, false
	}
	return info.Size(), info.ModTime().UnixNano(), true
}

func (e *XMLExtractor) findXMLPath(videoPath string) string {
	dir := filepath.Dir(videoPath)
	basename := strings.TrimSuffix(filepath.Base(videoPath), filepath.Ext(videoPath))

	xmlName := basename + "M01.XML"
	xmlPath := filepath.Join(dir, xmlName)

	if _, err := os.Stat(xmlPath); err == nil {
		return xmlPath
	}

	xmlNameLower := basename + "M01.xml"
	xmlPathLower := filepath.Join(dir, xmlNameLower)
	if _, err := os.Stat(xmlPathLower); err == nil {
		return xmlPathLower
	}

	return ""
}
