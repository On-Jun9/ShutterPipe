package web

import (
	"bytes"
	"context"
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

func cancelRunRequestBody(t *testing.T, s *Server, runID string) *bytes.Reader {
	t.Helper()
	body, err := json.Marshal(CancelRunRequest{RunID: runID, ServerID: s.instanceID()})
	if err != nil {
		t.Fatal(err)
	}
	return bytes.NewReader(body)
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
	if !s.runs().Start("existing-run", func() {}) {
		t.Fatal("failed to arrange active run")
	}

	req := httptest.NewRequest(http.MethodPost, "/api/run", strings.NewReader(`{}`))
	rr := httptest.NewRecorder()

	s.handleRun(rr, req)

	if rr.Code != http.StatusConflict {
		t.Fatalf("expected status 409, got %d", rr.Code)
	}
	response := decodeAPIErrorResponse(t, rr)
	if response.Message != "another operation is already running" {
		t.Fatalf("unexpected message: %s", response.Message)
	}
}

func TestHandleRun_RejectsReusedRunID(t *testing.T) {
	s := &Server{}
	if !s.runs().Start("reused-run", func() {}) {
		t.Fatal("failed to arrange first run")
	}
	if _, finished := s.runs().Finish("reused-run", RunStatusComplete, nil, ""); !finished {
		t.Fatal("failed to finish first run")
	}
	tmpDir := t.TempDir()
	source := filepath.Join(tmpDir, "source")
	dest := filepath.Join(tmpDir, "dest")
	if err := os.MkdirAll(source, 0755); err != nil {
		t.Fatal(err)
	}
	body, err := json.Marshal(map[string]any{"run_id": "reused-run", "source": source, "dest": dest})
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/run", bytes.NewReader(body))
	rr := httptest.NewRecorder()

	s.handleRun(rr, req)

	if rr.Code != http.StatusConflict {
		t.Fatalf("expected status 409, got %d", rr.Code)
	}
	if message := decodeAPIErrorResponse(t, rr).Message; message != ErrRunIDReused.Error() {
		t.Fatalf("unexpected reused-ID message: %s", message)
	}
}

func TestHandleRun_DoesNotAdmitCanceledRequest(t *testing.T) {
	s := &Server{}
	tmpDir := t.TempDir()
	source := filepath.Join(tmpDir, "source")
	dest := filepath.Join(tmpDir, "dest")
	if err := os.MkdirAll(source, 0755); err != nil {
		t.Fatal(err)
	}
	body, err := json.Marshal(map[string]any{"run_id": "cancelled-request", "source": source, "dest": dest})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	req := httptest.NewRequest(http.MethodPost, "/api/run", bytes.NewReader(body)).WithContext(ctx)
	rr := httptest.NewRecorder()

	s.handleRun(rr, req)

	if s.runs().IsActive() {
		t.Fatal("canceled HTTP request admitted a hidden backup run")
	}
	if status := s.runs().Status(); status.Status != RunStatusIdle {
		t.Fatalf("canceled request changed run state: %+v", status)
	}
}

// TestHandleCancelRun_ReturnsConflictWhenNotRunning는 테스트 코드 동작을 검증하거나 보조합니다.
func TestHandleCancelRun_ReturnsConflictWhenNotRunning(t *testing.T) {
	// 실행 중인 백업이 없으면 취소 요청은 409 JSON 에러를 반환해야 한다.
	s := &Server{}

	req := httptest.NewRequest(http.MethodPost, "/api/run/cancel", cancelRunRequestBody(t, s, "run-1"))
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
	s.runs().Start("run-1", func() {
		called = true
	})

	req := httptest.NewRequest(http.MethodPost, "/api/run/cancel", cancelRunRequestBody(t, s, "run-1"))
	rr := httptest.NewRecorder()
	s.handleCancelRun(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", rr.Code)
	}
	if !called {
		t.Fatal("expected cancel function to be called")
	}

	var body RunStatusResponse
	if err := json.NewDecoder(rr.Body).Decode(&body); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if body.Status != RunStatusCancelling {
		t.Fatalf("unexpected response body: %+v", body)
	}
	if body.RunID != "run-1" {
		t.Fatalf("expected run ID in response, got %+v", body)
	}
}

func TestHandleCancelRun_RejectsDifferentRunID(t *testing.T) {
	s := &Server{}
	cancelled := false
	s.runs().Start("current-run", func() { cancelled = true })

	req := httptest.NewRequest(http.MethodPost, "/api/run/cancel", cancelRunRequestBody(t, s, "stale-run"))
	rr := httptest.NewRecorder()
	s.handleCancelRun(rr, req)

	if rr.Code != http.StatusConflict {
		t.Fatalf("expected status 409, got %d", rr.Code)
	}
	if cancelled {
		t.Fatal("mismatched run ID cancelled the active run")
	}
	if status := s.runs().Status(); status.Status != RunStatusRunning || status.RunID != "current-run" {
		t.Fatalf("active run changed unexpectedly: %+v", status)
	}
}

func TestHandleCancelRun_RejectsMissingRunIDWhileRunning(t *testing.T) {
	s := &Server{}
	cancelled := false
	s.runs().Start("current-run", func() { cancelled = true })

	req := httptest.NewRequest(http.MethodPost, "/api/run/cancel", nil)
	rr := httptest.NewRecorder()
	s.handleCancelRun(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected status 400, got %d", rr.Code)
	}
	if cancelled {
		t.Fatal("missing run ID cancelled the active run")
	}
	if message := decodeAPIErrorResponse(t, rr).Message; message != "run_id is required" {
		t.Fatalf("unexpected missing-ID error: %s", message)
	}
	if status := s.runs().Status(); status.Status != RunStatusRunning || status.RunID != "current-run" {
		t.Fatalf("missing-ID request changed active run: %+v", status)
	}
}

func TestHandleCancelRun_RejectsMissingOrStaleServerID(t *testing.T) {
	s := &Server{}
	cancelled := false
	s.runs().Start("current-run", func() { cancelled = true })

	for name, body := range map[string]string{
		"missing": `{"run_id":"current-run"}`,
		"stale":   `{"run_id":"current-run","server_id":"previous-server"}`,
	} {
		t.Run(name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/api/run/cancel", strings.NewReader(body))
			rr := httptest.NewRecorder()
			s.handleCancelRun(rr, req)

			expected := http.StatusBadRequest
			if name == "stale" {
				expected = http.StatusConflict
			}
			if rr.Code != expected {
				t.Fatalf("expected status %d, got %d", expected, rr.Code)
			}
			if cancelled {
				t.Fatal("invalid server ID cancelled the active run")
			}
		})
	}
}

