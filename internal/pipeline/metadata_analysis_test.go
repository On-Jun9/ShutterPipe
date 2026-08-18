package pipeline

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/On-Jun9/ShutterPipe/internal/config"
	"github.com/On-Jun9/ShutterPipe/internal/metadata"
	"github.com/On-Jun9/ShutterPipe/internal/state"
	"github.com/On-Jun9/ShutterPipe/pkg/types"
)

type parallelMetadataExtractor struct {
	active  atomic.Int32
	maximum atomic.Int32
	started chan string
	release chan struct{}
}

func (e *parallelMetadataExtractor) ExtractWithContext(ctx context.Context, entry types.FileEntry) (types.MediaMetadata, error) {
	active := e.active.Add(1)
	defer e.active.Add(-1)
	for {
		maximum := e.maximum.Load()
		if active <= maximum || e.maximum.CompareAndSwap(maximum, active) {
			break
		}
	}

	e.started <- entry.Name
	select {
	case <-ctx.Done():
		return types.MediaMetadata{}, ctx.Err()
	case <-e.release:
		return types.MediaMetadata{Source: entry.Name}, nil
	}
}

func TestEffectiveMetadataWorkers_UsesConfiguredJobs(t *testing.T) {
	tests := []struct {
		configured int
		want       int
	}{
		{configured: 0, want: 2},
		{configured: 1, want: 1},
		{configured: 2, want: 2},
		{configured: 8, want: 8},
	}
	for _, test := range tests {
		if got := effectiveMetadataWorkers(test.configured); got != test.want {
			t.Fatalf("effectiveMetadataWorkers(%d) = %d, want %d", test.configured, got, test.want)
		}
	}
}

func TestMetadataAnalysisStream_BoundsBuffersToWorkerCount(t *testing.T) {
	p := &Pipeline{
		cfg: &config.Config{Jobs: 32, MetadataJobs: 8},
	}
	entries := make([]types.FileEntry, 100_000)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	stream := p.startMetadataAnalysis(ctx, "fingerprint", entries)
	defer stream.Close()
	if cap(stream.jobs) != 8 {
		t.Fatalf("jobs capacity = %d, want 8", cap(stream.jobs))
	}
	if cap(stream.completed) != 8 {
		t.Fatalf("completed capacity = %d, want 8", cap(stream.completed))
	}
}

func TestAnalyzeMetadata_UsesConfiguredWorkersAndPreservesScanOrder(t *testing.T) {
	extractor := &parallelMetadataExtractor{
		started: make(chan string, 4),
		release: make(chan struct{}),
	}
	p := &Pipeline{
		cfg: &config.Config{
			Jobs:           8,
			MetadataJobs:   3,
			IgnoreState:    true,
			ConflictPolicy: types.ConflictPolicyRename,
		},
		meta:  extractor,
		state: state.New(t.TempDir() + "/state.json"),
	}
	entries := []types.FileEntry{
		{Name: "first.jpg", Path: "/source/first.jpg", Extension: "jpg"},
		{Name: "second.jpg", Path: "/source/second.jpg", Extension: "jpg"},
		{Name: "third.jpg", Path: "/source/third.jpg", Extension: "jpg"},
		{Name: "fourth.jpg", Path: "/source/fourth.jpg", Extension: "jpg"},
	}

	stream := p.startMetadataAnalysis(context.Background(), "fingerprint", entries)
	defer stream.Close()

	for index := 0; index < 3; index++ {
		<-extractor.started
	}
	if got := extractor.maximum.Load(); got != 3 {
		t.Fatalf("maximum concurrent metadata extractions = %d, want 3", got)
	}
	select {
	case name := <-extractor.started:
		t.Fatalf("fourth extraction %q started before a worker was released", name)
	default:
	}

	close(extractor.release)
	for index := range entries {
		result, ok := stream.Next()
		if !ok {
			t.Fatalf("missing result %d", index)
		}
		if result.entry != entries[index] {
			t.Fatalf("result %d entry = %+v, want %+v", index, result.entry, entries[index])
		}
		wantSource := entries[index].Name
		if result.metadata.Source != wantSource {
			t.Fatalf("result %d metadata source = %q, want %q", index, result.metadata.Source, wantSource)
		}
	}
}

type cancellationBlockingExtractor struct {
	started chan struct{}
}

func (e cancellationBlockingExtractor) ExtractWithContext(ctx context.Context, _ types.FileEntry) (types.MediaMetadata, error) {
	close(e.started)
	<-ctx.Done()
	return types.MediaMetadata{}, ctx.Err()
}

func TestPipelineRunWithContext_FatalAnalysisErrorCancelsLaterWork(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("HOME", filepath.Join(tmpDir, "home"))
	sourceDir := filepath.Join(tmpDir, "source")
	destDir := filepath.Join(tmpDir, "dest")
	if err := os.MkdirAll(sourceDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(destDir, 0755); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"a.jpg", "b.jpg"} {
		if err := os.WriteFile(filepath.Join(sourceDir, name), []byte(name), 0644); err != nil {
			t.Fatal(err)
		}
	}

	cfg := newTestConfig(tmpDir, sourceDir, destDir)
	cfg.Jobs = 2
	cfg.MetadataJobs = 2
	cfg.IgnoreState = true
	p, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()

	extractor := cancellationBlockingExtractor{started: make(chan struct{})}
	sidecarErr := errors.New("sidecar failure")
	p.meta = extractor
	p.sidecarIdentity = func(_ context.Context, entry types.FileEntry) (metadata.SidecarIdentityInfo, bool, error) {
		if entry.Name == "a.jpg" {
			<-extractor.started
			return metadata.SidecarIdentityInfo{}, false, sidecarErr
		}
		return metadata.SidecarIdentityInfo{}, false, nil
	}

	type runResult struct {
		err error
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan runResult, 1)
	go func() {
		_, runErr := p.RunWithContext(ctx)
		done <- runResult{err: runErr}
	}()

	select {
	case result := <-done:
		if !errors.Is(result.err, sidecarErr) {
			t.Fatalf("expected sidecar failure, got %v", result.err)
		}
	case <-time.After(2 * time.Second):
		cancel()
		<-done
		t.Fatal("fatal analysis error waited for later metadata work")
	}
}

