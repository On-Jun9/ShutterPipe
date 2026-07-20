package config

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// TestConfigValidate_RequiresSource는 테스트 코드 동작을 검증하거나 보조합니다.
func TestConfigValidate_RequiresSource(t *testing.T) {
	// source 누락은 ValidationError(field=source)로 반환되어야 한다.
	cfg := &Config{
		Dest: "/tmp/dest",
	}

	err := cfg.Validate()
	if err == nil {
		t.Fatal("expected validation error")
	}

	var validationErr *ValidationError
	if !errors.As(err, &validationErr) {
		t.Fatalf("expected ValidationError, got %T", err)
	}
	if validationErr.Field != "source" {
		t.Fatalf("expected field source, got %s", validationErr.Field)
	}
}

// TestConfigValidate_RequiresDest는 테스트 코드 동작을 검증하거나 보조합니다.
func TestConfigValidate_RequiresDest(t *testing.T) {
	// dest 누락은 ValidationError(field=dest)로 반환되어야 한다.
	cfg := &Config{
		Source: "/tmp/source",
	}

	err := cfg.Validate()
	if err == nil {
		t.Fatal("expected validation error")
	}

	var validationErr *ValidationError
	if !errors.As(err, &validationErr) {
		t.Fatalf("expected ValidationError, got %T", err)
	}
	if validationErr.Field != "dest" {
		t.Fatalf("expected field dest, got %s", validationErr.Field)
	}
}

// TestConfigValidate_FillsDefaults는 테스트 코드 동작을 검증하거나 보조합니다.
func TestConfigValidate_FillsDefaults(t *testing.T) {
	// 기본값 자동 보정(jobs/log/state/unclassified/quarantine)이 적용되어야 한다.
	cfg := &Config{
		Source: "/tmp/source",
		Dest:   "/tmp/dest",
		Jobs:   0,
	}

	if err := cfg.Validate(); err != nil {
		t.Fatalf("validate failed: %v", err)
	}

	expectedJobs := runtime.NumCPU()
	if expectedJobs < 1 {
		expectedJobs = 4
	}
	if cfg.Jobs != expectedJobs {
		t.Fatalf("expected jobs=%d, got %d", expectedJobs, cfg.Jobs)
	}

	homeDir, err := os.UserHomeDir()
	if err != nil {
		t.Fatalf("failed to get home dir: %v", err)
	}
	stateDir := filepath.Join(homeDir, ".shutterpipe")

	if cfg.LogFile != filepath.Join(stateDir, "shutterpipe.log") {
		t.Fatalf("unexpected log file: %s", cfg.LogFile)
	}
	if cfg.StateFile != filepath.Join(stateDir, "state.json") {
		t.Fatalf("unexpected state file: %s", cfg.StateFile)
	}
	if cfg.UnclassifiedDir != "unclassified" {
		t.Fatalf("unexpected unclassified dir: %s", cfg.UnclassifiedDir)
	}
	if cfg.QuarantineDir != "quarantine" {
		t.Fatalf("unexpected quarantine dir: %s", cfg.QuarantineDir)
	}
}

// TestConfigValidate_NormalizesNegativeJobs는 테스트 코드 동작을 검증하거나 보조합니다.
func TestConfigValidate_RejectsJobsOutsideSupportedRange(t *testing.T) {
	// UI와 서버는 동일하게 0(자동) 또는 1..32만 허용해야 한다.
	for _, jobs := range []int{-2, 33, 1 << 20} {
		cfg := &Config{
			Source: "/tmp/source",
			Dest:   "/tmp/dest",
			Jobs:   jobs,
		}

		err := cfg.Validate()
		var validationErr *ValidationError
		if !errors.As(err, &validationErr) || validationErr.Field != "jobs" {
			t.Fatalf("expected jobs ValidationError for %d, got %T %v", jobs, err, err)
		}
	}
}

