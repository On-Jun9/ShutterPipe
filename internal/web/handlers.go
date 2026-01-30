package web

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
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
		json.NewEncoder(w).Encode(BrowseResponse{Error: err.Error()})
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
		http.Error(w, err.Error(), http.StatusBadRequest)
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
		http.Error(w, "backup already running", http.StatusConflict)
		return
	}

	var cfg config.Config
	if err := json.NewDecoder(r.Body).Decode(&cfg); err != nil {
		runMutex.Unlock()
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	// DEBUG LOG: Check received configuration
	fmt.Printf("Received Run Request: Source='%s', Dest='%s'\n", cfg.Source, cfg.Dest)

	if err := cfg.Validate(); err != nil {
		runMutex.Unlock()
		http.Error(w, err.Error(), http.StatusBadRequest)
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
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	presets, err := pm.ListPresets()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
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
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	if req.Name == "" {
		http.Error(w, "preset name is required", http.StatusBadRequest)
		return
	}

	pm, err := config.NewPresetManager()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	preset := config.ConfigToPreset(&req.Config, req.Name, req.Description)
	if err := pm.SavePreset(preset); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

func (s *Server) handleLoadPreset(w http.ResponseWriter, r *http.Request) {
	name := r.URL.Query().Get("name")
	if name == "" {
		http.Error(w, "preset name is required", http.StatusBadRequest)
		return
	}

	pm, err := config.NewPresetManager()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	preset, err := pm.LoadPreset(name)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}

	cfg := config.PresetToConfig(preset)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(cfg)
}

func (s *Server) handleDeletePreset(w http.ResponseWriter, r *http.Request) {
	name := r.URL.Query().Get("name")
	if name == "" {
		http.Error(w, "preset name is required", http.StatusBadRequest)
		return
	}

	pm, err := config.NewPresetManager()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	if err := pm.DeletePreset(name); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

// UserData-related handlers (settings, bookmarks, path history)

func (s *Server) handleGetSettings(w http.ResponseWriter, r *http.Request) {
	m, err := config.NewUserDataManager()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	settings, err := m.LoadSettings()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(settings)
}

func (s *Server) handleSaveSettings(w http.ResponseWriter, r *http.Request) {
	var settings types.UserSettings
	if err := json.NewDecoder(r.Body).Decode(&settings); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	m, err := config.NewUserDataManager()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	if err := m.SaveSettings(&settings); err != nil {
		errMsg := err.Error()

		// Check if this is a validation error or internal error
		if strings.Contains(errMsg, "invalid") {
			// Validation error - return 400
			valErr := ValidationError{Message: errMsg}

			if strings.Contains(errMsg, "source") {
				valErr.Field = "source"
			} else if strings.Contains(errMsg, "destination") || strings.Contains(errMsg, "dest") {
				valErr.Field = "dest"
			}

			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(w).Encode(valErr)
		} else {
			// Internal error (IO, permissions, etc) - return 500
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

func (s *Server) handleGetBookmarks(w http.ResponseWriter, r *http.Request) {
	m, err := config.NewUserDataManager()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	bookmarks, err := m.LoadBookmarks()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(bookmarks)
}

func (s *Server) handleSaveBookmarks(w http.ResponseWriter, r *http.Request) {
	var bookmarks types.Bookmarks
	if err := json.NewDecoder(r.Body).Decode(&bookmarks); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	m, err := config.NewUserDataManager()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	if err := m.SaveBookmarks(&bookmarks); err != nil {
		// Check if it's a validation error
		if strings.Contains(err.Error(), "invalid") || strings.Contains(err.Error(), "malicious") {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(w).Encode(ValidationError{
				Field:   "bookmarks",
				Message: err.Error(),
			})
			return
		}
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

func (s *Server) handleGetPathHistory(w http.ResponseWriter, r *http.Request) {
	m, err := config.NewUserDataManager()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	history, err := m.LoadPathHistory()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(history)
}

func (s *Server) handleSavePathHistory(w http.ResponseWriter, r *http.Request) {
	var history types.PathHistory
	if err := json.NewDecoder(r.Body).Decode(&history); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	m, err := config.NewUserDataManager()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	if err := m.SavePathHistory(&history); err != nil {
		// Check if it's a validation error
		if strings.Contains(err.Error(), "invalid") || strings.Contains(err.Error(), "malicious") {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(w).Encode(ValidationError{
				Field:   "path_history",
				Message: err.Error(),
			})
			return
		}
		http.Error(w, err.Error(), http.StatusInternalServerError)
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
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	history, err := m.LoadBackupHistory()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
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
