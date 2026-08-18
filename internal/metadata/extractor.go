package metadata

import (
	"context"
	"io"

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
	meta, _ := e.ExtractWithContext(context.Background(), entry)
	return meta
}

func (e *Extractor) ExtractWithContext(ctx context.Context, entry types.FileEntry) (types.MediaMetadata, error) {
	ctx = normalizeContext(ctx)
	if err := ctx.Err(); err != nil {
		return types.MediaMetadata{}, err
	}

	var meta types.MediaMetadata
	if entry.IsVideo {
		meta = e.xml.ExtractWithContext(ctx, entry)
	} else if entry.Extension == "xml" {
		meta = e.xml.ExtractFromXMLFileWithContext(ctx, entry)
	} else {
		meta = e.exif.ExtractWithContext(ctx, entry)
	}

	if err := ctx.Err(); err != nil {
		return types.MediaMetadata{}, err
	}
	return meta, nil
}

func normalizeContext(ctx context.Context) context.Context {
	if ctx == nil {
		return context.Background()
	}
	return ctx
}

type contextReader struct {
	ctx context.Context
	r   io.Reader
}

type contextReadSeeker struct {
	ctx context.Context
	r   io.ReadSeeker
}

func (r contextReadSeeker) Read(p []byte) (int, error) {
	return contextReader{ctx: r.ctx, r: r.r}.Read(p)
}

func (r contextReadSeeker) Seek(offset int64, whence int) (int64, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}

	position, err := r.r.Seek(offset, whence)
	if ctxErr := r.ctx.Err(); ctxErr != nil {
		return position, ctxErr
	}
	return position, err
}

func (r contextReader) Read(p []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}

	n, err := r.r.Read(p)
	if ctxErr := r.ctx.Err(); ctxErr != nil {
		return n, ctxErr
	}
	return n, err
}