func TestConfigValidate_RejectsUnknownEnums(t *testing.T) {
	for _, tc := range []struct {
		field string
		set   func(*Config)
	}{
		{field: "conflict_policy", set: func(cfg *Config) { cfg.ConflictPolicy = "overwite" }},
		{field: "dedup_method", set: func(cfg *Config) { cfg.DedupMethod = "sha" }},
		{field: "organize_strategy", set: func(cfg *Config) { cfg.OrganizeStrategy = "calendar" }},
	} {
		t.Run(tc.field, func(t *testing.T) {
			cfg := &Config{Source: "/tmp/source", Dest: "/tmp/dest"}
			tc.set(cfg)
			err := cfg.Validate()
			var validationErr *ValidationError
			if !errors.As(err, &validationErr) || validationErr.Field != tc.field {
				t.Fatalf("expected %s ValidationError, got %T %v", tc.field, err, err)
			}
		})
	}
}

func TestConfigValidate_AppliesMissingEnumDefaults(t *testing.T) {
	cfg := &Config{Source: "/tmp/source", Dest: "/tmp/dest"}
	if err := cfg.Validate(); err != nil {
		t.Fatal(err)
	}
	if cfg.ConflictPolicy != "skip" || cfg.DedupMethod != "name-size" || cfg.OrganizeStrategy != "date" {
		t.Fatalf("missing enum defaults not applied: %+v", cfg)
	}
}

func TestConfigValidate_RejectsInvalidDateRange(t *testing.T) {
	for _, tc := range []struct {
		name  string
		start string
		end   string
		field string
	}{
		{name: "invalid start", start: "2026-02-30", field: "date_filter_start"},
		{name: "invalid end", end: "02/10/2026", field: "date_filter_end"},
		{name: "reversed", start: "2026-02-10", end: "2026-02-01", field: "date_filter_end"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg := &Config{Source: "/tmp/source", Dest: "/tmp/dest", DateFilterStart: tc.start, DateFilterEnd: tc.end}
			err := cfg.Validate()
			var validationErr *ValidationError
			if !errors.As(err, &validationErr) || validationErr.Field != tc.field {
				t.Fatalf("expected %s ValidationError, got %T %v", tc.field, err, err)
			}
		})
	}
}

func TestConfigValidate_RejectsDestinationSubdirEscape(t *testing.T) {
	for _, tc := range []struct {
		field string
		value string
	}{
		{field: "unclassified_dir", value: "../../outside"},
		{field: "quarantine_dir", value: filepath.Join("..", "outside")},
		{field: "unclassified_dir", value: filepath.Join(string(filepath.Separator), "absolute")},
	} {
		t.Run(tc.field+"_"+filepath.Base(tc.value), func(t *testing.T) {
			cfg := &Config{Source: "/tmp/source", Dest: "/tmp/dest"}
			if tc.field == "unclassified_dir" {
				cfg.UnclassifiedDir = tc.value
			} else {
				cfg.QuarantineDir = tc.value
			}
			err := cfg.Validate()
			var validationErr *ValidationError
			if !errors.As(err, &validationErr) || validationErr.Field != tc.field {
				t.Fatalf("expected %s ValidationError, got %T %v", tc.field, err, err)
			}
		})
	}
}

func TestConfigValidate_RejectsEventNamePathEscape(t *testing.T) {
	for _, eventName := range []string{"../../../../outside", `..\..\outside`, "bad\x00name"} {
		cfg := &Config{Source: "/tmp/source", Dest: "/tmp/dest", EventName: eventName}
		err := cfg.Validate()
		var validationErr *ValidationError
		if !errors.As(err, &validationErr) || validationErr.Field != "event_name" {
			t.Fatalf("expected event_name ValidationError for %q, got %T %v", eventName, err, err)
		}
	}
}

func TestConfigValidate_RejectsDestinationInsideSource(t *testing.T) {
	tmpDir := t.TempDir()
	source := filepath.Join(tmpDir, "photos")
	if err := os.MkdirAll(source, 0755); err != nil {
		t.Fatal(err)
	}
	alias := filepath.Join(tmpDir, "photos-alias")
	if err := os.Symlink(source, alias); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}

	for _, tc := range []struct {
		name string
		dest string
	}{
		{name: "same path", dest: source},
		{name: "direct child", dest: filepath.Join(source, "backup")},
		{name: "symlink resolved child", dest: filepath.Join(alias, "backup")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg := &Config{Source: source, Dest: tc.dest}
			err := cfg.Validate()
			var validationErr *ValidationError
			if !errors.As(err, &validationErr) || validationErr.Field != "dest" {
				t.Fatalf("expected dest ValidationError, got %T %v", err, err)
			}
		})
	}
}

