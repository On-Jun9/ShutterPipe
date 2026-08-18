package config

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/On-Jun9/ShutterPipe/pkg/types"
	"gopkg.in/yaml.v3"
)

type Config struct {
	Source            string                 `yaml:"source" json:"source"`
	Dest              string                 `yaml:"dest" json:"dest"`
	IncludeExtensions []string               `yaml:"include_extensions" json:"include_extensions"`
	Jobs              int                    `yaml:"jobs" json:"jobs"`
	MetadataJobs      int                    `yaml:"metadata_jobs" json:"metadata_jobs"`
	DedupMethod       types.DedupMethod      `yaml:"dedup_method" json:"dedup_method"`
	ConflictPolicy    types.ConflictPolicy   `yaml:"conflict_policy" json:"conflict_policy"`
	OrganizeStrategy  types.OrganizeStrategy `yaml:"organize_strategy" json:"organize_strategy"`
	EventName         string                 `yaml:"event_name" json:"event_name"`
	UnclassifiedDir   string                 `yaml:"unclassified_dir" json:"unclassified_dir"`
	QuarantineDir     string                 `yaml:"quarantine_dir" json:"quarantine_dir"`
	StateFile         string                 `yaml:"state_file" json:"state_file"`
	LogFile           string                 `yaml:"log_file" json:"log_file"`
	LogJSON           bool                   `yaml:"log_json" json:"log_json"`
	DryRun            bool                   `yaml:"dry_run" json:"dry_run"`
	HashVerify        bool                   `yaml:"hash_verify" json:"hash_verify"`
	IgnoreState       bool                   `yaml:"ignore_state" json:"ignore_state"`
	DateFilterStart   string                 `yaml:"date_filter_start,omitempty" json:"date_filter_start,omitempty"`
	DateFilterEnd     string                 `yaml:"date_filter_end,omitempty" json:"date_filter_end,omitempty"`
}

func DefaultConfig() *Config {
	jobs := runtime.NumCPU()
	if jobs < 1 {
		jobs = 4
	} else if jobs > 32 {
		jobs = 32
	}

	homeDir, _ := os.UserHomeDir()
	stateDir := filepath.Join(homeDir, ".shutterpipe")

	return &Config{
		IncludeExtensions: []string{
			"jpg", "jpeg", "heic", "heif", "png", "raw", "arw", "cr2", "nef", "dng",
			"mp4", "mov", "avi", "mkv", "mxf", "xml",
		},
		Jobs:             jobs,
		MetadataJobs:     2,
		DedupMethod:      types.DedupMethodNameSize,
		ConflictPolicy:   types.ConflictPolicySkip,
		OrganizeStrategy: types.OrganizeByDate,
		EventName:        "",
		UnclassifiedDir:  "unclassified",
		QuarantineDir:    "quarantine",
		StateFile:        filepath.Join(stateDir, "state.json"),
		LogFile:          filepath.Join(stateDir, "shutterpipe.log"),
		LogJSON:          false,
		DryRun:           false,
		HashVerify:       false,
		IgnoreState:      false,
	}
}

func LoadFromFile(path string) (*Config, error) {
	cfg := DefaultConfig()

	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	if err := yaml.Unmarshal(data, cfg); err != nil {
		return nil, err
	}

	return cfg, nil
}

