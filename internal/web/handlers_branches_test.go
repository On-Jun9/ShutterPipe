package web

import (
	"encoding/json"
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

// TestHandleBrowse_UsesHomeWhenPathIsEmpty는 테스트 코드 동작을 검증하거나 보조합니다.
func TestHandleBrowse_UsesHomeWhenPathIsEmpty(t *testing.T) {
	// path 쿼리가 비어 있으면 HOME 경로를 기준으로 browse 해야 한다.
	home := t.TempDir()
	t.Setenv("HOME", home)
	if err := os.WriteFile(filepath.Join(home, "visible.txt"), []byte("ok"), 0644); err != nil {
		t.Fatalf("failed to create visible file in home: %v", err)
	}

	s := &Server{}
	req := httptest.NewRequest(http.MethodGet, "/api/browse", nil)
	rr := httptest.NewRecorder()
	s.handleBrowse(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", rr.Code)
	}

	var resp BrowseResponse
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode browse response: %v", err)
	}
	if resp.Path != home {
		t.Fatalf("expected browse path=%s, got %s", home, resp.Path)
	}
}

// TestHandleBrowse_ReturnsInternalErrorOnInvalidPath는 테스트 코드 동작을 검증하거나 보조합니다.
func TestHandleBrowse_ReturnsInternalErrorOnInvalidPath(t *testing.T) {
	// notfound/permission이 아닌 read 에러는 500으로 내려야 한다.
	s := &Server{}
	req := httptest.NewRequest(http.MethodGet, "/api/browse?path=%00", nil)
	rr := httptest.NewRecorder()
	s.handleBrowse(rr, req)

	if rr.Code != http.StatusInternalServerError {
		t.Fatalf("expected status 500, got %d", rr.Code)
	}
	if decodeAPIErrorResponse(t, rr).Message == "" {
		t.Fatal("expected internal error message")
	}
}

// TestHandleRun_BackgroundPipelineInitFailureBroadcastsError는 테스트 코드 동작을 검증하거나 보조합니다.
func TestHandleRun_BackgroundPipelineInitFailureBroadcastsError(t *testing.T) {
	// background goroutine에서 pipeline.New 실패 시 error progress를 broadcast 해야 한다.
	waitForRunMutexFree(t)

	tmpDir := t.TempDir()
	homeFile := filepath.Join(tmpDir, "home-file")
	if err := os.WriteFile(homeFile, []byte("x"), 0644); err != nil {
		t.Fatalf("failed to create fake home file: %v", err)
	}
	t.Setenv("HOME", homeFile)

	sourceDir := filepath.Join(tmpDir, "src")
	destDir := filepath.Join(tmpDir, "dest")
	if err := os.MkdirAll(sourceDir, 0755); err != nil {
		t.Fatalf("failed to create source dir: %v", err)
	}
	if err := os.MkdirAll(destDir, 0755); err != nil {
		t.Fatalf("failed to create dest dir: %v", err)
	}

	s := &Server{hub: NewHub()}
	req := httptest.NewRequest(
		http.MethodPost,
		"/api/run",
		strings.NewReader(`{"source":"`+sourceDir+`","dest":"`+destDir+`","include_extensions":["jpg"],"jobs":1}`),
	)
	rr := httptest.NewRecorder()
	s.handleRun(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", rr.Code)
	}

	waitForErrorProgressMessage(t, s.hub.broadcast, 2*time.Second)

	waitForRunMutexFree(t)
}