func TestPipelineRunWithContext_LaterFatalAnalysisErrorDoesNotWaitForEarlierWork(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("HOME", filepath.Join(tmpDir, "home"))
	sourceDir := filepath.Join(tmpDir, "source")
	destDir := filepath.Join(tmpDir, "dest")
	if err := os.MkdirAll(sourceDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(destDir, 0755); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"a.jpg", "b.jpg"} {
		if err := os.WriteFile(filepath.Join(sourceDir, name), []byte(name), 0644); err != nil {
			t.Fatal(err)
		}
	}

	cfg := newTestConfig(tmpDir, sourceDir, destDir)
	cfg.MetadataJobs = 2
	cfg.IgnoreState = true
	p, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()

	extractor := cancellationBlockingExtractor{started: make(chan struct{})}
	sidecarErr := errors.New("later sidecar failure")
	p.meta = extractor
	p.sidecarIdentity = func(_ context.Context, entry types.FileEntry) (metadata.SidecarIdentityInfo, bool, error) {
		if entry.Name == "b.jpg" {
			<-extractor.started
			return metadata.SidecarIdentityInfo{}, false, sidecarErr
		}
		return metadata.SidecarIdentityInfo{}, false, nil
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		_, runErr := p.RunWithContext(ctx)
		done <- runErr
	}()
	select {
	case runErr := <-done:
		if !errors.Is(runErr, sidecarErr) {
			t.Fatalf("expected later sidecar failure, got %v", runErr)
		}
	case <-time.After(2 * time.Second):
		cancel()
		<-done
		t.Fatal("later fatal analysis error waited for earlier metadata work")
	}
}

type stubbornMetadataExtractor struct {
	started chan struct{}
	release chan struct{}
}

func (e stubbornMetadataExtractor) ExtractWithContext(context.Context, types.FileEntry) (types.MediaMetadata, error) {
	close(e.started)
	<-e.release
	return types.MediaMetadata{}, nil
}

func TestMetadataAnalysisStream_CloseWaitsForWorkers(t *testing.T) {
	extractor := stubbornMetadataExtractor{started: make(chan struct{}), release: make(chan struct{})}
	p := &Pipeline{
		cfg:   &config.Config{MetadataJobs: 1, IgnoreState: true},
		meta:  extractor,
		state: state.New(filepath.Join(t.TempDir(), "state.json")),
	}
	stream := p.startMetadataAnalysis(context.Background(), "fingerprint", []types.FileEntry{{
		Name: "photo.jpg", Path: "/source/photo.jpg", Extension: "jpg",
	}})
	<-extractor.started
	closed := make(chan struct{})
	go func() {
		stream.Close()
		close(closed)
	}()
	select {
	case <-closed:
		t.Fatal("Close returned before metadata worker stopped")
	case <-time.After(20 * time.Millisecond):
	}
	close(extractor.release)
	select {
	case <-closed:
	case <-time.After(2 * time.Second):
		t.Fatal("Close did not return after metadata worker stopped")
	}
}

type cancelOnSecondMetadataExtractor struct {
	calls         atomic.Int32
	secondStarted chan struct{}
}

func (e *cancelOnSecondMetadataExtractor) ExtractWithContext(ctx context.Context, _ types.FileEntry) (types.MediaMetadata, error) {
	if e.calls.Add(1) == 1 {
		return types.MediaMetadata{}, nil
	}
	close(e.secondStarted)
	<-ctx.Done()
	return types.MediaMetadata{}, ctx.Err()
}

func TestPipelineRunWithContext_CancelPreservesCompletedAnalysisCounts(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("HOME", filepath.Join(tmpDir, "home"))
	sourceDir := filepath.Join(tmpDir, "source")
	destDir := filepath.Join(tmpDir, "dest")
	if err := os.MkdirAll(sourceDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(destDir, 0755); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"a.jpg", "b.jpg"} {
		if err := os.WriteFile(filepath.Join(sourceDir, name), []byte(name), 0644); err != nil {
			t.Fatal(err)
		}
	}

	cfg := newTestConfig(tmpDir, sourceDir, destDir)
	cfg.Jobs = 1
	cfg.MetadataJobs = 1
	cfg.IgnoreState = true
	p, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()

	extractor := &cancelOnSecondMetadataExtractor{secondStarted: make(chan struct{})}
	p.meta = extractor
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct {
		summary *types.RunSummary
		err     error
	}, 1)
	go func() {
		summary, runErr := p.RunWithContext(ctx)
		done <- struct {
			summary *types.RunSummary
			err     error
		}{summary: summary, err: runErr}
	}()

	<-extractor.secondStarted
	cancel()
	result := <-done
	if !errors.Is(result.err, ErrRunCanceled) {
		t.Fatalf("expected canceled run, got %v", result.err)
	}
	if result.summary.TotalFiles != 1 || result.summary.Unclassified != 1 {
		t.Fatalf("completed analysis counts were lost: %+v", result.summary)
	}
}
