package state

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"time"
)

type ProcessedFile struct {
	Path      string    `json:"path"`
	Size      int64     `json:"size"`
	Hash      string    `json:"hash,omitempty"`
	DestPath  string    `json:"dest_path"`
	Timestamp time.Time `json:"timestamp"`
}

type State struct {
	mu        sync.RWMutex
	filePath  string
	Processed map[string]ProcessedFile `json:"processed"`
	LastRun   time.Time                `json:"last_run"`
}

func New(filePath string) *State {
	return &State{
		filePath:  filePath,
		Processed: make(map[string]ProcessedFile),
	}
}

func Load(filePath string) (*State, error) {
	s := New(filePath)

	data, err := os.ReadFile(filePath)
	if os.IsNotExist(err) {
		return s, nil
	}
	if err != nil {
		return nil, err
	}

	if err := json.Unmarshal(data, s); err != nil {
		return nil, err
	}

	return s, nil
}

func (s *State) Save() error {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if err := os.MkdirAll(filepath.Dir(s.filePath), 0755); err != nil {
		return err
	}

	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(s.filePath, data, 0644)
}

func (s *State) IsProcessed(path string, size int64) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if p, ok := s.Processed[path]; ok {
		return p.Size == size
	}
	return false
}

func (s *State) MarkProcessed(path string, size int64, destPath string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.Processed[path] = ProcessedFile{
		Path:      path,
		Size:      size,
		DestPath:  destPath,
		Timestamp: time.Now(),
	}
	s.LastRun = time.Now()
}