// TestHandleRun_BackgroundPipelineRunFailureBroadcastsError는 테스트 코드 동작을 검증하거나 보조합니다.
func TestHandleRun_BackgroundPipelineRunFailureBroadcastsError(t *testing.T) {
	// pipeline.New 성공 후 Run 실패(스캔 실패)도 error progress를 broadcast 해야 한다.
	waitForRunMutexFree(t)

	tmpDir := t.TempDir()
	t.Setenv("HOME", filepath.Join(tmpDir, "home"))

	missingSource := filepath.Join(tmpDir, "missing-src")
	destDir := filepath.Join(tmpDir, "dest")
	if err := os.MkdirAll(destDir, 0755); err != nil {
		t.Fatalf("failed to create dest dir: %v", err)
	}

	s := &Server{hub: NewHub()}
	req := httptest.NewRequest(
		http.MethodPost,
		"/api/run",
		strings.NewReader(`{"source":"`+missingSource+`","dest":"`+destDir+`","include_extensions":["jpg"],"jobs":1}`),
	)
	rr := httptest.NewRecorder()
	s.handleRun(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", rr.Code)
	}

	waitForErrorProgressMessage(t, s.hub.broadcast, 2*time.Second)

	waitForRunMutexFree(t)
}

// TestBroadcastJSON_IgnoresMarshalError는 테스트 코드 동작을 검증하거나 보조합니다.
func TestBroadcastJSON_IgnoresMarshalError(t *testing.T) {
	// JSON marshal 불가 값은 무시하고 broadcast 채널로 보내지 않아야 한다.
	s := &Server{hub: NewHub()}
	s.broadcastJSON(make(chan int))

	select {
	case <-s.hub.broadcast:
		t.Fatal("expected no broadcast message on marshal error")
	default:
	}
}

// TestHandleListPresets_ReturnsInternalErrorWhenListFails는 테스트 코드 동작을 검증하거나 보조합니다.
func TestHandleListPresets_ReturnsInternalErrorWhenListFails(t *testing.T) {
	// preset 디렉터리 read 실패 시 500을 반환해야 한다.
	s := &Server{}
	home := t.TempDir()
	t.Setenv("HOME", home)

	presetsDir := filepath.Join(home, ".shutterpipe", "presets")
	if err := os.MkdirAll(presetsDir, 0755); err != nil {
		t.Fatalf("failed to create presets dir: %v", err)
	}
	if err := os.Chmod(presetsDir, 0000); err != nil {
		t.Fatalf("failed to chmod presets dir: %v", err)
	}
	defer os.Chmod(presetsDir, 0755)

	if _, err := os.ReadDir(presetsDir); err == nil {
		t.Skip("list error branch is not reproducible in this environment")
	}

	req := httptest.NewRequest(http.MethodGet, "/api/presets", nil)
	rr := httptest.NewRecorder()
	s.handleListPresets(rr, req)

	if rr.Code != http.StatusInternalServerError {
		t.Fatalf("expected status 500, got %d", rr.Code)
	}
}

// TestHandleSavePreset_ManagerAndSaveErrorBranches는 테스트 코드 동작을 검증하거나 보조합니다.
func TestHandleSavePreset_ManagerAndSaveErrorBranches(t *testing.T) {
	// SavePreset은 manager init 실패와 save 실패를 각각 500으로 내려야 한다.
	s := &Server{}

	homeFile := filepath.Join(t.TempDir(), "home-file")
	if err := os.WriteFile(homeFile, []byte("x"), 0644); err != nil {
		t.Fatalf("failed to create fake home file: %v", err)
	}
	t.Setenv("HOME", homeFile)

	managerErrReq := httptest.NewRequest(
		http.MethodPost,
		"/api/presets",
		strings.NewReader(`{"name":"demo","config":{"source":"/src","dest":"/dest"}}`),
	)
	managerErrRR := httptest.NewRecorder()
	s.handleSavePreset(managerErrRR, managerErrReq)
	if managerErrRR.Code != http.StatusInternalServerError {
		t.Fatalf("expected status 500 on manager init error, got %d", managerErrRR.Code)
	}

	t.Setenv("HOME", t.TempDir())
	saveErrReq := httptest.NewRequest(
		http.MethodPost,
		"/api/presets",
		strings.NewReader(`{"name":"bad/name","config":{"source":"/src","dest":"/dest"}}`),
	)
	saveErrRR := httptest.NewRecorder()
	s.handleSavePreset(saveErrRR, saveErrReq)
	if saveErrRR.Code != http.StatusInternalServerError {
		t.Fatalf("expected status 500 on save preset error, got %d", saveErrRR.Code)
	}
}

