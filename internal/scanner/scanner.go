package scanner

import (
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
}

func New(extensions []string) *Scanner {
	extMap := make(map[string]bool)
	for _, ext := range extensions {
		extMap[strings.ToLower(ext)] = true
	}
	return &Scanner{includeExt: extMap}
}

func (s *Scanner) Scan(root string) ([]types.FileEntry, error) {
	var entries []types.FileEntry

	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
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

		info, err := d.Info()
		if err != nil {
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

	return entries, err
}
