package planner

import (
	"path/filepath"
	"strings"

	"github.com/On-Jun9/ShutterPipe/pkg/types"
)

type Planner struct {
	destRoot         string
	unclassifiedDir  string
	organizeStrategy types.OrganizeStrategy
	eventName        string
}

func New(destRoot, unclassifiedDir string, organizeStrategy types.OrganizeStrategy, eventName string) *Planner {
	return &Planner{
		destRoot:         destRoot,
		unclassifiedDir:  unclassifiedDir,
		organizeStrategy: organizeStrategy,
		eventName:        eventName,
	}
}

func (p *Planner) Plan(entry types.FileEntry, meta types.MediaMetadata) types.CopyTask {
	task := types.CopyTask{
		Source:   entry,
		Metadata: meta,
		Status:   types.TaskStatusPending,
	}

	if meta.CaptureTime == nil {
		task.DestDir = filepath.Join(p.destRoot, p.unclassifiedDir)
	} else {
		t := *meta.CaptureTime

		switch p.organizeStrategy {
		case types.OrganizeByEvent:
			// YYYY/YYMMDD-EventName/FileType structure
			year := t.Format("2006")
			datePrefix := t.Format("060102") // YYMMDD

			// Build folder name: YYMMDD-EventName or just YYMMDD if no event name
			var folderName string
			if p.eventName != "" {
				folderName = datePrefix + "-" + p.eventName
			} else {
				folderName = datePrefix
			}

			// Determine file type subfolder
			fileType := p.getFileTypeFolder(entry.Extension)

			task.DestDir = filepath.Join(p.destRoot, year, folderName, fileType)

		default: // OrganizeByDate
			// YYYY/MM/DD structure (기존 방식)
			task.DestDir = filepath.Join(
				p.destRoot,
				t.Format("2006"),
				t.Format("01"),
				t.Format("02"),
			)
		}
	}

	task.DestPath = filepath.Join(task.DestDir, entry.Name)
	return task
}

// getFileTypeFolder returns the folder name based on file extension
func (p *Planner) getFileTypeFolder(ext string) string {
	ext = strings.ToLower(ext)

	// RAW formats
	rawFormats := []string{"raw", "arw", "cr2", "nef", "dng", "raf", "orf", "rw2", "srw"}
	for _, raw := range rawFormats {
		if ext == raw {
			return "RAW"
		}
	}

	// Video formats and related files (XML metadata)
	videoFormats := []string{"mp4", "mov", "avi", "mkv", "mxf", "mts", "m2ts", "xml"}
	for _, video := range videoFormats {
		if ext == video {
			return "MP4"
		}
	}

	// Default: JPG (includes jpg, jpeg, heic, heif, png, etc.)
	return "JPG"
}
