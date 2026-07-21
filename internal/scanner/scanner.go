package scanner

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/On-Jun9/ShutterPipe/pkg/types"
)

var videoExtensions = map[string]bool{
	"mp4": true, "mov": true, "avi": true, "mkv": true, "mxf": true,
	"m4v": true, "webm": true, "wmv": true, "flv": true,
}

type Scanner struct {
	includeExt map[string]bool
	includeAll bool
	entryInfo  func(os.DirEntry) (os.FileInfo, error)
}

type ScanIssue struct {
	Path  string
	IsDir bool
	Err   error
}

func New(extensions []string) *Scanner {
	extMap := make(map[string]bool)
	for _, ext := range extensions {
		extMap[strings.ToLower(ext)] = true
	}
	return &Scanner{
		includeExt: extMap,
		entryInfo:  func(entry os.DirEntry) (os.FileInfo, error) { return entry.Info() },
	}
}

// NewAll creates a scanner for every regular file below a destination root.
// It is intentionally separate from New so backup source filtering keeps its
// existing behavior.
func NewAll() *Scanner {
	return &Scanner{
		includeAll: true,
		entryInfo:  func(entry os.DirEntry) (os.FileInfo, error) { return entry.Info() },
	}
}

func (s *Scanner) Scan(root string) ([]types.FileEntry, error) {
	return s.ScanWithContext(context.Background(), root)
}

func (s *Scanner) ScanWithContext(ctx context.Context, root string) ([]types.FileEntry, error) {
	if ctx == nil {
		ctx = context.Background()
	}

	var entries []types.FileEntry

	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err := ctx.Err(); err != nil {
			return err
		}

		if err != nil {
			return err
		}

		if d.IsDir() {
			return nil
		}

		ext := strings.TrimPrefix(strings.ToLower(filepath.Ext(path)), ".")
		if !s.includeExt[ext] {
			return nil
		}

		entryInfo := s.entryInfo
		if entryInfo == nil {
			entryInfo = func(entry os.DirEntry) (os.FileInfo, error) { return entry.Info() }
		}
		info, err := entryInfo(d)
		if err != nil {
			return fmt.Errorf("failed to inspect source file %s: %w", path, err)
		}

		entries = append(entries, types.FileEntry{
			Path:      path,
			Name:      d.Name(),
			Size:      info.Size(),
			ModTime:   info.ModTime(),
			Extension: ext,
			IsVideo:   videoExtensions[ext],
		})

		return nil
	})

	return entries, err
}

// ScanCollectingWithContext keeps walking after an individual entry or
// subtree cannot be inspected. A failure at the root and context termination
// remain fatal because no meaningful partial scan can be established.
func (s *Scanner) ScanCollectingWithContext(ctx context.Context, root string) ([]types.FileEntry, []ScanIssue, error) {
	if ctx == nil {
		ctx = context.Background()
	}

	var entries []types.FileEntry
	var issues []ScanIssue
	cleanRoot := filepath.Clean(root)

	err := filepath.WalkDir(root, func(path string, d os.DirEntry, walkErr error) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if walkErr != nil {
			if filepath.Clean(path) == cleanRoot {
				return walkErr
			}
			issue := ScanIssue{Path: path, Err: walkErr}
			if d != nil {
				issue.IsDir = d.IsDir()
			}
			issues = append(issues, issue)
			return nil
		}
		if d.IsDir() {
			return nil
		}

		ext := strings.TrimPrefix(strings.ToLower(filepath.Ext(path)), ".")
		if !s.includeAll && !s.includeExt[ext] {
			return nil
		}

		entryInfo := s.entryInfo
		if entryInfo == nil {
			entryInfo = func(entry os.DirEntry) (os.FileInfo, error) { return entry.Info() }
		}
		info, infoErr := entryInfo(d)
		if infoErr != nil {
			issues = append(issues, ScanIssue{Path: path, Err: infoErr})
			return nil
		}
		if s.includeAll && !info.Mode().IsRegular() {
			return nil
		}

		entries = append(entries, types.FileEntry{
			Path:      path,
			Name:      d.Name(),
			Size:      info.Size(),
			ModTime:   info.ModTime(),
			Extension: ext,
			IsVideo:   videoExtensions[ext],
		})
		return nil
	})

	return entries, issues, err
}
