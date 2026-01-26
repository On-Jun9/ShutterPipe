// Package types defines core data structures used across ShutterPipe modules.
package types

import (
	"time"
)

// FileEntry represents a scanned file with its metadata.
type FileEntry struct {
	// Path is the absolute path to the source file.
	Path string
	// Name is the base filename.
	Name string
	// Size is the file size in bytes.
	Size int64
	// ModTime is the file modification time.
	ModTime time.Time
	// Extension is the lowercase file extension without dot (e.g., "jpg", "mp4").
	Extension string
	// IsVideo indicates if this is a video file.
	IsVideo bool
}

// MediaMetadata contains extracted metadata from a media file.
type MediaMetadata struct {
	// CaptureTime is the shooting/creation time extracted from metadata.
	// Nil if extraction failed.
	CaptureTime *time.Time
	// Source indicates where the metadata came from (e.g., "EXIF:DateTimeOriginal", "XML:CreationDate").
	Source string
	// Error contains extraction error message if any.
	Error string
}

// CopyTask represents a planned file copy operation.
type CopyTask struct {
	// Source is the source FileEntry.
	Source FileEntry
	// Metadata contains extracted metadata.
	Metadata MediaMetadata
	// DestDir is the destination directory (e.g., "DEST/2025/12/31" or "DEST/unclassified").
	DestDir string
	// DestPath is the full destination file path.
	DestPath string
	// Status indicates the task status.
	Status TaskStatus
	// Error contains error message if task failed.
	Error string
	// Action indicates what action was taken (copied, skipped, renamed, etc.).
	Action CopyAction
}

// TaskStatus represents the status of a copy task.
type TaskStatus string

const (
	TaskStatusPending   TaskStatus = "pending"
	TaskStatusRunning   TaskStatus = "running"
	TaskStatusCompleted TaskStatus = "completed"
	TaskStatusFailed    TaskStatus = "failed"
	TaskStatusSkipped   TaskStatus = "skipped"
)

// CopyAction represents the action taken for a file.
type CopyAction string

const (
	CopyActionCopied      CopyAction = "copied"
	CopyActionSkipped     CopyAction = "skipped"
	CopyActionRenamed     CopyAction = "renamed"
	CopyActionOverwritten CopyAction = "overwritten"
	CopyActionQuarantined CopyAction = "quarantined"
	CopyActionFailed      CopyAction = "failed"
)

// ConflictPolicy defines how to handle filename conflicts.
type ConflictPolicy string

const (
	ConflictPolicySkip       ConflictPolicy = "skip"
	ConflictPolicyRename     ConflictPolicy = "rename"
	ConflictPolicyOverwrite  ConflictPolicy = "overwrite"
	ConflictPolicyQuarantine ConflictPolicy = "quarantine"
)

// DedupMethod defines how to detect duplicate files.
type DedupMethod string

const (
	DedupMethodNameSize DedupMethod = "name-size"
	DedupMethodHash     DedupMethod = "hash"
)

// OrganizeStrategy defines how files are organized into directories.
type OrganizeStrategy string

const (
	// OrganizeByDate: YYYY/MM/DD structure
	OrganizeByDate OrganizeStrategy = "date"
	// OrganizeByEvent: YYYY/YYMMDD-EventName/FileType structure
	OrganizeByEvent OrganizeStrategy = "event"
)

// RunSummary contains statistics for a completed run.
type RunSummary struct {
	ScannedFiles   int
	TotalFiles     int
	Copied         int
	Skipped        int
	Renamed        int
	Overwritten    int
	Quarantined    int
	Failed         int
	Unclassified   int
	StartTime      time.Time
	EndTime        time.Time
	Duration       time.Duration
	BytesCopied    int64
	BytesPerSecond float64
}

// ConfigPreset represents a saved configuration preset.
type ConfigPreset struct {
	Name              string           `json:"name"`
	Description       string           `json:"description,omitempty"`
	Source            string           `json:"source,omitempty"`
	Dest              string           `json:"dest,omitempty"`
	IncludeExtensions []string         `json:"include_extensions"`
	Jobs              int              `json:"jobs"`
	DedupMethod       DedupMethod      `json:"dedup_method"`
	ConflictPolicy    ConflictPolicy   `json:"conflict_policy"`
	OrganizeStrategy  OrganizeStrategy `json:"organize_strategy"`
	EventName         string           `json:"event_name,omitempty"`
	UnclassifiedDir   string           `json:"unclassified_dir"`
	QuarantineDir     string           `json:"quarantine_dir"`
	DryRun            bool             `json:"dry_run"`
	HashVerify        bool             `json:"hash_verify"`
	IgnoreState       bool             `json:"ignore_state"`
	DateFilterStart   string           `json:"date_filter_start,omitempty"`
	DateFilterEnd     string           `json:"date_filter_end,omitempty"`
	CreatedAt         time.Time        `json:"created_at"`
}
