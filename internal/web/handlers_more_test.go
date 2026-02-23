package web

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/On-Jun9/ShutterPipe/internal/config"
	"github.com/On-Jun9/ShutterPipe/pkg/types"
)

// TestHandleGetConfig_ReturnsDefaultConfigJSON는 테스트 코드 동작을 검증하거나 보조합니다.
func TestHandleGetConfig_ReturnsDefaultConfigJSON(t *testing.T) {
	// 기본 설정 조회는 JSON 형식의 기본 Config를 반환해야 한다.
	s := &Server{}
	req := httptest.NewRequest(http.MethodGet, "/api/config", nil)
	rr := httptest.NewRecorder()

	s.handleGetConfig(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", rr.Code)
	}
	var cfg config.Config
	if err := json.NewDecoder(rr.Body).Decode(&cfg); err != nil {
		t.Fatalf("failed to decode config: %v", err)
	}
	if len(cfg.IncludeExtensions) == 0 {
		t.Fatal("expected default include extensions")
	}
}

// TestHandleSaveConfig_ParsesJSONAndReturnsOK는 테스트 코드 동작을 검증하거나 보조합니다.
func TestHandleSaveConfig_ParsesJSONAndReturnsOK(t *testing.T) {
	// 설정 저장 API는 JSON 파싱 성공 시 status=ok를 반환해야 한다.
	s := &Server{}
	req := httptest.NewRequest(http.MethodPost, "/api/config", strings.NewReader(`{"source":"/a","dest":"/b"}`))
	rr := httptest.NewRecorder()

	s.handleSaveConfig(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", rr.Code)
	}
	var body map[string]string
	if err := json.NewDecoder(rr.Body).Decode(&body); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if body["status"] != "ok" {
		t.Fatalf("expected status=ok, got %v", body)
	}
}

// TestHandlePresetHandlers_CRUDFlow는 테스트 코드 동작을 검증하거나 보조합니다.
func TestHandlePresetHandlers_CRUDFlow(t *testing.T) {
	// preset 저장/목록/로드/삭제 플로우가 일관되게 동작해야 한다.
	s := &Server{}
	t.Setenv("HOME", t.TempDir())

	saveReq := httptest.NewRequest(
		http.MethodPost,
		"/api/presets",
		strings.NewReader(`{"name":"travel","description":"trip preset","config":{"source":"/src","dest":"/dest","include_extensions":["jpg"]}}`),
	)
	saveRR := httptest.NewRecorder()
	s.handleSavePreset(saveRR, saveReq)
	if saveRR.Code != http.StatusOK {
		t.Fatalf("save preset expected 200, got %d", saveRR.Code)
	}

	listReq := httptest.NewRequest(http.MethodGet, "/api/presets", nil)
	listRR := httptest.NewRecorder()
	s.handleListPresets(listRR, listReq)
	if listRR.Code != http.StatusOK {
		t.Fatalf("list presets expected 200, got %d", listRR.Code)
	}
	var presets []types.ConfigPreset
	if err := json.NewDecoder(listRR.Body).Decode(&presets); err != nil {
		t.Fatalf("failed to decode presets list: %v", err)
	}
	if len(presets) != 1 || presets[0].Name != "travel" {
		t.Fatalf("unexpected presets result: %+v", presets)
	}

	loadReq := httptest.NewRequest(http.MethodGet, "/api/presets/load?name=travel", nil)
	loadRR := httptest.NewRecorder()
	s.handleLoadPreset(loadRR, loadReq)
	if loadRR.Code != http.StatusOK {
		t.Fatalf("load preset expected 200, got %d", loadRR.Code)
	}
	var loaded config.Config
	if err := json.NewDecoder(loadRR.Body).Decode(&loaded); err != nil {
		t.Fatalf("failed to decode loaded config: %v", err)
	}
	if loaded.Source != "/src" || loaded.Dest != "/dest" {
		t.Fatalf("unexpected loaded config: %+v", loaded)
	}

	deleteReq := httptest.NewRequest(http.MethodDelete, "/api/presets/delete?name=travel", nil)
	deleteRR := httptest.NewRecorder()
	s.handleDeletePreset(deleteRR, deleteReq)
	if deleteRR.Code != http.StatusOK {
		t.Fatalf("delete preset expected 200, got %d", deleteRR.Code)
	}
}

