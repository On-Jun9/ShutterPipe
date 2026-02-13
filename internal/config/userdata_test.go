package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/On-Jun9/ShutterPipe/pkg/types"
)

// TestValidatePath는 테스트 코드 동작을 검증하거나 보조합니다.
func TestValidatePath(t *testing.T) {
	// XSS 관련 패턴은 차단하고, 일반 경로는 허용해야 한다.
	tests := []struct {
		name    string
		path    string
		wantErr bool
	}{
		{
			name:    "empty path is allowed",
			path:    "",
			wantErr: false,
		},
		{
			name:    "angle brackets only are allowed",
			path:    "/tmp/a<b>.jpg",
			wantErr: false,
		},
		{
			name:    "html tag pattern is rejected",
			path:    "/tmp/<script>alert(1)</script>",
			wantErr: true,
		},
		{
			name:    "javascript url is rejected",
			path:    "javascript:alert(1)",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validatePath(tt.path)
			if (err != nil) != tt.wantErr {
				t.Fatalf("validatePath(%q) error = %v, wantErr=%v", tt.path, err, tt.wantErr)
			}
		})
	}
}

// TestUserDataManager_SaveSettings_ReturnsValidationError는 테스트 코드 동작을 검증하거나 보조합니다.
func TestUserDataManager_SaveSettings_ReturnsValidationError(t *testing.T) {
	// 설정 저장 시 경로 검증 실패는 ValidationError로 노출되어야 한다.
	m := &UserDataManager{dataDir: t.TempDir()}
	settings := &types.UserSettings{
		Source: "/tmp/<script>alert(1)</script>",
		Dest:   "/tmp/dest",
	}

	err := m.SaveSettings(settings)
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

// TestUserDataManager_SaveBookmarks_ReturnsValidationError는 테스트 코드 동작을 검증하거나 보조합니다.
func TestUserDataManager_SaveBookmarks_ReturnsValidationError(t *testing.T) {
	// 북마크 저장도 동일한 타입 기반 ValidationError를 반환해야 한다.
	m := &UserDataManager{dataDir: t.TempDir()}
	bookmarks := &types.Bookmarks{
		Source: []string{"javascript:alert(1)"},
	}

	err := m.SaveBookmarks(bookmarks)
	if err == nil {
		t.Fatal("expected validation error")
	}

	var validationErr *ValidationError
	if !errors.As(err, &validationErr) {
		t.Fatalf("expected ValidationError, got %T", err)
	}
	if validationErr.Field != "bookmarks" {
		t.Fatalf("expected field bookmarks, got %s", validationErr.Field)
	}
}

// TestUserDataManager_SavePathHistory_ReturnsValidationError는 테스트 코드 동작을 검증하거나 보조합니다.
func TestUserDataManager_SavePathHistory_ReturnsValidationError(t *testing.T) {
	// 경로 히스토리 저장도 ValidationError(field=path_history)를 반환해야 한다.
	m := &UserDataManager{dataDir: t.TempDir()}
	history := &types.PathHistory{
		Dest: []string{"<iframe src=x>"},
	}

	err := m.SavePathHistory(history)
	if err == nil {
		t.Fatal("expected validation error")
	}

	var validationErr *ValidationError
	if !errors.As(err, &validationErr) {
		t.Fatalf("expected ValidationError, got %T", err)
	}
	if validationErr.Field != "path_history" {
		t.Fatalf("expected field path_history, got %s", validationErr.Field)
	}
}

// TestUserDataManager_LoadSettings_ReturnsDefaultWhenMissing는 테스트 코드 동작을 검증하거나 보조합니다.
func TestUserDataManager_LoadSettings_ReturnsDefaultWhenMissing(t *testing.T) {
	// 설정 파일이 없으면 기본 설정이 채워진 객체를 반환해야 한다.
	dataDir := t.TempDir()
	m := &UserDataManager{dataDir: dataDir}

	settings, err := m.LoadSettings()
	if err != nil {
		t.Fatalf("load settings failed: %v", err)
	}

	if settings.StateFile != filepath.Join(dataDir, "state.json") {
		t.Fatalf("unexpected state file: %s", settings.StateFile)
	}
	if settings.LogFile != filepath.Join(dataDir, "shutterpipe.log") {
		t.Fatalf("unexpected log file: %s", settings.LogFile)
	}
	if len(settings.IncludeExtensions) == 0 {
		t.Fatal("expected default include extensions")
	}
}

// TestUserDataManager_AddHistoryEntry_TrimsTo100는 테스트 코드 동작을 검증하거나 보조합니다.
func TestUserDataManager_AddHistoryEntry_TrimsTo100(t *testing.T) {
	// 백업 히스토리는 최신 100개까지만 유지해야 한다.
	m := &UserDataManager{dataDir: t.TempDir()}

	for i := 0; i < 105; i++ {
		entry := types.BackupHistoryEntry{ID: fmt.Sprintf("%d", i)}
		if err := m.AddHistoryEntry(entry); err != nil {
			t.Fatalf("add history entry failed at %d: %v", i, err)
		}
	}

	history, err := m.LoadBackupHistory()
	if err != nil {
		t.Fatalf("load backup history failed: %v", err)
	}

	if len(history.Entries) != 100 {
		t.Fatalf("expected 100 entries, got %d", len(history.Entries))
	}
	if history.Entries[0].ID != "104" {
		t.Fatalf("expected newest id 104, got %s", history.Entries[0].ID)
	}
	if history.Entries[len(history.Entries)-1].ID != "5" {
		t.Fatalf("expected oldest id 5, got %s", history.Entries[len(history.Entries)-1].ID)
	}
}

// TestNewUserDataManager_CreatesDefaultDirectory는 테스트 코드 동작을 검증하거나 보조합니다.
func TestNewUserDataManager_CreatesDefaultDirectory(t *testing.T) {
	// NewUserDataManager는 HOME 기준 ~/.shutterpipe 디렉터리를 생성해야 한다.
	home := t.TempDir()
	t.Setenv("HOME", home)

	m, err := NewUserDataManager()
	if err != nil {
		t.Fatalf("new user data manager failed: %v", err)
	}
	if m == nil {
		t.Fatal("expected non-nil manager")
	}
	if _, err := os.Stat(filepath.Join(home, ".shutterpipe")); err != nil {
		t.Fatalf("expected user data dir to exist: %v", err)
	}
}

// TestNewUserDataManager_ReturnsErrorWhenHomeIsFile는 테스트 코드 동작을 검증하거나 보조합니다.
func TestNewUserDataManager_ReturnsErrorWhenHomeIsFile(t *testing.T) {
	// HOME이 파일이면 user data 디렉터리 생성이 실패해야 한다.
	homeFile := filepath.Join(t.TempDir(), "home-file")
	if err := os.WriteFile(homeFile, []byte("x"), 0644); err != nil {
		t.Fatalf("failed to create fake home file: %v", err)
	}
	t.Setenv("HOME", homeFile)

	_, err := NewUserDataManager()
	if err == nil {
		t.Fatal("expected NewUserDataManager error")
	}
}

// TestUserDataManager_SaveAndLoadSettings_RoundTrip는 테스트 코드 동작을 검증하거나 보조합니다.
func TestUserDataManager_SaveAndLoadSettings_RoundTrip(t *testing.T) {
	// settings 저장 후 로드 시 핵심 필드가 유지되어야 한다.
	m := &UserDataManager{dataDir: t.TempDir()}
	settings := &types.UserSettings{
		Source:            "/source",
		Dest:              "/dest",
		OrganizeStrategy:  types.OrganizeByEvent,
		EventName:         "trip",
		ConflictPolicy:    types.ConflictPolicyRename,
		DedupMethod:       types.DedupMethodHash,
		Jobs:              4,
		IncludeExtensions: []string{"jpg", "mp4"},
		UnclassifiedDir:   "unc",
		QuarantineDir:     "quar",
	}

	if err := m.SaveSettings(settings); err != nil {
		t.Fatalf("save settings failed: %v", err)
	}

	loaded, err := m.LoadSettings()
	if err != nil {
		t.Fatalf("load settings failed: %v", err)
	}
	if loaded.Source != settings.Source || loaded.Dest != settings.Dest {
		t.Fatalf("unexpected loaded settings: %+v", loaded)
	}
	if loaded.EventName != "trip" || loaded.ConflictPolicy != types.ConflictPolicyRename {
		t.Fatalf("unexpected loaded settings fields: %+v", loaded)
	}
	if loaded.UpdatedAt.IsZero() {
		t.Fatal("expected updated_at to be set")
	}
}

// TestUserDataManager_SaveSettings_ReturnsWriteError는 테스트 코드 동작을 검증하거나 보조합니다.
func TestUserDataManager_SaveSettings_ReturnsWriteError(t *testing.T) {
	// dataDir가 파일이면 settings 저장 시 write 에러를 반환해야 한다.
	blocker := filepath.Join(t.TempDir(), "not-dir")
	if err := os.WriteFile(blocker, []byte("x"), 0644); err != nil {
		t.Fatalf("failed to create blocker file: %v", err)
	}

	m := &UserDataManager{dataDir: blocker}
	err := m.SaveSettings(&types.UserSettings{Source: "/src", Dest: "/dest"})
	if err == nil {
		t.Fatal("expected settings write error")
	}
}

// TestUserDataManager_SaveSettings_ReturnsRenameError는 테스트 코드 동작을 검증하거나 보조합니다.
func TestUserDataManager_SaveSettings_ReturnsRenameError(t *testing.T) {
	// 대상 파일명이 디렉터리면 atomic rename 단계에서 실패해야 한다.
	dataDir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dataDir, "settings.json"), 0755); err != nil {
		t.Fatalf("failed to create settings target dir: %v", err)
	}

	m := &UserDataManager{dataDir: dataDir}
	err := m.SaveSettings(&types.UserSettings{Source: "/src", Dest: "/dest"})
	if err == nil {
		t.Fatal("expected settings rename error")
	}
}

