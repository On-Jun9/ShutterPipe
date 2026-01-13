package metadata

import (
	"github.com/On-Jun9/ShutterPipe/pkg/types"
)

type Extractor struct {
	exif *EXIFExtractor
	xml  *XMLExtractor
}

func New() *Extractor {
	return &Extractor{
		exif: NewEXIFExtractor(),
		xml:  NewXMLExtractor(),
	}
}

func (e *Extractor) Extract(entry types.FileEntry) types.MediaMetadata {
	if entry.IsVideo {
		return e.xml.Extract(entry)
	}
	// If this is an XML file itself, parse it directly
	if entry.Extension == "xml" {
		return e.xml.ExtractFromXMLFile(entry)
	}
	return e.exif.Extract(entry)
}
