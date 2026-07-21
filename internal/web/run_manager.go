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
	Status        RunStatus            `json:"status"`
	Kind          types.RunKind        `json:"kind,omitempty"`
	RunID         string               `json:"run_id,omitempty"`
	ServerID      string               `json:"server_id,omitempty"`
	Summary       *types.RunSummary    `json:"summary,omitempty"`
	VerifySummary *types.VerifySummary `json:"verify_summary,omitempty"`
	Error         string               `json:"error,omitempty"`
	Revision      uint64               `json:"revision"`
}

type activeRun struct {
	id       string
	kind     types.RunKind
	status   RunStatus
	cancel   func()
	done     chan struct{}
	revision uint64
}

type RunManager struct {
	mu            sync.Mutex
	active        *activeRun
	terminal      *RunStatusResponse
	terminals     map[string]RunStatusResponse
	terminalOrder []string
	revision      uint64
	mutationDone  chan struct{}
	// usedIDs deliberately grows for the server's lifetime. Evicting old IDs
	// would let a reused run_id match a delayed cancel from the evicted run.
	// Runs are user-initiated and an ID is ~tens of bytes, so unbounded growth
	// is negligible for this tool; the absolute no-reuse guarantee is not.
	usedIDs map[string]struct{}
}

const retainedTerminalRuns = 64

func NewRunManager() *RunManager {
	return &RunManager{
		terminals: make(map[string]RunStatusResponse),
		usedIDs:   make(map[string]struct{}),
	}
}

func (m *RunManager) Start(runID string, cancel func()) bool {
	_, err := m.TryStart(runID, cancel)
	return err == nil
}

func (m *RunManager) TryStart(runID string, cancel func()) (RunStatusResponse, error) {
	return m.TryStartKind(runID, types.RunKindBackup, cancel)
}

func (m *RunManager) TryStartKind(runID string, kind types.RunKind, cancel func()) (RunStatusResponse, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.active != nil || m.mutationDone != nil {
		return m.statusLocked(), ErrRunAlreadyActive
	}
	if kind == "" {
		kind = types.RunKindBackup
	}
	if m.usedIDs == nil {
		m.usedIDs = make(map[string]struct{})
	}
	if _, reused := m.usedIDs[runID]; reused {
		return m.statusLocked(), ErrRunIDReused
	}
	m.usedIDs[runID] = struct{}{}
	m.revision++
	m.active = &activeRun{
		id: runID, kind: kind, status: RunStatusRunning, cancel: cancel,
		done: make(chan struct{}), revision: m.revision,
	}
	return m.statusLocked(), nil
}

func (m *RunManager) Status() RunStatusResponse {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.statusLocked()
}

// StatusFor returns a retained snapshot for runID when one exists. If the
// requested run is unknown, it falls back to the current server snapshot so a
// client can distinguish idle from a different active or terminal run.
func (m *RunManager) StatusFor(runID string) RunStatusResponse {
	m.mu.Lock()
	defer m.mu.Unlock()
	if runID == "" {
		return m.statusLocked()
	}
	if m.active != nil && m.active.id == runID {
		return m.activeStatusLocked()
	}
	if terminal, ok := m.terminals[runID]; ok {
		return cloneRunStatusResponse(terminal)
	}
	return m.statusLocked()
}

func (m *RunManager) IsActive() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.active != nil || m.mutationDone != nil
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
	return m.FinishDetailed(runID, status, summary, nil, errMessage)
}

func (m *RunManager) FinishDetailed(runID string, status RunStatus, summary *types.RunSummary, verifySummary *types.VerifySummary, errMessage string) (RunStatusResponse, bool) {
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
	verifySummary = cloneVerifySummary(verifySummary)
	terminal := RunStatusResponse{
		Status: status, Kind: m.active.kind, RunID: runID,
		Summary: summary, VerifySummary: verifySummary,
		Error: errMessage, Revision: m.revision,
	}
	close(m.active.done)
	m.active = nil
	stored := cloneRunStatusResponse(terminal)
	m.terminal = &stored
	m.retainTerminalLocked(stored)
	return cloneRunStatusResponse(terminal), true
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
	var done <-chan struct{}
	if m.active != nil {
		done = m.active.done
	} else if m.mutationDone != nil {
		done = m.mutationDone
	}
	if done == nil {
		m.mu.Unlock()
		return nil
	}
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
		return m.activeStatusLocked()
	}
	if m.terminal != nil {
		return cloneRunStatusResponse(*m.terminal)
	}
	return RunStatusResponse{Status: RunStatusIdle, Revision: m.revision}
}

func (m *RunManager) activeStatusLocked() RunStatusResponse {
	return RunStatusResponse{
		Status: m.active.status, Kind: m.active.kind,
		RunID: m.active.id, Revision: m.active.revision,
	}
}

// TryBeginMutation reserves the shared run lifecycle for a synchronous state
// mutation such as verify requeue without exposing it as a user-visible run.
func (m *RunManager) TryBeginMutation() (func(), error) {
	m.mu.Lock()
	if m.active != nil || m.mutationDone != nil {
		m.mu.Unlock()
		return nil, ErrRunAlreadyActive
	}
	done := make(chan struct{})
	m.mutationDone = done
	m.mu.Unlock()

	var once sync.Once
	return func() {
		once.Do(func() {
			m.mu.Lock()
			if m.mutationDone == done {
				m.mutationDone = nil
				close(done)
			}
			m.mu.Unlock()
		})
	}, nil
}

func (m *RunManager) retainTerminalLocked(terminal RunStatusResponse) {
	if m.terminals == nil {
		m.terminals = make(map[string]RunStatusResponse)
	}
	if _, exists := m.terminals[terminal.RunID]; !exists {
		m.terminalOrder = append(m.terminalOrder, terminal.RunID)
	}
	m.terminals[terminal.RunID] = terminal
	for len(m.terminalOrder) > retainedTerminalRuns {
		oldest := m.terminalOrder[0]
		m.terminalOrder = m.terminalOrder[1:]
		delete(m.terminals, oldest)
	}
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

func cloneRunStatusResponse(status RunStatusResponse) RunStatusResponse {
	status.Summary = cloneRunSummary(status.Summary)
	status.VerifySummary = cloneVerifySummary(status.VerifySummary)
	return status
}

func cloneVerifySummary(summary *types.VerifySummary) *types.VerifySummary {
	if summary == nil {
		return nil
	}
	cloned := *summary
	cloned.Problems = append([]types.VerifyProblem(nil), summary.Problems...)
	cloned.Warnings = append([]string(nil), summary.Warnings...)
	if summary.Manifest != nil {
		manifest := *summary.Manifest
		cloned.Manifest = &manifest
	}
	return &cloned
}
