package pipeline

import (
	"context"

	"github.com/On-Jun9/ShutterPipe/internal/state"
	fileverify "github.com/On-Jun9/ShutterPipe/internal/verify"
	"github.com/On-Jun9/ShutterPipe/pkg/types"
)

func (p *Pipeline) applicableRebackupMarker(ctx context.Context, entry types.FileEntry, fingerprint string) (state.RebackupMarker, bool, error) {
	marker, ok := p.state.RebackupMarker(entry.Path)
	if !ok {
		return state.RebackupMarker{}, false, nil
	}
	if marker.SourcePath != entry.Path {
		p.state.RemoveRebackupMarker(entry.Path)
		return state.RebackupMarker{}, false, nil
	}
	if marker.ConfigFingerprint != fingerprint {
		return state.RebackupMarker{}, false, nil
	}
	if marker.SourceSize != entry.Size || marker.SourceModTimeUnixNano != entry.ModTime.UnixNano() {
		p.state.RemoveRebackupMarker(entry.Path)
		return state.RebackupMarker{}, false, nil
	}
	if marker.SourceHash == "" {
		return marker, true, nil
	}

	currentHash, info, err := fileverify.HashStableFileWithContext(ctx, entry.Path)
	if err != nil {
		return state.RebackupMarker{}, false, err
	}
	if info.Size() != entry.Size || info.ModTime().UnixNano() != entry.ModTime.UnixNano() || currentHash != marker.SourceHash {
		p.state.RemoveRebackupMarker(entry.Path)
		return state.RebackupMarker{}, false, nil
	}
	return marker, true, nil
}