// TestHandlePresetHandlers_ReturnValidationStyleErrors는 테스트 코드 동작을 검증하거나 보조합니다.
func TestHandlePresetHandlers_ReturnValidationStyleErrors(t *testing.T) {
	// 필수 파라미터 누락 시 의미에 맞는 400 JSON 에러를 반환해야 한다.
	s := &Server{}

	saveReq := httptest.NewRequest(http.MethodPost, "/api/presets", strings.NewReader(`{"name":"","config":{}}`))
	saveRR := httptest.NewRecorder()
	s.handleSavePreset(saveRR, saveReq)
	if saveRR.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for empty preset name, got %d", saveRR.Code)
	}
	if decodeAPIErrorResponse(t, saveRR).Message == "" {
		t.Fatal("expected api error message")
	}

	loadReq := httptest.NewRequest(http.MethodGet, "/api/presets/load", nil)
	loadRR := httptest.NewRecorder()
	s.handleLoadPreset(loadRR, loadReq)
	if loadRR.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for missing load name, got %d", loadRR.Code)
	}

	deleteReq := httptest.NewRequest(http.MethodDelete, "/api/presets/delete", nil)
	deleteRR := httptest.NewRecorder()
	s.handleDeletePreset(deleteRR, deleteReq)
	if deleteRR.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for missing delete name, got %d", deleteRR.Code)
	}
}

// TestHandleGetUserDataEndpoints_ReturnDefaults는 테스트 코드 동작을 검증하거나 보조합니다.
func TestHandleGetUserDataEndpoints_ReturnDefaults(t *testing.T) {
	// settings/bookmarks/path-history 조회는 파일이 없어도 기본 JSON을 반환해야 한다.
	s := &Server{}
	t.Setenv("HOME", t.TempDir())

	settingsReq := httptest.NewRequest(http.MethodGet, "/api/settings", nil)
	settingsRR := httptest.NewRecorder()
	s.handleGetSettings(settingsRR, settingsReq)
	if settingsRR.Code != http.StatusOK {
		t.Fatalf("settings expected 200, got %d", settingsRR.Code)
	}
	var settings types.UserSettings
	if err := json.NewDecoder(settingsRR.Body).Decode(&settings); err != nil {
		t.Fatalf("failed to decode settings: %v", err)
	}
	if settings.UnclassifiedDir == "" || settings.QuarantineDir == "" {
		t.Fatalf("expected default dirs in settings: %+v", settings)
	}

	bookmarksReq := httptest.NewRequest(http.MethodGet, "/api/bookmarks", nil)
	bookmarksRR := httptest.NewRecorder()
	s.handleGetBookmarks(bookmarksRR, bookmarksReq)
	if bookmarksRR.Code != http.StatusOK {
		t.Fatalf("bookmarks expected 200, got %d", bookmarksRR.Code)
	}
	var bookmarks types.Bookmarks
	if err := json.NewDecoder(bookmarksRR.Body).Decode(&bookmarks); err != nil {
		t.Fatalf("failed to decode bookmarks: %v", err)
	}
	if len(bookmarks.Source) != 0 || len(bookmarks.Dest) != 0 {
		t.Fatalf("expected empty bookmarks, got %+v", bookmarks)
	}

	historyReq := httptest.NewRequest(http.MethodGet, "/api/path-history", nil)
	historyRR := httptest.NewRecorder()
	s.handleGetPathHistory(historyRR, historyReq)
	if historyRR.Code != http.StatusOK {
		t.Fatalf("path history expected 200, got %d", historyRR.Code)
	}
	var history types.PathHistory
	if err := json.NewDecoder(historyRR.Body).Decode(&history); err != nil {
		t.Fatalf("failed to decode path history: %v", err)
	}
	if len(history.Source) != 0 || len(history.Dest) != 0 {
		t.Fatalf("expected empty path history, got %+v", history)
	}
}

// TestHandleGetBackupHistory_RespectsLimit는 테스트 코드 동작을 검증하거나 보조합니다.
func TestHandleGetBackupHistory_RespectsLimit(t *testing.T) {
	// backup history 조회는 limit 파라미터에 맞게 항목 수를 제한해야 한다.
	s := &Server{}
	t.Setenv("HOME", t.TempDir())

	m, err := config.NewUserDataManager()
	if err != nil {
		t.Fatalf("failed to create user data manager: %v", err)
	}
	for i := 0; i < 3; i++ {
		err := m.AddHistoryEntry(types.BackupHistoryEntry{
			ID:     string(rune('a' + i)),
			Status: types.BackupStatusSuccess,
		})
		if err != nil {
			t.Fatalf("failed to add history entry: %v", err)
		}
	}

	req := httptest.NewRequest(http.MethodGet, "/api/backup-history?limit=2", nil)
	rr := httptest.NewRecorder()
	s.handleGetBackupHistory(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", rr.Code)
	}
	var history types.BackupHistory
	if err := json.NewDecoder(rr.Body).Decode(&history); err != nil {
		t.Fatalf("failed to decode backup history: %v", err)
	}
	if len(history.Entries) != 2 {
		t.Fatalf("expected 2 history entries, got %d", len(history.Entries))
	}
}

