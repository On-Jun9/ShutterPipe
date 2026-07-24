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
	// ConflictPolicy is retained through commit so a destination created after
	// planning still follows the selected policy.
	ConflictPolicy ConflictPolicy
	// QuarantineDir is the commit-time fallback destination for quarantine.
	QuarantineDir string
	// DestinationRoot constrains commit paths after resolving existing symlinks.
	DestinationRoot string
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
	// Warnings reports non-copy failures, such as state or history persistence
	// errors, without misclassifying successfully copied files as failed.
	Warnings []string
}

// RunKind identifies which operation owns the shared run lifecycle.
type RunKind string

const (
	RunKindBackup RunKind = "backup"
	RunKindVerify RunKind = "verify"
)

// VerifyMode controls how source files are matched against the destination.
type VerifyMode string

const (
	VerifyModeQuick VerifyMode = "quick"
	VerifyModeHash  VerifyMode = "hash"
)

// VerifyVerdict is the result of comparing one source file.
type VerifyVerdict string

const (
	VerifyVerdictOK           VerifyVerdict = "ok"
	VerifyVerdictMissing      VerifyVerdict = "missing"
	VerifyVerdictMismatch     VerifyVerdict = "mismatch"
	VerifyVerdictUnverifiable VerifyVerdict = "unverifiable"
)

// VerifyProblem is the bounded, client-facing representation of a problem
// found during verification. The server keeps the complete requeue data
// separately from this preview.
type VerifyProblem struct {
	Verdict    VerifyVerdict `json:"verdict"`
	SourcePath string        `json:"source_path"`
	Name       string        `json:"name"`
	Size       int64         `json:"size"`
	Reason     string        `json:"reason"`
}

// HashManifestSummary describes an uploaded GNU sha256sum manifest.
type HashManifestSummary struct {
	Filename    string `json:"filename"`
	Entries     int    `json:"entries"`
	ParseErrors int    `json:"parse_errors"`
}

// VerifySummary contains the terminal snapshot for a verification run.
type VerifySummary struct {
	Mode               VerifyMode           `json:"mode"`
	Source             string               `json:"source"`
	Dest               string               `json:"dest"`
	SourceFiles        int                  `json:"source_files"`
	SourceBytes        int64                `json:"source_bytes"`
	Normal             int                  `json:"normal"`
	Missing            int                  `json:"missing"`
	Mismatch           int                  `json:"mismatch"`
	Unverifiable       int                  `json:"unverifiable"`
	ProblemCount       int                  `json:"problem_count"`
	Problems           []VerifyProblem      `json:"problems,omitempty"`
	ProblemsTruncated  bool                 `json:"problems_truncated,omitempty"`
	Warnings           []string             `json:"warnings,omitempty"`
	Manifest           *HashManifestSummary `json:"manifest,omitempty"`
	IncompleteManifest bool                 `json:"incomplete_manifest,omitempty"`
	RequeueAllowed     bool                 `json:"requeue_allowed"`
	RequeueEligible    int                  `json:"requeue_eligible"`
	StartTime          time.Time            `json:"start_time"`
	EndTime            time.Time            `json:"end_time"`
	Duration           time.Duration        `json:"duration"`
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

// UserSettings represents the current user settings (migrated from localStorage).
type UserSettings struct {
	Source            string           `json:"source"`
	Dest              string           `json:"dest"`
	OrganizeStrategy  OrganizeStrategy `json:"organize_strategy"`
	EventName         string           `json:"event_name,omitempty"`
	ConflictPolicy    ConflictPolicy   `json:"conflict_policy"`
	DedupMethod       DedupMethod      `json:"dedup_method"`
	DryRun            bool             `json:"dry_run"`
	HashVerify        bool             `json:"hash_verify"`
	IgnoreState       bool             `json:"ignore_state"`
	DateFilterStart   string           `json:"date_filter_start,omitempty"`
	DateFilterEnd     string           `json:"date_filter_end,omitempty"`
	Jobs              int              `json:"jobs"`
	IncludeExtensions []string         `json:"include_extensions"`
	UnclassifiedDir   string           `json:"unclassified_dir"`
	QuarantineDir     string           `json:"quarantine_dir"`
	StateFile         string           `json:"state_file"`
	LogFile           string           `json:"log_file"`
	LogJSON           bool             `json:"log_json"`
	UpdatedAt         time.Time        `json:"updated_at"`
}

// PathHistory stores the list of recently used paths for autocomplete.
type PathHistory struct {
	Source    []string  `json:"source"`
	Dest      []string  `json:"dest"`
	UpdatedAt time.Time `json:"updated_at"`
}

// Bookmarks stores the user's bookmarked paths.
type Bookmarks struct {
	Source    []string  `json:"source"`
	Dest      []string  `json:"dest"`
	UpdatedAt time.Time `json:"updated_at"`
}

// BackupStatus represents the status of a backup operation.
type BackupStatus string

const (
	BackupStatusSuccess  BackupStatus = "success"
	BackupStatusFailed   BackupStatus = "failed"
	BackupStatusCanceled BackupStatus = "canceled"
)

// BackupConfig contains the configuration used for a backup operation.
type BackupConfig struct {
	Source            string           `json:"source"`
	Dest              string           `json:"dest"`
	OrganizeStrategy  OrganizeStrategy `json:"organize_strategy"`
	EventName         string           `json:"event_name,omitempty"`
	ConflictPolicy    ConflictPolicy   `json:"conflict_policy"`
	DedupMethod       DedupMethod      `json:"dedup_method"`
	DryRun            bool             `json:"dry_run"`
	HashVerify        bool             `json:"hash_verify"`
	IgnoreState       bool             `json:"ignore_state"`
	DateFilterStart   string           `json:"date_filter_start,omitempty"`
	DateFilterEnd     string           `json:"date_filter_end,omitempty"`
	Jobs              int              `json:"jobs"`
	IncludeExtensions []string         `json:"include_extensions"`
	UnclassifiedDir   string           `json:"unclassified_dir"`
	QuarantineDir     string           `json:"quarantine_dir"`
}

// BackupHistoryEntry represents a single backup session record.
type BackupHistoryEntry struct {
	ID            string         `json:"id"`
	Kind          RunKind        `json:"kind,omitempty"`
	Summary       RunSummary     `json:"summary"`
	VerifySummary *VerifySummary `json:"verify_summary,omitempty"`
	Config        BackupConfig   `json:"config"`
	Status        BackupStatus   `json:"status"`
	CreatedAt     time.Time      `json:"created_at"`
}

// BackupHistory stores the collection of backup history entries.
type BackupHistory struct {
	Entries   []BackupHistoryEntry `json:"entries"`
	UpdatedAt time.Time            `json:"updated_at"`
}
