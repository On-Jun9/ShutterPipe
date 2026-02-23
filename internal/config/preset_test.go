package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/On-Jun9/ShutterPipe/pkg/types"
)

// TestConfigToPresetAndBack는 테스트 코드 동작을 검증하거나 보조합니다.
func TestConfigToPresetAndBack(t *testing.T) {
	// Config <-> Preset 변환 시 핵심 필드가 보존되어야 한다.
	cfg := &Config{
		Source:            "/src",
		Dest:              "/dest",
		IncludeExtensions: []string{"jpg", "mp4"},
		Jobs:              3,
		DedupMethod:       types.DedupMethodHash,
		ConflictPolicy:    types.ConflictPolicyRename,
		OrganizeStrategy:  types.OrganizeByEvent,
		EventName:         "trip",
		UnclassifiedDir:   "unc",
		QuarantineDir:     "quar",
		DryRun:            true,
		HashVerify:        true,
		IgnoreState:       true,
		DateFilterStart:   "2025-01-01",
		DateFilterEnd:     "2025-01-31",
	}

	preset := ConfigToPreset(cfg, "my-preset", "desc")
	roundTrip := PresetToConfig(preset)

	if roundTrip.Source != cfg.Source || roundTrip.Dest != cfg.Dest {
		t.Fatalf("source/dest mismatch after round trip: %+v", roundTrip)
	}
	if roundTrip.EventName != cfg.EventName || roundTrip.Jobs != cfg.Jobs {
		t.Fatalf("event/jobs mismatch after round trip: %+v", roundTrip)
	}
	if roundTrip.DedupMethod != cfg.DedupMethod || roundTrip.ConflictPolicy != cfg.ConflictPolicy {
		t.Fatalf("policy mismatch after round trip: %+v", roundTrip)
	}
}

