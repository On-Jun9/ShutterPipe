package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/On-Jun9/ShutterPipe/pkg/types"
)

// UserDataManager manages user data (settings, bookmarks, path history).
type UserDataManager struct {
	dataDir string
}

// RunLockPath returns the shared lock used to prevent the CLI and web server
// from mutating backup state and history concurrently.
func (m *UserDataManager) RunLockPath() string {
	return filepath.Join(m.dataDir, "run.lock")
}

// validatePath checks for potentially malicious characters in paths.
// Prevents XSS attacks by rejecting paths with HTML/script patterns.
// Note: <> alone are allowed as they're valid in Unix filenames.
func validatePath(path string) error {
	if path == "" {
		return nil // Empty paths are allowed
	}

	lowerPath := strings.ToLower(path)

	// Check for HTML tags (must have both < and tag name)
	htmlTagPatterns := []string{
		"<script",
		"</script",
		"<iframe",
		"<object",
		"<embed",
		"<img",
	}

	for _, pattern := range htmlTagPatterns {
		if strings.Contains(lowerPath, pattern) {
			return fmt.Errorf("path contains HTML tag pattern: %s", pattern)
		}
	}

	// Check for event handlers and javascript
	dangerousPatterns := []string{
		"javascript:",
		"onerror=",
		"onload=",
		"onclick=",
		"onmouseover=",
	}

	for _, pattern := range dangerousPatterns {
		if strings.Contains(lowerPath, pattern) {
			return fmt.Errorf("path contains potentially malicious pattern: %s", pattern)
		}
	}

	// Check maximum length (paths longer than 4096 are suspicious)
	if len(path) > 4096 {
		return fmt.Errorf("path too long (max 4096 characters)")
	}

	return nil
}

