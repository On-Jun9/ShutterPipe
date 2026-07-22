package web

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"

	"github.com/On-Jun9/ShutterPipe/internal/config"
	"github.com/On-Jun9/ShutterPipe/internal/pipeline"
	"github.com/On-Jun9/ShutterPipe/pkg/types"
)

// ValidationError represents a field validation error.
type ValidationError struct {
	Field   string `json:"field"`
	Message string `json:"message"`
}

type APIErrorResponse struct {
	Message string `json:"message"`
}

func writeAPIError(w http.ResponseWriter, status int, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(APIErrorResponse{Message: message})
}

func writeValidationError(w http.ResponseWriter, field, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusBadRequest)
	json.NewEncoder(w).Encode(ValidationError{
		Field:   field,
		Message: message,
	})
}

type BrowseRequest struct {
	Path string `json:"path"`
}

type BrowseResponse struct {
	Path    string     `json:"path"`
	Entries []DirEntry `json:"entries"`
	Error   string     `json:"error,omitempty"`
}

type DirEntry struct {
	Name  string `json:"name"`
	Path  string `json:"path"`
	IsDir bool   `json:"is_dir"`
}

func (s *Server) handleBrowse(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Query().Get("path")
	if path == "" {
		homeDir, _ := os.UserHomeDir()
		path = homeDir
	}

	entries, err := os.ReadDir(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			writeAPIError(w, http.StatusNotFound, err.Error())
			return
		}
		if errors.Is(err, os.ErrPermission) {
			writeAPIError(w, http.StatusForbidden, err.Error())
			return
		}
		writeAPIError(w, http.StatusInternalServerError, err.Error())
		return
	}

	var dirEntries []DirEntry
	for _, entry := range entries {
		if entry.Name()[0] == '.' {
			continue
		}
		dirEntries = append(dirEntries, DirEntry{
			Name:  entry.Name(),
			Path:  filepath.Join(path, entry.Name()),
			IsDir: entry.IsDir(),
		})
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(BrowseResponse{
		Path:    path,
		Entries: dirEntries,
	})
}

func (s *Server) handleGetConfig(w http.ResponseWriter, r *http.Request) {
	cfg := config.DefaultConfig()
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(cfg)
}

