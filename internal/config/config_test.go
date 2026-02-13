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
func TestConfigValidate_NormalizesNegativeJobs(t *testing.T) {
	// 음수 jobs 값은 안전한 최소값(1)으로 정규화되어야 한다.
	cfg := &Config{
		Source: "/tmp/source",
		Dest:   "/tmp/dest",
		Jobs:   -2,
	}

	if err := cfg.Validate(); err != nil {
		t.Fatalf("validate failed: %v", err)
	}
	if cfg.Jobs != 1 {
		t.Fatalf("expected jobs=1, got %d", cfg.Jobs)
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