// TestHandleBrowse_SuccessSkipsHiddenFiles는 테스트 코드 동작을 검증하거나 보조합니다.
func TestHandleBrowse_SuccessSkipsHiddenFiles(t *testing.T) {
	// browse 성공 응답에서 숨김 파일은 제외되어야 한다.
	s := &Server{}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "visible.txt"), []byte("ok"), 0644); err != nil {
		t.Fatalf("failed to write visible file: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".hidden.txt"), []byte("x"), 0644); err != nil {
		t.Fatalf("failed to write hidden file: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/browse?path="+url.QueryEscape(dir), nil)
	rr := httptest.NewRecorder()
	s.handleBrowse(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", rr.Code)
	}
	var response BrowseResponse
	if err := json.NewDecoder(rr.Body).Decode(&response); err != nil {
		t.Fatalf("failed to decode browse response: %v", err)
	}
	if len(response.Entries) != 1 || response.Entries[0].Name != "visible.txt" {
		t.Fatalf("unexpected browse entries: %+v", response.Entries)
	}
}

// TestHandleBrowse_ReturnsForbiddenOnPermissionDenied는 테스트 코드 동작을 검증하거나 보조합니다.
func TestHandleBrowse_ReturnsForbiddenOnPermissionDenied(t *testing.T) {
	// browse는 권한 없는 경로 접근 시 403 JSON 에러를 반환해야 한다.
	s := &Server{}
	parent := t.TempDir()
	noPermDir := filepath.Join(parent, "blocked")
	if err := os.MkdirAll(noPermDir, 0755); err != nil {
		t.Fatalf("failed to create blocked dir: %v", err)
	}

	if err := os.Chmod(noPermDir, 0000); err != nil {
		t.Fatalf("failed to chmod blocked dir: %v", err)
	}
	defer os.Chmod(noPermDir, 0755)

	_, precheckErr := os.ReadDir(noPermDir)
	if precheckErr == nil || !errors.Is(precheckErr, os.ErrPermission) {
		t.Skip("permission denied branch is not reproducible in this environment")
	}

	req := httptest.NewRequest(http.MethodGet, "/api/browse?path="+url.QueryEscape(noPermDir), nil)
	rr := httptest.NewRecorder()
	s.handleBrowse(rr, req)

	if rr.Code != http.StatusForbidden {
		t.Fatalf("expected status 403, got %d", rr.Code)
	}
	if decodeAPIErrorResponse(t, rr).Message == "" {
		t.Fatal("expected permission error message")
	}
}

// TestHandleSaveConfig_ReturnsBadRequestOnInvalidJSON는 테스트 코드 동작을 검증하거나 보조합니다.
func TestHandleSaveConfig_ReturnsBadRequestOnInvalidJSON(t *testing.T) {
	// 설정 저장 API는 JSON 파싱 실패 시 400 JSON 에러를 반환해야 한다.
	s := &Server{}
	req := httptest.NewRequest(http.MethodPost, "/api/config", strings.NewReader("{"))
	rr := httptest.NewRecorder()

	s.handleSaveConfig(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected status 400, got %d", rr.Code)
	}
	if decodeAPIErrorResponse(t, rr).Message == "" {
		t.Fatal("expected parse error message")
	}
}

// TestHandlePresetHandlers_ErrorBranches는 테스트 코드 동작을 검증하거나 보조합니다.
func TestHandlePresetHandlers_ErrorBranches(t *testing.T) {
	// preset 핸들러의 파싱/미존재/초기화 실패 분기를 검증한다.
	s := &Server{}

	invalidJSONReq := httptest.NewRequest(http.MethodPost, "/api/presets", strings.NewReader("{"))
	invalidJSONRR := httptest.NewRecorder()
	s.handleSavePreset(invalidJSONRR, invalidJSONReq)
	if invalidJSONRR.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for invalid preset json, got %d", invalidJSONRR.Code)
	}

	t.Setenv("HOME", t.TempDir())
	missingReq := httptest.NewRequest(http.MethodGet, "/api/presets/load?name=nope", nil)
	missingRR := httptest.NewRecorder()
	s.handleLoadPreset(missingRR, missingReq)
	if missingRR.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for missing preset, got %d", missingRR.Code)
	}

	homeFile := filepath.Join(t.TempDir(), "home-file")
	if err := os.WriteFile(homeFile, []byte("x"), 0644); err != nil {
		t.Fatalf("failed to create fake home file: %v", err)
	}
	t.Setenv("HOME", homeFile)

	listReq := httptest.NewRequest(http.MethodGet, "/api/presets", nil)
	listRR := httptest.NewRecorder()
	s.handleListPresets(listRR, listReq)
	if listRR.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500 for preset manager init failure, got %d", listRR.Code)
	}

	t.Setenv("HOME", t.TempDir())
	deleteMissingReq := httptest.NewRequest(http.MethodDelete, "/api/presets/delete?name=missing", nil)
	deleteMissingRR := httptest.NewRecorder()
	s.handleDeletePreset(deleteMissingRR, deleteMissingReq)
	if deleteMissingRR.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500 for deleting missing preset, got %d", deleteMissingRR.Code)
	}
}

