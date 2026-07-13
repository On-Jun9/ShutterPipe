package state

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/On-Jun9/ShutterPipe/pkg/types"
)

type ProcessedFile struct {
	Path                  string    `json:"path"`
	Size                  int64     `json:"size"`
	Hash                  string    `json:"hash,omitempty"`
	DestPath              string    `json:"dest_path"`
	Timestamp             time.Time `json:"timestamp"`
	IdentityVersion       int       `json:"identity_version,omitempty"`
	SourceModTimeUnixNano int64     `json:"source_mod_time_unix_nano,omitempty"`
	DestSize              int64     `json:"dest_size,omitempty"`
	DestModTimeUnixNano   int64     `json:"dest_mod_time_unix_nano,omitempty"`
	SourceHash            string    `json:"source_hash,omitempty"`
	DestHash              string    `json:"dest_hash,omitempty"`
	Superseded            bool      `json:"superseded,omitempty"`
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
	data, err := json.MarshalIndent(s, "", "  ")
	filePath := s.filePath
	s.mu.RUnlock()
	if err != nil {
		return err
	}
	dir := filepath.Dir(filePath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	temp, err := os.CreateTemp(dir, "."+filepath.Base(filePath)+".*.tmp")
	if err != nil {
		return err
	}
	tempPath := temp.Name()
	defer os.Remove(tempPath)
	if _, err := temp.Write(data); err != nil {
		temp.Close()
		return err
	}
	if err := temp.Sync(); err != nil {
		temp.Close()
		return err
	}
	if err := temp.Close(); err != nil {
		return err
	}
	if err := replaceStateFile(tempPath, filePath); err != nil {
		return err
	}
	return syncStateDir(dir)
}

func (s *State) IsEntryProcessed(entry types.FileEntry, hashVerify bool) bool {
	s.mu.RLock()
	record, ok := s.Processed[entry.Path]
	s.mu.RUnlock()
	if !ok || record.IdentityVersion < 2 || record.Size != entry.Size ||
		record.SourceModTimeUnixNano != entry.ModTime.UnixNano() || record.DestPath == "" {
		return false
	}
	destInfo, err := os.Stat(record.DestPath)
	if err != nil || !destInfo.Mode().IsRegular() || destInfo.Size() != record.DestSize ||
		destInfo.ModTime().UnixNano() != record.DestModTimeUnixNano {
		return false
	}
	if !hashVerify {
		return true
	}
	if record.SourceHash == "" || record.DestHash == "" {
		return false
	}
	sourceHash, _, err := hashStableFile(entry.Path)
	if err != nil || sourceHash != record.SourceHash {
		return false
	}
	destHash, _, err := hashStableFile(record.DestPath)
	return err == nil && destHash == record.DestHash
}

func (s *State) MarkProcessedEntry(entry types.FileEntry, destPath string, hashVerify bool) error {
	return s.markProcessedEntry(entry, destPath, hashVerify, nil, false, false)
}

// MarkProcessedEntryWithVerifiedHash binds the state record to the exact
// source snapshot that the copier verified before publishing the destination.
func (s *State) MarkProcessedEntryWithVerifiedHash(entry types.FileEntry, destPath string, verifiedHash []byte) error {
	if len(verifiedHash) != sha256.Size {
		return fmt.Errorf("verified source hash is required for state commit: %s", entry.Path)
	}
	return s.markProcessedEntry(entry, destPath, true, verifiedHash, true, false)
}

// MarkSupersededEntry records an overwrite loser whose content is intentionally
// represented by the deterministic winner at destPath.
func (s *State) MarkSupersededEntry(entry types.FileEntry, destPath string, hashVerify bool) error {
	return s.markProcessedEntry(entry, destPath, hashVerify, nil, false, true)
}

func (s *State) markProcessedEntry(entry types.FileEntry, destPath string, hashVerify bool, verifiedHash []byte, requireVerifiedHash, superseded bool) error {
	sourceInfo, err := os.Stat(entry.Path)
	if err != nil {
		return err
	}
	if sourceInfo.Size() != entry.Size || !sourceInfo.ModTime().Equal(entry.ModTime) {
		return fmt.Errorf("source changed before state commit: %s", entry.Path)
	}
	destInfo, err := os.Stat(destPath)
	if err != nil {
		return err
	}
	if !destInfo.Mode().IsRegular() {
		return fmt.Errorf("destination identity mismatch: %s", destPath)
	}
	var sourceHash, destHash string
	if hashVerify {
		sourceHash, sourceInfo, err = hashStableFile(entry.Path)
		if err != nil {
			return err
		}
		destHash, destInfo, err = hashStableFile(destPath)
		if err != nil {
			return err
		}
		if requireVerifiedHash {
			expectedHash := fmt.Sprintf("%x", verifiedHash)
			if sourceHash != expectedHash || destHash != expectedHash {
				return fmt.Errorf("verified snapshot changed before state commit: %s", entry.Path)
			}
		} else if !superseded && sourceHash != destHash {
			return fmt.Errorf("source and destination differ at state commit: %s", entry.Path)
		}
	}

	now := time.Now()
	s.mu.Lock()
	s.Processed[entry.Path] = ProcessedFile{
		Path: entry.Path, Size: entry.Size, DestPath: destPath, Timestamp: now,
		IdentityVersion: 2, SourceModTimeUnixNano: sourceInfo.ModTime().UnixNano(),
		DestSize: destInfo.Size(), DestModTimeUnixNano: destInfo.ModTime().UnixNano(),
		SourceHash: sourceHash, DestHash: destHash, Superseded: superseded,
	}
	s.LastRun = now
	s.mu.Unlock()
	return nil
}

func hashStableFile(path string) (string, os.FileInfo, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", nil, err
	}
	defer f.Close()
	before, err := f.Stat()
	if err != nil {
		return "", nil, err
	}
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", nil, err
	}
	after, err := f.Stat()
	if err != nil {
		return "", nil, err
	}
	if !os.SameFile(before, after) || before.Size() != after.Size() || !before.ModTime().Equal(after.ModTime()) {
		return "", nil, fmt.Errorf("file changed while hashing: %s", path)
	}
	return fmt.Sprintf("%x", h.Sum(nil)), after, nil
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
