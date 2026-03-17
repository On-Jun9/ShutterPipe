package web

import (
	"bytes"
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

// decodeAPIErrorResponse는 테스트 코드 동작을 검증하거나 보조합니다.
func decodeAPIErrorResponse(t *testing.T, rr *httptest.ResponseRecorder) APIErrorResponse {
	t.Helper()

	var response APIErrorResponse
	if err := json.NewDecoder(rr.Body).Decode(&response); err != nil {
		t.Fatalf("failed to decode APIErrorResponse: %v", err)
	}
	return response
}

// decodeValidationErrorResponse는 테스트 코드 동작을 검증하거나 보조합니다.
func decodeValidationErrorResponse(t *testing.T, rr *httptest.ResponseRecorder) ValidationError {
	t.Helper()

	var response ValidationError
	if err := json.NewDecoder(rr.Body).Decode(&response); err != nil {
		t.Fatalf("failed to decode ValidationError: %v", err)
	}
	return response
}

// TestHandleRun_ReturnsBadRequestOnInvalidJSON는 테스트 코드 동작을 검증하거나 보조합니다.
func TestHandleRun_ReturnsBadRequestOnInvalidJSON(t *testing.T) {
	// 요청 바디 파싱 실패는 400 + JSON 에러 응답이어야 한다.
	s := &Server{}
	req := httptest.NewRequest(http.MethodPost, "/api/run", strings.NewReader("{"))
	rr := httptest.NewRecorder()

	s.handleRun(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected status 400, got %d", rr.Code)
	}
	if rr.Header().Get("Content-Type") != "application/json" {
		t.Fatalf("expected application/json, got %s", rr.Header().Get("Content-Type"))
	}
	response := decodeAPIErrorResponse(t, rr)
	if response.Message == "" {
		t.Fatal("expected error message")
	}
}

// TestHandleRun_ReturnsValidationErrorForInvalidConfig는 테스트 코드 동작을 검증하거나 보조합니다.
func TestHandleRun_ReturnsValidationErrorForInvalidConfig(t *testing.T) {
	// 설정 검증 실패는 400 + {field,message} 포맷이어야 한다.
	s := &Server{}
	req := httptest.NewRequest(http.MethodPost, "/api/run", strings.NewReader(`{"source":"/tmp/source"}`))
	rr := httptest.NewRecorder()

	s.handleRun(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected status 400, got %d", rr.Code)
	}
	if rr.Header().Get("Content-Type") != "application/json" {
		t.Fatalf("expected application/json, got %s", rr.Header().Get("Content-Type"))
	}
	response := decodeValidationErrorResponse(t, rr)
	if response.Field != "dest" {
		t.Fatalf("expected field dest, got %s", response.Field)
	}
	if response.Message == "" {
		t.Fatal("expected validation message")
	}
}

// TestHandleRun_ReturnsConflictWhenAlreadyRunning는 테스트 코드 동작을 검증하거나 보조합니다.
func TestHandleRun_ReturnsConflictWhenAlreadyRunning(t *testing.T) {
	// 중복 실행 시 409 JSON 에러를 반환해야 한다.
	s := &Server{}
	runMutex.Lock()
	defer runMutex.Unlock()

	req := httptest.NewRequest(http.MethodPost, "/api/run", strings.NewReader(`{}`))
	rr := httptest.NewRecorder()

	s.handleRun(rr, req)

	if rr.Code != http.StatusConflict {
		t.Fatalf("expected status 409, got %d", rr.Code)
	}
	response := decodeAPIErrorResponse(t, rr)
	if response.Message != "backup already running" {
		t.Fatalf("unexpected message: %s", response.Message)
	}
}

// TestHandleCancelRun_ReturnsConflictWhenNotRunning는 테스트 코드 동작을 검증하거나 보조합니다.
func TestHandleCancelRun_ReturnsConflictWhenNotRunning(t *testing.T) {
	// 실행 중인 백업이 없으면 취소 요청은 409 JSON 에러를 반환해야 한다.
	s := &Server{}
	clearActiveRunCancel()

	req := httptest.NewRequest(http.MethodPost, "/api/run/cancel", nil)
	rr := httptest.NewRecorder()
	s.handleCancelRun(rr, req)

	if rr.Code != http.StatusConflict {
		t.Fatalf("expected status 409, got %d", rr.Code)
	}
	if decodeAPIErrorResponse(t, rr).Message != "backup is not running" {
		t.Fatalf("unexpected cancel error message")
	}
}

// TestHandleCancelRun_RequestsCancellationWhenRunning는 테스트 코드 동작을 검증하거나 보조합니다.
func TestHandleCancelRun_RequestsCancellationWhenRunning(t *testing.T) {
	// 실행 중인 백업이 있으면 취소 함수를 호출하고 200을 반환해야 한다.
	s := &Server{}
	called := false
	setActiveRunCancel(func() {
		called = true
	})
	t.Cleanup(clearActiveRunCancel)

	req := httptest.NewRequest(http.MethodPost, "/api/run/cancel", nil)
	rr := httptest.NewRecorder()
	s.handleCancelRun(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", rr.Code)
	}
	if !called {
		t.Fatal("expected cancel function to be called")
	}

	var body map[string]string
	if err := json.NewDecoder(rr.Body).Decode(&body); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if body["status"] != "cancelling" {
		t.Fatalf("unexpected response body: %+v", body)
	}
}

// TestHandleRun_AfterCancelMutexReleasedForNewRun은 취소 완료 후 runMutex가 해제되어
// 새 백업 요청이 409 없이 처리되어야 함을 검증한다.
func TestHandleRun_AfterCancelMutexReleasedForNewRun(t *testing.T) {
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
	if err := os.WriteFile(filepath.Join(sourceDir, "photo.jpg"), []byte("photo"), 0644); err != nil {
		t.Fatalf("failed to write source file: %v", err)
	}

	s := &Server{hub: NewHub()}
	go s.hub.Run()

	cfg := config.Config{
		Source:            sourceDir,
		Dest:              destDir,
		IncludeExtensions: []string{"jpg"},
		Jobs:              1,
		DedupMethod:       types.DedupMethodNameSize,
		ConflictPolicy:    types.ConflictPolicySkip,
		OrganizeStrategy:  types.OrganizeByDate,
		UnclassifiedDir:   "unclassified",
		QuarantineDir:     "quarantine",
		StateFile:         filepath.Join(tmpDir, "state.json"),
		LogFile:           filepath.Join(tmpDir, "shutterpipe.log"),
	}
	body, _ := json.Marshal(cfg)

	// 첫 번째 실행 시작
	req1 := httptest.NewRequest(http.MethodPost, "/api/run", bytes.NewReader(body))
	rr1 := httptest.NewRecorder()
	s.handleRun(rr1, req1)
	if rr1.Code != http.StatusOK {
		t.Fatalf("expected 200 for first run, got %d", rr1.Code)
	}

	// 취소 요청
	reqC := httptest.NewRequest(http.MethodPost, "/api/run/cancel", nil)
	rrC := httptest.NewRecorder()
	s.handleCancelRun(rrC, reqC)
	if rrC.Code != http.StatusOK {
		t.Fatalf("expected 200 for cancel, got %d", rrC.Code)
	}

	// 고루틴이 종료되어 runMutex가 해제될 때까지 대기
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if runMutex.TryLock() {
			runMutex.Unlock()
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if !runMutex.TryLock() {
		t.Fatal("runMutex still locked after cancel: goroutine did not release it in time")
	}
	runMutex.Unlock()

	// 두 번째 실행이 409 없이 시작되어야 한다
	body2, _ := json.Marshal(cfg)
	req2 := httptest.NewRequest(http.MethodPost, "/api/run", bytes.NewReader(body2))
	rr2 := httptest.NewRecorder()
	s.handleRun(rr2, req2)
	if rr2.Code == http.StatusConflict {
		t.Fatal("second run got 409: runMutex was not released after cancel")
	}

	// 두 번째 고루틴 정리
	t.Cleanup(func() {
		if fn := getActiveRunCancel(); fn != nil {
			fn()
		}
		cleanupDeadline := time.Now().Add(3 * time.Second)
		for time.Now().Before(cleanupDeadline) {
			if runMutex.TryLock() {
				runMutex.Unlock()
				break
			}
			time.Sleep(10 * time.Millisecond)
		}
		clearActiveRunCancel()
	})
}

// TestHandleBrowse_ReturnsNotFoundForMissingPath는 테스트 코드 동작을 검증하거나 보조합니다.
func TestHandleBrowse_ReturnsNotFoundForMissingPath(t *testing.T) {
	// 존재하지 않는 경로 탐색은 404 JSON 에러로 응답해야 한다.
	s := &Server{}
	missingPath := filepath.Join(t.TempDir(), "does-not-exist")
	req := httptest.NewRequest(
		http.MethodGet,
		"/api/browse?path="+url.QueryEscape(missingPath),
		nil,
	)
	rr := httptest.NewRecorder()

	s.handleBrowse(rr, req)

	if rr.Code != http.StatusNotFound {
		t.Fatalf("expected status 404, got %d", rr.Code)
	}
	if rr.Header().Get("Content-Type") != "application/json" {
		t.Fatalf("expected application/json, got %s", rr.Header().Get("Content-Type"))
	}
	response := decodeAPIErrorResponse(t, rr)
	if response.Message == "" {
		t.Fatal("expected error message")
	}
}

// TestHandleSaveSettings_ReturnsValidationError는 테스트 코드 동작을 검증하거나 보조합니다.
func TestHandleSaveSettings_ReturnsValidationError(t *testing.T) {
	// 설정 저장 API도 ValidationError를 그대로 JSON으로 내려야 한다.
	s := &Server{}
	t.Setenv("HOME", t.TempDir())

	req := httptest.NewRequest(
		http.MethodPost,
		"/api/settings",
		strings.NewReader(`{"source":"<script>alert(1)</script>","dest":"/tmp/dest"}`),
	)
	rr := httptest.NewRecorder()

	s.handleSaveSettings(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected status 400, got %d", rr.Code)
	}
	response := decodeValidationErrorResponse(t, rr)
	if response.Field != "source" {
		t.Fatalf("expected field source, got %s", response.Field)
	}
}

// TestHandleSaveBookmarks_ReturnsValidationError는 테스트 코드 동작을 검증하거나 보조합니다.
func TestHandleSaveBookmarks_ReturnsValidationError(t *testing.T) {
	// 북마크 저장 API도 field=bookmarks를 유지해야 한다.
	s := &Server{}
	t.Setenv("HOME", t.TempDir())

	req := httptest.NewRequest(
		http.MethodPost,
		"/api/bookmarks",
		strings.NewReader(`{"source":["javascript:alert(1)"],"dest":[]}`),
	)
	rr := httptest.NewRecorder()

	s.handleSaveBookmarks(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected status 400, got %d", rr.Code)
	}
	response := decodeValidationErrorResponse(t, rr)
	if response.Field != "bookmarks" {
		t.Fatalf("expected field bookmarks, got %s", response.Field)
	}
}

// TestHandleSavePathHistory_ReturnsValidationError는 테스트 코드 동작을 검증하거나 보조합니다.
func TestHandleSavePathHistory_ReturnsValidationError(t *testing.T) {
	// 경로 히스토리 저장 API도 field=path_history를 유지해야 한다.
	s := &Server{}
	t.Setenv("HOME", t.TempDir())

	req := httptest.NewRequest(
		http.MethodPost,
		"/api/path-history",
		strings.NewReader(`{"source":[],"dest":["<iframe src=x>"]}`),
	)
	rr := httptest.NewRecorder()

	s.handleSavePathHistory(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected status 400, got %d", rr.Code)
	}
	response := decodeValidationErrorResponse(t, rr)
	if response.Field != "path_history" {
		t.Fatalf("expected field path_history, got %s", response.Field)
	}
}