func (c *Config) Validate() error {
	if c.Source == "" {
		return &ValidationError{Field: "source", Message: "source path is required"}
	}
	if c.Dest == "" {
		return &ValidationError{Field: "dest", Message: "destination path is required"}
	}
	if c.ConflictPolicy == "" {
		c.ConflictPolicy = types.ConflictPolicySkip
	}
	switch c.ConflictPolicy {
	case types.ConflictPolicySkip, types.ConflictPolicyRename, types.ConflictPolicyOverwrite, types.ConflictPolicyQuarantine:
	default:
		return &ValidationError{Field: "conflict_policy", Message: "must be one of skip, rename, overwrite, quarantine"}
	}
	if c.DedupMethod == "" {
		c.DedupMethod = types.DedupMethodNameSize
	}
	switch c.DedupMethod {
	case types.DedupMethodNameSize, types.DedupMethodHash:
	default:
		return &ValidationError{Field: "dedup_method", Message: "must be one of name-size, hash"}
	}
	if c.OrganizeStrategy == "" {
		c.OrganizeStrategy = types.OrganizeByDate
	}
	switch c.OrganizeStrategy {
	case types.OrganizeByDate, types.OrganizeByEvent:
	default:
		return &ValidationError{Field: "organize_strategy", Message: "must be one of date, event"}
	}
	startDate, err := validateDateFilter("date_filter_start", c.DateFilterStart)
	if err != nil {
		return err
	}
	endDate, err := validateDateFilter("date_filter_end", c.DateFilterEnd)
	if err != nil {
		return err
	}
	if !startDate.IsZero() && !endDate.IsZero() && startDate.After(endDate) {
		return &ValidationError{Field: "date_filter_end", Message: "must be on or after date_filter_start"}
	}
	if err := validateDestinationOutsideSource(c.Source, c.Dest); err != nil {
		return err
	}
	// Jobs: 0 = auto (use CPU cores), 1..32 = explicit worker count.
	if err := ValidateJobs(c.Jobs); err != nil {
		return err
	}
	if c.Jobs == 0 {
		c.Jobs = runtime.NumCPU()
		if c.Jobs < 1 {
			c.Jobs = 4
		} else if c.Jobs > 32 {
			c.Jobs = 32
		}
	}
	// MetadataJobs: 0 keeps backward compatibility with settings and presets
	// created before this option existed; metadata analysis defaults to 2.
	if err := ValidateMetadataJobs(c.MetadataJobs); err != nil {
		return err
	}
	if c.MetadataJobs == 0 {
		c.MetadataJobs = 2
	}

	homeDir, _ := os.UserHomeDir()
	stateDir := filepath.Join(homeDir, ".shutterpipe")

	if c.LogFile == "" {
		c.LogFile = filepath.Join(stateDir, "shutterpipe.log")
	}
	if c.StateFile == "" {
		c.StateFile = filepath.Join(stateDir, "state.json")
	}
	if c.UnclassifiedDir == "" {
		c.UnclassifiedDir = "unclassified"
	}
	if c.QuarantineDir == "" {
		c.QuarantineDir = "quarantine"
	}
	if err := validateDestinationSubdir("unclassified_dir", c.UnclassifiedDir); err != nil {
		return err
	}
	if err := validateDestinationSubdir("quarantine_dir", c.QuarantineDir); err != nil {
		return err
	}
	if strings.ContainsAny(c.EventName, `/\`+"\x00") {
		return &ValidationError{Field: "event_name", Message: "must be a folder name without path separators"}
	}

	return nil
}

func ValidateJobs(value int) error {
	if value < 0 || value > 32 {
		return &ValidationError{Field: "jobs", Message: "must be between 0 and 32"}
	}
	return nil
}

func ValidateMetadataJobs(value int) error {
	if value < 0 || value > 32 {
		return &ValidationError{Field: "metadata_jobs", Message: "must be between 0 and 32"}
	}
	return nil
}

func validateDateFilter(field, value string) (time.Time, error) {
	if value == "" {
		return time.Time{}, nil
	}
	parsed, err := time.Parse("2006-01-02", value)
	if err != nil {
		return time.Time{}, &ValidationError{Field: field, Message: "must use YYYY-MM-DD format"}
	}
	return parsed, nil
}

func validateDestinationOutsideSource(source, dest string) error {
	resolvedSource, err := resolvePathWithMissingTail(source)
	if err != nil {
		return err
	}
	resolvedDest, err := resolvePathWithMissingTail(dest)
	if err != nil {
		return err
	}
	if pathContains(resolvedSource, resolvedDest) {
		return &ValidationError{Field: "dest", Message: "destination must be outside source"}
	}
	if pathContains(resolvedDest, resolvedSource) {
		return &ValidationError{Field: "source", Message: "source must be outside destination"}
	}

	// EvalSymlinks does not canonicalize case on every filesystem. Compare the
	// source directory with each existing destination ancestor as a second guard
	// for case-insensitive filesystems and symlinked missing destination tails.
	sourceInfo, statErr := os.Stat(source)
	if statErr != nil {
		if os.IsNotExist(statErr) {
			return nil
		}
		return statErr
	}
	destAbs, err := filepath.Abs(dest)
	if err != nil {
		return err
	}
	for current := filepath.Clean(destAbs); ; current = filepath.Dir(current) {
		if info, infoErr := os.Stat(current); infoErr == nil && os.SameFile(sourceInfo, info) {
			return &ValidationError{Field: "dest", Message: "destination must be outside source"}
		}
		parent := filepath.Dir(current)
		if parent == current {
			break
		}
	}
	destInfo, statErr := os.Stat(dest)
	if statErr != nil {
		if os.IsNotExist(statErr) {
			return nil
		}
		return statErr
	}
	sourceAbs, err := filepath.Abs(source)
	if err != nil {
		return err
	}
	for current := filepath.Clean(sourceAbs); ; current = filepath.Dir(current) {
		if info, infoErr := os.Stat(current); infoErr == nil && os.SameFile(destInfo, info) {
			return &ValidationError{Field: "source", Message: "source must be outside destination"}
		}
		parent := filepath.Dir(current)
		if parent == current {
			break
		}
	}
	return nil
}

func resolvePathWithMissingTail(path string) (string, error) {
	absPath, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	current := filepath.Clean(absPath)
	var missing []string
	for {
		resolved, resolveErr := filepath.EvalSymlinks(current)
		if resolveErr == nil {
			for index := len(missing) - 1; index >= 0; index-- {
				resolved = filepath.Join(resolved, missing[index])
			}
			return filepath.Clean(resolved), nil
		}
		if !os.IsNotExist(resolveErr) {
			return "", resolveErr
		}
		parent := filepath.Dir(current)
		if parent == current {
			return current, nil
		}
		missing = append(missing, filepath.Base(current))
		current = parent
	}
}

func pathContains(root, candidate string) bool {
	rel, err := filepath.Rel(root, candidate)
	if err != nil || filepath.IsAbs(rel) {
		return false
	}
	return rel == "." || (rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)))
}

func validateDestinationSubdir(field, value string) error {
	if filepath.IsAbs(value) || filepath.VolumeName(value) != "" || strings.Contains(value, `\`) || strings.ContainsRune(value, '\x00') {
		return &ValidationError{Field: field, Message: "must be a relative path inside destination"}
	}
	cleaned := filepath.Clean(value)
	if cleaned == ".." || strings.HasPrefix(cleaned, ".."+string(filepath.Separator)) {
		return &ValidationError{Field: field, Message: "must not escape destination"}
	}
	joined := filepath.Join("destination", cleaned)
	rel, err := filepath.Rel("destination", joined)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return &ValidationError{Field: field, Message: "must remain inside destination"}
	}
	return nil
}

type ValidationError struct {
	Field   string
	Message string
}

func (e *ValidationError) Error() string {
	return e.Field + ": " + e.Message
}