// TestUserDataManager_LoadSettings_ReturnsReadAndUnmarshalErrors는 테스트 코드 동작을 검증하거나 보조합니다.
func TestUserDataManager_LoadSettings_ReturnsReadAndUnmarshalErrors(t *testing.T) {
	// settings 로드는 read 에러와 unmarshal 에러를 각각 반환해야 한다.
	t.Run("read_error", func(t *testing.T) {
		dataDir := t.TempDir()
		if err := os.MkdirAll(filepath.Join(dataDir, "settings.json"), 0755); err != nil {
			t.Fatalf("failed to create settings dir path: %v", err)
		}

		m := &UserDataManager{dataDir: dataDir}
		_, err := m.LoadSettings()
		if err == nil {
			t.Fatal("expected settings read error")
		}
	})

	t.Run("unmarshal_error", func(t *testing.T) {
		dataDir := t.TempDir()
		if err := os.WriteFile(filepath.Join(dataDir, "settings.json"), []byte("{"), 0644); err != nil {
			t.Fatalf("failed to write broken settings: %v", err)
		}

		m := &UserDataManager{dataDir: dataDir}
		_, err := m.LoadSettings()
		if err == nil {
			t.Fatal("expected settings unmarshal error")
		}
	})
}

// TestUserDataManager_SaveAndLoadBookmarks_RoundTrip는 테스트 코드 동작을 검증하거나 보조합니다.
func TestUserDataManager_SaveAndLoadBookmarks_RoundTrip(t *testing.T) {
	// bookmarks 저장 후 로드 시 source/dest 목록이 유지되어야 한다.
	m := &UserDataManager{dataDir: t.TempDir()}
	bookmarks := &types.Bookmarks{
		Source: []string{"/src/a", "/src/b"},
		Dest:   []string{"/dest/a"},
	}

	if err := m.SaveBookmarks(bookmarks); err != nil {
		t.Fatalf("save bookmarks failed: %v", err)
	}

	loaded, err := m.LoadBookmarks()
	if err != nil {
		t.Fatalf("load bookmarks failed: %v", err)
	}
	if len(loaded.Source) != 2 || len(loaded.Dest) != 1 {
		t.Fatalf("unexpected loaded bookmarks: %+v", loaded)
	}
}

