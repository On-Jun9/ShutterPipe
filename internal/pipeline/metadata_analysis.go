package pipeline

import (
	"context"
	"sync"

	"github.com/On-Jun9/ShutterPipe/internal/state"
	"github.com/On-Jun9/ShutterPipe/pkg/types"
)

const defaultMetadataWorkers = 2

type metadataAnalysis struct {
	entry          types.FileEntry
	sourceContext  state.SourceContext
	rebackupMarker state.RebackupMarker
	metadata       types.MediaMetadata
	forceRebackup  bool
	stateProcessed bool
	skipProcessed  bool
	sourceErr      error
	markerErr      error
	metadataErr    error
}

func effectiveMetadataWorkers(configured int) int {
	if configured < 1 {
		return defaultMetadataWorkers
	}
	return configured
}

type indexedMetadataEntry struct {
	index int
	entry types.FileEntry
}

type indexedMetadataAnalysis struct {
	index    int
	analysis metadataAnalysis
}

type metadataAnalysisStream struct {
	ctx        context.Context
	cancel     context.CancelFunc
	entries    []types.FileEntry
	jobs       chan indexedMetadataEntry
	completed  chan indexedMetadataAnalysis
	fatal      chan indexedMetadataAnalysis
	pending    map[int]metadataAnalysis
	nextSubmit int
	nextResult int
	workers    sync.WaitGroup
	closeOnce  sync.Once
}

func (p *Pipeline) startMetadataAnalysis(ctx context.Context, fingerprint string, entries []types.FileEntry) *metadataAnalysisStream {
	analysisCtx, cancel := context.WithCancel(ctx)
	workerCount := effectiveMetadataWorkers(p.cfg.MetadataJobs)
	if workerCount > len(entries) {
		workerCount = len(entries)
	}
	stream := &metadataAnalysisStream{
		ctx:       analysisCtx,
		cancel:    cancel,
		entries:   entries,
		jobs:      make(chan indexedMetadataEntry, workerCount),
		completed: make(chan indexedMetadataAnalysis, workerCount),
		fatal:     make(chan indexedMetadataAnalysis, 1),
		pending:   make(map[int]metadataAnalysis, workerCount),
	}
	stream.workers.Add(workerCount)
	for range workerCount {
		go func() {
			defer stream.workers.Done()
			for {
				if analysisCtx.Err() != nil {
					return
				}
				select {
				case <-analysisCtx.Done():
					return
				case job := <-stream.jobs:
					result := indexedMetadataAnalysis{
						index:    job.index,
						analysis: p.analyzeMetadataEntry(analysisCtx, fingerprint, job.entry),
					}
					if result.analysis.sourceErr != nil && contextTermination(nil, result.analysis.sourceErr) == nil {
						select {
						case stream.fatal <- result:
						case <-analysisCtx.Done():
						}
						return
					}
					select {
					case stream.completed <- result:
					case <-analysisCtx.Done():
						return
					}
				}
			}
		}()
	}
	stream.submitAvailable(workerCount)
	return stream
}

func (s *metadataAnalysisStream) Close() {
	s.closeOnce.Do(func() {
		s.cancel()
		s.workers.Wait()
	})
}

func (s *metadataAnalysisStream) Next() (metadataAnalysis, bool) {
	if s.nextResult >= len(s.entries) {
		return metadataAnalysis{}, false
	}

	for {
		select {
		case result := <-s.fatal:
			return result.analysis, true
		default:
		}

		if result, ok := s.pending[s.nextResult]; ok {
			delete(s.pending, s.nextResult)
			return s.advance(result), true
		}

		select {
		case result := <-s.fatal:
			return result.analysis, true
		case result := <-s.completed:
			s.pending[result.index] = result.analysis
			continue
		default:
		}

		select {
		case result := <-s.fatal:
			return result.analysis, true
		case result := <-s.completed:
			s.pending[result.index] = result.analysis
		case <-s.ctx.Done():
			return metadataAnalysis{entry: s.entries[s.nextResult], sourceErr: s.ctx.Err()}, true
		}
	}
}

func (s *metadataAnalysisStream) advance(result metadataAnalysis) metadataAnalysis {
	s.nextResult++
	s.submitAvailable(1)
	return result
}

func (s *metadataAnalysisStream) submitAvailable(limit int) {
	for submitted := 0; submitted < limit && s.nextSubmit < len(s.entries); submitted++ {
		select {
		case s.jobs <- indexedMetadataEntry{index: s.nextSubmit, entry: s.entries[s.nextSubmit]}:
			s.nextSubmit++
		case <-s.ctx.Done():
			return
		}
	}
}

func (p *Pipeline) analyzeMetadataEntry(ctx context.Context, fingerprint string, entry types.FileEntry) metadataAnalysis {
	result := metadataAnalysis{entry: entry}
	if err := ctx.Err(); err != nil {
		result.sourceErr = err
		return result
	}

	sctx, sidecar, hasSidecar, err := p.sourceContext(ctx, fingerprint, entry)
	if err != nil {
		result.sourceErr = err
		return result
	}
	result.sourceContext = sctx

	result.rebackupMarker, result.forceRebackup, result.markerErr = p.applicableRebackupMarker(ctx, entry, sctx)
	if result.markerErr != nil {
		return result
	}

	result.stateProcessed = !result.forceRebackup && !p.cfg.IgnoreState &&
		p.state.IsEntryProcessed(ctx, entry, p.cfg.HashVerify, sctx)
	result.skipProcessed = result.stateProcessed && p.cfg.ConflictPolicy != types.ConflictPolicyOverwrite
	if result.skipProcessed {
		return result
	}

	result.metadata, result.metadataErr = extractMetadataWithExtractor(ctx, entry, sidecar, hasSidecar, p.meta)
	return result
}
