package metadata

import (
	"context"
	"crypto/sha256"
	"encoding/xml"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/On-Jun9/ShutterPipe/pkg/types"
	"golang.org/x/text/unicode/norm"
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

	return extractXMLBytes(data, source)
}

func extractXMLBytes(data []byte, source string) types.MediaMetadata {
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

type SidecarIdentityInfo struct {
	Path            string
	Size            int64
	ModTimeUnixNano int64
	Hash            string
	// Content is the exact byte snapshot the Hash was computed over. Callers
	// must classify from this snapshot (ExtractFromSidecar) instead of reopening
	// the sidecar, so a swap between hashing and parsing cannot record one
	// content's identity against another content's destination.
	Content []byte
}

// SidecarIdentity returns a stable identity for the XML sidecar that supplies
// the capture time for a video entry. Content is hashed because a sidecar can
// change classification while preserving both its size and modification time.
func SidecarIdentity(ctx context.Context, entry types.FileEntry) (SidecarIdentityInfo, bool, error) {
	if !entry.IsVideo {
		return SidecarIdentityInfo{}, false, nil
	}
	xmlPath := (&XMLExtractor{}).findXMLPath(entry.Path)
	if xmlPath == "" {
		return SidecarIdentityInfo{}, false, nil
	}
	ctx = normalizeContext(ctx)
	f, err := os.Open(xmlPath)
	if err != nil {
		return SidecarIdentityInfo{}, false, err
	}
	defer f.Close()
	before, err := f.Stat()
	if err != nil {
		return SidecarIdentityInfo{}, false, err
	}
	data, err := io.ReadAll(contextReader{ctx: ctx, r: f})
	if err != nil {
		return SidecarIdentityInfo{}, false, err
	}
	after, err := f.Stat()
	if err != nil {
		return SidecarIdentityInfo{}, false, err
	}
	if !os.SameFile(before, after) || before.Size() != after.Size() || !before.ModTime().Equal(after.ModTime()) {
		return SidecarIdentityInfo{}, false, fmt.Errorf("XML sidecar changed while hashing: %s", xmlPath)
	}
	actualPath := actualDirectoryEntryPath(xmlPath, after)
	absolutePath, err := filepath.Abs(actualPath)
	if err != nil {
		return SidecarIdentityInfo{}, false, err
	}
	return SidecarIdentityInfo{
		Path:            filepath.Clean(absolutePath),
		Size:            after.Size(),
		ModTimeUnixNano: after.ModTime().UnixNano(),
		Hash:            fmt.Sprintf("%x", sha256.Sum256(data)),
		Content:         data,
	}, true, nil
}

// ExtractFromSidecar parses capture metadata from the sidecar snapshot captured
// by SidecarIdentity, so classification can never see different sidecar content
// than the identity recorded in state.
func ExtractFromSidecar(identity SidecarIdentityInfo) types.MediaMetadata {
	return extractXMLBytes(identity.Content, "XML:CreationDate")
}

func actualDirectoryEntryPath(path string, requestedInfo os.FileInfo) string {
	parent := filepath.Dir(path)
	entries, err := os.ReadDir(parent)
	if err != nil {
		return path
	}
	requestedBase := filepath.Base(path)
	var sameFileNames []string
	for _, entry := range entries {
		info, infoErr := os.Stat(filepath.Join(parent, entry.Name()))
		if infoErr != nil || !os.SameFile(requestedInfo, info) {
			continue
		}
		if entry.Name() == requestedBase {
			return filepath.Join(parent, entry.Name())
		}
		sameFileNames = append(sameFileNames, entry.Name())
	}
	for _, name := range sameFileNames {
		if strings.EqualFold(name, requestedBase) ||
			norm.NFC.String(strings.ToLower(name)) == norm.NFC.String(strings.ToLower(requestedBase)) {
			return filepath.Join(parent, name)
		}
	}
	if len(sameFileNames) == 1 {
		return filepath.Join(parent, sameFileNames[0])
	}
	return path
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