func TestConfigValidate_AllowsDestinationOutsideSource(t *testing.T) {
	tmpDir := t.TempDir()
	source := filepath.Join(tmpDir, "photos")
	dest := filepath.Join(tmpDir, "backup")
	if err := os.MkdirAll(source, 0755); err != nil {
		t.Fatal(err)
	}
	if err := (&Config{Source: source, Dest: dest}).Validate(); err != nil {
		t.Fatalf("valid sibling destination rejected: %v", err)
	}
}

func TestConfigValidate_RejectsSourceInsideDestination(t *testing.T) {
	tmpDir := t.TempDir()
	dest := filepath.Join(tmpDir, "backup")
	source := filepath.Join(dest, "2026", "07", "13")
	if err := os.MkdirAll(source, 0755); err != nil {
		t.Fatal(err)
	}
	alias := filepath.Join(tmpDir, "backup-alias")
	if err := os.Symlink(dest, alias); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}

	for _, tc := range []struct {
		name   string
		source string
		dest   string
	}{
		{name: "direct child source", source: source, dest: dest},
		{name: "symlink resolved child source", source: filepath.Join(alias, "2026", "07", "13"), dest: dest},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := (&Config{Source: tc.source, Dest: tc.dest}).Validate()
			var validationErr *ValidationError
			if !errors.As(err, &validationErr) || validationErr.Field != "source" {
				t.Fatalf("expected source ValidationError, got %T %v", err, err)
			}
		})
	}
}

// TestLoadFromFile_ReadsYAMLIntoConfig는 테스트 코드 동작을 검증하거나 보조합니다.
func TestLoadFromFile_ReadsYAMLIntoConfig(t *testing.T) {
	// YAML 파일 로드 시 명시 필드가 Config에 반영되어야 한다.
	yamlContent := strings.Join([]string{
		"source: /data/source",
		"dest: /data/dest",
		"jobs: 8",
		"event_name: wedding",
	}, "\n")

	filePath := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(filePath, []byte(yamlContent), 0644); err != nil {
		t.Fatalf("failed to write config file: %v", err)
	}

	cfg, err := LoadFromFile(filePath)
	if err != nil {
		t.Fatalf("load from file failed: %v", err)
	}
	if cfg.Source != "/data/source" || cfg.Dest != "/data/dest" {
		t.Fatalf("unexpected source/dest: %+v", cfg)
	}
	if cfg.Jobs != 8 || cfg.EventName != "wedding" {
		t.Fatalf("unexpected jobs/event_name: %+v", cfg)
	}
}

// TestLoadFromFile_ReturnsReadError는 테스트 코드 동작을 검증하거나 보조합니다.
func TestLoadFromFile_ReturnsReadError(t *testing.T) {
	// 존재하지 않는 설정 파일은 read 에러를 반환해야 한다.
	_, err := LoadFromFile(filepath.Join(t.TempDir(), "missing.yaml"))
	if err == nil {
		t.Fatal("expected read error for missing config file")
	}
}

// TestLoadFromFile_ReturnsYAMLParseError는 테스트 코드 동작을 검증하거나 보조합니다.
func TestLoadFromFile_ReturnsYAMLParseError(t *testing.T) {
	// 잘못된 YAML 문법은 unmarshal 에러를 반환해야 한다.
	filePath := filepath.Join(t.TempDir(), "broken.yaml")
	if err := os.WriteFile(filePath, []byte("source: ["), 0644); err != nil {
		t.Fatalf("failed to write broken yaml: %v", err)
	}

	_, err := LoadFromFile(filePath)
	if err == nil {
		t.Fatal("expected yaml parse error")
	}
}

// TestValidationError_ErrorFormat는 테스트 코드 동작을 검증하거나 보조합니다.
func TestValidationError_ErrorFormat(t *testing.T) {
	// ValidationError.Error()는 "field: message" 형식을 반환해야 한다.
	err := (&ValidationError{Field: "source", Message: "is required"}).Error()
	if err != "source: is required" {
		t.Fatalf("unexpected validation error format: %s", err)
	}
}
