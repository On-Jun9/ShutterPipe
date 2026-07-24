package state

import (
	"context"
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
	// ConfigFingerprint captures the destination-shaping settings in effect when
	// this record was written. A record is only reusable when the current run's
	// settings resolve to the same destination layout, so changing the target
	// directory or organize strategy correctly forces a re-backup instead of
	// silently skipping the file against a stale destination.
	ConfigFingerprint string `json:"config_fingerprint,omitempty"`
	// Sidecar* capture the path, stat identity, and content hash of the metadata
	// sidecar (e.g. a video's M01.XML) that supplied the file's classification.
	// If the sidecar appears or changes after this record was written, the
	// destination may differ, so the file must be reprocessed.
	SidecarPresent         bool   `json:"sidecar_present,omitempty"`
	SidecarPath            string `json:"sidecar_path,omitempty"`
	SidecarSize            int64  `json:"sidecar_size,omitempty"`
	SidecarModTimeUnixNano int64  `json:"sidecar_mod_time_unix_nano,omitempty"`
	SidecarHash            string `json:"sidecar_hash,omitempty"`
}

type RebackupMarker struct {
	VerificationRunID     string              `json:"verification_run_id"`
	SourcePath            string              `json:"source_path"`
	DestPath              string              `json:"dest_path,omitempty"`
	Verdict               types.VerifyVerdict `json:"verdict"`
	ConfigFingerprint     string              `json:"config_fingerprint"`
	SourceSize            int64               `json:"source_size"`
	SourceModTimeUnixNano int64               `json:"source_mod_time_unix_nano"`
	SourceHash            string              `json:"source_hash,omitempty"`
	// Sidecar* pin the classification input that produced DestPath. If the
	// sidecar (e.g. a video's M01.XML) changes between marking and applying,
	// the planned destination may differ from the recorded DestPath, so the
	// marker must be discarded and the file re-classified through the normal
	// backup flow instead of being force-overwritten to a stale destination.
	SidecarPresent         bool   `json:"sidecar_present,omitempty"`
	SidecarPath            string `json:"sidecar_path,omitempty"`
	SidecarSize            int64  `json:"sidecar_size,omitempty"`
	SidecarModTimeUnixNano int64  `json:"sidecar_mod_time_unix_nano,omitempty"`
	SidecarHash            string `json:"sidecar_hash,omitempty"`
}

// SourceContext bundles the inputs, beyond the source file's own identity, that
// determine where the file is published. A change in any of them means a prior
// state record must not shortcut the run.
type SourceContext struct {
	ConfigFingerprint      string
	SidecarPresent         bool
	SidecarPath            string
	SidecarSize            int64
	SidecarModTimeUnixNano int64
	SidecarHash            string
}

func (c SourceContext) matchesRecord(record ProcessedFile) bool {
	return record.ConfigFingerprint == c.ConfigFingerprint &&
		record.SidecarPresent == c.SidecarPresent &&
		record.SidecarPath == c.SidecarPath &&
		record.SidecarSize == c.SidecarSize &&
		record.SidecarModTimeUnixNano == c.SidecarModTimeUnixNano &&
		record.SidecarHash == c.SidecarHash
}

type State struct {
	mu        sync.RWMutex
	filePath  string
	Processed map[string]ProcessedFile  `json:"processed"`
	Rebackup  map[string]RebackupMarker `json:"rebackup,omitempty"`
	LastRun   time.Time                 `json:"last_run"`
}

