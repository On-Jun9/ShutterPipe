package metadata

import (
	"os"
	"time"

	"github.com/rwcarlsen/goexif/exif"
	"github.com/On-Jun9/ShutterPipe/pkg/types"
)

type EXIFExtractor struct{}

func NewEXIFExtractor() *EXIFExtractor {
	return &EXIFExtractor{}
}

func (e *EXIFExtractor) Extract(entry types.FileEntry) types.MediaMetadata {
	f, err := os.Open(entry.Path)
	if err != nil {
		return types.MediaMetadata{Error: err.Error()}
	}
	defer f.Close()

	x, err := exif.Decode(f)
	if err != nil {
		return types.MediaMetadata{Error: "no EXIF data: " + err.Error()}
	}

	if t, err := x.DateTime(); err == nil {
		return types.MediaMetadata{
			CaptureTime: &t,
			Source:      "EXIF:DateTimeOriginal",
		}
	}

	if tag, err := x.Get(exif.DateTimeDigitized); err == nil {
		if strVal, err := tag.StringVal(); err == nil {
			if t, err := time.Parse("2006:01:02 15:04:05", strVal); err == nil {
				return types.MediaMetadata{
					CaptureTime: &t,
					Source:      "EXIF:DateTimeDigitized",
				}
			}
		}
	}

	return types.MediaMetadata{Error: "no capture time found in EXIF"}
}
