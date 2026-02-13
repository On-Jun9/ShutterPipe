package web

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"sync"

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

type ProgressUpdate struct {
	Type     string            `json:"type"`
	Current  int               `json:"current,omitempty"`
	Total    int               `json:"total,omitempty"`
	Filename string            `json:"filename,omitempty"`
	Action   types.CopyAction  `json:"action,omitempty"`
	Summary  *types.RunSummary `json:"summary,omitempty"`
	Error    string            `json:"error,omitempty"`
}

var runMutex sync.Mutex

func (s *Server) handleRun(w http.ResponseWriter, r *http.Request) {
	if !runMutex.TryLock() {
		writeAPIError(w, http.StatusConflict, "backup already running")
		return
	}

	var cfg config.Config
	if err := json.NewDecoder(r.Body).Decode(&cfg); err != nil {
		runMutex.Unlock()
		writeAPIError(w, http.StatusBadRequest, err.Error())
		return
	}

	// DEBUG LOG: Check received configuration
	fmt.Printf("Received Run Request: Source='%s', Dest='%s'\n", cfg.Source, cfg.Dest)

	if err := cfg.Validate(); err != nil {
		runMutex.Unlock()
		var validationErr *config.ValidationError
		if errors.As(err, &validationErr) {
			writeValidationError(w, validationErr.Field, validationErr.Message)
			return
		}

		writeAPIError(w, http.StatusInternalServerError, err.Error())
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "started"})

	go func() {
		defer runMutex.Unlock()
		defer func() {
			if r := recover(); r != nil {
				fmt.Printf("PANIC RECOVERED: %v\n", r)
				s.broadcastProgress(pipeline.ProgressUpdate{Type: "error", Error: fmt.Sprintf("Internal Server Error: %v", r)})
			}
		}()

		p, err := pipeline.New(&cfg)
		if err != nil {
			s.broadcastProgress(pipeline.ProgressUpdate{Type: "error", Error: err.Error()})
			return
		}

		fmt.Println("Pipeline initialized")

		defer func() {
			fmt.Println("Closing pipeline...")
			p.Close()
			fmt.Println("Pipeline closed")
		}()

		p.SetProgressCallback(func(update pipeline.ProgressUpdate) {
			s.broadcastProgress(update)
		})

		fmt.Println("Starting pipeline run...")
		_, err = p.Run()
		if err != nil {
			fmt.Printf("Pipeline run failed: %v\n", err)
			s.broadcastProgress(pipeline.ProgressUpdate{Type: "error", Error: err.Error()})
			return
		}
		fmt.Println("Pipeline run completed successfully")
	}()
}

func (s *Server) broadcastJSON(v interface{}) {
	data, err := json.Marshal(v)
	if err != nil {
		return
	}
	s.hub.broadcast <- data
}

func (s *Server) broadcastProgress(update pipeline.ProgressUpdate) {
	s.broadcastJSON(update)
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