func New(filePath string) *State {
	return &State{
		filePath:  filePath,
		Processed: make(map[string]ProcessedFile),
		Rebackup:  make(map[string]RebackupMarker),
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
	if s.Processed == nil {
		s.Processed = make(map[string]ProcessedFile)
	}
	if s.Rebackup == nil {
		s.Rebackup = make(map[string]RebackupMarker)
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

func (s *State) IsEntryProcessed(ctx context.Context, entry types.FileEntry, hashVerify bool, sctx SourceContext) bool {
	s.mu.RLock()
	record, ok := s.Processed[entry.Path]
	s.mu.RUnlock()
	if !ok || record.IdentityVersion < 2 || record.Size != entry.Size ||
		record.SourceModTimeUnixNano != entry.ModTime.UnixNano() || record.DestPath == "" ||
		!sctx.matchesRecord(record) {
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
	sourceHash, _, err := hashStableFileWithContext(ctx, entry.Path)
	if err != nil || sourceHash != record.SourceHash {
		return false
	}
	destHash, _, err := hashStableFileWithContext(ctx, record.DestPath)
	return err == nil && destHash == record.DestHash
}

func (s *State) MarkProcessedEntry(ctx context.Context, entry types.FileEntry, destPath string, hashVerify bool, sctx SourceContext) error {
	return s.markProcessedEntry(ctx, entry, destPath, hashVerify, nil, false, false, sctx)
}

// MarkProcessedEntryWithVerifiedHash binds the state record to the exact
// source snapshot that the copier verified before publishing the destination.
func (s *State) MarkProcessedEntryWithVerifiedHash(ctx context.Context, entry types.FileEntry, destPath string, verifiedHash []byte, sctx SourceContext) error {
	if len(verifiedHash) != sha256.Size {
		return fmt.Errorf("verified source hash is required for state commit: %s", entry.Path)
	}
	return s.markProcessedEntry(ctx, entry, destPath, true, verifiedHash, true, false, sctx)
}

// MarkSupersededEntry records an overwrite loser whose content is intentionally
// represented by the deterministic winner at destPath.
func (s *State) MarkSupersededEntry(ctx context.Context, entry types.FileEntry, destPath string, hashVerify bool, sctx SourceContext) error {
	return s.markProcessedEntry(ctx, entry, destPath, hashVerify, nil, false, true, sctx)
}

func (s *State) markProcessedEntry(ctx context.Context, entry types.FileEntry, destPath string, hashVerify bool, verifiedHash []byte, requireVerifiedHash, superseded bool, sctx SourceContext) error {
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
		sourceHash, sourceInfo, err = hashStableFileWithContext(ctx, entry.Path)
		if err != nil {
			return err
		}
		destHash, destInfo, err = hashStableFileWithContext(ctx, destPath)
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
		ConfigFingerprint:      sctx.ConfigFingerprint,
		SidecarPresent:         sctx.SidecarPresent,
		SidecarPath:            sctx.SidecarPath,
		SidecarSize:            sctx.SidecarSize,
		SidecarModTimeUnixNano: sctx.SidecarModTimeUnixNano,
		SidecarHash:            sctx.SidecarHash,
	}
	s.LastRun = now
	s.mu.Unlock()
	return nil
}

func hashStableFileWithContext(ctx context.Context, path string) (string, os.FileInfo, error) {
	if ctx == nil {
		ctx = context.Background()
	}
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
	buf := make([]byte, 1024*1024)
	for {
		if err := ctx.Err(); err != nil {
			return "", nil, err
		}
		n, readErr := f.Read(buf)
		if n > 0 {
			if _, err := h.Write(buf[:n]); err != nil {
				return "", nil, err
			}
		}
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			return "", nil, readErr
		}
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

// ProcessedSnapshotForRequeue returns a stable identity for stale-result
// checks and a destination only when the record still describes this source
// and classification context. Destination contents are deliberately not
// checked: a missing or damaged destination is exactly what requeue repairs.
func (s *State) ProcessedSnapshotForRequeue(entry types.FileEntry, sctx SourceContext) (recordID string, present bool, trustedDestPath string) {
	s.mu.RLock()
	record, ok := s.Processed[entry.Path]
	s.mu.RUnlock()
	if !ok {
		return "", false, ""
	}
	recordID = ProcessedRecordID(record)
	trusted := record.IdentityVersion >= 2 &&
		record.Path == entry.Path &&
		record.Size == entry.Size &&
		record.SourceModTimeUnixNano == entry.ModTime.UnixNano() &&
		record.DestPath != "" &&
		!record.Superseded &&
		sctx.matchesRecord(record)
	if trusted {
		trustedDestPath = record.DestPath
	}
	return recordID, true, trustedDestPath
}

func ProcessedRecordID(record ProcessedFile) string {
	data, err := json.Marshal(record)
	if err != nil {
		return ""
	}
	sum := sha256.Sum256(data)
	return fmt.Sprintf("%x", sum[:])
}

func (s *State) RebackupMarker(path string) (RebackupMarker, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	marker, ok := s.Rebackup[path]
	return marker, ok
}

func (s *State) SetRebackupMarker(marker RebackupMarker) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.Rebackup == nil {
		s.Rebackup = make(map[string]RebackupMarker)
	}
	s.Rebackup[marker.SourcePath] = marker
}

func (s *State) RemoveRebackupMarker(path string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.Rebackup, path)
}
