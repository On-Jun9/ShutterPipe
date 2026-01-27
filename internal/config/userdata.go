package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/On-Jun9/ShutterPipe/pkg/types"
)

// UserDataManager manages user data (settings, bookmarks, path history).
type UserDataManager struct {
	dataDir string
}

// NewUserDataManager creates a new user data manager.
func NewUserDataManager() (*UserDataManager, error) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("failed to get home directory: %w", err)
	}

	dataDir := filepath.Join(homeDir, ".shutterpipe")
	if err := os.MkdirAll(dataDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create data directory: %w", err)
	}

	return &UserDataManager{dataDir: dataDir}, nil
}

// SaveSettings saves user settings to disk.
func (m *UserDataManager) SaveSettings(settings *types.UserSettings) error {
	settings.UpdatedAt = time.Now()

	filename := filepath.Join(m.dataDir, "settings.json")
	data, err := json.MarshalIndent(settings, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal settings: %w", err)
	}

	// Atomic write: write to temp file then rename
	tmpFile := filename + ".tmp"
	if err := os.WriteFile(tmpFile, data, 0644); err != nil {
		return fmt.Errorf("failed to write settings file: %w", err)
	}
	if err := os.Rename(tmpFile, filename); err != nil {
		os.Remove(tmpFile)
		return fmt.Errorf("failed to rename settings file: %w", err)
	}

	return nil
}

// LoadSettings loads user settings from disk.
// Returns default settings if file doesn't exist.
func (m *UserDataManager) LoadSettings() (*types.UserSettings, error) {
	filename := filepath.Join(m.dataDir, "settings.json")
	data, err := os.ReadFile(filename)
	if err != nil {
		if os.IsNotExist(err) {
			// Return default settings
			return &types.UserSettings{
				OrganizeStrategy:  types.OrganizeByDate,
				ConflictPolicy:    types.ConflictPolicyRename,
				DedupMethod:       types.DedupMethodNameSize,
				Jobs:              4,
				IncludeExtensions: []string{},
				UnclassifiedDir:   "unclassified",
				QuarantineDir:     "quarantine",
				StateFile:         "shutterpipe.state",
				LogFile:           "shutterpipe.log",
				UpdatedAt:         time.Now(),
			}, nil
		}
		return nil, fmt.Errorf("failed to read settings file: %w", err)
	}

	var settings types.UserSettings
	if err := json.Unmarshal(data, &settings); err != nil {
		return nil, fmt.Errorf("failed to unmarshal settings: %w", err)
	}

	return &settings, nil
}

// SaveBookmarks saves bookmarks to disk.
func (m *UserDataManager) SaveBookmarks(bookmarks *types.Bookmarks) error {
	bookmarks.UpdatedAt = time.Now()

	filename := filepath.Join(m.dataDir, "bookmarks.json")
	data, err := json.MarshalIndent(bookmarks, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal bookmarks: %w", err)
	}

	// Atomic write
	tmpFile := filename + ".tmp"
	if err := os.WriteFile(tmpFile, data, 0644); err != nil {
		return fmt.Errorf("failed to write bookmarks file: %w", err)
	}
	if err := os.Rename(tmpFile, filename); err != nil {
		os.Remove(tmpFile)
		return fmt.Errorf("failed to rename bookmarks file: %w", err)
	}

	return nil
}

// LoadBookmarks loads bookmarks from disk.
// Returns empty bookmarks if file doesn't exist.
func (m *UserDataManager) LoadBookmarks() (*types.Bookmarks, error) {
	filename := filepath.Join(m.dataDir, "bookmarks.json")
	data, err := os.ReadFile(filename)
	if err != nil {
		if os.IsNotExist(err) {
			// Return empty bookmarks
			return &types.Bookmarks{
				Source:    []string{},
				Dest:      []string{},
				UpdatedAt: time.Now(),
			}, nil
		}
		return nil, fmt.Errorf("failed to read bookmarks file: %w", err)
	}

	var bookmarks types.Bookmarks
	if err := json.Unmarshal(data, &bookmarks); err != nil {
		return nil, fmt.Errorf("failed to unmarshal bookmarks: %w", err)
	}

	return &bookmarks, nil
}

// SavePathHistory saves path history to disk.
func (m *UserDataManager) SavePathHistory(history *types.PathHistory) error {
	history.UpdatedAt = time.Now()

	filename := filepath.Join(m.dataDir, "path-history.json")
	data, err := json.MarshalIndent(history, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal path history: %w", err)
	}

	// Atomic write
	tmpFile := filename + ".tmp"
	if err := os.WriteFile(tmpFile, data, 0644); err != nil {
		return fmt.Errorf("failed to write path history file: %w", err)
	}
	if err := os.Rename(tmpFile, filename); err != nil {
		os.Remove(tmpFile)
		return fmt.Errorf("failed to rename path history file: %w", err)
	}

	return nil
}

// LoadPathHistory loads path history from disk.
// Returns empty history if file doesn't exist.
func (m *UserDataManager) LoadPathHistory() (*types.PathHistory, error) {
	filename := filepath.Join(m.dataDir, "path-history.json")
	data, err := os.ReadFile(filename)
	if err != nil {
		if os.IsNotExist(err) {
			// Return empty history
			return &types.PathHistory{
				Source:    []string{},
				Dest:      []string{},
				UpdatedAt: time.Now(),
			}, nil
		}
		return nil, fmt.Errorf("failed to read path history file: %w", err)
	}

	var history types.PathHistory
	if err := json.Unmarshal(data, &history); err != nil {
		return nil, fmt.Errorf("failed to unmarshal path history: %w", err)
	}

	return &history, nil
}