// TestUserDataManager_SaveBookmarks_ReturnsWriteError는 테스트 코드 동작을 검증하거나 보조합니다.
func TestUserDataManager_SaveBookmarks_ReturnsWriteError(t *testing.T) {
	// dataDir가 파일이면 bookmarks 저장 시 write 에러를 반환해야 한다.
	blocker := filepath.Join(t.TempDir(), "not-dir")
	if err := os.WriteFile(blocker, []byte("x"), 0644); err != nil {
		t.Fatalf("failed to create blocker file: %v", err)
	}

	m := &UserDataManager{dataDir: blocker}
	err := m.SaveBookmarks(&types.Bookmarks{Source: []string{"/src"}})
	if err == nil {
		t.Fatal("expected bookmarks write error")
	}
}

// TestUserDataManager_LoadBookmarks_ReturnsReadAndUnmarshalErrors는 테스트 코드 동작을 검증하거나 보조합니다.
func TestUserDataManager_LoadBookmarks_ReturnsReadAndUnmarshalErrors(t *testing.T) {
	// bookmarks 로드는 read 에러와 unmarshal 에러를 각각 반환해야 한다.
	t.Run("read_error", func(t *testing.T) {
		dataDir := t.TempDir()
		if err := os.MkdirAll(filepath.Join(dataDir, "bookmarks.json"), 0755); err != nil {
			t.Fatalf("failed to create bookmarks dir path: %v", err)
		}

		m := &UserDataManager{dataDir: dataDir}
		_, err := m.LoadBookmarks()
		if err == nil {
			t.Fatal("expected bookmarks read error")
		}
	})

	t.Run("unmarshal_error", func(t *testing.T) {
		dataDir := t.TempDir()
		if err := os.WriteFile(filepath.Join(dataDir, "bookmarks.json"), []byte("{"), 0644); err != nil {
			t.Fatalf("failed to write broken bookmarks: %v", err)
		}

		m := &UserDataManager{dataDir: dataDir}
		_, err := m.LoadBookmarks()
		if err == nil {
			t.Fatal("expected bookmarks unmarshal error")
		}
	})
}

