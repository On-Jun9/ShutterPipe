package web

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"
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
