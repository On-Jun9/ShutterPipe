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
	entryInfo  func(os.DirEntry) (os.FileInfo, error)
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