// TestUserDataManager_LoadBookmarks_ReturnsDefaultWhenMissing는 테스트 코드 동작을 검증하거나 보조합니다.
func TestUserDataManager_LoadBookmarks_ReturnsDefaultWhenMissing(t *testing.T) {
	// bookmarks 파일이 없으면 빈 배열 기본값을 반환해야 한다.
	m := &UserDataManager{dataDir: t.TempDir()}
	loaded, err := m.LoadBookmarks()
	if err != nil {
		t.Fatalf("load bookmarks failed: %v", err)
	}
	if len(loaded.Source) != 0 || len(loaded.Dest) != 0 {
		t.Fatalf("expected empty bookmarks, got %+v", loaded)
	}
}

// TestUserDataManager_SaveAndLoadPathHistory_RoundTrip는 테스트 코드 동작을 검증하거나 보조합니다.
func TestUserDataManager_SaveAndLoadPathHistory_RoundTrip(t *testing.T) {
	// path-history 저장 후 로드 시 source/dest 목록이 유지되어야 한다.
	m := &UserDataManager{dataDir: t.TempDir()}
	history := &types.PathHistory{
		Source: []string{"/src/recent"},
		Dest:   []string{"/dest/recent"},
	}

	if err := m.SavePathHistory(history); err != nil {
		t.Fatalf("save path history failed: %v", err)
	}

	loaded, err := m.LoadPathHistory()
	if err != nil {
		t.Fatalf("load path history failed: %v", err)
	}
	if len(loaded.Source) != 1 || len(loaded.Dest) != 1 {
		t.Fatalf("unexpected loaded path history: %+v", loaded)
	}
}

