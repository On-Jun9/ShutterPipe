package main

import (
	"fmt"
	"os"

	"github.com/On-Jun9/ShutterPipe/internal/config"
	"github.com/On-Jun9/ShutterPipe/internal/pipeline"
	"github.com/On-Jun9/ShutterPipe/pkg/types"
	"github.com/spf13/cobra"
)

var (
	appVersion    = "0.1.0"
	cfgFile        string
	source         string
	dest           string
	includeExt     []string
	jobs           int
	dedupMethod    string
	conflictPolicy string
	unclassified   string
	quarantine     string
	stateFile      string
	logFile        string
	logJSON        bool
	dryRun         bool
	hashVerify     bool
)

func main() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

var rootCmd = &cobra.Command{
	Use:   "shutterpipe",
	Short: "Auto-backup and organize photos/videos by capture date",
	Long: `ShutterPipe scans SD card for photos/videos, extracts capture date 
from metadata (EXIF/XML), and copies files to NAS organized by date (YYYY/MM/DD).`,
}

var runCmd = &cobra.Command{
	Use:   "run",
	Short: "Run the backup pipeline",
	RunE:  runPipeline,
}

var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Print version information",
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Println(appVersion)
	},
}

func init() {
	rootCmd.AddCommand(runCmd)
	rootCmd.AddCommand(versionCmd)

	runCmd.Flags().StringVarP(&cfgFile, "config", "c", "", "config file path")
	runCmd.Flags().StringVarP(&source, "source", "s", "", "source directory (SD card)")
	runCmd.Flags().StringVarP(&dest, "dest", "d", "", "destination directory (NAS)")
	runCmd.Flags().StringSliceVarP(&includeExt, "include-ext", "e", nil, "file extensions to include")
	runCmd.Flags().IntVarP(&jobs, "jobs", "j", 0, "number of concurrent workers (0=auto)")
	runCmd.Flags().StringVar(&dedupMethod, "dedup", "", "dedup method: name-size, hash")
	runCmd.Flags().StringVar(&conflictPolicy, "conflict", "", "conflict policy: skip, rename, overwrite, quarantine")
	runCmd.Flags().StringVar(&unclassified, "unclassified-dir", "", "directory for files without capture date")
	runCmd.Flags().StringVar(&quarantine, "quarantine-dir", "", "directory for conflicting files")
	runCmd.Flags().StringVar(&stateFile, "state-file", "", "state file for resume")
	runCmd.Flags().StringVar(&logFile, "log-file", "", "log file path")
	runCmd.Flags().BoolVar(&logJSON, "log-json", false, "output JSON logs")
	runCmd.Flags().BoolVar(&dryRun, "dry-run", false, "simulate without copying")
	runCmd.Flags().BoolVar(&hashVerify, "hash-verify", false, "verify copies with hash")
}

func runPipeline(cmd *cobra.Command, args []string) error {
	var cfg *config.Config
	var err error

	if cfgFile != "" {
		cfg, err = config.LoadFromFile(cfgFile)
		if err != nil {
			return fmt.Errorf("failed to load config: %w", err)
		}
	} else {
		cfg = config.DefaultConfig()
	}

	if source != "" {
		cfg.Source = source
	}
	if dest != "" {
		cfg.Dest = dest
	}
	if len(includeExt) > 0 {
		cfg.IncludeExtensions = includeExt
	}
	if jobs > 0 {
		cfg.Jobs = jobs
	}
	if dedupMethod != "" {
		cfg.DedupMethod = types.DedupMethod(dedupMethod)
	}
	if conflictPolicy != "" {
		cfg.ConflictPolicy = types.ConflictPolicy(conflictPolicy)
	}
	if unclassified != "" {
		cfg.UnclassifiedDir = unclassified
	}
	if quarantine != "" {
		cfg.QuarantineDir = quarantine
	}
	if stateFile != "" {
		cfg.StateFile = stateFile
	}
	if logFile != "" {
		cfg.LogFile = logFile
	}
	if logJSON {
		cfg.LogJSON = true
	}
	if dryRun {
		cfg.DryRun = true
	}
	if hashVerify {
		cfg.HashVerify = true
	}

	if err := cfg.Validate(); err != nil {
		return err
	}

	p, err := pipeline.New(cfg)
	if err != nil {
		return fmt.Errorf("failed to create pipeline: %w", err)
	}
	defer p.Close()

	_, err = p.Run()
	return err
}
