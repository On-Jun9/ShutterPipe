package metadata

import (
	"encoding/xml"
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
	xmlPath := e.findXMLPath(entry.Path)
	if xmlPath == "" {
		return types.MediaMetadata{Error: "XML metadata file not found"}
	}

	data, err := os.ReadFile(xmlPath)
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
		Source:      "XML:CreationDate",
	}
}

// ExtractFromXMLFile extracts metadata directly from an XML file
func (e *XMLExtractor) ExtractFromXMLFile(entry types.FileEntry) types.MediaMetadata {
	data, err := os.ReadFile(entry.Path)
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
		Source:      "XML:CreationDate(direct)",
	}
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
