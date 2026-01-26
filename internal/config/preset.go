package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/On-Jun9/ShutterPipe/pkg/types"
)

// PresetManager manages configuration presets.
type PresetManager struct {
	presetsDir string
}

// NewPresetManager creates a new preset manager.
func NewPresetManager() (*PresetManager, error) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("failed to get home directory: %w", err)
	}

	presetsDir := filepath.Join(homeDir, ".shutterpipe", "presets")
	if err := os.MkdirAll(presetsDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create presets directory: %w", err)
	}

	return &PresetManager{presetsDir: presetsDir}, nil
}

// ConfigToPreset converts a Config to a ConfigPreset.
func ConfigToPreset(cfg *Config, name, description string) *types.ConfigPreset {
	return &types.ConfigPreset{
		Name:              name,
		Description:       description,
		Source:            cfg.Source,
		Dest:              cfg.Dest,
		IncludeExtensions: cfg.IncludeExtensions,
		Jobs:              cfg.Jobs,
		DedupMethod:       cfg.DedupMethod,
		ConflictPolicy:    cfg.ConflictPolicy,
		OrganizeStrategy:  cfg.OrganizeStrategy,
		EventName:         cfg.EventName,
		UnclassifiedDir:   cfg.UnclassifiedDir,
		QuarantineDir:     cfg.QuarantineDir,
		DryRun:            cfg.DryRun,
		HashVerify:        cfg.HashVerify,
		IgnoreState:       cfg.IgnoreState,
		DateFilterStart:   cfg.DateFilterStart,
		DateFilterEnd:     cfg.DateFilterEnd,
		CreatedAt:         time.Now(),
	}
}

// PresetToConfig converts a ConfigPreset to a Config.
func PresetToConfig(preset *types.ConfigPreset) *Config {
	cfg := DefaultConfig()
	cfg.Source = preset.Source
	cfg.Dest = preset.Dest
	cfg.IncludeExtensions = preset.IncludeExtensions
	cfg.Jobs = preset.Jobs
	cfg.DedupMethod = preset.DedupMethod
	cfg.ConflictPolicy = preset.ConflictPolicy
	cfg.OrganizeStrategy = preset.OrganizeStrategy
	cfg.EventName = preset.EventName
	cfg.UnclassifiedDir = preset.UnclassifiedDir
	cfg.QuarantineDir = preset.QuarantineDir
	cfg.DryRun = preset.DryRun
	cfg.HashVerify = preset.HashVerify
	cfg.IgnoreState = preset.IgnoreState
	cfg.DateFilterStart = preset.DateFilterStart
	cfg.DateFilterEnd = preset.DateFilterEnd
	return cfg
}

// SavePreset saves a preset to disk.
func (pm *PresetManager) SavePreset(preset *types.ConfigPreset) error {
	if preset.Name == "" {
		return fmt.Errorf("preset name cannot be empty")
	}

	filename := filepath.Join(pm.presetsDir, preset.Name+".json")
	data, err := json.MarshalIndent(preset, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal preset: %w", err)
	}

	if err := os.WriteFile(filename, data, 0644); err != nil {
		return fmt.Errorf("failed to write preset file: %w", err)
	}

	return nil
}

// LoadPreset loads a preset from disk.
func (pm *PresetManager) LoadPreset(name string) (*types.ConfigPreset, error) {
	filename := filepath.Join(pm.presetsDir, name+".json")
	data, err := os.ReadFile(filename)
	if err != nil {
		return nil, fmt.Errorf("failed to read preset file: %w", err)
	}

	var preset types.ConfigPreset
	if err := json.Unmarshal(data, &preset); err != nil {
		return nil, fmt.Errorf("failed to unmarshal preset: %w", err)
	}

	return &preset, nil
}

// DeletePreset deletes a preset from disk.
func (pm *PresetManager) DeletePreset(name string) error {
	filename := filepath.Join(pm.presetsDir, name+".json")
	if err := os.Remove(filename); err != nil {
		return fmt.Errorf("failed to delete preset file: %w", err)
	}
	return nil
}

// ListPresets lists all available presets.
func (pm *PresetManager) ListPresets() ([]types.ConfigPreset, error) {
	entries, err := os.ReadDir(pm.presetsDir)
	if err != nil {
		return nil, fmt.Errorf("failed to read presets directory: %w", err)
	}

	var presets []types.ConfigPreset
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
			continue
		}

		name := entry.Name()[:len(entry.Name())-5] // Remove ".json"
		preset, err := pm.LoadPreset(name)
		if err != nil {
			continue // Skip invalid presets
		}
		presets = append(presets, *preset)
	}

	return presets, nil
}
