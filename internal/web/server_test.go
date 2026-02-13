package web

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/On-Jun9/ShutterPipe/internal/pipeline"
	"github.com/gorilla/mux"
)

// TestServerSetupRoutesAndVersionRoute는 테스트 코드 동작을 검증하거나 보조합니다.
func TestServerSetupRoutesAndVersionRoute(t *testing.T) {
	// 라우트 설정 후 /api/version이 현재 버전을 반환해야 한다.
	s := &Server{
		router:  mux.NewRouter(),
		hub:     NewHub(),
		version: "v9.9.9",
	}
	s.setupRoutes()

	req := httptest.NewRequest(http.MethodGet, "/api/version", nil)
	rr := httptest.NewRecorder()
	s.router.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", rr.Code)
	}

	var body map[string]string
	if err := json.NewDecoder(rr.Body).Decode(&body); err != nil {
		t.Fatalf("failed to decode version response: %v", err)
	}
	if body["version"] != "v9.9.9" {
		t.Fatalf("unexpected version response: %+v", body)
	}
}

// TestNewServerAndSetVersion는 테스트 코드 동작을 검증하거나 보조합니다.
func TestNewServerAndSetVersion(t *testing.T) {
	// NewServer와 SetVersion 호출 후 버전 응답이 반영되어야 한다.
	s := NewServer()
	s.SetVersion("v1.2.3")

	req := httptest.NewRequest(http.MethodGet, "/api/version", nil)
	rr := httptest.NewRecorder()
	s.router.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", rr.Code)
	}
	var body map[string]string
	if err := json.NewDecoder(rr.Body).Decode(&body); err != nil {
		t.Fatalf("failed to decode version response: %v", err)
	}
	if body["version"] != "v1.2.3" {
		t.Fatalf("unexpected version response: %+v", body)
	}
}

// TestServerBroadcastJSONAndProgress는 테스트 코드 동작을 검증하거나 보조합니다.
func TestServerBroadcastJSONAndProgress(t *testing.T) {
	// broadcastJSON/broadcastProgress는 hub broadcast 채널로 메시지를 전달해야 한다.
	s := &Server{hub: NewHub()}
	done := make(chan []byte, 2)

	go func() {
		done <- <-s.hub.broadcast
		done <- <-s.hub.broadcast
	}()

	s.broadcastJSON(map[string]string{"type": "status"})
	s.broadcastProgress(pipeline.ProgressUpdate{Type: "complete"})

	select {
	case first := <-done:
		if len(first) == 0 {
			t.Fatal("expected first broadcast payload")
		}
	case <-time.After(1 * time.Second):
		t.Fatal("timed out waiting first broadcast")
	}

	select {
	case second := <-done:
		if len(second) == 0 {
			t.Fatal("expected second broadcast payload")
		}
	case <-time.After(1 * time.Second):
		t.Fatal("timed out waiting second broadcast")
	}
}

// TestHandleWebSocket_UpgradeFailurePath는 테스트 코드 동작을 검증하거나 보조합니다.
func TestHandleWebSocket_UpgradeFailurePath(t *testing.T) {
	// 웹소켓 핸드셰이크가 아닌 요청은 업그레이드 실패 처리로 빠져야 한다.
	s := &Server{hub: NewHub()}
	req := httptest.NewRequest(http.MethodGet, "/api/ws", nil)
	rr := httptest.NewRecorder()

	s.handleWebSocket(rr, req)

	if rr.Code == http.StatusOK {
		t.Fatalf("expected non-200 response for invalid websocket handshake, got %d", rr.Code)
	}
}