// TestUserDataManager_SavePathHistory_ReturnsWriteError는 테스트 코드 동작을 검증하거나 보조합니다.
func TestUserDataManager_SavePathHistory_ReturnsWriteError(t *testing.T) {
	// dataDir가 파일이면 path history 저장 시 write 에러를 반환해야 한다.
	blocker := filepath.Join(t.TempDir(), "not-dir")
	if err := os.WriteFile(blocker, []byte("x"), 0644); err != nil {
		t.Fatalf("failed to create blocker file: %v", err)
	}

	m := &UserDataManager{dataDir: blocker}
	err := m.SavePathHistory(&types.PathHistory{Source: []string{"/src"}})
	if err == nil {
		t.Fatal("expected path history write error")
	}
}

// TestUserDataManager_LoadPathHistory_ReturnsReadAndUnmarshalErrors는 테스트 코드 동작을 검증하거나 보조합니다.
func TestUserDataManager_LoadPathHistory_ReturnsReadAndUnmarshalErrors(t *testing.T) {
	// path history 로드는 read 에러와 unmarshal 에러를 각각 반환해야 한다.
	t.Run("read_error", func(t *testing.T) {
		dataDir := t.TempDir()
		if err := os.MkdirAll(filepath.Join(dataDir, "path-history.json"), 0755); err != nil {
			t.Fatalf("failed to create path history dir path: %v", err)
		}

		m := &UserDataManager{dataDir: dataDir}
		_, err := m.LoadPathHistory()
		if err == nil {
			t.Fatal("expected path history read error")
		}
	})

	t.Run("unmarshal_error", func(t *testing.T) {
		dataDir := t.TempDir()
		if err := os.WriteFile(filepath.Join(dataDir, "path-history.json"), []byte("{"), 0644); err != nil {
			t.Fatalf("failed to write broken path history: %v", err)
		}

		m := &UserDataManager{dataDir: dataDir}
		_, err := m.LoadPathHistory()
		if err == nil {
			t.Fatal("expected path history unmarshal error")
		}
	})
}

// TestUserDataManager_LoadPathHistory_ReturnsDefaultWhenMissing는 테스트 코드 동작을 검증하거나 보조합니다.
func TestUserDataManager_LoadPathHistory_ReturnsDefaultWhenMissing(t *testing.T) {
	// path-history 파일이 없으면 빈 배열 기본값을 반환해야 한다.
	m := &UserDataManager{dataDir: t.TempDir()}
	loaded, err := m.LoadPathHistory()
	if err != nil {
		t.Fatalf("load path history failed: %v", err)
	}
	if len(loaded.Source) != 0 || len(loaded.Dest) != 0 {
		t.Fatalf("expected empty path history, got %+v", loaded)
	}
}

// TestUserDataManager_SaveAndLoadBackupHistory_RoundTrip는 테스트 코드 동작을 검증하거나 보조합니다.
func TestUserDataManager_SaveAndLoadBackupHistory_RoundTrip(t *testing.T) {
	// backup-history 저장 후 로드 시 엔트리 정보가 유지되어야 한다.
	m := &UserDataManager{dataDir: t.TempDir()}
	history := &types.BackupHistory{
		Entries: []types.BackupHistoryEntry{
			{ID: "run-1", Status: types.BackupStatusSuccess},
			{ID: "run-2", Status: types.BackupStatusFailed},
		},
	}

	if err := m.SaveBackupHistory(history); err != nil {
		t.Fatalf("save backup history failed: %v", err)
	}

	loaded, err := m.LoadBackupHistory()
	if err != nil {
		t.Fatalf("load backup history failed: %v", err)
	}
	if len(loaded.Entries) != 2 {
		t.Fatalf("unexpected loaded backup history: %+v", loaded)
	}
	if loaded.Entries[0].ID != "run-1" || loaded.Entries[1].Status != types.BackupStatusFailed {
		t.Fatalf("unexpected loaded backup entries: %+v", loaded.Entries)
	}
}