// TestPresetManager_SaveLoadListDelete는 테스트 코드 동작을 검증하거나 보조합니다.
func TestPresetManager_SaveLoadListDelete(t *testing.T) {
	// 저장/로드/목록/삭제 기본 플로우가 정상 동작해야 한다.
	dir := t.TempDir()
	pm := &PresetManager{presetsDir: dir}

	preset := &types.ConfigPreset{
		Name:              "test",
		Source:            "/src",
		Dest:              "/dest",
		IncludeExtensions: []string{"jpg"},
		UnclassifiedDir:   "unc",
		QuarantineDir:     "quar",
	}

	if err := pm.SavePreset(preset); err != nil {
		t.Fatalf("save preset failed: %v", err)
	}

	loaded, err := pm.LoadPreset("test")
	if err != nil {
		t.Fatalf("load preset failed: %v", err)
	}
	if loaded.Name != "test" || loaded.Source != "/src" {
		t.Fatalf("unexpected loaded preset: %+v", loaded)
	}

	presets, err := pm.ListPresets()
	if err != nil {
		t.Fatalf("list presets failed: %v", err)
	}
	if len(presets) != 1 {
		t.Fatalf("expected 1 preset, got %d", len(presets))
	}

	if err := pm.DeletePreset("test"); err != nil {
		t.Fatalf("delete preset failed: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "test.json")); !os.IsNotExist(err) {
		t.Fatalf("expected preset file to be deleted, stat error=%v", err)
	}
}

// TestPresetManager_SavePreset_EmptyNameFails는 테스트 코드 동작을 검증하거나 보조합니다.
func TestPresetManager_SavePreset_EmptyNameFails(t *testing.T) {
	// 이름 없는 preset 저장은 거부되어야 한다.
	pm := &PresetManager{presetsDir: t.TempDir()}
	err := pm.SavePreset(&types.ConfigPreset{Name: ""})
	if err == nil {
		t.Fatal("expected error for empty preset name")
	}
}

// TestPresetManager_ListPresets_SkipsInvalidJSON는 테스트 코드 동작을 검증하거나 보조합니다.
func TestPresetManager_ListPresets_SkipsInvalidJSON(t *testing.T) {
	// 깨진 JSON 파일은 목록에서 건너뛰고 정상 preset만 반환해야 한다.
	dir := t.TempDir()
	pm := &PresetManager{presetsDir: dir}

	if err := os.WriteFile(filepath.Join(dir, "broken.json"), []byte("{"), 0644); err != nil {
		t.Fatalf("failed to write broken preset: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "note.txt"), []byte("ignore"), 0644); err != nil {
		t.Fatalf("failed to write txt file: %v", err)
	}

	good := &types.ConfigPreset{Name: "ok", UnclassifiedDir: "unc", QuarantineDir: "quar"}
	if err := pm.SavePreset(good); err != nil {
		t.Fatalf("save good preset failed: %v", err)
	}

	presets, err := pm.ListPresets()
	if err != nil {
		t.Fatalf("list presets failed: %v", err)
	}
	if len(presets) != 1 || presets[0].Name != "ok" {
		t.Fatalf("unexpected presets result: %+v", presets)
	}
}

// TestNewPresetManager_CreatesDefaultDirectory는 테스트 코드 동작을 검증하거나 보조합니다.
func TestNewPresetManager_CreatesDefaultDirectory(t *testing.T) {
	// NewPresetManager는 HOME 기준 ~/.shutterpipe/presets 디렉터리를 생성해야 한다.
	home := t.TempDir()
	t.Setenv("HOME", home)

	pm, err := NewPresetManager()
	if err != nil {
		t.Fatalf("new preset manager failed: %v", err)
	}
	if pm == nil {
		t.Fatal("expected non-nil preset manager")
	}
	if _, err := os.Stat(filepath.Join(home, ".shutterpipe", "presets")); err != nil {
		t.Fatalf("expected presets dir to exist: %v", err)
	}
}

// TestNewPresetManager_ReturnsErrorWhenHomeIsFile는 테스트 코드 동작을 검증하거나 보조합니다.
func TestNewPresetManager_ReturnsErrorWhenHomeIsFile(t *testing.T) {
	// HOME이 파일이면 presets 디렉터리 생성에 실패해야 한다.
	homeFile := filepath.Join(t.TempDir(), "home-file")
	if err := os.WriteFile(homeFile, []byte("x"), 0644); err != nil {
		t.Fatalf("failed to create fake home file: %v", err)
	}
	t.Setenv("HOME", homeFile)

	_, err := NewPresetManager()
	if err == nil {
		t.Fatal("expected NewPresetManager error")
	}
}

// TestPresetManager_SavePreset_ReturnsWriteError는 테스트 코드 동작을 검증하거나 보조합니다.
func TestPresetManager_SavePreset_ReturnsWriteError(t *testing.T) {
	// presetsDir가 파일이면 preset 저장 시 write 에러가 발생해야 한다.
	blocker := filepath.Join(t.TempDir(), "not-dir")
	if err := os.WriteFile(blocker, []byte("x"), 0644); err != nil {
		t.Fatalf("failed to create blocker file: %v", err)
	}

	pm := &PresetManager{presetsDir: blocker}
	err := pm.SavePreset(&types.ConfigPreset{Name: "demo"})
	if err == nil {
		t.Fatal("expected save preset write error")
	}
}

// TestPresetManager_LoadPreset_ReturnsReadError는 테스트 코드 동작을 검증하거나 보조합니다.
func TestPresetManager_LoadPreset_ReturnsReadError(t *testing.T) {
	// 없는 preset 로드는 read 에러를 반환해야 한다.
	pm := &PresetManager{presetsDir: t.TempDir()}
	_, err := pm.LoadPreset("missing")
	if err == nil {
		t.Fatal("expected load preset read error")
	}
}

// TestPresetManager_DeletePreset_ReturnsErrorWhenMissing는 테스트 코드 동작을 검증하거나 보조합니다.
func TestPresetManager_DeletePreset_ReturnsErrorWhenMissing(t *testing.T) {
	// 없는 preset 삭제는 remove 에러를 반환해야 한다.
	pm := &PresetManager{presetsDir: t.TempDir()}
	err := pm.DeletePreset("missing")
	if err == nil {
		t.Fatal("expected delete preset error")
	}
}

// TestPresetManager_ListPresets_ReturnsReadDirError는 테스트 코드 동작을 검증하거나 보조합니다.
func TestPresetManager_ListPresets_ReturnsReadDirError(t *testing.T) {
	// presetsDir가 없으면 ReadDir 에러를 반환해야 한다.
	pm := &PresetManager{presetsDir: filepath.Join(t.TempDir(), "not-exists")}
	_, err := pm.ListPresets()
	if err == nil {
		t.Fatal("expected list presets read dir error")
	}
}