// TestHandleUserDataGetHandlers_InternalErrorBranches는 테스트 코드 동작을 검증하거나 보조합니다.
func TestHandleUserDataGetHandlers_InternalErrorBranches(t *testing.T) {
	// userdata 조회 핸들러는 매니저 초기화 실패 시 500 JSON 에러를 반환해야 한다.
	s := &Server{}

	homeFile := filepath.Join(t.TempDir(), "home-file")
	if err := os.WriteFile(homeFile, []byte("x"), 0644); err != nil {
		t.Fatalf("failed to create fake home file: %v", err)
	}
	t.Setenv("HOME", homeFile)

	cases := []struct {
		name   string
		call   func(http.ResponseWriter, *http.Request)
		path   string
		method string
	}{
		{
			name:   "settings",
			call:   s.handleGetSettings,
			path:   "/api/settings",
			method: http.MethodGet,
		},
		{
			name:   "bookmarks",
			call:   s.handleGetBookmarks,
			path:   "/api/bookmarks",
			method: http.MethodGet,
		},
		{
			name:   "path_history",
			call:   s.handleGetPathHistory,
			path:   "/api/path-history",
			method: http.MethodGet,
		},
		{
			name:   "backup_history",
			call:   s.handleGetBackupHistory,
			path:   "/api/backup-history",
			method: http.MethodGet,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(tc.method, tc.path, nil)
			rr := httptest.NewRecorder()
			tc.call(rr, req)

			if rr.Code != http.StatusInternalServerError {
				t.Fatalf("expected status 500, got %d", rr.Code)
			}
			if decodeAPIErrorResponse(t, rr).Message == "" {
				t.Fatal("expected api error message")
			}
		})
	}
}

// TestHandleSaveUserDataHandlers_SuccessAndErrorBranches는 테스트 코드 동작을 검증하거나 보조합니다.
func TestHandleSaveUserDataHandlers_SuccessAndErrorBranches(t *testing.T) {
	// save(settings/bookmarks/path-history)는 성공 시 200, 매니저 실패 시 500을 반환해야 한다.
	s := &Server{}

	t.Setenv("HOME", t.TempDir())
	successCases := []struct {
		name    string
		call    func(http.ResponseWriter, *http.Request)
		path    string
		payload string
	}{
		{
			name:    "save_settings_success",
			call:    s.handleSaveSettings,
			path:    "/api/settings",
			payload: `{"source":"/src","dest":"/dest"}`,
		},
		{
			name:    "save_bookmarks_success",
			call:    s.handleSaveBookmarks,
			path:    "/api/bookmarks",
			payload: `{"source":["/src/a"],"dest":["/dest/a"]}`,
		},
		{
			name:    "save_path_history_success",
			call:    s.handleSavePathHistory,
			path:    "/api/path-history",
			payload: `{"source":["/src/recent"],"dest":["/dest/recent"]}`,
		},
	}

	for _, tc := range successCases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, tc.path, strings.NewReader(tc.payload))
			rr := httptest.NewRecorder()
			tc.call(rr, req)

			if rr.Code != http.StatusOK {
				t.Fatalf("expected status 200, got %d", rr.Code)
			}
			var body map[string]string
			if err := json.NewDecoder(rr.Body).Decode(&body); err != nil {
				t.Fatalf("failed to decode success response: %v", err)
			}
			if body["status"] != "ok" {
				t.Fatalf("unexpected success body: %+v", body)
			}
		})
	}

	homeFile := filepath.Join(t.TempDir(), "home-file")
	if err := os.WriteFile(homeFile, []byte("x"), 0644); err != nil {
		t.Fatalf("failed to create fake home file: %v", err)
	}
	t.Setenv("HOME", homeFile)

	errorCases := []struct {
		name    string
		call    func(http.ResponseWriter, *http.Request)
		path    string
		payload string
	}{
		{
			name:    "save_settings_manager_error",
			call:    s.handleSaveSettings,
			path:    "/api/settings",
			payload: `{"source":"/src","dest":"/dest"}`,
		},
		{
			name:    "save_bookmarks_manager_error",
			call:    s.handleSaveBookmarks,
			path:    "/api/bookmarks",
			payload: `{"source":[],"dest":[]}`,
		},
		{
			name:    "save_path_history_manager_error",
			call:    s.handleSavePathHistory,
			path:    "/api/path-history",
			payload: `{"source":[],"dest":[]}`,
		},
	}

	for _, tc := range errorCases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, tc.path, strings.NewReader(tc.payload))
			rr := httptest.NewRecorder()
			tc.call(rr, req)

			if rr.Code != http.StatusInternalServerError {
				t.Fatalf("expected status 500, got %d", rr.Code)
			}
			if decodeAPIErrorResponse(t, rr).Message == "" {
				t.Fatal("expected api error message")
			}
		})
	}
}