// writeFileAtomic writes data through a temp file in the same directory and
// renames it into place. The temp name must be unique per call: a shared
// "<name>.tmp" lets concurrent writers truncate each other's file, so one
// writer renames a partially overwritten file and the other fails with ENOENT.
func writeFileAtomic(filename string, data []byte) error {
	dir := filepath.Dir(filename)
	temp, err := os.CreateTemp(dir, "."+filepath.Base(filename)+".*.tmp")
	if err != nil {
		return err
	}
	tempPath := temp.Name()
	defer os.Remove(tempPath) // rename 성공 시에는 대상이 없으므로 무해하다
	if _, err := temp.Write(data); err != nil {
		temp.Close()
		return err
	}
	if err := temp.Close(); err != nil {
		return err
	}
	// CreateTemp은 0600으로 만들므로 기존 저장 권한(0644)에 맞춘다.
	if err := os.Chmod(tempPath, 0644); err != nil {
		return err
	}
	return os.Rename(tempPath, filename)
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
	// Validate paths for XSS prevention
	if err := validatePath(settings.Source); err != nil {
		return &ValidationError{
			Field:   "source",
			Message: fmt.Sprintf("invalid source path: %v", err),
		}
	}
	if err := validatePath(settings.Dest); err != nil {
		return &ValidationError{
			Field:   "dest",
			Message: fmt.Sprintf("invalid destination path: %v", err),
		}
	}
	if err := ValidateJobs(settings.Jobs); err != nil {
		return err
	}
	if err := ValidateMetadataJobs(settings.MetadataJobs); err != nil {
		return err
	}

	settings.UpdatedAt = time.Now()

	filename := filepath.Join(m.dataDir, "settings.json")
	data, err := json.MarshalIndent(settings, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal settings: %w", err)
	}

	// Atomic write: write to temp file then rename
	if err := writeFileAtomic(filename, data); err != nil {
		return fmt.Errorf("failed to write settings file: %w", err)
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
				OrganizeStrategy: types.OrganizeByDate,
				ConflictPolicy:   types.ConflictPolicySkip,
				DedupMethod:      types.DedupMethodNameSize,
				Jobs:             0,
				MetadataJobs:     2,
				IncludeExtensions: []string{
					"jpg", "jpeg", "heic", "heif", "png", "raw", "arw", "cr2", "nef", "dng",
					"mp4", "mov", "avi", "mkv", "mxf", "xml",
				},
				UnclassifiedDir: "unclassified",
				QuarantineDir:   "quarantine",
				StateFile:       filepath.Join(m.dataDir, "state.json"),
				LogFile:         filepath.Join(m.dataDir, "shutterpipe.log"),
				UpdatedAt:       time.Now(),
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
	// Validate all bookmark paths
	for _, path := range bookmarks.Source {
		if err := validatePath(path); err != nil {
			return &ValidationError{
				Field:   "bookmarks",
				Message: fmt.Sprintf("invalid source bookmark: %v", err),
			}
		}
	}
	for _, path := range bookmarks.Dest {
		if err := validatePath(path); err != nil {
			return &ValidationError{
				Field:   "bookmarks",
				Message: fmt.Sprintf("invalid dest bookmark: %v", err),
			}
		}
	}

	bookmarks.UpdatedAt = time.Now()

	filename := filepath.Join(m.dataDir, "bookmarks.json")
	data, err := json.MarshalIndent(bookmarks, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal bookmarks: %w", err)
	}

	// Atomic write
	if err := writeFileAtomic(filename, data); err != nil {
		return fmt.Errorf("failed to write bookmarks file: %w", err)
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
	// Validate all history paths
	for _, path := range history.Source {
		if err := validatePath(path); err != nil {
			return &ValidationError{
				Field:   "path_history",
				Message: fmt.Sprintf("invalid source path in history: %v", err),
			}
		}
	}
	for _, path := range history.Dest {
		if err := validatePath(path); err != nil {
			return &ValidationError{
				Field:   "path_history",
				Message: fmt.Sprintf("invalid dest path in history: %v", err),
			}
		}
	}

	history.UpdatedAt = time.Now()

	filename := filepath.Join(m.dataDir, "path-history.json")
	data, err := json.MarshalIndent(history, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal path history: %w", err)
	}

	// Atomic write
	if err := writeFileAtomic(filename, data); err != nil {
		return fmt.Errorf("failed to write path history file: %w", err)
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

// SaveBackupHistory saves backup history to disk.
func (m *UserDataManager) SaveBackupHistory(history *types.BackupHistory) error {
	history.UpdatedAt = time.Now()

	filename := filepath.Join(m.dataDir, "backup-history.json")
	data, err := json.MarshalIndent(history, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal backup history: %w", err)
	}

	// Atomic write
	if err := writeFileAtomic(filename, data); err != nil {
		return fmt.Errorf("failed to write backup history file: %w", err)
	}

	return nil
}

// LoadBackupHistory loads backup history from disk.
// Returns empty history if file doesn't exist.
func (m *UserDataManager) LoadBackupHistory() (*types.BackupHistory, error) {
	filename := filepath.Join(m.dataDir, "backup-history.json")
	data, err := os.ReadFile(filename)
	if err != nil {
		if os.IsNotExist(err) {
			// Return empty history
			return &types.BackupHistory{
				Entries:   []types.BackupHistoryEntry{},
				UpdatedAt: time.Now(),
			}, nil
		}
		return nil, fmt.Errorf("failed to read backup history file: %w", err)
	}

	var history types.BackupHistory
	if err := json.Unmarshal(data, &history); err != nil {
		return nil, fmt.Errorf("failed to unmarshal backup history: %w", err)
	}

	return &history, nil
}

// AddHistoryEntry adds a new entry to backup history with automatic 100-entry limit.
func (m *UserDataManager) AddHistoryEntry(entry types.BackupHistoryEntry) error {
	history, err := m.LoadBackupHistory()
	if err != nil {
		return fmt.Errorf("failed to load backup history: %w", err)
	}

	// Add new entry at the beginning
	history.Entries = append([]types.BackupHistoryEntry{entry}, history.Entries...)

	// Keep only the most recent 100 entries
	if len(history.Entries) > 100 {
		history.Entries = history.Entries[:100]
	}

	if err := m.SaveBackupHistory(history); err != nil {
		return fmt.Errorf("failed to save backup history: %w", err)
	}

	return nil
}