// TestHandleLoadAndDeletePreset_ManagerInitErrors는 테스트 코드 동작을 검증하거나 보조합니다.
func TestHandleLoadAndDeletePreset_ManagerInitErrors(t *testing.T) {
	// Load/Delete preset은 manager init 실패 시 500을 반환해야 한다.
	s := &Server{}

	homeFile := filepath.Join(t.TempDir(), "home-file")
	if err := os.WriteFile(homeFile, []byte("x"), 0644); err != nil {
		t.Fatalf("failed to create fake home file: %v", err)
	}
	t.Setenv("HOME", homeFile)

	loadReq := httptest.NewRequest(http.MethodGet, "/api/presets/load?name=x", nil)
	loadRR := httptest.NewRecorder()
	s.handleLoadPreset(loadRR, loadReq)
	if loadRR.Code != http.StatusInternalServerError {
		t.Fatalf("expected status 500 on load manager init error, got %d", loadRR.Code)
	}

	deleteReq := httptest.NewRequest(http.MethodDelete, "/api/presets/delete?name=x", nil)
	deleteRR := httptest.NewRecorder()
	s.handleDeletePreset(deleteRR, deleteReq)
	if deleteRR.Code != http.StatusInternalServerError {
		t.Fatalf("expected status 500 on delete manager init error, got %d", deleteRR.Code)
	}
}