func (s *Server) handleSaveConfig(w http.ResponseWriter, r *http.Request) {
	var cfg config.Config
	if err := json.NewDecoder(r.Body).Decode(&cfg); err != nil {
		writeAPIError(w, http.StatusBadRequest, err.Error())
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

type RunRequest struct {
	config.Config
	RunID string `json:"run_id,omitempty"`
}

type CancelRunRequest struct {
	RunID    string `json:"run_id,omitempty"`
	ServerID string `json:"server_id,omitempty"`
}

type StartRunResponse struct {
	Status   string        `json:"status"`
	Kind     types.RunKind `json:"kind"`
	RunID    string        `json:"run_id"`
	ServerID string        `json:"server_id"`
	Revision uint64        `json:"revision"`
}

func newRunID() (string, error) {
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("failed to generate run ID: %w", err)
	}
	return hex.EncodeToString(buf), nil
}

func (s *Server) handleRun(w http.ResponseWriter, r *http.Request) {
	if s.isShuttingDown() {
		writeAPIError(w, http.StatusServiceUnavailable, "server is shutting down")
		return
	}
	if s.runs().IsActive() {
		writeAPIError(w, http.StatusConflict, "backup already running")
		return
	}

	var req RunRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeAPIError(w, http.StatusBadRequest, err.Error())
		return
	}

	cfg := req.Config
	if err := cfg.Validate(); err != nil {
		var validationErr *config.ValidationError
		if errors.As(err, &validationErr) {
			writeValidationError(w, validationErr.Field, validationErr.Message)
			return
		}

		writeAPIError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if r.Context().Err() != nil {
		return
	}

	runID := req.RunID
	if runID == "" {
		var err error
		runID, err = newRunID()
		if err != nil {
			writeAPIError(w, http.StatusInternalServerError, err.Error())
			return
		}
	}
	if len(runID) > 128 {
		writeAPIError(w, http.StatusBadRequest, "run_id is too long")
		return
	}

	runCtx, cancelRun, startStatus, err := s.startRun(runID)
	if errors.Is(err, http.ErrServerClosed) {
		writeAPIError(w, http.StatusServiceUnavailable, "server is shutting down")
		return
	}
	if err != nil {
		writeAPIError(w, http.StatusConflict, err.Error())
		return
	}
	if r.Context().Err() != nil {
		cancelRun()
		s.finishRun(pipeline.ProgressUpdate{
			Type:    "cancelled",
			RunID:   runID,
			Message: "start request was cancelled before execution",
		})
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(StartRunResponse{
		Status: "started", Kind: types.RunKindBackup,
		RunID: runID, ServerID: s.instanceID(), Revision: startStatus.Revision,
	})

	go func() {
		defer cancelRun()
		var terminalUpdate pipeline.ProgressUpdate
		terminalPending := false
		defer func() {
			if recovered := recover(); recovered != nil {
				fmt.Printf("PANIC RECOVERED: %v\n", recovered)
				terminalUpdate = pipeline.ProgressUpdate{Type: "error", Kind: types.RunKindBackup, RunID: runID, Error: fmt.Sprintf("Internal Server Error: %v", recovered)}
				terminalPending = true
			}
			if !terminalPending {
				terminalUpdate = pipeline.ProgressUpdate{Type: "error", Kind: types.RunKindBackup, RunID: runID, Error: "backup ended without a terminal result"}
			}
			s.finishRun(terminalUpdate)
		}()

		p, err := pipeline.New(&cfg)
		if err != nil {
			terminalUpdate = pipeline.ProgressUpdate{Type: "error", Kind: types.RunKindBackup, RunID: runID, Error: err.Error()}
			terminalPending = true
			return
		}

		defer func() {
			if closeErr := p.Close(); closeErr != nil {
				// The backup outcome and history have already been finalized. Logger
				// close failure is a cleanup warning, not a contradictory run result.
				fmt.Printf("WARNING: failed to close backup logger for run %s: %v\n", runID, closeErr)
			}
		}()

		p.SetProgressCallback(func(update pipeline.ProgressUpdate) {
			update.Kind = types.RunKindBackup
			update.RunID = runID
			if isTerminalProgressType(update.Type) {
				terminalUpdate = update
				terminalPending = true
				return
			}
			s.broadcastActiveRunProgress(update)
		})

		summary, err := p.RunWithContext(runCtx)
		if err != nil {
			if errors.Is(err, pipeline.ErrRunCanceled) {
				terminalUpdate = pipeline.ProgressUpdate{
					Type:    "cancelled",
					Kind:    types.RunKindBackup,
					RunID:   runID,
					Message: "백업이 취소되었습니다.",
					Summary: summary,
				}
				terminalPending = true
				return
			}

			terminalUpdate = pipeline.ProgressUpdate{Type: "error", Kind: types.RunKindBackup, RunID: runID, Summary: summary, Error: err.Error()}
			terminalPending = true
			return
		}

		// Pipeline implementations normally emit complete through the callback.
		// Keep this fallback so every successful goroutine exit records a terminal snapshot.
		if !terminalPending {
			terminalUpdate = pipeline.ProgressUpdate{Type: "complete", Kind: types.RunKindBackup, RunID: runID, Summary: summary}
			terminalPending = true
		}
	}()
}

func (s *Server) handleCancelRun(w http.ResponseWriter, r *http.Request) {
	var req CancelRunRequest
	if r.Body != nil {
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil && !errors.Is(err, io.EOF) {
			writeAPIError(w, http.StatusBadRequest, err.Error())
			return
		}
	}
	if req.RunID == "" {
		writeAPIError(w, http.StatusBadRequest, ErrRunIDRequired.Error())
		return
	}
	if req.ServerID == "" {
		writeAPIError(w, http.StatusBadRequest, "server_id is required")
		return
	}
	if req.ServerID != s.instanceID() {
		writeAPIError(w, http.StatusConflict, "server_id does not match the current server")
		return
	}

	status, err := s.runs().Cancel(req.RunID)
	if err != nil {
		writeAPIError(w, http.StatusConflict, err.Error())
		return
	}
	status.ServerID = s.instanceID()

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(status)
}

func (s *Server) handleRunStatus(w http.ResponseWriter, r *http.Request) {
	status := s.runs().StatusFor(r.URL.Query().Get("run_id"))
	status.ServerID = s.instanceID()
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(status)
}

func (s *Server) broadcastJSON(v interface{}) {
	if s.hub == nil {
		return
	}
	data, err := json.Marshal(v)
	if err != nil {
		return
	}
	select {
	case s.hub.broadcast <- data:
	case <-s.hub.stop:
	}
}

func (s *Server) broadcastProgress(update pipeline.ProgressUpdate) {
	s.broadcastJSON(update)
}

func (s *Server) broadcastActiveRunProgress(update pipeline.ProgressUpdate) {
	status := s.runs().Status()
	if status.RunID != update.RunID || (status.Status != RunStatusRunning && status.Status != RunStatusCancelling) {
		return
	}
	update.Revision = status.Revision
	update.ServerID = s.instanceID()
	update.Kind = status.Kind
	s.broadcastProgress(update)
}

func (s *Server) finishRun(update pipeline.ProgressUpdate) {
	status := terminalStatusForProgress(update.Type)
	snapshot, finished := s.runs().FinishDetailed(update.RunID, status, update.Summary, update.VerifySummary, update.Error)
	if !finished {
		return
	}
	update.Revision = snapshot.Revision
	update.ServerID = s.instanceID()
	update.Kind = snapshot.Kind
	s.broadcastProgress(update)
}

func isTerminalProgressType(progressType string) bool {
	return progressType == "complete" || progressType == "cancelled" || progressType == "error"
}

func terminalStatusForProgress(progressType string) RunStatus {
	switch progressType {
	case "complete":
		return RunStatusComplete
	case "cancelled":
		return RunStatusCancelled
	default:
		return RunStatusError
	}
}

// Preset-related handlers

func (s *Server) handleListPresets(w http.ResponseWriter, r *http.Request) {
	pm, err := config.NewPresetManager()
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, err.Error())
		return
	}

	presets, err := pm.ListPresets()
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, err.Error())
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(presets)
}

