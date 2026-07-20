package web

import (
	"context"
	"errors"
	"fmt"
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
	if _, err := manager.TryStart("run-reused", func() {}); !errors.Is(err, ErrRunIDReused) {
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
	finished, _ := manager.Finish("snapshot-run", RunStatusComplete, summary, "")

	summary.Copied = 99
	finished.Summary.Copied = 77
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

func TestRunManagerRetainsPreviousTerminalAfterNextRunStarts(t *testing.T) {
	manager := NewRunManager()
	manager.Start("finished-run", func() {})
	finishedSummary := &types.RunSummary{Copied: 7}
	manager.Finish("finished-run", RunStatusComplete, finishedSummary, "")

	if !manager.Start("next-run", func() {}) {
		t.Fatal("expected next run to start")
	}
	if current := manager.Status(); current.Status != RunStatusRunning || current.RunID != "next-run" {
		t.Fatalf("unexpected current status: %+v", current)
	}
	retained := manager.StatusFor("finished-run")
	if retained.Status != RunStatusComplete || retained.RunID != "finished-run" ||
		retained.Summary == nil || retained.Summary.Copied != 7 {
		t.Fatalf("previous terminal snapshot was not retained: %+v", retained)
	}
}

func TestRunManagerBoundsRetainedTerminalHistory(t *testing.T) {
	manager := NewRunManager()
	for index := 0; index < retainedTerminalRuns+1; index++ {
		runID := fmt.Sprintf("run-%d", index)
		if !manager.Start(runID, func() {}) {
			t.Fatalf("failed to start %s", runID)
		}
		manager.Finish(runID, RunStatusComplete, nil, "")
	}

	if status := manager.StatusFor("run-0"); status.RunID == "run-0" {
		t.Fatalf("oldest terminal snapshot was not evicted: %+v", status)
	}
	if status := manager.StatusFor(fmt.Sprintf("run-%d", retainedTerminalRuns)); status.RunID == "" {
		t.Fatalf("latest terminal snapshot was not retained: %+v", status)
	}
}

// TestRunManagerNeverReusesRunIDWithinServerLifetime는 usedIDs가 서버 수명 동안
// 회수되지 않는지 검증한다. 오래된 run_id를 회수하면 같은 ID로 시작한 새 실행이
// 회수된 실행의 지연·재전송된 취소 요청에 취소될 수 있다.
func TestRunManagerNeverReusesRunIDWithinServerLifetime(t *testing.T) {
	manager := NewRunManager()
	const completedRuns = 2048
	for index := 0; index < completedRuns; index++ {
		runID := fmt.Sprintf("bounded-run-%d", index)
		if !manager.Start(runID, func() {}) {
			t.Fatalf("failed to start %s", runID)
		}
		manager.Finish(runID, RunStatusComplete, nil, "")
	}
	if len(manager.usedIDs) != completedRuns {
		t.Fatalf("used run IDs were discarded: ids=%d want=%d", len(manager.usedIDs), completedRuns)
	}
	if _, err := manager.TryStart("bounded-run-0", func() {}); !errors.Is(err, ErrRunIDReused) {
		t.Fatalf("oldest run ID became reusable: %v", err)
	}
}
