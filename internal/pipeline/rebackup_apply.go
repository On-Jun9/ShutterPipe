package pipeline

import (
	"context"

	"github.com/On-Jun9/ShutterPipe/internal/state"
	fileverify "github.com/On-Jun9/ShutterPipe/internal/verify"
	"github.com/On-Jun9/ShutterPipe/pkg/types"
)

func (p *Pipeline) applicableRebackupMarker(ctx context.Context, entry types.FileEntry, sctx state.SourceContext) (state.RebackupMarker, bool, error) {
	marker, ok := p.state.RebackupMarker(entry.Path)
	if !ok {
		return state.RebackupMarker{}, false, nil
	}
	if marker.SourcePath != entry.Path {
		p.state.RemoveRebackupMarker(entry.Path)
		return state.RebackupMarker{}, false, nil
	}
	if marker.ConfigFingerprint != sctx.ConfigFingerprint {
		return state.RebackupMarker{}, false, nil
	}
	if marker.SourceSize != entry.Size || marker.SourceModTimeUnixNano != entry.ModTime.UnixNano() {
		p.state.RemoveRebackupMarker(entry.Path)
		return state.RebackupMarker{}, false, nil
	}
	// The recorded DestPath was planned against the sidecar that supplied the
	// classification. If that sidecar has since changed, the destination may
	// differ; discard the marker so the file flows through the normal backup
	// and is re-classified to its current destination.
	if !markerSidecarMatches(marker, sctx) {
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

func markerSidecarMatches(marker state.RebackupMarker, sctx state.SourceContext) bool {
	return marker.SidecarPresent == sctx.SidecarPresent &&
		marker.SidecarPath == sctx.SidecarPath &&
		marker.SidecarSize == sctx.SidecarSize &&
		marker.SidecarModTimeUnixNano == sctx.SidecarModTimeUnixNano &&
		marker.SidecarHash == sctx.SidecarHash
}
