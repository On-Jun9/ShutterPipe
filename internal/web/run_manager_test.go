package web

import (
	"context"
	"errors"
	"testing"

	"github.com/On-Jun9/ShutterPipe/pkg/types"
)

func TestRunManagerLifecycle(t *testing.T) {
	manager := NewRunManager()
	cancelled := false

	if !manager.Start("run-1", func() { cancelled = true }) {
		t.Fatal("expected first run to start")
	}
	if manager.Start("run-2", func() {}) {
		t.Fatal("expected concurrent run to be rejected")
	}

	status := manager.Status()
	if status.Status != RunStatusRunning || status.RunID != "run-1" {
		t.Fatalf("unexpected running status: %+v", status)
	}

	status, err := manager.Cancel("run-1")
	if err != nil {
		t.Fatalf("failed to cancel active run: %v", err)
	}
	if !cancelled || status.Status != RunStatusCancelling {
		t.Fatalf("unexpected cancellation result: cancelled=%v status=%+v", cancelled, status)
	}

	terminal, finished := manager.Finish("run-1", RunStatusCancelled, nil, "")
	if !finished {
		t.Fatal("expected active run to finish")
	}
	if terminal.Status != RunStatusCancelled || terminal.RunID != "run-1" {
		t.Fatalf("unexpected terminal status: %+v", terminal)
	}
	if status := manager.Status(); status.Status != RunStatusCancelled || status.RunID != "run-1" {
		t.Fatalf("expected retained terminal status, got %+v", status)
	}
	if !manager.Start("run-2", func() {}) {
		t.Fatal("terminal snapshot must not block a new run")
	}
}

func TestRunManagerRejectsMismatchedCancelAndFinish(t *testing.T) {
	manager := NewRunManager()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if !manager.Start("run-current", cancel) {
		t.Fatal("expected run to start")
	}

	if _, err := manager.Cancel("run-stale"); err == nil {
		t.Fatal("expected stale run cancellation to fail")
	}
	if _, finished := manager.Finish("run-stale", RunStatusComplete, nil, ""); finished {
		t.Fatal("stale run must not finish current run")
	}
	if err := ctx.Err(); err != nil {
		t.Fatalf("stale cancellation affected current run: %v", err)
	}
	if status := manager.Status(); status.RunID != "run-current" || status.Status != RunStatusRunning {
		t.Fatalf("current run state changed unexpectedly: %+v", status)
	}
}

func TestRunManagerRejectsReusedRunID(t *testing.T) {
	manager := NewRunManager()
	if !manager.Start("run-reused", func() {}) {
		t.Fatal("expected first use to start")
	}
	if _, finished := manager.Finish("run-reused", RunStatusComplete, nil, ""); !finished {
		t.Fatal("expected first use to finish")
	}
	if err := manager.TryStart("run-reused", func() {}); !errors.Is(err, ErrRunIDReused) {
		t.Fatalf("expected reused run ID rejection, got %v", err)
	}
	if !manager.Start("run-new", func() {}) {
		t.Fatal("reused ID rejection blocked a distinct run")
	}
}

func TestRunManagerRejectsIDLessCancellation(t *testing.T) {
	manager := NewRunManager()
	cancelled := false
	manager.Start("current-run", func() { cancelled = true })

	status, err := manager.Cancel("")
	if !errors.Is(err, ErrRunIDRequired) {
		t.Fatalf("expected missing run ID rejection, got %v", err)
	}
	if cancelled || status.Status != RunStatusRunning || status.RunID != "current-run" {
		t.Fatalf("ID-less cancellation changed current run: cancelled=%v status=%+v", cancelled, status)
	}
}

func TestRunManagerConcurrentFinishAndStatusRetainsTerminal(t *testing.T) {
	manager := NewRunManager()
	manager.Start("fast-run", func() {})

	start := make(chan struct{})
	statusDone := make(chan RunStatusResponse, 1)
	go func() {
		<-start
		statusDone <- manager.Status()
	}()
	finishDone := make(chan struct{})
	go func() {
		<-start
		manager.Finish("fast-run", RunStatusComplete, &types.RunSummary{Copied: 1}, "")
		close(finishDone)
	}()
	close(start)
	<-statusDone // Either running or terminal is valid at the exact crossing point.
	<-finishDone

	status := manager.Status()
	if status.Status != RunStatusComplete || status.RunID != "fast-run" {
		t.Fatalf("terminal state was lost after concurrent status read: %+v", status)
	}
	if status.Summary == nil || status.Summary.Copied != 1 {
		t.Fatalf("terminal summary was lost: %+v", status.Summary)
	}
}

func TestRunManagerTerminalSummaryIsAnImmutableSnapshot(t *testing.T) {
	manager := NewRunManager()
	manager.Start("snapshot-run", func() {})
	summary := &types.RunSummary{Copied: 1}
	manager.Finish("snapshot-run", RunStatusComplete, summary, "")

	summary.Copied = 99
	first := manager.Status()
	if first.Summary == nil || first.Summary.Copied != 1 {
		t.Fatalf("stored summary followed caller mutation: %+v", first.Summary)
	}
	first.Summary.Copied = 42
	second := manager.Status()
	if second.Summary == nil || second.Summary.Copied != 1 {
		t.Fatalf("stored summary was mutable through status response: %+v", second.Summary)
	}
}