func TestHandleRunStatus_ReturnsActiveRun(t *testing.T) {
	s := &Server{}
	s.runs().Start("run-status", func() {})

	req := httptest.NewRequest(http.MethodGet, "/api/run/status", nil)
	rr := httptest.NewRecorder()
	s.handleRunStatus(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", rr.Code)
	}
	var response RunStatusResponse
	if err := json.NewDecoder(rr.Body).Decode(&response); err != nil {
		t.Fatalf("failed to decode run status: %v", err)
	}
	if response.Status != RunStatusRunning || response.RunID != "run-status" {
		t.Fatalf("unexpected run status: %+v", response)
	}
	if response.ServerID == "" {
		t.Fatal("expected server instance ID in run status")
	}
}

func TestHandleRunStatus_RetainsTerminalRunResult(t *testing.T) {
	s := &Server{}
	s.runs().Start("finished-run", func() {})
	summary := &types.RunSummary{Copied: 3, Failed: 1}
	terminal, finished := s.runs().Finish("finished-run", RunStatusError, summary, "copy failed")
	if !finished {
		t.Fatal("failed to arrange terminal run")
	}

	req := httptest.NewRequest(http.MethodGet, "/api/run/status", nil)
	rr := httptest.NewRecorder()
	s.handleRunStatus(rr, req)

	var response RunStatusResponse
	if err := json.NewDecoder(rr.Body).Decode(&response); err != nil {
		t.Fatalf("failed to decode run status: %v", err)
	}
	if response.Status != RunStatusError || response.RunID != "finished-run" || response.Error != "copy failed" {
		t.Fatalf("terminal result was not retained: %+v", response)
	}
	if response.Summary == nil || response.Summary.Copied != 3 || response.Summary.Failed != 1 {
		t.Fatalf("terminal summary was not retained: %+v", response.Summary)
	}
	if response.Revision != terminal.Revision {
		t.Fatalf("expected terminal revision %d, got %d", terminal.Revision, response.Revision)
	}
}

func TestHandleRunStatus_ReturnsRequestedTerminalWhileNextRunIsActive(t *testing.T) {
	s := &Server{}
	s.runs().Start("finished-run", func() {})
	s.runs().Finish("finished-run", RunStatusError, &types.RunSummary{Failed: 1}, "copy failed")
	s.runs().Start("next-run", func() {})

	req := httptest.NewRequest(http.MethodGet, "/api/run/status?run_id=finished-run", nil)
	rr := httptest.NewRecorder()
	s.handleRunStatus(rr, req)

	var response RunStatusResponse
	if err := json.NewDecoder(rr.Body).Decode(&response); err != nil {
		t.Fatalf("failed to decode requested run status: %v", err)
	}
	if response.Status != RunStatusError || response.RunID != "finished-run" ||
		response.Error != "copy failed" || response.Summary == nil || response.Summary.Failed != 1 {
		t.Fatalf("requested terminal was replaced by the active run: %+v", response)
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
	var started struct {
		RunID string `json:"run_id"`
	}
	if err := json.Unmarshal(rr1.Body.Bytes(), &started); err != nil {
		t.Fatalf("failed to decode first run response: %v", err)
	}
	reqC := httptest.NewRequest(http.MethodPost, "/api/run/cancel", cancelRunRequestBody(t, s, started.RunID))
	rrC := httptest.NewRecorder()
	s.handleCancelRun(rrC, reqC)
	if rrC.Code != http.StatusOK {
		t.Fatalf("expected 200 for cancel, got %d", rrC.Code)
	}

	// 고루틴이 종료되어 실행 관리자가 terminal 상태를 기록할 때까지 대기
	waitForRunManagerInactive(t, s)

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
		status := s.runs().Status()
		if s.runs().IsActive() {
			_, _ = s.runs().Cancel(status.RunID)
		}
		waitForRunManagerInactive(t, s)
	})
}

func waitForRunManagerInactive(t *testing.T, s *Server) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if !s.runs().IsActive() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("timeout waiting for run manager to become inactive")
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