// TestUserDataManager_SaveBackupHistory_ReturnsWriteError는 테스트 코드 동작을 검증하거나 보조합니다.
func TestUserDataManager_SaveBackupHistory_ReturnsWriteError(t *testing.T) {
	// dataDir가 파일이면 backup history 저장 시 write 에러를 반환해야 한다.
	blocker := filepath.Join(t.TempDir(), "not-dir")
	if err := os.WriteFile(blocker, []byte("x"), 0644); err != nil {
		t.Fatalf("failed to create blocker file: %v", err)
	}

	m := &UserDataManager{dataDir: blocker}
	err := m.SaveBackupHistory(&types.BackupHistory{})
	if err == nil {
		t.Fatal("expected backup history write error")
	}
}

// TestUserDataManager_SaveBackupHistory_ReturnsRenameError는 테스트 코드 동작을 검증하거나 보조합니다.
func TestUserDataManager_SaveBackupHistory_ReturnsRenameError(t *testing.T) {
	// 대상 파일명이 디렉터리면 backup history rename 단계에서 실패해야 한다.
	dataDir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dataDir, "backup-history.json"), 0755); err != nil {
		t.Fatalf("failed to create backup-history target dir: %v", err)
	}

	m := &UserDataManager{dataDir: dataDir}
	err := m.SaveBackupHistory(&types.BackupHistory{})
	if err == nil {
		t.Fatal("expected backup history rename error")
	}
}

// TestUserDataManager_LoadBackupHistory_ReturnsReadAndUnmarshalErrors는 테스트 코드 동작을 검증하거나 보조합니다.
func TestUserDataManager_LoadBackupHistory_ReturnsReadAndUnmarshalErrors(t *testing.T) {
	// backup history 로드는 read 에러와 unmarshal 에러를 각각 반환해야 한다.
	t.Run("read_error", func(t *testing.T) {
		dataDir := t.TempDir()
		if err := os.MkdirAll(filepath.Join(dataDir, "backup-history.json"), 0755); err != nil {
			t.Fatalf("failed to create backup history dir path: %v", err)
		}

		m := &UserDataManager{dataDir: dataDir}
		_, err := m.LoadBackupHistory()
		if err == nil {
			t.Fatal("expected backup history read error")
		}
	})

	t.Run("unmarshal_error", func(t *testing.T) {
		dataDir := t.TempDir()
		if err := os.WriteFile(filepath.Join(dataDir, "backup-history.json"), []byte("{"), 0644); err != nil {
			t.Fatalf("failed to write broken backup history: %v", err)
		}

		m := &UserDataManager{dataDir: dataDir}
		_, err := m.LoadBackupHistory()
		if err == nil {
			t.Fatal("expected backup history unmarshal error")
		}
	})
}

// TestUserDataManager_AddHistoryEntry_ReturnsLoadError는 테스트 코드 동작을 검증하거나 보조합니다.
func TestUserDataManager_AddHistoryEntry_ReturnsLoadError(t *testing.T) {
	// AddHistoryEntry는 기존 히스토리 로드 실패를 호출자에게 반환해야 한다.
	dataDir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dataDir, "backup-history.json"), 0755); err != nil {
		t.Fatalf("failed to create backup history dir path: %v", err)
	}

	m := &UserDataManager{dataDir: dataDir}
	err := m.AddHistoryEntry(types.BackupHistoryEntry{ID: "x"})
	if err == nil {
		t.Fatal("expected add history load error")
	}
}
