package config

import (
	"os"
	"path/filepath"
	"runtime"

	"github.com/On-Jun9/ShutterPipe/pkg/types"
	"gopkg.in/yaml.v3"
)

type Config struct {
	Source            string                  `yaml:"source" json:"source"`
	Dest              string                  `yaml:"dest" json:"dest"`
	IncludeExtensions []string                `yaml:"include_extensions" json:"include_extensions"`
	Jobs              int                     `yaml:"jobs" json:"jobs"`
	DedupMethod       types.DedupMethod       `yaml:"dedup_method" json:"dedup_method"`
	ConflictPolicy    types.ConflictPolicy    `yaml:"conflict_policy" json:"conflict_policy"`
	OrganizeStrategy  types.OrganizeStrategy  `yaml:"organize_strategy" json:"organize_strategy"`
	EventName         string                  `yaml:"event_name" json:"event_name"`
	UnclassifiedDir   string                  `yaml:"unclassified_dir" json:"unclassified_dir"`
	QuarantineDir     string                  `yaml:"quarantine_dir" json:"quarantine_dir"`
	StateFile         string                  `yaml:"state_file" json:"state_file"`
	LogFile           string                  `yaml:"log_file" json:"log_file"`
	LogJSON           bool                    `yaml:"log_json" json:"log_json"`
	DryRun            bool                    `yaml:"dry_run" json:"dry_run"`
	HashVerify        bool                    `yaml:"hash_verify" json:"hash_verify"`
	IgnoreState       bool                    `yaml:"ignore_state" json:"ignore_state"`
}

func DefaultConfig() *Config {
	jobs := runtime.NumCPU()
	if jobs < 1 {
		jobs = 4
	}

	homeDir, _ := os.UserHomeDir()
	stateDir := filepath.Join(homeDir, ".shutterpipe")

	return &Config{
		IncludeExtensions: []string{
			"jpg", "jpeg", "heic", "heif", "png", "raw", "arw", "cr2", "nef", "dng",
			"mp4", "mov", "avi", "mkv", "mxf", "xml",
		},
		Jobs:             jobs,
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
	if c.Jobs < 1 {
		c.Jobs = 1
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

	return nil
}

type ValidationError struct {
	Field   string
	Message string
}

func (e *ValidationError) Error() string {
	return e.Field + ": " + e.Message
}