// TestHandleGetUserDataHandlers_ReturnInternalErrorOnLoadFailures는 테스트 코드 동작을 검증하거나 보조합니다.
func TestHandleGetUserDataHandlers_ReturnInternalErrorOnLoadFailures(t *testing.T) {
	// manager 생성 성공 후 load 실패가 나면 500을 반환해야 한다.
	s := &Server{}
	home := t.TempDir()
	t.Setenv("HOME", home)

	baseDir := filepath.Join(home, ".shutterpipe")
	if err := os.MkdirAll(filepath.Join(baseDir, "settings.json"), 0755); err != nil {
		t.Fatalf("failed to create settings path dir: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(baseDir, "bookmarks.json"), 0755); err != nil {
		t.Fatalf("failed to create bookmarks path dir: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(baseDir, "path-history.json"), 0755); err != nil {
		t.Fatalf("failed to create path-history path dir: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(baseDir, "backup-history.json"), 0755); err != nil {
		t.Fatalf("failed to create backup-history path dir: %v", err)
	}

	cases := []struct {
		name string
		call func(http.ResponseWriter, *http.Request)
		path string
	}{
		{name: "settings_load_error", call: s.handleGetSettings, path: "/api/settings"},
		{name: "bookmarks_load_error", call: s.handleGetBookmarks, path: "/api/bookmarks"},
		{name: "path_history_load_error", call: s.handleGetPathHistory, path: "/api/path-history"},
		{name: "backup_history_load_error", call: s.handleGetBackupHistory, path: "/api/backup-history"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, tc.path, nil)
			rr := httptest.NewRecorder()
			tc.call(rr, req)
			if rr.Code != http.StatusInternalServerError {
				t.Fatalf("expected status 500, got %d", rr.Code)
			}
		})
	}
}

// TestHandleSaveUserDataHandlers_ReturnInternalErrorOnSaveFailures는 테스트 코드 동작을 검증하거나 보조합니다.
func TestHandleSaveUserDataHandlers_ReturnInternalErrorOnSaveFailures(t *testing.T) {
	// manager 생성 성공 후 save non-validation 에러는 500을 반환해야 한다.
	s := &Server{}
	home := t.TempDir()
	t.Setenv("HOME", home)

	baseDir := filepath.Join(home, ".shutterpipe")
	if err := os.MkdirAll(filepath.Join(baseDir, "settings.json"), 0755); err != nil {
		t.Fatalf("failed to create settings target dir: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(baseDir, "bookmarks.json"), 0755); err != nil {
		t.Fatalf("failed to create bookmarks target dir: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(baseDir, "path-history.json"), 0755); err != nil {
		t.Fatalf("failed to create path-history target dir: %v", err)
	}

	cases := []struct {
		name    string
		call    func(http.ResponseWriter, *http.Request)
		path    string
		payload string
	}{
		{
			name:    "settings_save_error",
			call:    s.handleSaveSettings,
			path:    "/api/settings",
			payload: `{"source":"/src","dest":"/dest"}`,
		},
		{
			name:    "bookmarks_save_error",
			call:    s.handleSaveBookmarks,
			path:    "/api/bookmarks",
			payload: `{"source":["/src"],"dest":[]}`,
		},
		{
			name:    "path_history_save_error",
			call:    s.handleSavePathHistory,
			path:    "/api/path-history",
			payload: `{"source":["/src"],"dest":[]}`,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, tc.path, strings.NewReader(tc.payload))
			rr := httptest.NewRecorder()
			tc.call(rr, req)
			if rr.Code != http.StatusInternalServerError {
				t.Fatalf("expected status 500, got %d", rr.Code)
			}
		})
	}
}

// TestHandleGetBackupHistory_LimitBounds는 테스트 코드 동작을 검증하거나 보조합니다.
func TestHandleGetBackupHistory_LimitBounds(t *testing.T) {
	// backup history limit은 상한(100), 하한(1 미만 -> 기본 20) 규칙을 적용해야 한다.
	s := &Server{}
	t.Setenv("HOME", t.TempDir())

	m, err := config.NewUserDataManager()
	if err != nil {
		t.Fatalf("failed to create user data manager: %v", err)
	}
	for i := 0; i < 3; i++ {
		if err := m.AddHistoryEntry(types.BackupHistoryEntry{ID: string(rune('x' + i))}); err != nil {
			t.Fatalf("failed to add history entry: %v", err)
		}
	}

	highReq := httptest.NewRequest(http.MethodGet, "/api/backup-history?limit=999", nil)
	highRR := httptest.NewRecorder()
	s.handleGetBackupHistory(highRR, highReq)
	if highRR.Code != http.StatusOK {
		t.Fatalf("expected status 200 for high limit, got %d", highRR.Code)
	}

	lowReq := httptest.NewRequest(http.MethodGet, "/api/backup-history?limit=0", nil)
	lowRR := httptest.NewRecorder()
	s.handleGetBackupHistory(lowRR, lowReq)
	if lowRR.Code != http.StatusOK {
		t.Fatalf("expected status 200 for low limit, got %d", lowRR.Code)
	}
}

// TestHandleBrowse_NotFoundBranch는 테스트 코드 동작을 검증하거나 보조합니다.
func TestHandleBrowse_NotFoundBranch(t *testing.T) {
	// browse는 존재하지 않는 경로에서 404를 반환해야 한다.
	s := &Server{}
	req := httptest.NewRequest(
		http.MethodGet,
		"/api/browse?path="+url.QueryEscape(filepath.Join(t.TempDir(), "missing")),
		nil,
	)
	rr := httptest.NewRecorder()
	s.handleBrowse(rr, req)
	if rr.Code != http.StatusNotFound {
		t.Fatalf("expected status 404, got %d", rr.Code)
	}
}

// waitForErrorProgressMessage는 테스트 코드 동작을 검증하거나 보조합니다.
func waitForErrorProgressMessage(t *testing.T, ch <-chan []byte, timeout time.Duration) {
	t.Helper()

	timer := time.NewTimer(timeout)
	defer timer.Stop()

	for {
		select {
		case msg := <-ch:
			if strings.Contains(string(msg), `"type":"error"`) {
				return
			}
		case <-timer.C:
			t.Fatal("timed out waiting for error progress message")
		}
	}
}
