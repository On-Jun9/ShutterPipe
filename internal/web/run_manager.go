package web

import (
	"context"
	"errors"
	"sync"

	"github.com/On-Jun9/ShutterPipe/pkg/types"
)

type RunStatus string

const (
	RunStatusIdle       RunStatus = "idle"
	RunStatusRunning    RunStatus = "running"
	RunStatusCancelling RunStatus = "cancelling"
	RunStatusComplete   RunStatus = "complete"
	RunStatusCancelled  RunStatus = "cancelled"
	RunStatusError      RunStatus = "error"
)

var (
	ErrNoActiveRun      = errors.New("backup is not running")
	ErrRunAlreadyActive = errors.New("backup already running")
	ErrRunIDReused      = errors.New("run_id has already been used")
	ErrRunIDRequired    = errors.New("run_id is required")
	ErrRunIDMismatch    = errors.New("backup run ID does not match the active run")
)

type RunStatusResponse struct {
	Status   RunStatus         `json:"status"`
	RunID    string            `json:"run_id,omitempty"`
	ServerID string            `json:"server_id,omitempty"`
	Summary  *types.RunSummary `json:"summary,omitempty"`
	Error    string            `json:"error,omitempty"`
	Revision uint64            `json:"revision"`
}

type activeRun struct {
	id       string
	status   RunStatus
	cancel   func()
	done     chan struct{}
	revision uint64
}

type RunManager struct {
	mu       sync.Mutex
	active   *activeRun
	terminal *RunStatusResponse
	revision uint64
	usedIDs  map[string]struct{}
}

func NewRunManager() *RunManager {
	return &RunManager{usedIDs: make(map[string]struct{})}
}

func (m *RunManager) Start(runID string, cancel func()) bool {
	return m.TryStart(runID, cancel) == nil
}

func (m *RunManager) TryStart(runID string, cancel func()) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.active != nil {
		return ErrRunAlreadyActive
	}
	if m.usedIDs == nil {
		m.usedIDs = make(map[string]struct{})
	}
	if _, reused := m.usedIDs[runID]; reused {
		return ErrRunIDReused
	}
	m.usedIDs[runID] = struct{}{}
	m.revision++
	m.terminal = nil
	m.active = &activeRun{
		id: runID, status: RunStatusRunning, cancel: cancel,
		done: make(chan struct{}), revision: m.revision,
	}
	return nil
}

func (m *RunManager) Status() RunStatusResponse {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.statusLocked()
}

func (m *RunManager) IsActive() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.active != nil
}

func (m *RunManager) Cancel(runID string) (RunStatusResponse, error) {
	m.mu.Lock()
	if runID == "" {
		status := m.statusLocked()
		m.mu.Unlock()
		return status, ErrRunIDRequired
	}
	if m.active == nil {
		status := m.statusLocked()
		m.mu.Unlock()
		return status, ErrNoActiveRun
	}
	if runID != m.active.id {
		status := m.statusLocked()
		m.mu.Unlock()
		return status, ErrRunIDMismatch
	}

	var cancel func()
	if m.active.status != RunStatusCancelling {
		m.revision++
		m.active.status = RunStatusCancelling
		m.active.revision = m.revision
		cancel = m.active.cancel
	}
	status := m.statusLocked()
	m.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	return status, nil
}

func (m *RunManager) Finish(runID string, status RunStatus, summary *types.RunSummary, errMessage string) (RunStatusResponse, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.active == nil || m.active.id != runID {
		return m.statusLocked(), false
	}
	if !isTerminalRunStatus(status) {
		return m.statusLocked(), false
	}
	m.revision++
	summary = cloneRunSummary(summary)
	terminal := RunStatusResponse{
		Status: status, RunID: runID, Summary: summary,
		Error: errMessage, Revision: m.revision,
	}
	close(m.active.done)
	m.active = nil
	m.terminal = &terminal
	return terminal, true
}

func (m *RunManager) CancelActive() {
	m.mu.Lock()
	if m.active == nil {
		m.mu.Unlock()
		return
	}
	if m.active.status != RunStatusCancelling {
		m.revision++
		m.active.status = RunStatusCancelling
		m.active.revision = m.revision
	}
	cancel := m.active.cancel
	m.mu.Unlock()
	if cancel != nil {
		cancel()
	}
}

func (m *RunManager) Wait(ctx context.Context) error {
	m.mu.Lock()
	if m.active == nil {
		m.mu.Unlock()
		return nil
	}
	done := m.active.done
	m.mu.Unlock()

	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (m *RunManager) statusLocked() RunStatusResponse {
	if m.active != nil {
		return RunStatusResponse{Status: m.active.status, RunID: m.active.id, Revision: m.active.revision}
	}
	if m.terminal != nil {
		status := *m.terminal
		status.Summary = cloneRunSummary(status.Summary)
		return status
	}
	return RunStatusResponse{Status: RunStatusIdle, Revision: m.revision}
}

func isTerminalRunStatus(status RunStatus) bool {
	return status == RunStatusComplete || status == RunStatusCancelled || status == RunStatusError
}

func cloneRunSummary(summary *types.RunSummary) *types.RunSummary {
	if summary == nil {
		return nil
	}
	cloned := *summary
	cloned.Warnings = append([]string(nil), summary.Warnings...)
	return &cloned
}
