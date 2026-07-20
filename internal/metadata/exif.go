package metadata

import (
	"context"
	"os"
	"time"

	"github.com/On-Jun9/ShutterPipe/pkg/types"
	"github.com/rwcarlsen/goexif/exif"
)

type EXIFExtractor struct{}

func NewEXIFExtractor() *EXIFExtractor {
	return &EXIFExtractor{}
}

func (e *EXIFExtractor) Extract(entry types.FileEntry) types.MediaMetadata {
	return e.ExtractWithContext(context.Background(), entry)
}

func (e *EXIFExtractor) ExtractWithContext(ctx context.Context, entry types.FileEntry) types.MediaMetadata {
	ctx = normalizeContext(ctx)
	if err := ctx.Err(); err != nil {
		return types.MediaMetadata{Error: err.Error()}
	}

	f, err := os.Open(entry.Path)
	if err != nil {
		return types.MediaMetadata{Error: err.Error()}
	}
	defer f.Close()

	x, err := exif.Decode(contextReader{ctx: ctx, r: f})
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