// TestHandleSaveUserDataHandlers_ReturnBadRequestOnInvalidJSON는 테스트 코드 동작을 검증하거나 보조합니다.
func TestHandleSaveUserDataHandlers_ReturnBadRequestOnInvalidJSON(t *testing.T) {
	// save(settings/bookmarks/path-history)는 JSON 파싱 실패 시 400을 반환해야 한다.
	s := &Server{}

	cases := []struct {
		name   string
		call   func(http.ResponseWriter, *http.Request)
		path   string
		method string
	}{
		{
			name:   "save_settings_invalid_json",
			call:   s.handleSaveSettings,
			path:   "/api/settings",
			method: http.MethodPost,
		},
		{
			name:   "save_bookmarks_invalid_json",
			call:   s.handleSaveBookmarks,
			path:   "/api/bookmarks",
			method: http.MethodPost,
		},
		{
			name:   "save_path_history_invalid_json",
			call:   s.handleSavePathHistory,
			path:   "/api/path-history",
			method: http.MethodPost,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(tc.method, tc.path, strings.NewReader("{"))
			rr := httptest.NewRecorder()
			tc.call(rr, req)

			if rr.Code != http.StatusBadRequest {
				t.Fatalf("expected status 400, got %d", rr.Code)
			}
			if decodeAPIErrorResponse(t, rr).Message == "" {
				t.Fatal("expected parse error message")
			}
		})
	}
}

// TestHandleRun_ReturnsStartedAndRunsPipeline는 테스트 코드 동작을 검증하거나 보조합니다.
func TestHandleRun_ReturnsStartedAndRunsPipeline(t *testing.T) {
	// 유효한 요청이면 /api/run은 즉시 started를 반환하고 백그라운드 실행해야 한다.
	waitForRunMutexFree(t)

	tmpDir := t.TempDir()
	t.Setenv("HOME", filepath.Join(tmpDir, "home"))

	sourceDir := filepath.Join(tmpDir, "src")
	destDir := filepath.Join(tmpDir, "dest")
	if err := os.MkdirAll(sourceDir, 0755); err != nil {
		t.Fatalf("failed to create source dir: %v", err)
	}
	if err := os.MkdirAll(destDir, 0755); err != nil {
		t.Fatalf("failed to create dest dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(sourceDir, "a.jpg"), []byte("photo"), 0644); err != nil {
		t.Fatalf("failed to write source file: %v", err)
	}

	s := &Server{hub: NewHub()}
	req := httptest.NewRequest(
		http.MethodPost,
		"/api/run",
		strings.NewReader(`{"source":"`+sourceDir+`","dest":"`+destDir+`","include_extensions":["jpg"],"jobs":1,"dry_run":true}`),
	)
	rr := httptest.NewRecorder()

	s.handleRun(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", rr.Code)
	}
	var body map[string]string
	if err := json.NewDecoder(rr.Body).Decode(&body); err != nil {
		t.Fatalf("failed to decode run response: %v", err)
	}
	if body["status"] != "started" {
		t.Fatalf("unexpected run response: %+v", body)
	}

	waitForRunMutexFree(t)
}

// waitForRunMutexFree는 테스트 코드 동작을 검증하거나 보조합니다.
func waitForRunMutexFree(t *testing.T) {
	t.Helper()

	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if runMutex.TryLock() {
			runMutex.Unlock()
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("timeout waiting for run mutex to be free")
}