func (s *Server) handleSavePreset(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name        string        `json:"name"`
		Description string        `json:"description"`
		Config      config.Config `json:"config"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeAPIError(w, http.StatusBadRequest, err.Error())
		return
	}

	if req.Name == "" {
		writeAPIError(w, http.StatusBadRequest, "preset name is required")
		return
	}

	pm, err := config.NewPresetManager()
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, err.Error())
		return
	}

	preset := config.ConfigToPreset(&req.Config, req.Name, req.Description)
	if err := pm.SavePreset(preset); err != nil {
		writeAPIError(w, http.StatusInternalServerError, err.Error())
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

func (s *Server) handleLoadPreset(w http.ResponseWriter, r *http.Request) {
	name := r.URL.Query().Get("name")
	if name == "" {
		writeAPIError(w, http.StatusBadRequest, "preset name is required")
		return
	}

	pm, err := config.NewPresetManager()
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, err.Error())
		return
	}

	preset, err := pm.LoadPreset(name)
	if err != nil {
		writeAPIError(w, http.StatusNotFound, err.Error())
		return
	}

	cfg := config.PresetToConfig(preset)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(cfg)
}

func (s *Server) handleDeletePreset(w http.ResponseWriter, r *http.Request) {
	name := r.URL.Query().Get("name")
	if name == "" {
		writeAPIError(w, http.StatusBadRequest, "preset name is required")
		return
	}

	pm, err := config.NewPresetManager()
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, err.Error())
		return
	}

	if err := pm.DeletePreset(name); err != nil {
		writeAPIError(w, http.StatusInternalServerError, err.Error())
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

// UserData-related handlers (settings, bookmarks, path history)

func (s *Server) handleGetSettings(w http.ResponseWriter, r *http.Request) {
	m, err := config.NewUserDataManager()
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, err.Error())
		return
	}

	settings, err := m.LoadSettings()
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, err.Error())
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(settings)
}

func (s *Server) handleSaveSettings(w http.ResponseWriter, r *http.Request) {
	var settings types.UserSettings
	if err := json.NewDecoder(r.Body).Decode(&settings); err != nil {
		writeAPIError(w, http.StatusBadRequest, err.Error())
		return
	}

	m, err := config.NewUserDataManager()
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, err.Error())
		return
	}

	if err := m.SaveSettings(&settings); err != nil {
		var validationErr *config.ValidationError
		if errors.As(err, &validationErr) {
			writeValidationError(w, validationErr.Field, validationErr.Message)
			return
		}

		writeAPIError(w, http.StatusInternalServerError, err.Error())
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

func (s *Server) handleGetBookmarks(w http.ResponseWriter, r *http.Request) {
	m, err := config.NewUserDataManager()
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, err.Error())
		return
	}

	bookmarks, err := m.LoadBookmarks()
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, err.Error())
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(bookmarks)
}

func (s *Server) handleSaveBookmarks(w http.ResponseWriter, r *http.Request) {
	var bookmarks types.Bookmarks
	if err := json.NewDecoder(r.Body).Decode(&bookmarks); err != nil {
		writeAPIError(w, http.StatusBadRequest, err.Error())
		return
	}

	m, err := config.NewUserDataManager()
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, err.Error())
		return
	}

	if err := m.SaveBookmarks(&bookmarks); err != nil {
		var validationErr *config.ValidationError
		if errors.As(err, &validationErr) {
			writeValidationError(w, validationErr.Field, validationErr.Message)
			return
		}

		writeAPIError(w, http.StatusInternalServerError, err.Error())
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

func (s *Server) handleGetPathHistory(w http.ResponseWriter, r *http.Request) {
	m, err := config.NewUserDataManager()
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, err.Error())
		return
	}

	history, err := m.LoadPathHistory()
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, err.Error())
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(history)
}

func (s *Server) handleSavePathHistory(w http.ResponseWriter, r *http.Request) {
	var history types.PathHistory
	if err := json.NewDecoder(r.Body).Decode(&history); err != nil {
		writeAPIError(w, http.StatusBadRequest, err.Error())
		return
	}

	m, err := config.NewUserDataManager()
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, err.Error())
		return
	}

	if err := m.SavePathHistory(&history); err != nil {
		var validationErr *config.ValidationError
		if errors.As(err, &validationErr) {
			writeValidationError(w, validationErr.Field, validationErr.Message)
			return
		}

		writeAPIError(w, http.StatusInternalServerError, err.Error())
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

func (s *Server) handleGetBackupHistory(w http.ResponseWriter, r *http.Request) {
	// Get limit from query parameter (default 20, max 100)
	limit := 20
	if limitStr := r.URL.Query().Get("limit"); limitStr != "" {
		if parsedLimit, err := strconv.Atoi(limitStr); err == nil {
			limit = parsedLimit
			if limit > 100 {
				limit = 100
			} else if limit < 1 {
				limit = 20
			}
		}
	}

	m, err := config.NewUserDataManager()
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, err.Error())
		return
	}

	history, err := m.LoadBackupHistory()
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, err.Error())
		return
	}

	// Return only the requested number of entries (already sorted newest first)
	if len(history.Entries) > limit {
		history.Entries = history.Entries[:limit]
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(history)
}

// Version handler

func (s *Server) handleVersion(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"version": s.version})
}
